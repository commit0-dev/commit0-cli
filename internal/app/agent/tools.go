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

// BuildTools creates ADK tools wrapping commit0's services.
func BuildTools(
	querySvc *app.QueryService,
	traceSvc *app.TraceService,
	blastSvc *app.BlastService,
	flowSvc *app.FieldFlowService,
	tempSvc *app.TemporalService,
	rootCauseSvc *app.RootCauseAnalysisService,
	store domain.GraphStore,
	gitWalker domain.GitWalker,
	explainer domain.LLMExplainer,
) ([]tool.Tool, error) {
	var tools []tool.Tool
	var t tool.Tool
	var err error

	// --- Existing tools ---

	t, err = newSearchTool(querySvc)
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

	// --- New tools for commit zero detection ---

	if flowSvc != nil {
		t, err = newFieldFlowTool(flowSvc)
		if err != nil {
			return nil, fmt.Errorf("flow: %w", err)
		}
		tools = append(tools, t)
	}

	if tempSvc != nil {
		t, err = newTemporalTool(tempSvc)
		if err != nil {
			return nil, fmt.Errorf("temporal: %w", err)
		}
		tools = append(tools, t)
	}

	if gitWalker != nil && explainer != nil {
		t, err = newAnalyzeCommitTool(gitWalker, explainer)
		if err != nil {
			return nil, fmt.Errorf("analyze_commit: %w", err)
		}
		tools = append(tools, t)
	}

	if rootCauseSvc != nil {
		t, err = newFindRootCauseTool(rootCauseSvc)
		if err != nil {
			return nil, fmt.Errorf("find_root: %w", err)
		}
		tools = append(tools, t)
	}

	// write_report — presentation tool for structured terminal output.
	t, err = newWriteReportTool()
	if err != nil {
		return nil, fmt.Errorf("write_report: %w", err)
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

func getRepoPath(ctx tool.Context) string {
	if state := ctx.ReadonlyState(); state != nil {
		if v, err := state.Get("repo_path"); err == nil {
			if s, ok := v.(string); ok {
				return s
			}
		}
	}
	return "."
}

// ==========================================================================
// Existing tools (search, trace, blast, lookup, neighborhood)
// ==========================================================================

type searchInput struct {
	Question string `json:"question"`
	TopK     int    `json:"top_k"`
}
type searchOutput struct {
	ResultCount int            `json:"result_count"`
	Results     []searchResult `json:"results"`
}
type searchResult struct {
	Qualified string   `json:"qualified"`
	Kind      string   `json:"kind"`
	FilePath  string   `json:"file_path"`
	StartLine int      `json:"start_line"`
	EndLine   int      `json:"end_line"`
	Score     float64  `json:"score"`
	Signature string   `json:"signature,omitempty"`
	Summary   string   `json:"summary,omitempty"`
	Docstring string   `json:"docstring,omitempty"`
	Concepts  []string `json:"concepts,omitempty"`
	Body      string   `json:"body,omitempty"` // truncated to 1500 chars
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
			Question: input.Question, RepoSlug: getRepoSlug(ctx), TopK: topK,
		})
		if err != nil {
			return searchOutput{}, err
		}
		out := searchOutput{ResultCount: len(result.Nodes)}
		for _, n := range result.Nodes {
			body := n.Node.Body
			if len(body) > 1500 {
				body = body[:1500] + "\n// ... truncated"
			}
			out.Results = append(out.Results, searchResult{
				Qualified: n.Node.Qualified, Kind: string(n.Node.Kind),
				FilePath: n.Node.FilePath, StartLine: n.Node.StartLine,
				EndLine: n.Node.EndLine, Score: n.FusedScore,
				Signature: n.Node.Signature, Summary: n.Node.Summary,
				Docstring: n.Node.Docstring, Concepts: n.Node.Concepts,
				Body: body,
			})
		}
		return out, nil
	})
}

