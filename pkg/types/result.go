package types

// QueryResult represents the result of a query operation
type QueryResult struct {
	Nodes       []ScoredNode // Ranked nodes matching the query
	Explanation string       // Natural language explanation
	Query       string       // Original query text
	RepoSlug    string       // Repository identifier
	Timing      TimingInfo   // Operation timing information
}

// ScoredNode wraps a CodeNode with relevance scores
type ScoredNode struct {
	Node       CodeNode // The code node
	VectorScore float64  // Score from vector similarity search
	FTSScore   float64   // Score from full-text search
	FusedScore float64   // Combined score from RRF
	Centrality int       // Graph centrality metric
}

// TraceResult represents the result of a code trace operation
type TraceResult struct {
	Root        CodeNode   // Starting node for trace
	Tree        []TraceHop // Hierarchical call chain
	Direction   string     // "forward" or "reverse"
	Explanation string     // Natural language explanation
	Timing      TimingInfo // Operation timing information
}

// TraceHop represents a single hop in a trace tree
type TraceHop struct {
	Depth    int         // Distance from root
	Node     CodeNode    // Node at this hop
	Edge     CodeEdge    // Edge to this node (from parent)
	Children []TraceHop  // Recursive children
}

// BlastResult represents the result of a blast radius analysis
type BlastResult struct {
	Target      CodeNode      // Original target node
	Affected    []AffectedNode // Nodes affected by changes to target
	Summary     string        // Natural language summary
	Timing      TimingInfo    // Operation timing information
}

// AffectedNode represents a node affected by a change
type AffectedNode struct {
	Node     CodeNode // The affected node
	HopCount int      // Distance from target node
	Module   string   // Module containing the node
	Path     string   // File path
}

// TimingInfo holds performance metrics for operations
type TimingInfo struct {
	EmbedMS   int64 // Time spent embedding
	SearchMS  int64 // Time spent searching
	GraphMS   int64 // Time spent on graph operations
	ExplainMS int64 // Time spent generating explanations
	TotalMS   int64 // Total operation time
}
