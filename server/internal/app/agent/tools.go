package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/commit0-dev/commit0/server/internal/app"
	"github.com/commit0-dev/commit0/server/internal/domain"
)

// BuildTools creates the agent tools wrapping commit0's services.
func BuildTools(
	querySvc *app.QueryService,
	traceSvc *app.TraceService,
	blastSvc *app.BlastService,
	flowSvc *app.FieldFlowService,
	tempSvc *app.TemporalService,
	rootCauseSvc *app.RootCauseAnalysisService,
	graph domain.OpenCodeGraph,
	gitWalker domain.GitWalker,
	explainer domain.LLMExplainer,
) []AgentTool {
	var tools []AgentTool

	tools = append(tools, &searchTool{svc: querySvc})
	tools = append(tools, &traceTool{svc: traceSvc})
	tools = append(tools, &blastTool{svc: blastSvc})
	tools = append(tools, &lookupTool{graph: graph})
	tools = append(tools, &neighborhoodTool{graph: graph})

	if flowSvc != nil {
		tools = append(tools, &fieldFlowTool{svc: flowSvc})
	}
	if tempSvc != nil {
		tools = append(tools, &temporalTool{svc: tempSvc})
	}
	if gitWalker != nil && explainer != nil {
		tools = append(tools, &analyzeCommitTool{gitWalker: gitWalker, explainer: explainer})
	}
	if rootCauseSvc != nil {
		tools = append(tools, &findRootCauseTool{svc: rootCauseSvc})
	}

	tools = append(tools, &writeReportTool{})

	return tools
}

// ==========================================================================
// Search tool
// ==========================================================================

type searchInput struct {
	Question string `json:"question"`
	TopK     int    `json:"top_k"`
	FilePath string `json:"file_path,omitempty"`
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
	Body      string   `json:"body,omitempty"`
}

type searchTool struct{ svc *app.QueryService }

func (t *searchTool) Def() ToolDef {
	return ToolDef{
		Name:         "search_code",
		Description:  "Search the codebase using natural language. Returns ranked functions and classes with relevance scores.",
		InputExample: searchInput{},
	}
}

func (t *searchTool) Invoke(ctx context.Context, argsJSON string) (string, error) {
	var input searchInput
	if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
		return "", fmt.Errorf("parse args: %w", err)
	}
	topK := input.TopK
	if topK <= 0 {
		topK = 10
	}
	if topK > 20 {
		topK = 20
	}
	result, err := t.svc.Query(context.Background(), app.QueryRequest{
		Question: input.Question, RepoSlug: RepoSlugFrom(ctx), TopK: topK,
		NoExplain: true, FilePath: input.FilePath,
	})
	if err != nil {
		return "", err
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
	return marshalJSON(out)
}

// ==========================================================================
// Trace tool
// ==========================================================================

