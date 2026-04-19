package types

import "time"

// QueryResult represents the result of a query operation.
type QueryResult struct {
	Explanation          string              // plain text (backward compat)
	StructuredExplanation *SearchExplanation  `json:"StructuredExplanation,omitempty"`
	Query                string
	RepoSlug             string
	Nodes                []ScoredNode
	Timing               TimingInfo
}

// ScoredNode wraps a CodeNode with relevance scores.
type ScoredNode struct {
	Node        CodeNode // The code node
	VectorScore float64  // Score from vector similarity search
	FTSScore    float64  // Score from full-text search
	FusedScore  float64  // Combined score from RRF
	Centrality  int      // Graph centrality metric
}

// TraceResult represents the result of a code trace operation.
type TraceResult struct {
	Direction             string
	Explanation           string
	StructuredExplanation *TraceExplanation `json:"StructuredExplanation,omitempty"`
	Tree                  []TraceHop
	Root                  CodeNode
	Timing                TimingInfo
}

// TraceHop represents a single hop in a trace tree.
type TraceHop struct {
	Edge     CodeEdge
	Children []TraceHop
	Node     CodeNode
	Depth    int
}

// BlastResult represents the result of a blast radius analysis.
type BlastResult struct {
	Summary           string
	StructuredSummary *BlastExplanation `json:"StructuredSummary,omitempty"`
	Affected          []AffectedNode
	Target            CodeNode
	Timing            TimingInfo
}

// AffectedNode represents a node affected by a change.
type AffectedNode struct {
	Module   string
	Path     string
	Node     CodeNode
	HopCount int
}

// DataFlowResult represents the result of a data-flow analysis rooted at a symbol.
type DataFlowResult struct {
	Explanation string
	Direction   string
	Paths       []FlowPath
	Root        CodeNode
	Timing      TimingInfo
}

// FlowPath is one chain of data movement through the codebase, tagged with the
// parameter name or field name being tracked.
type FlowPath struct {
	DataTag string
	Hops    []FlowHop
}

// FlowHop is a single step in a data-flow path.
type FlowHop struct {
	DataExpr string
	Edge     CodeEdge
	Node     CodeNode
	Depth    int
}

// SearchExplanation is a structured LLM response for search queries.
type SearchExplanation struct {
	Overview string         `json:"overview"`
	Evidence []EvidenceItem `json:"evidence"`
	Insights []string       `json:"insights"`
}

// EvidenceItem references a specific code location in an explanation.
type EvidenceItem struct {
	Function    string `json:"function"`
	File        string `json:"file"`
	Lines       string `json:"lines"`
	Description string `json:"description"`
	Relevance   string `json:"relevance"`
}

// TraceExplanation is a structured LLM response for trace queries.
type TraceExplanation struct {
	Overview    string     `json:"overview"`
	FlowSteps   []FlowStep `json:"flow_steps"`
	KeyInsights []string   `json:"key_insights"`
}

// FlowStep describes a single step in a traced execution flow.
type FlowStep struct {
	Hop         int    `json:"hop"`
	Function    string `json:"function"`
	Action      string `json:"action"`
	DataChanges string `json:"data_changes"`
}

// BlastExplanation is a structured LLM response for blast radius queries.
type BlastExplanation struct {
	Overview       string     `json:"overview"`
	Severity       string     `json:"severity"`
	RiskAreas      []RiskArea `json:"risk_areas"`
	MigrationSteps []string   `json:"migration_steps"`
}

// RiskArea identifies a component at risk from a code change.
type RiskArea struct {
	Function   string `json:"function"`
	File       string `json:"file"`
	Risk       string `json:"risk"`
	Mitigation string `json:"mitigation"`
}

// TimingInfo holds performance metrics for operations.
type TimingInfo struct {
	EmbedMS   int64 // Time spent embedding
	SearchMS  int64 // Time spent searching
	GraphMS   int64 // Time spent on graph operations
	ExplainMS int64 // Time spent generating explanations
	TotalMS   int64 // Total operation time
}

