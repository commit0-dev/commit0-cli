package mcp

import (
	"context"
	"log/slog"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/commit0-dev/commit0/server/internal/app"
	"github.com/commit0-dev/commit0/server/internal/domain"
)

// registerTraceTools adds the 4 trace/analysis tools to the server.
//
// These tools sit on top of the same OpenCodeGraph traversal primitives as
// the search tools but expose deeper analysis: full call-chain unrolling,
// transitive impact analysis, field-level data flow with mutation detection,
// and commit-zero (root-cause) detection.
//
// `commit0_find_root_cause` is the only tool here that may take >5s; it
// emits notifications/progress so the client UI can show a live spinner.
func registerTraceTools(server *mcpsdk.Server, deps Deps, log *slog.Logger) {
	addCommit0Trace(server, deps, log)
	addCommit0Blast(server, deps, log)
	addCommit0FieldFlow(server, deps, log)
	addCommit0FindRootCause(server, deps, log)
}

// ---------------------------------------------------------------------------
// Tool: commit0_trace
// ---------------------------------------------------------------------------

// traceInput is the typed input for commit0_trace.
type traceInput struct {
	Symbol          string   `json:"symbol"                     jsonschema:"Qualified name (e.g. 'app.IndexService.Index') or short name to trace from."`
	RepoSlug        string   `json:"repo_slug"                  jsonschema:"Indexed repository slug."`
	Direction       string   `json:"direction,omitempty"        jsonschema:"'forward' (callees) or 'reverse' (callers). Default 'forward'."`
	Depth           int      `json:"depth,omitempty"            jsonschema:"Max traversal depth (1-10). Default 5."`
	EdgeLabels      []string `json:"edge_labels,omitempty"      jsonschema:"Edge types to follow: calls, data_flow, reads, writes, imports, etc. Default ['calls']."`
	WithExplanation bool     `json:"with_explanation,omitempty" jsonschema:"Include LLM explanation (adds 5-15s). Default false."`
}

func addCommit0Trace(server *mcpsdk.Server, deps Deps, log *slog.Logger) {
	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name: "commit0_trace",
		Description: "Trace a call chain (or other edge-typed chain) from a symbol up to N hops. " +
			"`forward` walks outgoing edges (callees, data sinks); `reverse` walks incoming " +
			"edges (callers, data sources). Returns the full hop tree with file paths and line " +
			"ranges. Use this when you need the whole chain, not just one hop — for one hop, " +
			"prefer commit0_neighborhood.",
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, input traceInput) (*mcpsdk.CallToolResult, any, error) {
		if input.Symbol == "" {
			return toolError(domain.Validation("symbol is required")), nil, nil
		}
		if input.RepoSlug == "" {
			return toolError(domain.Validation("repo_slug is required")), nil, nil
		}

		traceSvc, errResult := traceServiceFromDeps(deps)
		if errResult != nil {
			return errResult, nil, nil
		}

		direction := input.Direction
		if direction == "" {
			direction = "forward"
		}
		depth := input.Depth
		if depth <= 0 {
			depth = 5
		}

		tr, err := traceSvc.Trace(ctx, app.TraceRequest{
			Symbol:     input.Symbol,
			RepoSlug:   input.RepoSlug,
			Direction:  direction,
			Depth:      depth,
			EdgeLabels: input.EdgeLabels,
			NoExplain:  !input.WithExplanation,
		})
		if err != nil {
			log.Warn("commit0_trace failed", "symbol", input.Symbol, "err", err)
			return toolError(err), nil, nil
		}

		result := traceResultOut(tr)
		return &mcpsdk.CallToolResult{
			Content: []mcpsdk.Content{
				&mcpsdk.TextContent{Text: traceMarkdown(result)},
			},
			StructuredContent: result,
		}, nil, nil
	})
}

// ---------------------------------------------------------------------------
// Tool: commit0_blast
// ---------------------------------------------------------------------------

