package linkers

import (
	"time"

	"github.com/commit0-dev/commit0/pkg/types"
	"github.com/commit0-dev/commit0/server/internal/domain"
)

// RouteLinker resolves EdgeRoute handler targets against the global symbol table.
// Route edges connect HTTP route registrations to handler functions.
type RouteLinker struct{}

func (l *RouteLinker) Name() string             { return "route" }
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
				e.Confidence = 0.95
				e.Provenance = &types.Provenance{
					Source:    "route_linker",
					Method:    "route_handler_resolution",
					CreatedAt: time.Now(),
				}
				stats.Resolved++
				stats.Processed++
				continue
			}
		}
		stats.Processed++

		resolved, ok := sym.Resolve(e.ToID, e.FromID)
		if ok {
			e.ToID = resolved
			e.Confidence = 0.95
			stats.Resolved++
		} else {
			e.Confidence = 0.5
			stats.Unresolved++
		}

		e.Provenance = &types.Provenance{
			Source:    "route_linker",
			Method:    "route_handler_resolution",
			CreatedAt: time.Now(),
		}
	}

	return edges, stats
}