type traceInput struct {
	Symbol     string   `json:"symbol"`
	Direction  string   `json:"direction"`
	Depth      int      `json:"depth"`
	EdgeLabels []string `json:"edge_labels"`
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

type traceTool struct{ svc *app.TraceService }

func (t *traceTool) Def() ToolDef {
	return ToolDef{
		Name:         "trace_calls",
		Description:  "Follow edges forward (callees/sinks) or reverse (callers/sources) from a function. Set edge_labels to choose which edges: calls, data_flow, reads, writes. Default: calls only.",
		InputExample: traceInput{},
	}
}

func (t *traceTool) Invoke(ctx context.Context, argsJSON string) (string, error) {
	var input traceInput
	if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
		return "", fmt.Errorf("parse args: %w", err)
	}
	dir := input.Direction
	if dir == "" {
		dir = "forward"
	}
	depth := input.Depth
	if depth <= 0 {
		depth = 5
	}
	result, err := t.svc.Trace(context.Background(), app.TraceRequest{
		Symbol: input.Symbol, RepoSlug: RepoSlugFrom(ctx), Direction: dir, Depth: depth,
		EdgeLabels: input.EdgeLabels,
	})
	if err != nil {
		return "", err
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
	return marshalJSON(out)
}

// ==========================================================================
// Blast radius tool
// ==========================================================================

type blastInput struct {
	Symbol     string   `json:"symbol"`
	MaxDepth   int      `json:"max_depth"`
	EdgeLabels []string `json:"edge_labels"`
}
type blastOutput struct {
	AffectedCount int      `json:"affected_count"`
	Summary       string   `json:"summary"`
	Affected      []string `json:"affected"`
}

type blastTool struct{ svc *app.BlastService }

func (t *blastTool) Def() ToolDef {
	return ToolDef{
		Name:         "blast_radius",
		Description:  "Analyze what would break if a given function changes. Set edge_labels to include data_flow for broader impact. Default: calls only.",
		InputExample: blastInput{},
	}
}

func (t *blastTool) Invoke(ctx context.Context, argsJSON string) (string, error) {
	var input blastInput
	if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
		return "", fmt.Errorf("parse args: %w", err)
	}
	maxDepth := input.MaxDepth
	if maxDepth <= 0 {
		maxDepth = 3
	}
	result, err := t.svc.Blast(context.Background(), app.BlastRequest{
		Symbol: input.Symbol, RepoSlug: RepoSlugFrom(ctx), MaxDepth: maxDepth,
		EdgeLabels: input.EdgeLabels,
	})
	if err != nil {
		return "", err
	}
	var affected []string
	for _, a := range result.Affected {
		affected = append(affected, fmt.Sprintf("[depth %d] %s (%s)", a.HopCount, a.Node.Qualified, a.Path))
	}
	return marshalJSON(blastOutput{AffectedCount: len(result.Affected), Summary: result.Summary, Affected: affected})
}

// ==========================================================================
// Lookup tool
// ==========================================================================

type lookupInput struct {
	Qualified string `json:"qualified"`
}

type lookupTool struct{ graph domain.OpenCodeGraph }

func (t *lookupTool) Def() ToolDef {
	return ToolDef{
		Name:         "lookup_node",
		Description:  "Look up a specific function or class by its qualified name.",
		InputExample: lookupInput{},
	}
}

func (t *lookupTool) Invoke(ctx context.Context, argsJSON string) (string, error) {
	var input lookupInput
	if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
		return "", fmt.Errorf("parse args: %w", err)
	}
	node, err := t.graph.FindNode(context.Background(), RepoSlugFrom(ctx), input.Qualified)
	if err != nil {
		return "", err
	}
	b, _ := json.Marshal(node)
	return string(b), nil
}

// ==========================================================================
// Neighborhood tool
// ==========================================================================

type neighborhoodInput struct {
	NodeID string `json:"node_id"`
}

type neighborhoodTool struct{ graph domain.OpenCodeGraph }

func (t *neighborhoodTool) Def() ToolDef {
	return ToolDef{
		Name:         "get_neighborhood",
		Description:  "Get callers, callees, and data flow for a code node by its ID.",
		InputExample: neighborhoodInput{},
	}
}

func (t *neighborhoodTool) Invoke(ctx context.Context, argsJSON string) (string, error) {
	var input neighborhoodInput
	if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
		return "", fmt.Errorf("parse args: %w", err)
	}
	nb, err := t.graph.Neighbors(context.Background(), input.NodeID)
	if err != nil {
		return "", err
	}
	b, _ := json.Marshal(nb)
	return string(b), nil
}

// ==========================================================================
// Field Flow Trace tool
// ==========================================================================

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

type fieldFlowTool struct{ svc *app.FieldFlowService }

func (t *fieldFlowTool) Def() ToolDef {
	return ToolDef{
		Name: "flow_trace",
		Description: "Trace field-level data flow through the codebase. Shows how a specific " +
			"data field (e.g. user.Email) flows through functions, and where it gets mutated. " +
			"Use this to find taint chains — where data is transformed in ways that cause downstream bugs.",
		InputExample: fieldFlowInput{},
	}
}