type traceInput struct {
	Symbol    string `json:"symbol"`
	Direction string `json:"direction"`
	Depth     int    `json:"depth"`
}
type traceHop struct {
	Qualified string `json:"qualified"`
	FilePath  string `json:"file_path"`
	Line      int    `json:"line"`
	Signature string `json:"signature,omitempty"`
	Depth     int    `json:"depth"`
}
type traceOutput struct {
	Direction string     `json:"direction"`
	Root      string     `json:"root"`
	HopCount  int        `json:"hop_count"`
	Hops      []traceHop `json:"hops"`
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
			Symbol: input.Symbol, RepoSlug: getRepoSlug(ctx), Direction: dir, Depth: depth,
		})
		if err != nil {
			return traceOutput{}, err
		}
		out := traceOutput{
			Direction: result.Direction, HopCount: len(result.Tree),
			Root: result.Root.Qualified,
		}
		for _, hop := range result.Tree {
			out.Hops = append(out.Hops, traceHop{
				Qualified: hop.Node.Qualified, FilePath: hop.Node.FilePath,
				Line: hop.Node.StartLine, Signature: hop.Node.Signature,
				Depth: hop.Depth,
			})
		}
		return out, nil
	})
}

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

// ==========================================================================
// New tools for commit zero detection
// ==========================================================================

// --- Field Flow Trace Tool ---

type fieldFlowInput struct {
	Symbol        string `json:"symbol"`
	FieldPath     string `json:"field_path"`
	Direction     string `json:"direction"`
	ShowMutations bool   `json:"show_mutations"`
}
type fieldFlowOutput struct {
	ChainCount    int              `json:"chain_count"`
	MutationCount int              `json:"mutation_count"`
	TaintPoints   []taintPointInfo `json:"taint_points"`
	Chains        []chainInfo      `json:"chains"`
}
type taintPointInfo struct {
	Function     string `json:"function"`
	FilePath     string `json:"file_path"`
	Line         int    `json:"line"`
	MutationType string `json:"mutation_type"`
	MutationExpr string `json:"mutation_expr"`
	FieldPath    string `json:"field_path"`
}
type chainInfo struct {
	FieldPath string   `json:"field_path"`
	HopCount  int      `json:"hop_count"`
	Functions []string `json:"functions"`
}

func newFieldFlowTool(svc *app.FieldFlowService) (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name: "flow_trace",
		Description: "Trace field-level data flow through the codebase. Shows how a specific " +
			"data field (e.g. user.Email) flows through functions, and where it gets mutated. " +
			"Use this to find taint chains — where data is transformed in ways that cause downstream bugs.",
	}, func(ctx tool.Context, input fieldFlowInput) (fieldFlowOutput, error) {
		dir := input.Direction
		if dir == "" {
			dir = "both"
		}
		result, err := svc.TraceFieldFlow(context.Background(), app.FieldFlowRequest{
			Symbol:        input.Symbol,
			FieldPath:     input.FieldPath,
			RepoSlug:      getRepoSlug(ctx),
			Direction:     dir,
			Depth:         10,
			ShowMutations: input.ShowMutations,
		})
		if err != nil {
			return fieldFlowOutput{}, err
		}
		out := fieldFlowOutput{ChainCount: len(result.Chains)}
		for _, chain := range result.Chains {
			out.MutationCount += len(chain.Mutations)
			// Collect taint points
			if chain.TaintPoint != nil {
				tp := chain.TaintPoint
				out.TaintPoints = append(out.TaintPoints, taintPointInfo{
					Function:     tp.Node.Qualified,
					FilePath:     tp.Node.FilePath,
					Line:         tp.MutationLine,
					MutationType: string(tp.MutationType),
					MutationExpr: tp.MutationExpr,
					FieldPath:    tp.FieldPath,
				})
			}
			// Summarize chain
			var funcs []string
			for _, hop := range chain.Hops {
				funcs = append(funcs, hop.Node.Qualified)
			}
			out.Chains = append(out.Chains, chainInfo{
				FieldPath: chain.FieldPath, HopCount: len(chain.Hops), Functions: funcs,
			})
		}
		return out, nil
	})
}

// --- Temporal Query Tool ---

