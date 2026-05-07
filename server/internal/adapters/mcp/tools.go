package mcp

import (
	"context"
	"fmt"
	"log/slog"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/commit0-dev/commit0/pkg/types"
	"github.com/commit0-dev/commit0/server/internal/app"
	"github.com/commit0-dev/commit0/server/internal/domain"
)

// ---------------------------------------------------------------------------
// Tool: commit0_query
// ---------------------------------------------------------------------------

// queryInput is the typed input struct for commit0_query.
// Required fields are non-pointer, non-omitempty; optional fields use omitempty.
// The jsonschema tag value is the field description (plain string — no key=value).
type queryInput struct {
	Question        string   `json:"question"                   jsonschema:"Natural-language question about the code."`
	RepoSlug        string   `json:"repo_slug"                  jsonschema:"Indexed repository slug."`
	TopK            int      `json:"top_k,omitempty"            jsonschema:"Maximum results to return (1-50). Default 10."`
	MinScore        float64  `json:"min_score,omitempty"        jsonschema:"Minimum relevance score 0-1. Default 0.5."`
	NodeKinds       []string `json:"node_kinds,omitempty"       jsonschema:"Filter by node kind: function, class, file, module."`
	FilePath        string   `json:"file_path,omitempty"        jsonschema:"Optional file path prefix filter."`
	WithExplanation bool     `json:"with_explanation,omitempty" jsonschema:"Include LLM explanation (adds 5-15s). Default false."`
}

func addCommit0Query(server *mcpsdk.Server, deps Deps, log *slog.Logger) {
	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name: "commit0_query",
		Description: "Hybrid semantic + full-text search over the indexed code graph. " +
			"Returns the top-K most relevant code nodes (functions, classes, files) " +
			"for a natural-language question, fused via reciprocal rank fusion and " +
			"graph-augmented with 1-hop neighbors. Use this for questions like " +
			"\"where is X implemented?\" or \"how does Y work?\" — prefer it over grep for conceptual queries.",
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, input queryInput) (*mcpsdk.CallToolResult, any, error) {
		if input.Question == "" {
			return toolError(domain.Validation("question is required")), nil, nil
		}
		if input.RepoSlug == "" {
			return toolError(domain.Validation("repo_slug is required")), nil, nil
		}

		querySvc, errResult := queryServiceFromDeps(deps)
		if errResult != nil {
			return errResult, nil, nil
		}

		if input.TopK <= 0 {
			input.TopK = 10
		}
		if input.MinScore <= 0 {
			input.MinScore = 0.5
		}

		var nodeKinds []types.NodeKind
		for _, k := range input.NodeKinds {
			nodeKinds = append(nodeKinds, types.NodeKind(k))
		}

		qr, err := querySvc.Query(ctx, app.QueryRequest{
			Question:  input.Question,
			RepoSlug:  input.RepoSlug,
			TopK:      input.TopK,
			MinScore:  input.MinScore,
			NodeKinds: nodeKinds,
			FilePath:  input.FilePath,
			NoExplain: !input.WithExplanation,
		})
		if err != nil {
			log.Warn("commit0_query failed", "err", err)
			return toolError(err), nil, nil
		}

		nodes := make([]ScoredNodeOut, len(qr.Nodes))
		for i, sn := range qr.Nodes {
			nodes[i] = scoredNodeOut(sn)
		}

		result := QueryToolResult{
			Nodes:       nodes,
			Explanation: qr.Explanation,
			RepoSlug:    qr.RepoSlug,
			Timing:      timingOut(qr.Timing),
		}

		return &mcpsdk.CallToolResult{
			Content: []mcpsdk.Content{
				&mcpsdk.TextContent{Text: queryMarkdown(result)},
			},
			StructuredContent: result,
		}, nil, nil
	})
}

// ---------------------------------------------------------------------------
// Tool: commit0_lookup
// ---------------------------------------------------------------------------

