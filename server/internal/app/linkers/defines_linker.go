package linkers

import (
	"strings"

	"github.com/commit0-dev/commit0/pkg/types"
	"github.com/commit0-dev/commit0/server/internal/domain"
)

// DefinesLinker generates EdgeDefines edges: file→function, file→class, class→method.
// Moved from per-file Resolver to global linker so it sees all nodes.
type DefinesLinker struct{}

func (l *DefinesLinker) Name() string             { return "defines" }
func (l *DefinesLinker) Labels() []types.EdgeKind { return []types.EdgeKind{types.EdgeDefines} }

func (l *DefinesLinker) Link(edges []types.CodeEdge, sym *domain.SymbolTable) ([]types.CodeEdge, domain.LinkStats) {
	stats := domain.LinkStats{LinkerName: l.Name()}

	// Collect class nodes for class→method defines
	type classEntry struct {
		id        string
		qualified string
	}
	var classNodes []classEntry

	for _, meta := range sym.Nodes {
		if meta.Kind == types.NodeClass {
			classNodes = append(classNodes, classEntry{id: meta.ID, qualified: meta.Qualified})
		}
	}

	// Generate defines edges
	var definesEdges []types.CodeEdge

	for _, meta := range sym.Nodes {
		if meta.Kind == types.NodeFile || meta.Kind == types.NodeModule {
			continue
		}

		// Find owning file
		fileID := sym.FilePathToID[meta.FilePath]

		// Find most specific owning class (longest matching prefix)
		ownerClassID := ""
		ownerClassLen := 0
		for _, cls := range classNodes {
			if cls.id == meta.ID {
				continue
			}
			prefix := cls.qualified + "."
			if strings.HasPrefix(meta.Qualified, prefix) && len(prefix) > ownerClassLen {
				ownerClassID = cls.id
				ownerClassLen = len(prefix)
			}
		}

		if ownerClassID != "" {
			definesEdges = append(definesEdges, types.CodeEdge{
				Kind:   types.EdgeDefines,
				FromID: ownerClassID,
				ToID:   meta.ID,
			})
			stats.Resolved++
		} else if fileID != "" {
			definesEdges = append(definesEdges, types.CodeEdge{
				Kind:   types.EdgeDefines,
				FromID: fileID,
				ToID:   meta.ID,
			})
			stats.Resolved++
		}
		stats.Processed++
	}

	return append(edges, definesEdges...), stats
}
