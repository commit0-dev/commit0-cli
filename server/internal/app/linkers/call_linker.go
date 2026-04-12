package linkers

import (
	"strings"

	"github.com/commit0-dev/commit0/server/internal/domain"
	"github.com/commit0-dev/commit0/pkg/types"
)

// CallLinker resolves EdgeCalls targets against the global symbol table.
// Replaces the old per-file Resolver's call resolution logic.
type CallLinker struct{}

func (l *CallLinker) Name() string                  { return "call" }
func (l *CallLinker) Labels() []types.EdgeKind       { return []types.EdgeKind{types.EdgeCalls} }

func (l *CallLinker) Link(edges []types.CodeEdge, sym *domain.SymbolTable) ([]types.CodeEdge, domain.LinkStats) {
	stats := domain.LinkStats{LinkerName: l.Name()}

	for i := range edges {
		e := &edges[i]
		if e.Kind != types.EdgeCalls {
			continue
		}
		if isResolved(e.ToID) {
			continue
		}
		stats.Processed++

		resolved, ok := sym.Resolve(e.ToID, e.FromID)
		if ok {
			e.ToID = resolved
			stats.Resolved++
		} else {
			stats.Unresolved++
		}
	}

	return edges, stats
}

// isResolved returns true if the ID is already a concrete node ID (has ":" prefix).
func isResolved(id string) bool {
	return strings.Contains(id, ":")
}