// lookupInput is the typed input struct for commit0_lookup.
type lookupInput struct {
	Qualified string `json:"qualified" jsonschema:"Fully-qualified symbol name to look up."`
	RepoSlug  string `json:"repo_slug" jsonschema:"Indexed repository slug."`
}

func addCommit0Lookup(server *mcpsdk.Server, deps Deps, log *slog.Logger) {
	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name: "commit0_lookup",
		Description: "Resolve a qualified symbol name to a single code node with its metadata " +
			"(file path, line range, kind, qualified name). " +
			"No search, no LLM — pure index lookup. " +
			"Use this when an earlier tool returned a symbol and you need its exact location.",
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, input lookupInput) (*mcpsdk.CallToolResult, any, error) {
		if input.Qualified == "" {
			return toolError(domain.Validation("qualified is required")), nil, nil
		}
		if input.RepoSlug == "" {
			return toolError(domain.Validation("repo_slug is required")), nil, nil
		}

		graph, errResult := graphFromDeps(deps)
		if errResult != nil {
			return errResult, nil, nil
		}

		node, err := graph.FindNode(ctx, input.RepoSlug, input.Qualified)
		if err != nil {
			// NotFound is expected — return null node, not an error.
			var de types.DomainError
			if asDomainError(err, &de) && de.Code == types.ErrNotFound {
				result := LookupToolResult{Node: nil}
				return &mcpsdk.CallToolResult{
					Content: []mcpsdk.Content{
						&mcpsdk.TextContent{Text: lookupMarkdown(result)},
					},
					StructuredContent: result,
				}, nil, nil
			}
			log.Warn("commit0_lookup failed", "err", err)
			return toolError(err), nil, nil
		}

		var nodeOut *CodeNodeOut
		if node != nil {
			n := codeNodeOut(*node, false)
			nodeOut = &n
		}
		result := LookupToolResult{Node: nodeOut}
		return &mcpsdk.CallToolResult{
			Content: []mcpsdk.Content{
				&mcpsdk.TextContent{Text: lookupMarkdown(result)},
			},
			StructuredContent: result,
		}, nil, nil
	})
}

// ---------------------------------------------------------------------------
// Tool: commit0_neighborhood
// ---------------------------------------------------------------------------

// neighborhoodInput is the typed input for commit0_neighborhood.
// Both node_id and qualified are optional at the struct level;
// server-side validation enforces one-of semantics.
type neighborhoodInput struct {
	NodeID    string `json:"node_id,omitempty"   jsonschema:"Internal graph ID returned by an earlier tool. Provide this OR qualified."`
	Qualified string `json:"qualified,omitempty" jsonschema:"Qualified symbol name (resolved internally). Provide this OR node_id."`
	RepoSlug  string `json:"repo_slug,omitempty" jsonschema:"Required when looking up by qualified."`
}

func addCommit0Neighborhood(server *mcpsdk.Server, deps Deps, log *slog.Logger) {
	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name: "commit0_neighborhood",
		Description: "Return the immediate graph neighborhood of a node: callers, callees, " +
			"data sources, data sinks, reads, writes. Cheaper than trace (one hop only). " +
			"Use this to understand what one function touches before deciding whether to call trace or blast. " +
			"Provide either node_id or qualified (+ repo_slug).",
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, input neighborhoodInput) (*mcpsdk.CallToolResult, any, error) {
		graph, errResult := graphFromDeps(deps)
		if errResult != nil {
			return errResult, nil, nil
		}

		// Resolve the node ID.
		nodeID := input.NodeID
		if nodeID == "" {
			if input.Qualified == "" {
				return toolError(domain.Validation("provide either node_id or qualified (+ repo_slug)")), nil, nil
			}
			node, err := graph.FindNode(ctx, input.RepoSlug, input.Qualified)
			if err != nil {
				log.Warn("commit0_neighborhood lookup failed", "qualified", input.Qualified, "err", err)
				return toolError(err), nil, nil
			}
			if node == nil {
				result := NeighborhoodToolResult{}
				return &mcpsdk.CallToolResult{
					Content: []mcpsdk.Content{
						&mcpsdk.TextContent{Text: "Node not found: " + input.Qualified},
					},
					StructuredContent: result,
				}, nil, nil
			}
			nodeID = node.ID
		}

		nb, err := graph.Neighbors(ctx, nodeID)
		if err != nil {
			log.Warn("commit0_neighborhood failed", "node_id", nodeID, "err", err)
			return toolError(err), nil, nil
		}

		result := neighborhoodFromDomain(nb)
		return &mcpsdk.CallToolResult{
			Content: []mcpsdk.Content{
				&mcpsdk.TextContent{Text: neighborhoodMarkdown(result)},
			},
			StructuredContent: result,
		}, nil, nil
	})
}

