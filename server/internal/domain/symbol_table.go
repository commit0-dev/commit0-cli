package domain

import (
	"strings"

	"github.com/commit0-dev/commit0/pkg/types"
)

// NodeMeta holds lightweight metadata about a node for linker heuristics.
type NodeMeta struct {
	ID        string
	Qualified string
	Name      string
	Kind      types.NodeKind
	FilePath  string
	IsTest    bool // true if FilePath contains "_test.go" or "_test.py"
}

// SymbolTable is the complete name→ID mapping built from ALL parsed files.
// Constructed once after the Extract phase. Immutable. Passed to all EdgeLinkers
// for global cross-file symbol resolution.
type SymbolTable struct {
	// Primary lookup: fully qualified name → node ID
	QualifiedToID map[string]string

	// File path → file node ID
	FilePathToID map[string]string

	// Suffix disambiguation: ".MethodName" → list of matching node IDs
	// Used when the caller text is "receiver.Method" and we match on ".Method"
	SuffixToIDs map[string][]string

	// Package → list of node IDs in that package
	// "app" → ["function:app⋅IndexService⋅Index", "function:app⋅NewIndexService", ...]
	PackageNodes map[string][]string

	// Fast metadata lookup by node ID
	Nodes map[string]*NodeMeta
}

// BuildSymbolTable constructs a SymbolTable from all parsed nodes.
// Called once after all files are parsed, before the link phase.
func BuildSymbolTable(nodes []types.CodeNode) *SymbolTable {
	st := &SymbolTable{
		QualifiedToID: make(map[string]string, len(nodes)),
		FilePathToID:  make(map[string]string),
		SuffixToIDs:   make(map[string][]string, len(nodes)),
		PackageNodes:  make(map[string][]string),
		Nodes:         make(map[string]*NodeMeta, len(nodes)),
	}

	for i := range nodes {
		n := &nodes[i]
		if n.Qualified == "" {
			continue
		}

		// Primary lookup
		st.QualifiedToID[n.Qualified] = n.ID

		// File path lookup (for file nodes)
		if n.Kind == types.NodeFile && n.FilePath != "" {
			st.FilePathToID[n.FilePath] = n.ID
		}

		// Suffix map (for method resolution: ".UpsertNode" → [IDs...])
		if n.Kind == types.NodeFunction {
			if dot := strings.LastIndex(n.Qualified, "."); dot >= 0 {
				suffix := n.Qualified[dot:] // ".UpsertNode"
				st.SuffixToIDs[suffix] = append(st.SuffixToIDs[suffix], n.ID)
			}
		}

		// Package map
		if pkg := PackageFromQualified(n.Qualified); pkg != "" {
			st.PackageNodes[pkg] = append(st.PackageNodes[pkg], n.ID)
		}

		// Metadata
		isTest := strings.Contains(n.FilePath, "_test.go") ||
			strings.Contains(n.FilePath, "_test.py") ||
			strings.HasPrefix(n.Name, "Test")
		st.Nodes[n.ID] = &NodeMeta{
			ID:        n.ID,
			Qualified: n.Qualified,
			Name:      n.Name,
			Kind:      n.Kind,
			FilePath:  n.FilePath,
			IsTest:    isTest,
		}
	}

	return st
}

// Resolve attempts to resolve a raw callee string to a concrete node ID.
// Uses 4 strategies in order: exact → same-package → suffix → interface dispatch.
// Returns the resolved ID and true, or the original string and false.
func (st *SymbolTable) Resolve(raw string, fromID string) (string, bool) {
	// Strategy 1: exact match against qualified names
	if id, ok := st.QualifiedToID[raw]; ok {
		return id, true
	}

	// Strategy 2: same-package prefix for bare function calls
	// "NewContextBuilder" → "app.NewContextBuilder" (if caller is in "app" package)
	if !strings.Contains(raw, ".") {
		if fromMeta, ok := st.Nodes[fromID]; ok {
			if pkg := PackageFromQualified(fromMeta.Qualified); pkg != "" {
				candidate := pkg + "." + raw
				if id, ok := st.QualifiedToID[candidate]; ok {
					return id, true
				}
			}
		}
	}

	// Strategy 3: suffix match for receiver-based calls
	// "s.ImportBundle" → last segment ".ImportBundle" → match
	if dot := strings.LastIndex(raw, "."); dot >= 0 {
		suffix := raw[dot:] // ".ImportBundle"
		ids := st.SuffixToIDs[suffix]

		// Unambiguous: exactly one match
		if len(ids) == 1 {
			return ids[0], true
		}

		// Strategy 4: interface dispatch — multiple matches, pick first
		// non-test production implementation (alphabetical for determinism)
		if len(ids) > 1 {
			return st.resolveAmbiguous(suffix, ids)
		}
	}

	return raw, false
}

// resolveAmbiguous picks the best candidate from multiple suffix matches.
// Filters out test files, then picks first alphabetically.
func (st *SymbolTable) resolveAmbiguous(_ string, ids []string) (string, bool) {
	var candidates []string
	for _, id := range ids {
		meta, ok := st.Nodes[id]
		if !ok {
			continue
		}
		if !meta.IsTest {
			candidates = append(candidates, id)
		}
	}

	if len(candidates) == 0 {
		return "", false
	}

	// Single non-test candidate: unambiguous
	if len(candidates) == 1 {
		return candidates[0], true
	}

	// Multiple non-test candidates: pick first alphabetically (deterministic)
	best := candidates[0]
	for _, c := range candidates[1:] {
		if c < best {
			best = c
		}
	}
	return best, true
}

// PackageFromQualified extracts the package prefix from a qualified name.
// "app.IndexService.Index" → "app"
func PackageFromQualified(qual string) string {
	if dot := strings.Index(qual, "."); dot > 0 {
		return qual[:dot]
	}
	return ""
}
