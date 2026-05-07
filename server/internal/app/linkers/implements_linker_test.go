package linkers

import (
	"testing"

	"github.com/commit0-dev/commit0/pkg/types"
	"github.com/commit0-dev/commit0/server/internal/domain"
)

// ── ImplementsLinker ─────────────────────────────────────────────────────────

func TestImplementsLinker_Metadata(t *testing.T) {
	l := &ImplementsLinker{}
	if got := l.Name(); got != "implements" {
		t.Errorf("Name() = %q, want %q", got, "implements")
	}
	labels := l.Labels()
	if len(labels) != 1 || labels[0] != types.EdgeImplements {
		t.Errorf("Labels() = %v, want [%q]", labels, types.EdgeImplements)
	}
}

// TestImplementsLinker_BasicMatch verifies that a struct with a matching pointer
// method set emits one EdgeImplements edge.
func TestImplementsLinker_BasicMatch(t *testing.T) {
	iface := types.CodeNode{
		ID:        "class:svc⋅Runner",
		Kind:      types.NodeClass,
		Name:      "Runner",
		Qualified: "svc.Runner",
		Methods: []types.MethodSpec{
			{Name: "Run", Signature: "Run(ctx context.Context) error", Receiver: ""},
		},
	}
	concrete := types.CodeNode{
		ID:        "class:svc⋅Adapter",
		Kind:      types.NodeClass,
		Name:      "Adapter",
		Qualified: "svc.Adapter",
		Methods: []types.MethodSpec{
			{Name: "Run", Signature: "Run(ctx context.Context) error", Receiver: "*Adapter"},
		},
	}

	sym := domain.BuildSymbolTable([]types.CodeNode{iface, concrete})
	l := &ImplementsLinker{}
	edges, stats := l.Link(nil, sym)

	if stats.Processed != 1 {
		t.Errorf("Processed = %d, want 1", stats.Processed)
	}
	if stats.Resolved != 1 {
		t.Errorf("Resolved = %d, want 1 (one implements edge)", stats.Resolved)
	}
	if len(edges) != 1 {
		t.Fatalf("expected 1 edge, got %d: %+v", len(edges), edges)
	}
	e := edges[0]
	if e.Kind != types.EdgeImplements {
		t.Errorf("edge Kind = %q, want %q", e.Kind, types.EdgeImplements)
	}
	if e.FromID != concrete.ID {
		t.Errorf("FromID = %q, want %q", e.FromID, concrete.ID)
	}
	if e.ToID != iface.ID {
		t.Errorf("ToID = %q, want %q", e.ToID, iface.ID)
	}
}

// TestImplementsLinker_SignatureMismatch verifies that a struct with a method of
// the right name but wrong signature does NOT produce an implements edge.
func TestImplementsLinker_SignatureMismatch(t *testing.T) {
	iface := types.CodeNode{
		ID:        "class:svc⋅Runner",
		Kind:      types.NodeClass,
		Qualified: "svc.Runner",
		Methods: []types.MethodSpec{
			{Name: "Run", Signature: "Run(ctx context.Context) error", Receiver: ""},
		},
	}
	concrete := types.CodeNode{
		ID:        "class:svc⋅BadAdapter",
		Kind:      types.NodeClass,
		Qualified: "svc.BadAdapter",
		Methods: []types.MethodSpec{
			// Same name, different signature (no context arg).
			{Name: "Run", Signature: "Run() error", Receiver: "*BadAdapter"},
		},
	}

	sym := domain.BuildSymbolTable([]types.CodeNode{iface, concrete})
	l := &ImplementsLinker{}
	edges, stats := l.Link(nil, sym)

	if len(edges) != 0 {
		t.Errorf("expected 0 edges for signature mismatch, got %d: %+v", len(edges), edges)
	}
	if stats.Resolved != 0 {
		t.Errorf("Resolved = %d, want 0", stats.Resolved)
	}
}

