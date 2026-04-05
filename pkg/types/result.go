package types

// QueryResult represents the result of a query operation.
type QueryResult struct {
	Explanation string
	Query       string
	RepoSlug    string
	Nodes       []ScoredNode
	Timing      TimingInfo
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
	Direction   string
	Explanation string
	Tree        []TraceHop
	Root        CodeNode
	Timing      TimingInfo
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
	Summary  string
	Affected []AffectedNode
	Target   CodeNode
	Timing   TimingInfo
}

// AffectedNode represents a node affected by a change.
type AffectedNode struct {
	Module   string
	Path     string
	Node     CodeNode
	HopCount int
}

// TimingInfo holds performance metrics for operations.
type TimingInfo struct {
	EmbedMS   int64 // Time spent embedding
	SearchMS  int64 // Time spent searching
	GraphMS   int64 // Time spent on graph operations
	ExplainMS int64 // Time spent generating explanations
	TotalMS   int64 // Total operation time
}
