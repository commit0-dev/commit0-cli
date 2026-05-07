package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/commit0-dev/commit0/pkg/types"
	"github.com/commit0-dev/commit0/server/internal/domain"
)

// registerSimilarTools adds the similarity-based search tools to the server.
func registerSimilarTools(server *mcpsdk.Server, deps Deps, log *slog.Logger) {
	addCommit0SimilarTo(server, deps, log)
}

// ---------------------------------------------------------------------------
// Tool: commit0_similar_to
// ---------------------------------------------------------------------------

// similarToInput is the typed input for commit0_similar_to.
type similarToInput struct {
	NodeID          string   `json:"node_id"              jsonschema:"Node ID returned by an earlier tool (e.g. from commit0_query or commit0_lookup)."`
	K               int      `json:"k,omitempty"          jsonschema:"Number of similar nodes to return (1-50). Default 10."`
	ExcludeSameFile bool     `json:"exclude_same_file,omitempty" jsonschema:"If true, exclude neighbors from the same file as the source node. Default false."`
	NodeKinds       []string `json:"node_kinds,omitempty" jsonschema:"Filter results by node kind (e.g. ['function', 'class']). Default: all kinds."`
}

func addCommit0SimilarTo(server *mcpsdk.Server, deps Deps, log *slog.Logger) {
	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name: "commit0_similar_to",
		Description: "Find code that looks similar to a given node by embedding cosine similarity. " +
			"Takes a node ID (from commit0_query, commit0_lookup, etc.) and returns its K nearest neighbors " +
			"by embedding distance, excluding the source node. Useful for finding code to refactor the same way, " +
			"or discovering helpers you're about to reinvent.",
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, input similarToInput) (*mcpsdk.CallToolResult, any, error) {
		if input.NodeID == "" {
			return toolError(domain.Validation("node_id is required")), nil, nil
		}

		graph, errResult := graphFromDeps(deps)
		if errResult != nil {
			return errResult, nil, nil
		}

		// Get the source node and its embedding
		sourceNode, err := graph.GetNode(ctx, input.NodeID)
		if err != nil {
			var de types.DomainError
			if asDomainError(err, &de) && de.Code == types.ErrNotFound {
				return notFoundResult(input.NodeID), nil, nil
			}
			log.Warn("commit0_similar_to get source node failed", "node_id", input.NodeID, "err", err)
			return toolError(err), nil, nil
		}

		// Get the embedding for this node
		embedding, err := graph.GetNodeEmbedding(ctx, input.NodeID)
		if err != nil {
			var de types.DomainError
			if asDomainError(err, &de) && de.Code == types.ErrNotFound {
				msg := fmt.Sprintf(
					"Node `%s` (%s) has no embedding — it may be too small to embed or still indexing. Try another node.",
					sourceNode.Qualified, sourceNode.Kind,
				)
				return &mcpsdk.CallToolResult{
					Content: []mcpsdk.Content{
						&mcpsdk.TextContent{Text: msg},
					},
				}, nil, nil
			}
			log.Warn("commit0_similar_to get embedding failed", "node_id", input.NodeID, "err", err)
			return toolError(err), nil, nil
		}

		// Build node kinds filter
		var nodeKinds []types.NodeKind
		for _, k := range input.NodeKinds {
			nodeKinds = append(nodeKinds, types.NodeKind(k))
		}

		// Perform vector search: fetch one extra to account for the source node being #1
		k := input.K
		if k <= 0 {
			k = 10
		}
		if k > 50 {
			k = 50
		}

		scored, err := graph.VectorSearch(ctx, embedding, domain.VectorSearchOpts{
			RepoSlug:  sourceNode.RepoSlug,
			NodeKinds: nodeKinds,
			TopK:      k + 1, // fetch one extra to drop the source node
			Effort:    40,
		})
		if err != nil {
			log.Warn("commit0_similar_to vector search failed", "node_id", input.NodeID, "err", err)
			return toolError(err), nil, nil
		}

		// Filter out the source node and optionally nodes from the same file
		var neighbors []types.ScoredNode
		for _, sn := range scored {
			if sn.Node.ID == input.NodeID {
				continue // skip the source node
			}
			if input.ExcludeSameFile && sn.Node.FilePath == sourceNode.FilePath {
				continue // skip neighbors in the same file
			}
			neighbors = append(neighbors, sn)
			if len(neighbors) >= k {
				break
			}
		}

		result := buildSimilarToResult(*sourceNode, neighbors)
		return &mcpsdk.CallToolResult{
			Content: []mcpsdk.Content{
				&mcpsdk.TextContent{Text: similarToMarkdown(result)},
			},
			StructuredContent: result,
		}, nil, nil
	})
}

// ---------------------------------------------------------------------------
// Result construction helpers
// ---------------------------------------------------------------------------

// SimilarNodeRef is one similar node returned by commit0_similar_to.
type SimilarNodeRef struct {
	ID        string  `json:"id"`
	Qualified string  `json:"qualified"`
	Kind      string  `json:"kind"`
	FilePath  string  `json:"file_path"`
	Score     float64 `json:"score"`
	StartLine int     `json:"start_line,omitempty"`
}

// SimilarToToolResult is the structured output of commit0_similar_to.
type SimilarToToolResult struct {
	Source    CodeNodeOut      `json:"source"`
	Neighbors []SimilarNodeRef `json:"neighbors"`
	Total     int              `json:"total"`
}

// buildSimilarToResult constructs the output from scored neighbors.
func buildSimilarToResult(source types.CodeNode, neighbors []types.ScoredNode) SimilarToToolResult {
	refs := make([]SimilarNodeRef, 0, len(neighbors))
	for _, sn := range neighbors {
		refs = append(refs, SimilarNodeRef{
			ID:        sn.Node.ID,
			Qualified: sn.Node.Qualified,
			Kind:      string(sn.Node.Kind),
			FilePath:  sn.Node.FilePath,
			Score:     sn.VectorScore,
			StartLine: sn.Node.StartLine,
		})
	}
	sort.SliceStable(refs, func(i, j int) bool {
		if refs[i].Score != refs[j].Score {
			return refs[i].Score > refs[j].Score // descending by score
		}
		return refs[i].Qualified < refs[j].Qualified
	})
	return SimilarToToolResult{
		Source:    codeNodeOut(source, false),
		Neighbors: refs,
		Total:     len(refs),
	}
}

// ---------------------------------------------------------------------------
// Markdown formatter
// ---------------------------------------------------------------------------

func similarToMarkdown(r SimilarToToolResult) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "## Similar to `%s` (%d match(es))\n\n", r.Source.Qualified, r.Total)
	if r.Total == 0 {
		sb.WriteString("_No similar code found in the indexed repository._\n")
		return sb.String()
	}
	for i, n := range r.Neighbors {
		fmt.Fprintf(&sb, "%d. `%s` (%s, score: %.3f) — %s", i+1, n.Qualified, n.Kind, n.Score, n.FilePath)
		if n.StartLine > 0 {
			fmt.Fprintf(&sb, ":%d", n.StartLine)
		}
		sb.WriteString("\n")
	}
	return sb.String()
}
