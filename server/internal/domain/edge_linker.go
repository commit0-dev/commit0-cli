package domain

import (
	"github.com/commit0-dev/commit0/pkg/types"
)

// EdgeLinker resolves raw edges against the complete SymbolTable.
// Each analysis technique provides a linker for its edge labels.
// New techniques register a linker — zero changes to the pipeline
// or existing linkers.
type EdgeLinker interface {
	// Name returns a human-readable identifier for logging and metrics.
	Name() string

	// Labels returns which edge kinds this linker processes.
	// Edges with other kinds pass through unchanged.
	Labels() []types.EdgeKind

	// Link resolves unresolved edges against the symbol table.
	// Input edges may have unresolved FromID/ToID (raw AST text).
	// Returns all edges (resolved where possible) and stats.
	Link(edges []types.CodeEdge, symbols *SymbolTable) ([]types.CodeEdge, LinkStats)
}

// LinkStats reports resolution metrics for one linker pass.
type LinkStats struct {
	LinkerName string `json:"linker_name"`
	Processed  int    `json:"processed"`  // edges this linker examined
	Resolved   int    `json:"resolved"`   // edges successfully resolved
	Ambiguous  int    `json:"ambiguous"`  // resolved but with ambiguity
	Unresolved int    `json:"unresolved"` // could not resolve
}