// neighborhoodFromDomain converts a domain Neighborhood to its output representation.
func neighborhoodFromDomain(nb *domain.Neighborhood) NeighborhoodToolResult {
	if nb == nil {
		return NeighborhoodToolResult{}
	}
	toNeighborOuts := func(nodes []domain.NeighborNode) []NeighborOut {
		if len(nodes) == 0 {
			return nil
		}
		out := make([]NeighborOut, len(nodes))
		for i, n := range nodes {
			out[i] = NeighborOut{
				Qualified: n.Qualified,
				FilePath:  n.FilePath,
			}
		}
		return out
	}
	return NeighborhoodToolResult{
		Callers:     toNeighborOuts(nb.Callers),
		Callees:     toNeighborOuts(nb.Callees),
		DataSources: toNeighborOuts(nb.DataSources),
		DataSinks:   toNeighborOuts(nb.DataSinks),
		Reads:       nb.Reads,
		Writes:      nb.Writes,
	}
}

// ---------------------------------------------------------------------------
// Tool: commit0_show_node
// ---------------------------------------------------------------------------

// showNodeInput is the typed input for commit0_show_node.
type showNodeInput struct {
	NodeID string `json:"node_id" jsonschema:"Internal graph node ID returned by commit0_query or commit0_lookup."`
}

func addCommit0ShowNode(server *mcpsdk.Server, deps Deps, log *slog.Logger) {
	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name: "commit0_show_node",
		Description: "Return the full body and metadata for one node by ID. " +
			"Use after commit0_query or commit0_lookup when you need the actual source code. " +
			"Other tools omit the body to reduce token cost.",
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, input showNodeInput) (*mcpsdk.CallToolResult, any, error) {
		if input.NodeID == "" {
			return toolError(domain.Validation("node_id is required")), nil, nil
		}

		graph, errResult := graphFromDeps(deps)
		if errResult != nil {
			return errResult, nil, nil
		}

		node, err := graph.GetNode(ctx, input.NodeID)
		if err != nil {
			var de types.DomainError
			if asDomainError(err, &de) && de.Code == types.ErrNotFound {
				result := ShowNodeToolResult{Node: nil}
				return &mcpsdk.CallToolResult{
					Content: []mcpsdk.Content{
						&mcpsdk.TextContent{Text: showNodeMarkdown(result)},
					},
					StructuredContent: result,
				}, nil, nil
			}
			log.Warn("commit0_show_node failed", "node_id", input.NodeID, "err", err)
			return toolError(err), nil, nil
		}

		var nodeOut *CodeNodeOut
		if node != nil {
			n := codeNodeOut(*node, true) // withBody=true
			nodeOut = &n
		}
		result := ShowNodeToolResult{Node: nodeOut}
		return &mcpsdk.CallToolResult{
			Content: []mcpsdk.Content{
				&mcpsdk.TextContent{Text: showNodeMarkdown(result)},
			},
			StructuredContent: result,
		}, nil, nil
	})
}

// Compile-time guard to ensure fmt is used.
var _ = fmt.Sprintf
