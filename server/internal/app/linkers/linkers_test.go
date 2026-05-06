package linkers

import (
	"testing"

	"github.com/commit0-dev/commit0/pkg/types"
	"github.com/commit0-dev/commit0/server/internal/domain"
)

// node is a tiny constructor to keep tests readable.
func node(id, qualified, name, kind, file string) types.CodeNode {
	return types.CodeNode{
		ID:        id,
		Qualified: qualified,
		Name:      name,
		Kind:      kind,
		FilePath:  file,
	}
}

func buildTable(nodes ...types.CodeNode) *domain.SymbolTable {
	return domain.BuildSymbolTable(nodes)
}

// ── CallLinker ────────────────────────────────────────────────────────────

func TestCallLinker_Metadata(t *testing.T) {
	l := &CallLinker{}
	if got := l.Name(); got != "call" {
		t.Errorf("Name() = %q, want %q", got, "call")
	}
	labels := l.Labels()
	if len(labels) != 1 || labels[0] != types.EdgeCalls {
		t.Errorf("Labels() = %v, want [calls]", labels)
	}
}

func TestCallLinker_SkipsNonCallEdges(t *testing.T) {
	l := &CallLinker{}
	sym := buildTable()
	in := []types.CodeEdge{
		{Kind: types.EdgeImports, FromID: "function:a.X", ToID: "raw"},
		{Kind: types.EdgeReads, FromID: "function:a.X", ToID: "class:Y"},
	}
	out, stats := l.Link(in, sym)
	if stats.Processed != 0 || stats.Resolved != 0 || stats.Unresolved != 0 {
		t.Errorf("non-call edges should be skipped: %+v", stats)
	}
	if len(out) != 2 || out[0].Kind != types.EdgeImports {
		t.Errorf("non-call edges should be passed through unchanged: %+v", out)
	}
}

func TestCallLinker_AlreadyResolved_StillClassifies(t *testing.T) {
	l := &CallLinker{}
	caller := node("function:app.NewWidget", "app.NewWidget", "NewWidget", types.NodeFunction, "wire.go")
	callee := node("function:app.MakeIt", "app.MakeIt", "MakeIt", types.NodeFunction, "x.go")
	sym := buildTable(caller, callee)

	in := []types.CodeEdge{{Kind: types.EdgeCalls, FromID: caller.ID, ToID: callee.ID}}
	out, stats := l.Link(in, sym)

	// Resolution is already done — must NOT count as Processed/Resolved.
	if stats.Processed != 0 {
		t.Errorf("resolved edges must not count as processed: %+v", stats)
	}
	// But classification still runs.
	if out[0].Kind != types.EdgeConstructs {
		t.Errorf("New* caller should produce constructs edge, got %q", out[0].Kind)
	}
}

func TestCallLinker_ResolvesUnresolved(t *testing.T) {
	l := &CallLinker{}
	caller := node("function:app.Foo", "app.Foo", "Foo", types.NodeFunction, "foo.go")
	target := node("function:app.Bar", "app.Bar", "Bar", types.NodeFunction, "bar.go")
	sym := buildTable(caller, target)

	in := []types.CodeEdge{{Kind: types.EdgeCalls, FromID: caller.ID, ToID: "Bar"}}
	out, stats := l.Link(in, sym)
	if stats.Processed != 1 || stats.Resolved != 1 {
		t.Errorf("expected processed=1 resolved=1, got %+v", stats)
	}
	if out[0].ToID != target.ID {
		t.Errorf("ToID not resolved: %q", out[0].ToID)
	}
	if out[0].Kind != types.EdgeCalls {
		t.Errorf("plain runtime caller should yield calls edge, got %q", out[0].Kind)
	}
}

func TestCallLinker_UnresolvedCounted(t *testing.T) {
	l := &CallLinker{}
	caller := node("function:app.Foo", "app.Foo", "Foo", types.NodeFunction, "foo.go")
	sym := buildTable(caller)

	in := []types.CodeEdge{{Kind: types.EdgeCalls, FromID: caller.ID, ToID: "DoesNotExist"}}
	_, stats := l.Link(in, sym)
	if stats.Processed != 1 || stats.Resolved != 0 || stats.Unresolved != 1 {
		t.Errorf("expected processed=1 unresolved=1, got %+v", stats)
	}
}

