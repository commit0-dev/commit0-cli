package linkers

import (
	"github.com/commit0-dev/commit0/server/internal/domain"
	"github.com/commit0-dev/commit0/pkg/types"
)

// RouteLinker resolves EdgeRoute handler targets against the global symbol table.
// Route edges connect HTTP route registrations to handler functions.
type RouteLinker struct{}

func (l *RouteLinker) Name() string            { return "route" }
func (l *RouteLinker) Labels() []types.EdgeKind { return []types.EdgeKind{types.EdgeRoute} }

func (l *RouteLinker) Link(edges []types.CodeEdge, sym *domain.SymbolTable) ([]types.CodeEdge, domain.LinkStats) {
	stats := domain.LinkStats{LinkerName: l.Name()}

	for i := range edges {
		e := &edges[i]
		if e.Kind != types.EdgeRoute {
			continue
		}
		if isResolved(e.ToID) {
			if _, ok := sym.Nodes[e.ToID]; ok {
				stats.Resolved++
				stats.Processed++
				continue
			}
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