func (t *fieldFlowTool) Invoke(ctx context.Context, argsJSON string) (string, error) {
	var input fieldFlowInput
	if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
		return "", fmt.Errorf("parse args: %w", err)
	}
	dir := input.Direction
	if dir == "" {
		dir = "both"
	}
	result, err := t.svc.TraceFieldFlow(context.Background(), app.FieldFlowRequest{
		Symbol: input.Symbol, FieldPath: input.FieldPath,
		RepoSlug: RepoSlugFrom(ctx), Direction: dir,
		Depth: 10, ShowMutations: input.ShowMutations,
	})
	if err != nil {
		return "", err
	}
	out := fieldFlowOutput{ChainCount: len(result.Chains)}
	for _, chain := range result.Chains {
		out.MutationCount += len(chain.Mutations)
		if chain.TaintPoint != nil {
			tp := chain.TaintPoint
			out.TaintPoints = append(out.TaintPoints, taintPointInfo{
				Function: tp.Node.Qualified, FilePath: tp.Node.FilePath,
				Line: tp.MutationLine, MutationType: string(tp.MutationType),
				MutationExpr: tp.MutationExpr, FieldPath: tp.FieldPath,
			})
		}
		var funcs []string
		for _, hop := range chain.Hops {
			funcs = append(funcs, hop.Node.Qualified)
		}
		out.Chains = append(out.Chains, chainInfo{
			FieldPath: chain.FieldPath, HopCount: len(chain.Hops), Functions: funcs,
		})
	}
	return marshalJSON(out)
}

// ==========================================================================
// Temporal Query tool
// ==========================================================================

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

type temporalTool struct{ svc *app.TemporalService }

func (t *temporalTool) Def() ToolDef {
	return ToolDef{
		Name: "temporal_query",
		Description: "Query the temporal history of a code element. Shows when a function was " +
			"introduced, when it was last modified, and what commits changed it. " +
			"Use this to find WHEN a suspicious change was made.",
		InputExample: temporalInput{},
	}
}

func (t *temporalTool) Invoke(ctx context.Context, argsJSON string) (string, error) {
	var input temporalInput
	if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
		return "", fmt.Errorf("parse args: %w", err)
	}
	changes, err := t.svc.QueryHistory(context.Background(), app.TemporalQueryRequest{
		RepoSlug: RepoSlugFrom(ctx), NodeQualified: input.Symbol,
		FromCommit: input.FromCommit, ToCommit: input.ToCommit,
	})
	if err != nil {
		return "", err
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
	if len(changes) > 0 {
		first := changes[0]
		out.IntroducedCommit = first.CommitHash
		out.IntroducedAt = first.Timestamp.Format("2006-01-02 15:04")
		last := changes[len(changes)-1]
		out.LastModifiedCommit = last.CommitHash
		out.LastModifiedAt = last.Timestamp.Format("2006-01-02 15:04")
	}
	return marshalJSON(out)
}

// ==========================================================================
// Analyze Commit Diff tool
// ==========================================================================

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

type analyzeCommitTool struct {
	gitWalker domain.GitWalker
	explainer domain.LLMExplainer
}

func (t *analyzeCommitTool) Def() ToolDef {
	return ToolDef{
		Name: "analyze_commit_diff",
		Description: "Analyze a specific git commit's diff to understand what changed and assess " +
			"its risk. Shows files changed, a summary of the modifications, and potential risk areas. " +
			"Use this to VERIFY whether a suspect commit could have caused a bug.",
		InputExample: analyzeCommitInput{},
	}
}