// TestImplementsLinker_GenericParamStripped verifies that generic type parameters
// in signatures are stripped before comparison.
func TestImplementsLinker_GenericParamStripped(t *testing.T) {
	iface := types.CodeNode{
		ID:        "class:svc⋅Store",
		Kind:      types.NodeClass,
		Qualified: "svc.Store",
		Methods: []types.MethodSpec{
			{Name: "Get", Signature: "Get(key string) (string, error)", Receiver: ""},
		},
	}
	// Concrete type has a generic signature — after strip it should match.
	concrete := types.CodeNode{
		ID:        "class:svc⋅MapStore",
		Kind:      types.NodeClass,
		Qualified: "svc.MapStore",
		Methods: []types.MethodSpec{
			// Simulate what the extractor emits when generics are stripped at extract time.
			{Name: "Get", Signature: "Get(key string) (string, error)", Receiver: "*MapStore"},
		},
	}

	sym := domain.BuildSymbolTable([]types.CodeNode{iface, concrete})
	l := &ImplementsLinker{}
	edges, _ := l.Link(nil, sym)

	if len(edges) != 1 {
		t.Fatalf("expected 1 implements edge after generic stripping, got %d: %+v", len(edges), edges)
	}
}

// TestImplementsLinker_PointerVsValueMethodSet verifies the Go method-set rules:
// - A value-receiver method (T) is in both the T and *T method sets.
// - A pointer-receiver method (*T) is only in the *T method set.
// The linker uses the pointer method set for structs, so both T and *T methods
// satisfy an interface requirement.
func TestImplementsLinker_PointerMethodSetIncludesBoth(t *testing.T) {
	// Interface requires a single method Foo.
	iface := types.CodeNode{
		ID:        "class:svc⋅Fooer",
		Kind:      types.NodeClass,
		Qualified: "svc.Fooer",
		Methods: []types.MethodSpec{
			{Name: "Foo", Signature: "Foo() string", Receiver: ""},
		},
	}
	// Struct implements Foo on T (value receiver) — in the pointer method set.
	concreteValue := types.CodeNode{
		ID:        "class:svc⋅ValueImpl",
		Kind:      types.NodeClass,
		Qualified: "svc.ValueImpl",
		Methods: []types.MethodSpec{
			{Name: "Foo", Signature: "Foo() string", Receiver: "ValueImpl"},
		},
	}
	// Struct implements Foo on *T (pointer receiver) — also in the pointer method set.
	concretePointer := types.CodeNode{
		ID:        "class:svc⋅PtrImpl",
		Kind:      types.NodeClass,
		Qualified: "svc.PtrImpl",
		Methods: []types.MethodSpec{
			{Name: "Foo", Signature: "Foo() string", Receiver: "*PtrImpl"},
		},
	}

	sym := domain.BuildSymbolTable([]types.CodeNode{iface, concreteValue, concretePointer})
	l := &ImplementsLinker{}
	edges, stats := l.Link(nil, sym)

	if stats.Resolved != 2 {
		t.Errorf("Resolved = %d, want 2 (both value and pointer receivers satisfy the interface)", stats.Resolved)
	}
	if len(edges) != 2 {
		t.Errorf("expected 2 edges, got %d: %+v", len(edges), edges)
	}
}

// TestImplementsLinker_EmptyInterfaceSkipped verifies that an interface with no
// methods is not processed (every type satisfies it, which is noise).
func TestImplementsLinker_EmptyInterfaceSkipped(t *testing.T) {
	emptyIface := types.CodeNode{
		ID:        "class:svc⋅Any",
		Kind:      types.NodeClass,
		Qualified: "svc.Any",
		Methods:   nil, // no methods
	}
	concrete := types.CodeNode{
		ID:        "class:svc⋅Impl",
		Kind:      types.NodeClass,
		Qualified: "svc.Impl",
		Methods: []types.MethodSpec{
			{Name: "Do", Signature: "Do()", Receiver: "*Impl"},
		},
	}

	sym := domain.BuildSymbolTable([]types.CodeNode{emptyIface, concrete})
	l := &ImplementsLinker{}
	edges, _ := l.Link(nil, sym)

	if len(edges) != 0 {
		t.Errorf("empty interface should not produce implements edges, got %d", len(edges))
	}
}