func TestCallLinker_ClassifyKinds(t *testing.T) {
	cases := []struct {
		caller types.CodeNode
		want   types.EdgeKind
		name   string
	}{
		{name: "test_file", caller: node("function:p.X", "p.X", "Helper", types.NodeFunction, "foo_test.go"), want: types.EdgeTests},
		{name: "Test_prefix", caller: node("function:p.X", "p.X", "TestThing", types.NodeFunction, "x.go"), want: types.EdgeTests},
		{name: "Benchmark_prefix", caller: node("function:p.X", "p.X", "BenchmarkX", types.NodeFunction, "x.go"), want: types.EdgeTests},
		{name: "New_prefix", caller: node("function:p.X", "p.X", "NewServer", types.NodeFunction, "x.go"), want: types.EdgeConstructs},
		{name: "init", caller: node("function:p.X", "p.X", "init", types.NodeFunction, "x.go"), want: types.EdgeConstructs},
		{name: "main", caller: node("function:p.X", "p.X", "main", types.NodeFunction, "x.go"), want: types.EdgeConstructs},
		{name: "wire_prefix", caller: node("function:p.X", "p.X", "wireDeps", types.NodeFunction, "x.go"), want: types.EdgeConstructs},
		{name: "setup_prefix", caller: node("function:p.X", "p.X", "setupRoutes", types.NodeFunction, "x.go"), want: types.EdgeConstructs},
		{name: "register_prefix", caller: node("function:p.X", "p.X", "registerHandlers", types.NodeFunction, "x.go"), want: types.EdgeConstructs},
		{name: "wire_file", caller: node("function:p.X", "p.X", "doSomething", types.NodeFunction, "wire.go"), want: types.EdgeConstructs},
		{name: "main_file", caller: node("function:p.X", "p.X", "doSomething", types.NodeFunction, "main.go"), want: types.EdgeConstructs},
		{name: "plain_runtime", caller: node("function:p.X", "p.X", "Process", types.NodeFunction, "service.go"), want: types.EdgeCalls},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sym := buildTable(tc.caller)
			got := classifyCallKind(tc.caller.ID, sym)
			if got != tc.want {
				t.Errorf("classifyCallKind(%s) = %q, want %q", tc.caller.Name, got, tc.want)
			}
		})
	}
}

func TestCallLinker_ClassifyUnknownCaller(t *testing.T) {
	sym := buildTable()
	if got := classifyCallKind("function:absent", sym); got != types.EdgeCalls {
		t.Errorf("unknown caller should default to calls, got %q", got)
	}
}

// ── DataFlowLinker ────────────────────────────────────────────────────────

func TestDataFlowLinker_Metadata(t *testing.T) {
	l := &DataFlowLinker{}
	if l.Name() != "data_flow" {
		t.Errorf("Name() = %q", l.Name())
	}
	if labels := l.Labels(); len(labels) != 1 || labels[0] != types.EdgeDataFlow {
		t.Errorf("Labels() = %v", labels)
	}
}

func TestDataFlowLinker_Link(t *testing.T) {
	l := &DataFlowLinker{}
	caller := node("function:app.Foo", "app.Foo", "Foo", types.NodeFunction, "foo.go")
	target := node("function:app.Bar", "app.Bar", "Bar", types.NodeFunction, "bar.go")
	sym := buildTable(caller, target)

	in := []types.CodeEdge{
		{Kind: types.EdgeDataFlow, FromID: caller.ID, ToID: "Bar"},                                      // resolves
		{Kind: types.EdgeDataFlow, FromID: caller.ID, ToID: "function:app.Bar"},                          // already resolved → skipped
		{Kind: types.EdgeDataFlow, FromID: caller.ID, ToID: "Nope"},                                      // unresolved
		{Kind: types.EdgeCalls, FromID: caller.ID, ToID: "Bar"},                                          // wrong kind → skipped
	}
	out, stats := l.Link(in, sym)
	if stats.Processed != 2 || stats.Resolved != 1 || stats.Unresolved != 1 {
		t.Errorf("stats = %+v", stats)
	}
	if out[0].ToID != target.ID {
		t.Errorf("first edge not resolved: %q", out[0].ToID)
	}
}

// ── DefinesLinker ─────────────────────────────────────────────────────────

func TestDefinesLinker_Metadata(t *testing.T) {
	l := &DefinesLinker{}
	if l.Name() != "defines" {
		t.Errorf("Name() = %q", l.Name())
	}
	if labels := l.Labels(); len(labels) != 1 || labels[0] != types.EdgeDefines {
		t.Errorf("Labels() = %v", labels)
	}
}