func (t *analyzeCommitTool) Invoke(ctx context.Context, argsJSON string) (string, error) {
	var input analyzeCommitInput
	if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
		return "", fmt.Errorf("parse args: %w", err)
	}
	repoPath := RepoPathFrom(ctx)

	info, err := t.gitWalker.CommitInfo(context.Background(), repoPath, input.CommitHash)
	if err != nil {
		return "", err
	}
	diffs, err := t.gitWalker.DiffCommit(context.Background(), repoPath, input.CommitHash)
	if err != nil {
		return "", err
	}

	out := analyzeCommitOutput{
		Hash: info.Hash, Author: info.Author,
		Message: info.Message, FilesChanged: len(diffs),
	}

	var diffDesc string
	for _, d := range diffs {
		diffDesc += fmt.Sprintf("  %s %s (+%d -%d)\n", d.Status, d.Path, d.Additions, d.Deletions)
		if d.Patch != "" && len(d.Patch) < 2000 {
			diffDesc += d.Patch + "\n"
		}
	}

	if t.explainer != nil {
		raw, err := t.explainer.ExplainStructured(context.Background(), domain.ExplainRequest{
			QueryType: "search",
			UserQuery: fmt.Sprintf(
				"Analyze this commit and identify risk areas:\nCommit: %s\nAuthor: %s\nMessage: %s\n\nChanges:\n%s",
				info.Hash[:8], info.Author, info.Message, diffDesc,
			),
			ResponseSchema: domain.SchemaForQueryType("search"),
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

	return marshalJSON(out)
}

// ==========================================================================
// Find Root Cause tool
// ==========================================================================

type findRootInput struct {
	Description string `json:"description"`
	Since       string `json:"since"`
}
type findRootOutput struct {
	CommitZero   string   `json:"commit_zero"`
	Author       string   `json:"author"`
	Message      string   `json:"message"`
	Confidence   float64  `json:"confidence"`
	Explanation  string   `json:"explanation"`
	SuggestedFix string   `json:"suggested_fix"`
	CausalChain  []string `json:"causal_chain"`
	SuspectCount int      `json:"suspect_count"`
}

type findRootCauseTool struct{ svc *app.RootCauseAnalysisService }

func (t *findRootCauseTool) Def() ToolDef {
	return ToolDef{
		Name: "find_root_cause",
		Description: "Automatically find the commit that introduced a bug (commit zero). " +
			"This runs the full 6-step root cause analysis: LOCATE relevant functions, " +
			"TRACE data flow for mutations, query TIMELINE for when changes were introduced, " +
			"CORRELATE to score suspect commits, VERIFY the top suspect, and REPORT findings. " +
			"Use this for complex investigations that span multiple functions and commits.",
		InputExample: findRootInput{},
	}
}

func (t *findRootCauseTool) Invoke(ctx context.Context, argsJSON string) (string, error) {
	var input findRootInput
	if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
		return "", fmt.Errorf("parse args: %w", err)
	}
	result, err := t.svc.FindRootCause(context.Background(), app.RootCauseRequest{
		Description: input.Description, RepoSlug: RepoSlugFrom(ctx),
		RepoPath: RepoPathFrom(ctx), Since: input.Since,
	})
	if err != nil {
		return "", err
	}
	out := findRootOutput{
		CommitZero: result.CommitHash, Author: result.Author,
		Message: result.CommitMessage, Confidence: result.Confidence,
		Explanation: result.Explanation, SuggestedFix: result.SuggestedFix,
		SuspectCount: len(result.SuspectCommits),
	}
	for _, hop := range result.CausalChain {
		out.CausalChain = append(out.CausalChain,
			fmt.Sprintf("%s (%s:%d)", hop.Node.Qualified, hop.Node.FilePath, hop.Node.StartLine))
	}
	return marshalJSON(out)
}

// ==========================================================================
// Write Report tool — presentation tool for structured terminal output
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

type writeReportTool struct{}

func (t *writeReportTool) Def() ToolDef {
	return ToolDef{
		Name: "write_report",
		Description: "Present your analysis as a structured report. ALWAYS use this tool to deliver " +
			"your final answer instead of writing raw text. Structure your findings into sections with " +
			"headings, explanations, code snippets, call chains, and file references. " +
			"The report will be rendered with proper formatting for the user's display.",
		InputExample: ReportInput{},
	}
}

func (t *writeReportTool) Invoke(_ context.Context, _ string) (string, error) {
	return `{"status":"rendered"}`, nil
}

// ==========================================================================
// Helper
// ==========================================================================

func marshalJSON(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("marshal output: %w", err)
	}
	return string(b), nil
}
