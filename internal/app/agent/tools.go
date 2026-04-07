package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"

	"github.com/commit0-dev/commit0/internal/app"
	"github.com/commit0-dev/commit0/internal/domain"
)

// BuildTools creates ADK tools wrapping commit0's existing services.
func BuildTools(
	querySvc *app.QueryService,
	traceSvc *app.TraceService,
	blastSvc *app.BlastService,
	store domain.GraphStore,
) ([]tool.Tool, error) {
	var tools []tool.Tool

	t, err := newSearchTool(querySvc)
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}
	tools = append(tools, t)

	t, err = newTraceTool(traceSvc)
	if err != nil {
		return nil, fmt.Errorf("trace: %w", err)
	}
	tools = append(tools, t)

	t, err = newBlastTool(blastSvc)
	if err != nil {
		return nil, fmt.Errorf("blast: %w", err)
	}
	tools = append(tools, t)

	t, err = newLookupTool(store)
	if err != nil {
		return nil, fmt.Errorf("lookup: %w", err)
	}
	tools = append(tools, t)

	t, err = newNeighborhoodTool(store)
	if err != nil {
		return nil, fmt.Errorf("neighborhood: %w", err)
	}
	tools = append(tools, t)

	return tools, nil
}

// getRepoSlug extracts repo_slug from the tool context state.
func getRepoSlug(ctx tool.Context) string {
	if state := ctx.ReadonlyState(); state != nil {
		if v, err := state.Get("repo_slug"); err == nil {
			if s, ok := v.(string); ok {
				return s
			}
		}
	}
	return ""
}

// --- Search Tool ---

type searchInput struct {
	Question string `json:"question"`
	TopK     int    `json:"top_k"`
}

type searchOutput struct {
	ResultCount int            `json:"result_count"`
	Explanation string         `json:"explanation"`
	Results     []searchResult `json:"results"`
}

type searchResult struct {
	Qualified string  `json:"qualified"`
	Kind      string  `json:"kind"`
	FilePath  string  `json:"file_path"`
	StartLine int     `json:"start_line"`
	Score     float64 `json:"score"`
	Summary   string  `json:"summary"`
}

func newSearchTool(svc *app.QueryService) (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "search_code",
		Description: "Search the codebase using natural language. Returns ranked functions and classes with relevance scores.",
	}, func(ctx tool.Context, input searchInput) (searchOutput, error) {
		topK := input.TopK
		if topK <= 0 {
			topK = 10
		}
		result, err := svc.Query(context.Background(), app.QueryRequest{
			Question: input.Question,
			RepoSlug: getRepoSlug(ctx),
			TopK:     topK,
		})
		if err != nil {
			return searchOutput{}, err
		}
		out := searchOutput{ResultCount: len(result.Nodes), Explanation: result.Explanation}
		for _, n := range result.Nodes {
			out.Results = append(out.Results, searchResult{
				Qualified: n.Node.Qualified, Kind: string(n.Node.Kind),
				FilePath: n.Node.FilePath, StartLine: n.Node.StartLine,
				Score: n.FusedScore, Summary: n.Node.Summary,
			})
		}
		return out, nil
	})
}

// --- Trace Tool ---

type traceInput struct {
	Symbol    string `json:"symbol"`
	Direction string `json:"direction"`
	Depth     int    `json:"depth"`
}

type traceOutput struct {
	Direction   string `json:"direction"`
	HopCount    int    `json:"hop_count"`
	Explanation string `json:"explanation"`
	Root        string `json:"root"`
}

func newTraceTool(svc *app.TraceService) (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "trace_calls",
		Description: "Follow call chains forward (callees) or reverse (callers) from a function.",
	}, func(ctx tool.Context, input traceInput) (traceOutput, error) {
		dir := input.Direction
		if dir == "" {
			dir = "forward"
		}
		depth := input.Depth
		if depth <= 0 {
			depth = 5
		}
		result, err := svc.Trace(context.Background(), app.TraceRequest{
			Symbol: input.Symbol, RepoSlug: getRepoSlug(ctx),
			Direction: dir, Depth: depth,
		})
		if err != nil {
			return traceOutput{}, err
		}
		return traceOutput{
			Direction: result.Direction, HopCount: len(result.Tree),
			Explanation: result.Explanation, Root: result.Root.Qualified,
		}, nil
	})
}

// --- Blast Tool ---

type blastInput struct {
	Symbol   string `json:"symbol"`
	MaxDepth int    `json:"max_depth"`
}

type blastOutput struct {
	AffectedCount int      `json:"affected_count"`
	Summary       string   `json:"summary"`
	Affected      []string `json:"affected"`
}

func newBlastTool(svc *app.BlastService) (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "blast_radius",
		Description: "Analyze what would break if a given function changes.",
	}, func(ctx tool.Context, input blastInput) (blastOutput, error) {
		maxDepth := input.MaxDepth
		if maxDepth <= 0 {
			maxDepth = 3
		}
		result, err := svc.Blast(context.Background(), app.BlastRequest{
			Symbol: input.Symbol, RepoSlug: getRepoSlug(ctx), MaxDepth: maxDepth,
		})
		if err != nil {
			return blastOutput{}, err
		}
		var affected []string
		for _, a := range result.Affected {
			affected = append(affected, fmt.Sprintf("[depth %d] %s (%s)", a.HopCount, a.Node.Qualified, a.Path))
		}
		return blastOutput{AffectedCount: len(result.Affected), Summary: result.Summary, Affected: affected}, nil
	})
}

// --- Lookup Tool ---

type lookupInput struct {
	Qualified string `json:"qualified"`
}

func newLookupTool(store domain.GraphStore) (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "lookup_node",
		Description: "Look up a specific function or class by its qualified name.",
	}, func(ctx tool.Context, input lookupInput) (json.RawMessage, error) {
		node, err := store.GetNodeByQualified(context.Background(), getRepoSlug(ctx), input.Qualified)
		if err != nil {
			return nil, err
		}
		b, _ := json.Marshal(node)
		return b, nil
	})
}

// --- Neighborhood Tool ---

type neighborhoodInput struct {
	NodeID string `json:"node_id"`
}

func newNeighborhoodTool(store domain.GraphStore) (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "get_neighborhood",
		Description: "Get callers, callees, and data flow for a code node by its ID.",
	}, func(ctx tool.Context, input neighborhoodInput) (json.RawMessage, error) {
		nb, err := store.GetNeighborhood(context.Background(), input.NodeID)
		if err != nil {
			return nil, err
		}
		b, _ := json.Marshal(nb)
		return b, nil
	})
}
