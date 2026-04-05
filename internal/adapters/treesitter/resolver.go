package treesitter

import (
	"strings"

	"github.com/commit0-dev/commit0/pkg/types"
)

// Resolver performs a second AST pass over already-extracted nodes and edges:
//
//  1. Builds a qualified-name → node-ID map from all extracted nodes.
//  2. Attempts to resolve the ToID of every EdgeCalls edge against that map;
//     unresolvable call targets are left unchanged (kept as raw name strings so
//     the graph store can do a best-effort lookup later).
//  3. Generates EdgeDefines edges: each NodeFile defines every NodeFunction /
//     NodeClass whose FilePath matches; each NodeClass defines every NodeFunction
//     whose qualified name is prefixed by the class qualified name.
type Resolver struct{}

// Resolve runs both resolution passes and returns updated node + edge slices.
func (r *Resolver) Resolve(nodes []types.CodeNode, edges []types.CodeEdge) ([]types.CodeNode, []types.CodeEdge) {
	// ── Pass 1: build lookup maps ──────────────────────────────────────────

	// qualifiedToID maps a node's Qualified name to its ID.
	qualifiedToID := make(map[string]string, len(nodes))
	// filePathToID maps a file path to the NodeFile's ID.
	filePathToID := make(map[string]string)
	// classNodes holds all NodeClass nodes for the defines pass.
	type classEntry struct {
		id        string
		qualified string
	}
	var classNodes []classEntry

	for i := range nodes {
		n := &nodes[i]
		if n.Qualified != "" {
			qualifiedToID[n.Qualified] = n.ID
		}
		switch n.Kind {
		case types.NodeFile:
			filePathToID[n.FilePath] = n.ID
		case types.NodeClass:
			classNodes = append(classNodes, classEntry{id: n.ID, qualified: n.Qualified})
		}
	}

	// ── Pass 2: resolve EdgeCalls targets ─────────────────────────────────

	for i := range edges {
		e := &edges[i]
		if e.Kind != types.EdgeCalls {
			continue
		}
		// ToID was stored as the raw call target (unqualified or qualified name)
		// by the extractors. Try to resolve it.
		if id, ok := qualifiedToID[e.ToID]; ok {
			e.ToID = id
		}
		// If not found, leave as-is — the graph store may resolve it later.
	}

	// ── Pass 3: generate EdgeDefines ──────────────────────────────────────

	var definesEdges []types.CodeEdge

	for i := range nodes {
		n := &nodes[i]
		if n.Kind == types.NodeFile || n.Kind == types.NodeModule {
			continue
		}

		// Find the owning file node.
		fileID, fileOK := filePathToID[n.FilePath]

		// Find the most specific owning class (longest matching prefix).
		ownerClassID := ""
		ownerClassLen := 0
		for _, cls := range classNodes {
			if cls.id == n.ID {
				continue
			}
			prefix := cls.qualified + "."
			if strings.HasPrefix(n.Qualified, prefix) && len(prefix) > ownerClassLen {
				ownerClassID = cls.id
				ownerClassLen = len(prefix)
			}
		}

		if ownerClassID != "" {
			// Class defines this function/nested class.
			definesEdges = append(definesEdges, types.CodeEdge{
				Kind:   types.EdgeDefines,
				FromID: ownerClassID,
				ToID:   n.ID,
			})
		} else if fileOK {
			// File defines top-level functions and classes.
			definesEdges = append(definesEdges, types.CodeEdge{
				Kind:   types.EdgeDefines,
				FromID: fileID,
				ToID:   n.ID,
			})
		}
	}

	return nodes, append(edges, definesEdges...)
}
