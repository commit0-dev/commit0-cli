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

// ── TestResolver_SuffixMatchResolvesReceiverCalls ───────────────────────────
// Verifies that "s.ImportBundle" resolves to "app.SyncService.ImportBundle"
// via suffix matching — the key fix for method-on-receiver call resolution.
func TestResolver_SuffixMatchResolvesReceiverCalls(t *testing.T) {
	nodes := []types.CodeNode{
		{ID: "function:app⋅SyncService⋅Pull", Kind: types.NodeFunction, Qualified: "app.SyncService.Pull", FilePath: "sync.go"},
		{ID: "function:app⋅SyncService⋅ImportBundle", Kind: types.NodeFunction, Qualified: "app.SyncService.ImportBundle", FilePath: "sync.go"},
		{ID: "function:app⋅SyncService⋅BuildBundle", Kind: types.NodeFunction, Qualified: "app.SyncService.BuildBundle", FilePath: "sync.go"},
		{ID: "file:sync⋅go", Kind: types.NodeFile, FilePath: "sync.go", Qualified: "sync.go"},
	}
	edges := []types.CodeEdge{
		// Tree-sitter gives us "s.ImportBundle" as the raw call target
		{Kind: types.EdgeCalls, FromID: "function:app⋅SyncService⋅Pull", ToID: "s.ImportBundle"},
		// And "s.BuildBundle" from Push
		{Kind: types.EdgeCalls, FromID: "function:app⋅SyncService⋅Pull", ToID: "s.BuildBundle"},
	}

	r := &Resolver{}
	_, resolved := r.Resolve(nodes, edges)

	// Check that suffix matching resolved the calls
	callEdges := 0
	for _, e := range resolved {
		if e.Kind != types.EdgeCalls {
			continue
		}
		callEdges++
		switch e.ToID {
		case "function:app⋅SyncService⋅ImportBundle":
			// OK — resolved via suffix ".ImportBundle"
		case "function:app⋅SyncService⋅BuildBundle":
			// OK — resolved via suffix ".BuildBundle"
		default:
			t.Errorf("unresolved call edge: FromID=%s ToID=%s", e.FromID, e.ToID)
		}
	}
	if callEdges != 2 {
		t.Errorf("expected 2 call edges, got %d", callEdges)
	}
}

// ── TestResolver_SuffixMatchSkipsAmbiguous ──────────────────────────────────
// When multiple functions share the same method name, suffix matching must
// NOT resolve (ambiguous) to avoid wrong edges.
func TestResolver_SuffixMatchSkipsAmbiguous(t *testing.T) {
	nodes := []types.CodeNode{
		{ID: "function:a⋅Foo⋅Close", Kind: types.NodeFunction, Qualified: "a.Foo.Close", FilePath: "a.go"},
		{ID: "function:b⋅Bar⋅Close", Kind: types.NodeFunction, Qualified: "b.Bar.Close", FilePath: "b.go"},
		{ID: "function:main⋅run", Kind: types.NodeFunction, Qualified: "main.run", FilePath: "main.go"},
		{ID: "file:main⋅go", Kind: types.NodeFile, FilePath: "main.go", Qualified: "main.go"},
	}
	edges := []types.CodeEdge{
		{Kind: types.EdgeCalls, FromID: "function:main⋅run", ToID: "x.Close"},
	}

	r := &Resolver{}
	_, resolved := r.Resolve(nodes, edges)

	for _, e := range resolved {
		if e.Kind == types.EdgeCalls && e.FromID == "function:main⋅run" {
			if e.ToID != "x.Close" {
				t.Errorf("ambiguous call should NOT be resolved; got ToID=%s", e.ToID)
			}
			return
		}
	}
	t.Error("call edge not found")
}

// Strategy 4: interface dispatch — ambiguous suffix resolved to single non-test impl.
func TestResolver_InterfaceDispatchResolvesToProductionImpl(t *testing.T) {
	nodes := []types.CodeNode{
		// Production implementation
		{ID: "function:surreal⋅SurrealAdapter⋅GetNode", Kind: types.NodeFunction, Qualified: "surreal.SurrealAdapter.GetNode", FilePath: "adapters/surreal/graph_store.go"},
		// Test stub (should be excluded)
		{ID: "function:app⋅stubGraphStore⋅GetNode", Kind: types.NodeFunction, Qualified: "app.stubGraphStore.GetNode", FilePath: "app/stubs_test.go"},
		// Another test stub
		{ID: "function:http⋅httpTestGraphStore⋅GetNode", Kind: types.NodeFunction, Qualified: "http.httpTestGraphStore.GetNode", FilePath: "adapters/http/handlers_test.go"},
		// Caller
		{ID: "function:app⋅IndexService⋅Index", Kind: types.NodeFunction, Qualified: "app.IndexService.Index", FilePath: "app/index_service.go"},
		{ID: "file:app⋅index", Kind: types.NodeFile, FilePath: "app/index_service.go", Qualified: "app/index_service.go"},
	}
	edges := []types.CodeEdge{
		// is.store.GetNode — ambiguous suffix ".GetNode" matches 3 types
		{Kind: types.EdgeCalls, FromID: "function:app⋅IndexService⋅Index", ToID: "is.store.GetNode"},
	}

	r := &Resolver{}
	_, resolved := r.Resolve(nodes, edges)

	for _, e := range resolved {
		if e.Kind == types.EdgeCalls && e.FromID == "function:app⋅IndexService⋅Index" {
			if e.ToID != "function:surreal⋅SurrealAdapter⋅GetNode" {
				t.Errorf("interface dispatch: got ToID=%s, want surreal.SurrealAdapter.GetNode", e.ToID)
			}
			return
		}
	}
	t.Error("call edge not found")
}

func TestResolver_SamePackagePrefixResolves(t *testing.T) {
	nodes := []types.CodeNode{
		{ID: "function:app⋅IndexService⋅Reembed", Kind: types.NodeFunction, Qualified: "app.IndexService.Reembed", FilePath: "index.go"},
		{ID: "function:app⋅NewCtxBuilder", Kind: types.NodeFunction, Qualified: "app.NewCtxBuilder", FilePath: "context.go"},
		{ID: "file:index⋅go", Kind: types.NodeFile, FilePath: "index.go", Qualified: "index.go"},
	}
	edges := []types.CodeEdge{
		{Kind: types.EdgeCalls, FromID: "function:app⋅IndexService⋅Reembed", ToID: "NewCtxBuilder"},
	}

	r := &Resolver{}
	_, resolved := r.Resolve(nodes, edges)

	for _, e := range resolved {
		if e.Kind == types.EdgeCalls && e.FromID == "function:app⋅IndexService⋅Reembed" {
			if e.ToID != "function:app⋅NewCtxBuilder" {
				t.Errorf("same-package call not resolved: got ToID=%s, want function:app⋅NewCtxBuilder", e.ToID)
			}
			return
		}
	}
	t.Error("call edge not found")
}

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
