package types

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
