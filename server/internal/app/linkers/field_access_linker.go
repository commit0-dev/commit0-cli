package linkers

import (
	"strings"

	"github.com/commit0-dev/commit0/server/internal/domain"
	"github.com/commit0-dev/commit0/pkg/types"
)

// FieldAccessLinker resolves EdgeReads and EdgeWrites targets.
//
// Current problem: reads/writes edges have ToID = makeNodeID("class", operandText)
// where operandText is a VARIABLE NAME like "is" or "ctx" — not a real class.
// The linker infers the receiver type from the FromID's qualified name.
//
// Example:
//
//	FromID: function:app⋅IndexService⋅Index
//	ToID:   class:is  (broken — "is" is a local variable, not a type)
//	→ Infer: "IndexService" is the receiver → resolve to class:app⋅IndexService
type FieldAccessLinker struct{}

func (l *FieldAccessLinker) Name() string { return "field_access" }
func (l *FieldAccessLinker) Labels() []types.EdgeKind {
	return []types.EdgeKind{types.EdgeReads, types.EdgeWrites}
}

func (l *FieldAccessLinker) Link(edges []types.CodeEdge, sym *domain.SymbolTable) ([]types.CodeEdge, domain.LinkStats) {
	stats := domain.LinkStats{LinkerName: l.Name()}

	for i := range edges {
		e := &edges[i]
		if e.Kind != types.EdgeReads && e.Kind != types.EdgeWrites {
			continue
		}
		stats.Processed++

		// Check if already resolved (has a known class: prefix with a real node)
		if isResolved(e.ToID) {
			if _, ok := sym.Nodes[e.ToID]; ok {
				stats.Resolved++
				continue
			}
		}

		// Try to infer the receiver type from the caller's qualified name.
		// If FromID = "function:app⋅IndexService⋅Index",
		// the parent struct is "app.IndexService" → "class:app⋅IndexService"
		fromMeta, ok := sym.Nodes[e.FromID]
		if !ok {
			stats.Unresolved++
			continue
		}

		// Extract parent class from qualified name:
		// "app.IndexService.Index" → parent = "app.IndexService"
		parts := strings.Split(fromMeta.Qualified, ".")
		if len(parts) >= 3 {
			// method: pkg.Type.Method → parent = pkg.Type
			parentQualified := strings.Join(parts[:len(parts)-1], ".")
			if id, exists := sym.QualifiedToID[parentQualified]; exists {
				e.ToID = id
				stats.Resolved++
				continue
			}
		}

		// Fallback: try to match the operand text against known class names
		// ToID format is "class:OPERAND" — extract the operand
		if strings.HasPrefix(e.ToID, "class:") {
			operand := strings.TrimPrefix(e.ToID, "class:")
			operand = strings.ReplaceAll(operand, "⋅", ".")

			// Try packageName.OperandAsType
			if pkg := domain.PackageFromQualified(fromMeta.Qualified); pkg != "" {
				// Capitalize first letter
				candidate := pkg + "." + strings.ToUpper(operand[:1]) + operand[1:]
				if id, ok := sym.QualifiedToID[candidate]; ok {
					e.ToID = id
					stats.Resolved++
					continue
				}
			}
		}

		stats.Unresolved++
	}

	return edges, stats
}
