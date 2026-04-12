package linkers

import (
	"github.com/commit0-dev/commit0/server/internal/domain"
	"github.com/commit0-dev/commit0/pkg/types"
)

// DataFlowLinker resolves EdgeDataFlow targets against the global symbol table.
// data_flow edges have the same ToID format as calls (raw callee text),
// so the same resolution strategies apply.
type DataFlowLinker struct{}

func (l *DataFlowLinker) Name() string            { return "data_flow" }
func (l *DataFlowLinker) Labels() []types.EdgeKind { return []types.EdgeKind{types.EdgeDataFlow} }

func (l *DataFlowLinker) Link(edges []types.CodeEdge, sym *domain.SymbolTable) ([]types.CodeEdge, domain.LinkStats) {
	stats := domain.LinkStats{LinkerName: l.Name()}

	for i := range edges {
		e := &edges[i]
		if e.Kind != types.EdgeDataFlow {
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