type temporalInput struct {
	Symbol     string `json:"symbol"`
	FromCommit string `json:"from_commit"`
	ToCommit   string `json:"to_commit"`
}
type temporalOutput struct {
	IntroducedCommit   string       `json:"introduced_commit"`
	IntroducedAt       string       `json:"introduced_at"`
	LastModifiedCommit string       `json:"last_modified_commit"`
	LastModifiedAt     string       `json:"last_modified_at"`
	ChangeCount        int          `json:"change_count"`
	Changes            []changeInfo `json:"changes"`
}
type changeInfo struct {
	CommitHash string `json:"commit_hash"`
	Author     string `json:"author"`
	Message    string `json:"message"`
	Timestamp  string `json:"timestamp"`
	ChangeType string `json:"change_type"`
}

func newTemporalTool(svc *app.TemporalService) (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name: "temporal_query",
		Description: "Query the temporal history of a code element. Shows when a function was " +
			"introduced, when it was last modified, and what commits changed it. " +
			"Use this to find WHEN a suspicious change was made.",
	}, func(ctx tool.Context, input temporalInput) (temporalOutput, error) {
		changes, err := svc.QueryHistory(context.Background(), app.TemporalQueryRequest{
			RepoSlug:      getRepoSlug(ctx),
			NodeQualified: input.Symbol,
			FromCommit:    input.FromCommit,
			ToCommit:      input.ToCommit,
		})
		if err != nil {
			return temporalOutput{}, err
		}
		out := temporalOutput{ChangeCount: len(changes)}
		for _, c := range changes {
			ct := "modified"
			if len(c.NodesAdded) > 0 {
				ct = "introduced"
			}
			if len(c.NodesRemoved) > 0 {
				ct = "removed"
			}
			out.Changes = append(out.Changes, changeInfo{
				CommitHash: c.CommitHash, Author: c.Author,
				Message: c.CommitMessage, Timestamp: c.Timestamp.Format("2006-01-02 15:04"),
				ChangeType: ct,
			})
		}
		// Set top-level fields from first/last change
		if len(changes) > 0 {
			first := changes[0]
			out.IntroducedCommit = first.CommitHash
			out.IntroducedAt = first.Timestamp.Format("2006-01-02 15:04")
			last := changes[len(changes)-1]
			out.LastModifiedCommit = last.CommitHash
			out.LastModifiedAt = last.Timestamp.Format("2006-01-02 15:04")
		}
		return out, nil
	})
}

// --- Analyze Commit Diff Tool ---

type analyzeCommitInput struct {
	CommitHash string `json:"commit_hash"`
}
type analyzeCommitOutput struct {
	Hash         string   `json:"hash"`
	Author       string   `json:"author"`
	Message      string   `json:"message"`
	FilesChanged int      `json:"files_changed"`
	Summary      string   `json:"summary"`
	RiskAreas    []string `json:"risk_areas"`
}

func newAnalyzeCommitTool(gitWalker domain.GitWalker, explainer domain.LLMExplainer) (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name: "analyze_commit_diff",
		Description: "Analyze a specific git commit's diff to understand what changed and assess " +
			"its risk. Shows files changed, a summary of the modifications, and potential risk areas. " +
			"Use this to VERIFY whether a suspect commit could have caused a bug.",
	}, func(ctx tool.Context, input analyzeCommitInput) (analyzeCommitOutput, error) {
		repoPath := getRepoPath(ctx)

		info, err := gitWalker.CommitInfo(context.Background(), repoPath, input.CommitHash)
		if err != nil {
			return analyzeCommitOutput{}, err
		}
		diffs, err := gitWalker.DiffCommit(context.Background(), repoPath, input.CommitHash)
		if err != nil {
			return analyzeCommitOutput{}, err
		}

		out := analyzeCommitOutput{
			Hash:         info.Hash,
			Author:       info.Author,
			Message:      info.Message,
			FilesChanged: len(diffs),
		}

		// Build diff summary for LLM analysis
		var diffDesc string
		for _, d := range diffs {
			diffDesc += fmt.Sprintf("  %s %s (+%d -%d)\n", d.Status, d.Path, d.Additions, d.Deletions)
			if d.Patch != "" && len(d.Patch) < 2000 {
				diffDesc += d.Patch + "\n"
			}
		}

		// Ask LLM to summarize the commit
		if explainer != nil {
			raw, err := explainer.ExplainStructured(context.Background(), domain.ExplainRequest{
				QueryType: "search",
				UserQuery: fmt.Sprintf(
					"Analyze this commit and identify risk areas:\nCommit: %s\nAuthor: %s\nMessage: %s\n\nChanges:\n%s",
					info.Hash[:8], info.Author, info.Message, diffDesc,
				),
			})
			if err == nil {
				var result struct {
					Overview string   `json:"overview"`
					Insights []string `json:"insights"`
				}
				if json.Unmarshal(raw, &result) == nil {
					out.Summary = result.Overview
					out.RiskAreas = result.Insights
				}
			}
		}

		if out.Summary == "" {
			out.Summary = fmt.Sprintf("Commit %s by %s: %s (%d files changed)", info.Hash[:8], info.Author, info.Message, len(diffs))
		}

		return out, nil
	})
}