// blastInput is the typed input for commit0_blast.
type blastInput struct {
	Symbol          string   `json:"symbol"                     jsonschema:"Qualified name of the function/class about to change."`
	RepoSlug        string   `json:"repo_slug"                  jsonschema:"Indexed repository slug."`
	MaxDepth        int      `json:"max_depth,omitempty"        jsonschema:"Max upstream depth (1-10). Default 5."`
	EdgeLabels      []string `json:"edge_labels,omitempty"      jsonschema:"Edge types to follow upstream: calls (default), reads, data_flow, etc."`
	WithExplanation bool     `json:"with_explanation,omitempty" jsonschema:"Include LLM-generated migration steps and risk assessment. Default false."`
}

func addCommit0Blast(server *mcpsdk.Server, deps Deps, log *slog.Logger) {
	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name: "commit0_blast",
		Description: "Compute the blast radius of a symbol — every function transitively affected if " +
			"the symbol is changed or removed. Walks the graph in reverse from the target up to " +
			"max_depth hops along the chosen edge labels (default: calls). Returns affected nodes " +
			"sorted by hop count. Run this BEFORE editing any non-trivial function.",
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, input blastInput) (*mcpsdk.CallToolResult, any, error) {
		if input.Symbol == "" {
			return toolError(domain.Validation("symbol is required")), nil, nil
		}
		if input.RepoSlug == "" {
			return toolError(domain.Validation("repo_slug is required")), nil, nil
		}

		blastSvc, errResult := blastServiceFromDeps(deps)
		if errResult != nil {
			return errResult, nil, nil
		}

		maxDepth := input.MaxDepth
		if maxDepth <= 0 {
			maxDepth = 5
		}

		br, err := blastSvc.Blast(ctx, app.BlastRequest{
			Symbol:     input.Symbol,
			RepoSlug:   input.RepoSlug,
			MaxDepth:   maxDepth,
			EdgeLabels: input.EdgeLabels,
			NoExplain:  !input.WithExplanation,
		})
		if err != nil {
			log.Warn("commit0_blast failed", "symbol", input.Symbol, "err", err)
			return toolError(err), nil, nil
		}

		result := blastResultOut(br)
		return &mcpsdk.CallToolResult{
			Content: []mcpsdk.Content{
				&mcpsdk.TextContent{Text: blastMarkdown(result)},
			},
			StructuredContent: result,
		}, nil, nil
	})
}

// ---------------------------------------------------------------------------
// Tool: commit0_field_flow
// ---------------------------------------------------------------------------

// fieldFlowInput is the typed input for commit0_field_flow.
type fieldFlowInput struct {
	Symbol        string `json:"symbol"                   jsonschema:"Qualified function name where the field originates (e.g. 'http.NewLoginHandler')."`
	RepoSlug      string `json:"repo_slug"                jsonschema:"Indexed repository slug."`
	FieldPath     string `json:"field_path,omitempty"     jsonschema:"Optional dotted field path to track (e.g. 'user.Email'). Empty = trace all fields."`
	Direction     string `json:"direction,omitempty"      jsonschema:"'forward', 'reverse', or 'both'. Default 'forward'."`
	Depth         int    `json:"depth,omitempty"          jsonschema:"Max traversal depth (1-10). Default 5."`
	ShowMutations bool   `json:"show_mutations,omitempty" jsonschema:"If true, only return chains that contain at least one mutation."`
}