func TestDefinesLinker_FileAndClassDefines(t *testing.T) {
	l := &DefinesLinker{}
	file := node("file:foo.go", "foo.go", "foo.go", types.NodeFile, "foo.go")
	mod := node("module:p", "p", "p", types.NodeModule, "")
	cls := node("class:p.Widget", "p.Widget", "Widget", types.NodeClass, "foo.go")
	method := node("function:p.Widget.Do", "p.Widget.Do", "Do", types.NodeFunction, "foo.go")
	freeFn := node("function:p.Helper", "p.Helper", "Helper", types.NodeFunction, "foo.go")
	sym := buildTable(file, mod, cls, method, freeFn)

	out, stats := l.Link(nil, sym)

	// Module/file have no incoming defines; class+method+freeFn = 3 processed.
	if stats.Processed != 3 {
		t.Errorf("expected 3 processed, got %d", stats.Processed)
	}
	// All three resolve (class via file, method via class, freeFn via file).
	if stats.Resolved != 3 {
		t.Errorf("expected 3 resolved, got %d", stats.Resolved)
	}

	// Inspect produced edges.
	got := map[string]string{}
	for _, e := range out {
		got[e.ToID] = e.FromID
	}
	if got[method.ID] != cls.ID {
		t.Errorf("method should be defined by class, got %q", got[method.ID])
	}
	if got[cls.ID] != file.ID {
		t.Errorf("class should be defined by file, got %q", got[cls.ID])
	}
	if got[freeFn.ID] != file.ID {
		t.Errorf("free function should be defined by file, got %q", got[freeFn.ID])
	}
}

func TestDefinesLinker_NoOwnerNoEdge(t *testing.T) {
	l := &DefinesLinker{}
	// Function with a FilePath that has no corresponding file node and no class context.
	orphan := node("function:p.Lonely", "p.Lonely", "Lonely", types.NodeFunction, "missing.go")
	sym := buildTable(orphan)

	out, stats := l.Link(nil, sym)
	if stats.Processed != 1 {
		t.Errorf("processed=%d", stats.Processed)
	}
	if stats.Resolved != 0 {
		t.Errorf("orphan should not produce edge, resolved=%d", stats.Resolved)
	}
	if len(out) != 0 {
		t.Errorf("no edges expected, got %v", out)
	}
}

func TestDefinesLinker_PreservesIncoming(t *testing.T) {
	l := &DefinesLinker{}
	file := node("file:f", "f", "f", types.NodeFile, "f.go")
	fn := node("function:p.X", "p.X", "X", types.NodeFunction, "f.go")
	sym := buildTable(file, fn)

	pre := []types.CodeEdge{{Kind: types.EdgeImports, FromID: "a", ToID: "b"}}
	out, _ := l.Link(pre, sym)
	if len(out) < 2 || out[0].Kind != types.EdgeImports {
		t.Errorf("incoming edges should be preserved at the front: %+v", out)
	}
}

// ── FieldAccessLinker ─────────────────────────────────────────────────────

func TestFieldAccessLinker_Metadata(t *testing.T) {
	l := &FieldAccessLinker{}
	if l.Name() != "field_access" {
		t.Errorf("Name() = %q", l.Name())
	}
	got := l.Labels()
	if len(got) != 2 {
		t.Fatalf("Labels len=%d", len(got))
	}
}

func TestFieldAccessLinker_SkipsWrongKind(t *testing.T) {
	l := &FieldAccessLinker{}
	sym := buildTable()
	in := []types.CodeEdge{{Kind: types.EdgeCalls, FromID: "a", ToID: "b"}}
	_, stats := l.Link(in, sym)
	if stats.Processed != 0 {
		t.Errorf("non-reads/writes edges should be skipped, got %+v", stats)
	}
}

func TestFieldAccessLinker_AlreadyResolved(t *testing.T) {
	l := &FieldAccessLinker{}
	cls := node("class:app.Service", "app.Service", "Service", types.NodeClass, "s.go")
	fn := node("function:app.Service.Do", "app.Service.Do", "Do", types.NodeFunction, "s.go")
	sym := buildTable(cls, fn)

	in := []types.CodeEdge{{Kind: types.EdgeReads, FromID: fn.ID, ToID: cls.ID}}
	out, stats := l.Link(in, sym)
	if stats.Processed != 1 || stats.Resolved != 1 {
		t.Errorf("stats=%+v", stats)
	}
	if out[0].ToID != cls.ID {
		t.Errorf("ToID changed when it shouldn't: %q", out[0].ToID)
	}
}

func TestFieldAccessLinker_InfersReceiverFromCaller(t *testing.T) {
	l := &FieldAccessLinker{}
	cls := node("class:app.IndexService", "app.IndexService", "IndexService", types.NodeClass, "i.go")
	method := node("function:app.IndexService.Index", "app.IndexService.Index", "Index", types.NodeFunction, "i.go")
	sym := buildTable(cls, method)

	in := []types.CodeEdge{{Kind: types.EdgeWrites, FromID: method.ID, ToID: "class:is"}}
	out, stats := l.Link(in, sym)
	if stats.Resolved != 1 {
		t.Errorf("expected resolution by parent inference, got %+v", stats)
	}
	if out[0].ToID != cls.ID {
		t.Errorf("ToID = %q, want %q", out[0].ToID, cls.ID)
	}
}