// --- Find Root Cause Tool (automated pipeline) ---

type findRootInput struct {
	Description string `json:"description"`
	Since       string `json:"since"`
}
type findRootOutput struct {
	CommitZero    string   `json:"commit_zero"`
	Author        string   `json:"author"`
	Message       string   `json:"message"`
	Confidence    float64  `json:"confidence"`
	Explanation   string   `json:"explanation"`
	SuggestedFix  string   `json:"suggested_fix"`
	CausalChain   []string `json:"causal_chain"`
	SuspectCount  int      `json:"suspect_count"`
}

func newFindRootCauseTool(svc *app.RootCauseAnalysisService) (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name: "find_root_cause",
		Description: "Automatically find the commit that introduced a bug (commit zero). " +
			"This runs the full 6-step root cause analysis: LOCATE relevant functions, " +
			"TRACE data flow for mutations, query TIMELINE for when changes were introduced, " +
			"CORRELATE to score suspect commits, VERIFY the top suspect, and REPORT findings. " +
			"Use this for complex investigations that span multiple functions and commits.",
	}, func(ctx tool.Context, input findRootInput) (findRootOutput, error) {
		result, err := svc.FindRootCause(context.Background(), app.RootCauseRequest{
			Description: input.Description,
			RepoSlug:    getRepoSlug(ctx),
			RepoPath:    getRepoPath(ctx),
			Since:       input.Since,
		})
		if err != nil {
			return findRootOutput{}, err
		}
		out := findRootOutput{
			CommitZero:   result.CommitHash,
			Author:       result.Author,
			Message:      result.CommitMessage,
			Confidence:   result.Confidence,
			Explanation:  result.Explanation,
			SuggestedFix: result.SuggestedFix,
			SuspectCount: len(result.SuspectCommits),
		}
		for _, hop := range result.CausalChain {
			out.CausalChain = append(out.CausalChain,
				fmt.Sprintf("%s (%s:%d)", hop.Node.Qualified, hop.Node.FilePath, hop.Node.StartLine))
		}
		return out, nil
	})
}

// ==========================================================================
// Presentation tool — structured terminal output
// ==========================================================================

// ReportSection is a single section of a structured report.
// Exported so the CLI can deserialize and render it.
type ReportSection struct {
	Heading    string   `json:"heading"`
	Content    string   `json:"content,omitempty"`
	Code       string   `json:"code,omitempty"`
	CodeLang   string   `json:"code_lang,omitempty"`
	CallChain  []string `json:"call_chain,omitempty"`
	References []string `json:"references,omitempty"`
}

// ReportInput is the structured input for the write_report tool.
// Exported so the CLI can deserialize and render it.
type ReportInput struct {
	Title    string          `json:"title"`
	Summary  string          `json:"summary"`
	Sections []ReportSection `json:"sections"`
}

type reportAck struct {
	Status string `json:"status"`
}

func newWriteReportTool() (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name: "write_report",
		Description: "Present your analysis as a structured report. ALWAYS use this tool to deliver " +
			"your final answer instead of writing raw text. Structure your findings into sections with " +
			"headings, explanations, code snippets, call chains, and file references. " +
			"The report will be rendered with proper formatting for the user's display.",
	}, func(_ tool.Context, _ ReportInput) (reportAck, error) {
		return reportAck{Status: "rendered"}, nil
	})
}