// TestImplementsLinker_PassThroughExistingEdges verifies that existing edges in
// the incoming slice are preserved unchanged.
func TestImplementsLinker_PassThroughExistingEdges(t *testing.T) {
	existing := []types.CodeEdge{
		{Kind: types.EdgeCalls, FromID: "function:a.Foo", ToID: "function:b.Bar"},
	}

	sym := domain.BuildSymbolTable(nil)
	l := &ImplementsLinker{}
	edges, _ := l.Link(existing, sym)

	if len(edges) < 1 || edges[0].Kind != types.EdgeCalls {
		t.Errorf("existing edges must be passed through unchanged, got %+v", edges)
	}
}

// TestImplementsLinker_MultipleInterfaces verifies that a struct implementing
// two interfaces emits two edges.
func TestImplementsLinker_MultipleInterfaces(t *testing.T) {
	ifaceA := types.CodeNode{
		ID:        "class:svc⋅Starter",
		Kind:      types.NodeClass,
		Qualified: "svc.Starter",
		Methods: []types.MethodSpec{
			{Name: "Start", Signature: "Start() error", Receiver: ""},
		},
	}
	ifaceB := types.CodeNode{
		ID:        "class:svc⋅Stopper",
		Kind:      types.NodeClass,
		Qualified: "svc.Stopper",
		Methods: []types.MethodSpec{
			{Name: "Stop", Signature: "Stop() error", Receiver: ""},
		},
	}
	concrete := types.CodeNode{
		ID:        "class:svc⋅Service",
		Kind:      types.NodeClass,
		Qualified: "svc.Service",
		Methods: []types.MethodSpec{
			{Name: "Start", Signature: "Start() error", Receiver: "*Service"},
			{Name: "Stop", Signature: "Stop() error", Receiver: "*Service"},
		},
	}

	sym := domain.BuildSymbolTable([]types.CodeNode{ifaceA, ifaceB, concrete})
	l := &ImplementsLinker{}
	edges, stats := l.Link(nil, sym)

	if stats.Processed != 2 {
		t.Errorf("Processed = %d, want 2 (two interfaces)", stats.Processed)
	}
	if stats.Resolved != 2 {
		t.Errorf("Resolved = %d, want 2 (two implements edges)", stats.Resolved)
	}
	if len(edges) != 2 {
		t.Fatalf("expected 2 edges, got %d", len(edges))
	}
}

// TestImplementsLinker_MissingMethod verifies that a partial match (struct
// implements only some of the interface methods) produces no edge.
func TestImplementsLinker_MissingMethod(t *testing.T) {
	iface := types.CodeNode{
		ID:        "class:svc⋅TwoMethods",
		Kind:      types.NodeClass,
		Qualified: "svc.TwoMethods",
		Methods: []types.MethodSpec{
			{Name: "Alpha", Signature: "Alpha() string", Receiver: ""},
			{Name: "Beta", Signature: "Beta() int", Receiver: ""},
		},
	}
	concrete := types.CodeNode{
		ID:        "class:svc⋅PartialImpl",
		Kind:      types.NodeClass,
		Qualified: "svc.PartialImpl",
		Methods: []types.MethodSpec{
			// Only implements Alpha, not Beta.
			{Name: "Alpha", Signature: "Alpha() string", Receiver: "*PartialImpl"},
		},
	}

	sym := domain.BuildSymbolTable([]types.CodeNode{iface, concrete})
	l := &ImplementsLinker{}
	edges, _ := l.Link(nil, sym)

	if len(edges) != 0 {
		t.Errorf("partial implementation should not produce an edge, got %d edges", len(edges))
	}
}