func TestFieldAccessLinker_FallbackToPackageCandidate(t *testing.T) {
	l := &FieldAccessLinker{}
	cls := node("class:app.Widget", "app.Widget", "Widget", types.NodeClass, "w.go")
	caller := node("function:app.Bare", "app.Bare", "Bare", types.NodeFunction, "x.go")
	sym := buildTable(cls, caller)

	in := []types.CodeEdge{{Kind: types.EdgeReads, FromID: caller.ID, ToID: "class:widget"}}
	out, stats := l.Link(in, sym)
	if stats.Resolved != 1 {
		t.Errorf("expected fallback resolution, got %+v", stats)
	}
	if out[0].ToID != cls.ID {
		t.Errorf("ToID = %q, want %q", out[0].ToID, cls.ID)
	}
}

func TestFieldAccessLinker_UnresolvedWhenCallerUnknown(t *testing.T) {
	l := &FieldAccessLinker{}
	sym := buildTable()
	in := []types.CodeEdge{{Kind: types.EdgeReads, FromID: "ghost", ToID: "class:Foo"}}
	_, stats := l.Link(in, sym)
	if stats.Unresolved != 1 {
		t.Errorf("expected unresolved=1, got %+v", stats)
	}
}

func TestFieldAccessLinker_UnresolvedWhenNoMatch(t *testing.T) {
	l := &FieldAccessLinker{}
	caller := node("function:app.Bare", "app.Bare", "Bare", types.NodeFunction, "x.go")
	sym := buildTable(caller)

	// Caller has only 2 segments (pkg.Func), no parent class — and the operand
	// "missing" doesn't match any known type in the package.
	in := []types.CodeEdge{{Kind: types.EdgeReads, FromID: caller.ID, ToID: "class:missing"}}
	_, stats := l.Link(in, sym)
	if stats.Unresolved != 1 {
		t.Errorf("expected unresolved=1, got %+v", stats)
	}
}

// ── RouteLinker ───────────────────────────────────────────────────────────

func TestRouteLinker_Metadata(t *testing.T) {
	l := &RouteLinker{}
	if l.Name() != "route" {
		t.Errorf("Name() = %q", l.Name())
	}
	if labels := l.Labels(); len(labels) != 1 || labels[0] != types.EdgeRoute {
		t.Errorf("Labels() = %v", labels)
	}
}

func TestRouteLinker_Link(t *testing.T) {
	l := &RouteLinker{}
	handler := node("function:app.Handler", "app.Handler", "Handler", types.NodeFunction, "h.go")
	caller := node("function:app.Register", "app.Register", "Register", types.NodeFunction, "r.go")
	sym := buildTable(handler, caller)

	in := []types.CodeEdge{
		{Kind: types.EdgeRoute, FromID: caller.ID, ToID: "Handler"},                  // resolves
		{Kind: types.EdgeRoute, FromID: caller.ID, ToID: handler.ID},                  // already resolved
		{Kind: types.EdgeRoute, FromID: caller.ID, ToID: "function:not-in-symbols"},   // pre-resolved-shape but missing → falls through to Resolve
		{Kind: types.EdgeRoute, FromID: caller.ID, ToID: "Nonsense"},                  // unresolved
		{Kind: types.EdgeImports, FromID: caller.ID, ToID: "x"},                        // wrong kind
	}
	out, stats := l.Link(in, sym)

	if stats.Processed != 4 {
		t.Errorf("expected 4 processed, got %d", stats.Processed)
	}
	if stats.Resolved != 2 {
		t.Errorf("expected 2 resolved, got %d", stats.Resolved)
	}
	if stats.Unresolved != 2 {
		t.Errorf("expected 2 unresolved, got %d", stats.Unresolved)
	}

	if out[0].ToID != handler.ID {
		t.Errorf("first edge not resolved: %q", out[0].ToID)
	}
	if out[1].ToID != handler.ID {
		t.Errorf("second edge mutated: %q", out[1].ToID)
	}
}

// ── isResolved helper ─────────────────────────────────────────────────────

func TestIsResolved(t *testing.T) {
	if !isResolved("function:app.Foo") {
		t.Error("ID with colon should be reported as resolved")
	}
	if isResolved("Foo") {
		t.Error("bare name should not be reported as resolved")
	}
}