// ---------------------------------------------------------------------------
// Field-Level Data Flow Tracing
// ---------------------------------------------------------------------------

// FieldFlowHop represents a single step in a field-level data flow trace.
type FieldFlowHop struct {
	Node         CodeNode
	Edge         CodeEdge
	FieldPath    string       // e.g. "user.Email"
	ParamName    string       // callee parameter receiving the data
	ArgExpr      string       // expression passed at call site
	MutationType MutationKind // what mutation occurs here
	MutationExpr string       // e.g. "strings.ToLower(email)"
	MutationLine int
	Depth        int
}

// FieldFlowChain is one end-to-end path of a specific field through functions.
type FieldFlowChain struct {
	FieldPath  string          // the tracked field, e.g. "user.Email"
	Hops       []FieldFlowHop
	Mutations  []FieldFlowHop  // subset where MutationType != "none"
	TaintPoint *FieldFlowHop   // the first mutation point (if any)
}

// FieldFlowResult represents the full result of a field-level data flow query.
type FieldFlowResult struct {
	Root        CodeNode
	Direction   string
	Chains      []FieldFlowChain
	Explanation string
	Timing      TimingInfo
}

// ---------------------------------------------------------------------------
// Temporal Code Graph
// ---------------------------------------------------------------------------

// TemporalChange records what changed in the code graph at a specific commit.
type TemporalChange struct {
	CommitHash    string
	CommitMessage string
	Author        string
	Timestamp     time.Time
	NodesAdded    []CodeNode
	NodesModified []CodeNode
	NodesRemoved  []string // qualified names
	EdgesAdded    []CodeEdge
	EdgesRemoved  []CodeEdge
}

// ---------------------------------------------------------------------------
// Root Cause Detection (Commit Zero)
// ---------------------------------------------------------------------------

// SuspectCommit is a candidate commit that may have caused the bug.
type SuspectCommit struct {
	Hash        string
	Message     string
	Author      string
	Timestamp   time.Time
	Score       float64 // composite: temporal_proximity × data_flow_position × change_magnitude
	Reasoning   string  // LLM explanation of why this commit is suspicious
	DiffSummary string  // summarized diff
}

// RootCauseReport is the final output of commit zero detection.
type RootCauseReport struct {
	CommitHash     string          // the commit zero
	CommitMessage  string
	Author         string
	Timestamp      time.Time
	Confidence     float64         // 0.0 - 1.0
	CausalChain    []FieldFlowHop  // data flow from root cause to symptom
	Explanation    string          // LLM-generated full explanation
	SuggestedFix   string          // LLM-suggested fix
	SuspectCommits []SuspectCommit // ranked candidates
	Timing         TimingInfo
}

// ---------------------------------------------------------------------------
// Agent Streaming
// ---------------------------------------------------------------------------

// ChatEvent is streamed back as the agent reasons and calls tools.
type ChatEvent struct {
	Type     string `json:"type"`      // "thinking", "tool_call", "tool_result", "message", "error", "done"
	Content  string `json:"content"`   // text content or JSON
	ToolName string `json:"tool_name"` // set for tool_call/tool_result
	Done     bool   `json:"done"`
}

// ---------------------------------------------------------------------------
// Memory Management
// ---------------------------------------------------------------------------

// MemoryTier identifies which tier of the memory hierarchy an entry belongs to.
type MemoryTier string

const (
	MemoryWorking    MemoryTier = "working"
	MemorySession    MemoryTier = "session"
	MemoryPersistent MemoryTier = "persistent"
)

// MemoryEntry is a single piece of stored memory.
type MemoryEntry struct {
	ID         string
	Tier       MemoryTier
	SessionID  string
	RepoSlug   string
	Content    string
	Concepts   []string  // semantic tags for retrieval
	Embedding  []float32
	CreatedAt  time.Time
	TokenCount int
}
