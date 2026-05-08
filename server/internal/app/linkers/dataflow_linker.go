package linkers

import (
	"time"

	"github.com/commit0-dev/commit0/pkg/types"
	"github.com/commit0-dev/commit0/server/internal/domain"
)

// DataFlowLinker resolves EdgeDataFlow targets against the global symbol table.
// data_flow edges have the same ToID format as calls (raw callee text),
// so the same resolution strategies apply.
type DataFlowLinker struct{}

func (l *DataFlowLinker) Name() string             { return "data_flow" }
func (l *DataFlowLinker) Labels() []types.EdgeKind { return []types.EdgeKind{types.EdgeDataFlow} }

func (l *DataFlowLinker) Link(edges []types.CodeEdge, sym *domain.SymbolTable) ([]types.CodeEdge, domain.LinkStats) {
	stats := domain.LinkStats{LinkerName: l.Name()}

	for i := range edges {
		e := &edges[i]
		if e.Kind != types.EdgeDataFlow {
			continue
		}
		if isResolved(e.ToID) {
			e.Confidence = 1.0
			e.Provenance = &types.Provenance{
				Source:    "dataflow_linker",
				Method:    "symbol_resolution",
				CreatedAt: time.Now(),
			}
			continue
		}
		stats.Processed++

		resolved, ok := sym.Resolve(e.ToID, e.FromID)
		if ok {
			e.ToID = resolved
			e.Confidence = 0.9
			stats.Resolved++
		} else {
			e.Confidence = 0.5
			stats.Unresolved++
		}

		e.Provenance = &types.Provenance{
			Source:    "dataflow_linker",
			Method:    "symbol_resolution",
			CreatedAt: time.Now(),
		}
	}

	return edges, stats
}