func addCommit0FieldFlow(server *mcpsdk.Server, deps Deps, log *slog.Logger) {
	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name: "commit0_field_flow",
		Description: "Trace the data flow of a struct field through the codebase, with explicit mutation " +
			"tracking. Each chain shows the field's path through call sites and any mutations " +
			"(reassignment, transformation, sanitization) along the way. Use this for taint " +
			"analysis, sensitive-data leakage audits, and understanding how a value is " +
			"transformed end-to-end.",
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, input fieldFlowInput) (*mcpsdk.CallToolResult, any, error) {
		if input.Symbol == "" {
			return toolError(domain.Validation("symbol is required")), nil, nil
		}
		if input.RepoSlug == "" {
			return toolError(domain.Validation("repo_slug is required")), nil, nil
		}

		flowSvc, errResult := fieldFlowServiceFromDeps(deps)
		if errResult != nil {
			return errResult, nil, nil
		}

		direction := input.Direction
		if direction == "" {
			direction = "forward"
		}
		depth := input.Depth
		if depth <= 0 {
			depth = 5
		}

		fr, err := flowSvc.TraceFieldFlow(ctx, app.FieldFlowRequest{
			Symbol:        input.Symbol,
			RepoSlug:      input.RepoSlug,
			FieldPath:     input.FieldPath,
			Direction:     direction,
			Depth:         depth,
			ShowMutations: input.ShowMutations,
		})
		if err != nil {
			log.Warn("commit0_field_flow failed", "symbol", input.Symbol, "err", err)
			return toolError(err), nil, nil
		}

		result := fieldFlowResultOut(fr)
		return &mcpsdk.CallToolResult{
			Content: []mcpsdk.Content{
				&mcpsdk.TextContent{Text: fieldFlowMarkdown(result)},
			},
			StructuredContent: result,
		}, nil, nil
	})
}

// ---------------------------------------------------------------------------
// Tool: commit0_find_root_cause
// ---------------------------------------------------------------------------

// findRootCauseInput is the typed input for commit0_find_root_cause.
type findRootCauseInput struct {
	Description string `json:"description"         jsonschema:"Bug description, error message, or symptom."`
	RepoSlug    string `json:"repo_slug"           jsonschema:"Indexed repository slug."`
	RepoPath    string `json:"repo_path"           jsonschema:"Local filesystem path to the repository (needed for git history)."`
	TestName    string `json:"test_name,omitempty" jsonschema:"Optional: qualified name of a failing test."`
	Since       string `json:"since,omitempty"     jsonschema:"Optional time constraint (e.g. '3 days ago' or a commit hash)."`
}

func addCommit0FindRootCause(server *mcpsdk.Server, deps Deps, log *slog.Logger) {
	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name: "commit0_find_root_cause",
		Description: "Identify the commit that most likely introduced a bug ('commit zero') by " +
			"correlating the bug description with the data flow graph and recent git history. " +
			"Returns a ranked list of suspect commits, the causal chain through the code, and a " +
			"suggested fix. Slow (5-30s) — emits progress notifications.",
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, input findRootCauseInput) (*mcpsdk.CallToolResult, any, error) {
		if input.Description == "" {
			return toolError(domain.Validation("description is required")), nil, nil
		}
		if input.RepoSlug == "" {
			return toolError(domain.Validation("repo_slug is required")), nil, nil
		}
		if input.RepoPath == "" {
			return toolError(domain.Validation("repo_path is required (local fs path for git history)")), nil, nil
		}

		rcSvc, errResult := rootCauseServiceFromDeps(deps)
		if errResult != nil {
			return errResult, nil, nil
		}

		// Progress: 4 phases — search, flow, score, explain.
		progress := NewProgressEmitter(req.Session, req.Params.GetProgressToken(), 4)
		progress.Emit(ctx, 1, "Locating bug-related symbols…")

		report, err := rcSvc.FindRootCause(ctx, app.RootCauseRequest{
			Description: input.Description,
			TestName:    input.TestName,
			RepoSlug:    input.RepoSlug,
			RepoPath:    input.RepoPath,
			Since:       input.Since,
		})
		if err != nil {
			log.Warn("commit0_find_root_cause failed", "err", err)
			return toolError(err), nil, nil
		}

		progress.Emit(ctx, 4, "Done — ranking suspect commits.")

		result := rootCauseReportOut(report)
		return &mcpsdk.CallToolResult{
			Content: []mcpsdk.Content{
				&mcpsdk.TextContent{Text: rootCauseMarkdown(result)},
			},
			StructuredContent: result,
		}, nil, nil
	})
}
