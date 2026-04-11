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

	// Build a suffix map for method resolution: ".MethodName" → node ID.
	// This handles receiver-based calls like s.ImportBundle where the
	// tree-sitter gives us "s.ImportBundle" but we need "app.SyncService.ImportBundle".
	suffixToID := make(map[string]string, len(nodes))
	suffixAmbiguous := make(map[string]bool)
	for _, n := range nodes {
		if n.Kind != types.NodeFunction || n.Qualified == "" {
			continue
		}
		// Extract the last segment: "app.SyncService.ImportBundle" → ".ImportBundle"
		if dot := strings.LastIndex(n.Qualified, "."); dot >= 0 {
			suffix := n.Qualified[dot:] // ".ImportBundle"
			if _, exists := suffixToID[suffix]; exists {
				suffixAmbiguous[suffix] = true // ambiguous — multiple matches
			} else {
				suffixToID[suffix] = n.ID
			}
		}
	}

	// Build a fromID → qualified name map for same-package resolution.
	idToQualified := make(map[string]string, len(nodes))
	for _, n := range nodes {
		if n.ID != "" && n.Qualified != "" {
			idToQualified[n.ID] = n.Qualified
		}
	}

	for i := range edges {
		e := &edges[i]
		if e.Kind != types.EdgeCalls {
			continue
		}
		// ToID was stored as the raw call target (unqualified or qualified name)
		// by the extractors. Try to resolve it.

		// Strategy 1: exact match against qualified names.
		if id, ok := qualifiedToID[e.ToID]; ok {
			e.ToID = id
			continue
		}

		// Strategy 2: same-package prefix — bare function calls like
		// "NewContextBuilderWithStore" → "app.NewContextBuilderWithStore".
		// Extract the caller's package prefix from FromID's qualified name.
		if !strings.Contains(e.ToID, ".") {
			if fromQual, ok := idToQualified[e.FromID]; ok {
				if dot := strings.Index(fromQual, "."); dot >= 0 {
					pkg := fromQual[:dot] // "app" from "app.SyncService.Pull"
					candidate := pkg + "." + e.ToID // "app.NewContextBuilderWithStore"
					if id, ok := qualifiedToID[candidate]; ok {
						e.ToID = id
						continue
					}
				}
			}
		}

		// Strategy 3: suffix match — "s.ImportBundle" → ".ImportBundle" → match.
		if dot := strings.LastIndex(e.ToID, "."); dot >= 0 {
			suffix := e.ToID[dot:]
			if !suffixAmbiguous[suffix] {
				if id, ok := suffixToID[suffix]; ok {
					e.ToID = id
					continue
				}
			}

			// Strategy 4: interface dispatch — for ambiguous suffix matches
			// (e.g., ".GetNode" matches SurrealAdapter, stubGraphStore, etc.),
			// resolve to the single non-test production implementation.
			// This works because commit0 uses hexagonal arch: each interface
			// has exactly one production adapter + test stubs in *_test.go.
			if suffixAmbiguous[suffix] {
				var candidates []string
				for _, n := range nodes {
					if n.Kind != types.NodeFunction || n.Qualified == "" {
						continue
					}
					if strings.HasSuffix(n.Qualified, suffix[1:]) &&
						!strings.Contains(n.FilePath, "_test.go") &&
						!strings.HasPrefix(n.Name, "Test") {
						candidates = append(candidates, n.ID)
					}
				}
				if len(candidates) == 1 {
					e.ToID = candidates[0]
					continue
				}
			}
		}
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
