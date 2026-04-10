package treesitter

import (
	"testing"

	"github.com/commit0-dev/commit0/pkg/types"
)

// ── TestResolver_EmptyInput ───────────────────────────────────────────────────

func TestResolver_EmptyInput(t *testing.T) {
	r := &Resolver{}
	nodes, edges := r.Resolve(nil, nil)
	// Must not panic; both can be nil or empty
	_ = nodes
	_ = edges
}

// ── TestResolver_ResolvesCallTargets ─────────────────────────────────────────

func TestResolver_ResolvesCallTargets(t *testing.T) {
	nodes := []types.CodeNode{
		{ID: "function:pkg⋅A", Kind: types.NodeFunction, Qualified: "pkg.A", FilePath: "f.go"},
		{ID: "function:pkg⋅B", Kind: types.NodeFunction, Qualified: "pkg.B", FilePath: "f.go"},
	}
	edges := []types.CodeEdge{
		{Kind: types.EdgeCalls, FromID: "function:pkg⋅A", ToID: "pkg.B"},
	}

	r := &Resolver{}
	_, resolvedEdges := r.Resolve(nodes, edges)

	// Find the EdgeCalls edge and verify its ToID was resolved
	for _, e := range resolvedEdges {
		if e.Kind == types.EdgeCalls && e.FromID == "function:pkg⋅A" {
			if e.ToID != "function:pkg⋅B" {
				t.Errorf("EdgeCalls ToID = %q; want %q", e.ToID, "function:pkg⋅B")
			}
			return
		}
	}
	t.Error("EdgeCalls edge not found in result")
}

// ── TestResolver_UnresolvableCallLeft ────────────────────────────────────────

func TestResolver_UnresolvableCallLeft(t *testing.T) {
	nodes := []types.CodeNode{
		{ID: "function:pkg⋅A", Kind: types.NodeFunction, Qualified: "pkg.A", FilePath: "f.go"},
	}
	edges := []types.CodeEdge{
		{Kind: types.EdgeCalls, FromID: "function:pkg⋅A", ToID: "external.DoSomething"},
	}

	r := &Resolver{}
	_, resolvedEdges := r.Resolve(nodes, edges)

	for _, e := range resolvedEdges {
		if e.Kind == types.EdgeCalls && e.FromID == "function:pkg⋅A" {
			// Unresolvable: ToID should be left as the raw name
			if e.ToID != "external.DoSomething" {
				t.Errorf("unresolvable ToID = %q; want %q", e.ToID, "external.DoSomething")
			}
			return
		}
	}
	t.Error("EdgeCalls edge not found in result")
}

// ── TestResolver_GeneratesFileDefines ────────────────────────────────────────

func TestResolver_GeneratesFileDefines(t *testing.T) {
	nodes := []types.CodeNode{
		{ID: "file:main⋅go", Kind: types.NodeFile, FilePath: "main.go", Qualified: "main.go"},
		{ID: "function:main⋅F", Kind: types.NodeFunction, Qualified: "main.F", FilePath: "main.go"},
	}

	r := &Resolver{}
	_, edges := r.Resolve(nodes, nil)

	// There should be an EdgeDefines from the file to the function
	found := false
	for _, e := range edges {
		if e.Kind == types.EdgeDefines && e.FromID == "file:main⋅go" && e.ToID == "function:main⋅F" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("EdgeDefines (file→function) not found; edges: %v", edges)
	}
}

// ── TestResolver_GeneratesClassDefines ───────────────────────────────────────

func TestResolver_GeneratesClassDefines(t *testing.T) {
	nodes := []types.CodeNode{
		{ID: "file:pkg⋅go", Kind: types.NodeFile, FilePath: "pkg.go", Qualified: "pkg.go"},
		{ID: "class:pkg⋅MyClass", Kind: types.NodeClass, Qualified: "pkg.MyClass", FilePath: "pkg.go"},
		{ID: "function:pkg⋅MyClass⋅Method", Kind: types.NodeFunction, Qualified: "pkg.MyClass.Method", FilePath: "pkg.go"},
	}

	r := &Resolver{}
	_, edges := r.Resolve(nodes, nil)

	// file → class
	fileToClass := false
	// class → method
	classToMethod := false
	// file should NOT directly define the method
	fileToMethod := false

	for _, e := range edges {
		if e.Kind != types.EdgeDefines {
			continue
		}
		switch {
		case e.FromID == "file:pkg⋅go" && e.ToID == "class:pkg⋅MyClass":
			fileToClass = true
		case e.FromID == "class:pkg⋅MyClass" && e.ToID == "function:pkg⋅MyClass⋅Method":
			classToMethod = true
		case e.FromID == "file:pkg⋅go" && e.ToID == "function:pkg⋅MyClass⋅Method":
			fileToMethod = true
		}
	}

	if !fileToClass {
		t.Error("expected EdgeDefines from file to class")
	}
	if !classToMethod {
		t.Error("expected EdgeDefines from class to method")
	}
	if fileToMethod {
		t.Error("unexpected EdgeDefines from file directly to method (should be class→method)")
	}
}

// ── TestResolver_NonCallEdgesUntouched ───────────────────────────────────────

func TestResolver_NonCallEdgesUntouched(t *testing.T) {
	nodes := []types.CodeNode{
		{ID: "file:f⋅go", Kind: types.NodeFile, FilePath: "f.go", Qualified: "f.go"},
	}
	edges := []types.CodeEdge{
		{Kind: types.EdgeImports, FromID: "file:f⋅go", ToID: "module:fmt"},
	}

	r := &Resolver{}
	_, resolvedEdges := r.Resolve(nodes, edges)

	for _, e := range resolvedEdges {
		if e.Kind == types.EdgeImports {
			// ToID must be unchanged
			if e.ToID != "module:fmt" {
				t.Errorf("EdgeImports ToID was modified: got %q; want %q", e.ToID, "module:fmt")
			}
			return
		}
	}
	t.Error("EdgeImports edge not found in result")
}

// ── TestResolver_ModuleNodesSkipped ──────────────────────────────────────────

func TestResolver_ModuleNodesSkipped(t *testing.T) {
	nodes := []types.CodeNode{
		{ID: "file:f⋅go", Kind: types.NodeFile, FilePath: "f.go", Qualified: "f.go"},
		{ID: "module:fmt", Kind: types.NodeModule, Qualified: "fmt", FilePath: ""},
	}

	r := &Resolver{}
	_, edges := r.Resolve(nodes, nil)

	// No EdgeDefines should reference the module node as a target from the file
	for _, e := range edges {
		if e.Kind == types.EdgeDefines && e.ToID == "module:fmt" {
			t.Errorf("unexpected EdgeDefines targeting module node: %+v", e)
		}
	}
}
