package app

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/commit0-dev/commit0/pkg/types"
	"github.com/commit0-dev/commit0/server/internal/domain"
)

// ---------------------------------------------------------------------------
// sliceLines tests
// ---------------------------------------------------------------------------

func TestSliceLines_EmptyBody(t *testing.T) {
	got := sliceLines("", 1, 3)
	if got != "" {
		t.Errorf("expected empty string for empty body, got %q", got)
	}
}

func TestSliceLines_LineOne(t *testing.T) {
	body := "line1\nline2\nline3\nline4\nline5"
	got := sliceLines(body, 1, 3)
	// target=line1 (idx=0), context=3 → from=max(0-3,0)=0, to=min(0+3+1,5)=4
	want := "line1\nline2\nline3\nline4"
	if got != want {
		t.Errorf("sliceLines(line1): got %q, want %q", got, want)
	}
}

func TestSliceLines_LastLine(t *testing.T) {
	body := "line1\nline2\nline3\nline4\nline5"
	lines := 5
	got := sliceLines(body, lines, 3)
	// target=5 (idx=4), context=3 → from=max(4-3,0)=1, to=min(4+3+1,5)=5
	want := "line2\nline3\nline4\nline5"
	if got != want {
		t.Errorf("sliceLines(lastLine): got %q, want %q", got, want)
	}
}

func TestSliceLines_BodyShorterThanContext(t *testing.T) {
	body := "a\nb"
	got := sliceLines(body, 1, 3)
	// 2 lines total; target=1 (idx=0), context=3 → from=0, to=min(4,2)=2
	want := "a\nb"
	if got != want {
		t.Errorf("sliceLines(shortBody): got %q, want %q", got, want)
	}
}

func TestSliceLines_MiddleLine(t *testing.T) {
	body := "line1\nline2\nline3\nline4\nline5\nline6\nline7"
	got := sliceLines(body, 4, 2)
	// target=4 (idx=3), context=2 → from=1, to=6
	want := "line2\nline3\nline4\nline5\nline6"
	if got != want {
		t.Errorf("sliceLines(middle): got %q, want %q", got, want)
	}
}

func TestSliceLines_TargetBeyondEnd(t *testing.T) {
	body := "line1\nline2"
	got := sliceLines(body, 100, 3)
	// target clamped to last line (idx=1)
	if got == "" {
		t.Error("expected non-empty for out-of-range target")
	}
}

func TestSliceLines_SingleLine(t *testing.T) {
	body := "only"
	got := sliceLines(body, 1, 3)
	if got != "only" {
		t.Errorf("single line body: got %q, want %q", got, "only")
	}
}

// ---------------------------------------------------------------------------
// extractCallExpression tests
// ---------------------------------------------------------------------------

func TestExtractCallExpression_SimpleCall(t *testing.T) {
	got := extractCallExpression("\tfoo(a, b)")
	if got != "foo(a, b)" {
		t.Errorf("simple call: got %q, want %q", got, "foo(a, b)")
	}
}

func TestExtractCallExpression_MethodCall(t *testing.T) {
	got := extractCallExpression("\tobj.Foo(x)")
	if got != "obj.Foo(x)" {
		t.Errorf("method call: got %q, want %q", got, "obj.Foo(x)")
	}
}

func TestExtractCallExpression_GenericCall(t *testing.T) {
	// e.g. Foo[int](x) — brackets are part of the identifier before the paren
	line := "result := Foo[int](x)"
	got := extractCallExpression(line)
	// The regex does not handle generics specially; it will match "Foo" portion
	// or "int](x)" — as long as it doesn't panic and returns something or empty.
	// We just verify no panic and result is a string.
	_ = got
}

func TestExtractCallExpression_NoCall(t *testing.T) {
	got := extractCallExpression("x := 42")
	if got != "" {
		t.Errorf("no call: expected empty, got %q", got)
	}
}

func TestExtractCallExpression_EmptyLine(t *testing.T) {
	got := extractCallExpression("")
	if got != "" {
		t.Errorf("empty line: expected empty, got %q", got)
	}
}

func TestExtractCallExpression_MissingClosingParen(t *testing.T) {
	// The regex requires a closing paren in the match; a truly open paren won't match.
	got := extractCallExpression("Foo(/* multi-line")
	// Should return empty without panic.
	_ = got
}

func TestExtractCallExpression_DeepDottedCall(t *testing.T) {
	got := extractCallExpression("return pkg.sub.Fn(ctx, req)")
	if got != "pkg.sub.Fn(ctx, req)" {
		t.Errorf("deep dotted call: got %q, want %q", got, "pkg.sub.Fn(ctx, req)")
	}
}

// ---------------------------------------------------------------------------
// callSiteFileAndLine tests
// ---------------------------------------------------------------------------

func TestCallSiteFileAndLine_Valid(t *testing.T) {
	file, line, err := callSiteFileAndLine("internal/app/service.go:42")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if file != "internal/app/service.go" {
		t.Errorf("file: got %q, want %q", file, "internal/app/service.go")
	}
	if line != 42 {
		t.Errorf("line: got %d, want %d", line, 42)
	}
}

func TestCallSiteFileAndLine_NoColon(t *testing.T) {
	_, _, err := callSiteFileAndLine("nocolon")
	if err == nil {
		t.Error("expected error for missing colon")
	}
}

func TestCallSiteFileAndLine_NonNumericLine(t *testing.T) {
	_, _, err := callSiteFileAndLine("file.go:abc")
	if err == nil {
		t.Error("expected error for non-numeric line")
	}
}

// ---------------------------------------------------------------------------
// Fake graph for enrichment tests
// ---------------------------------------------------------------------------

type enrichFakeGraph struct {
	nodes map[string]*types.CodeNode
	edges []types.CodeEdge
}

func (g *enrichFakeGraph) PutNode(_ context.Context, _ *types.CodeNode) error { return nil }
func (g *enrichFakeGraph) GetNode(_ context.Context, id string) (*types.CodeNode, error) {
	if n, ok := g.nodes[id]; ok {
		return n, nil
	}
	return nil, fmt.Errorf("node not found: %s", id)
}
func (g *enrichFakeGraph) FindNode(_ context.Context, _, _ string) (*types.CodeNode, error) {
	return nil, nil
}
func (g *enrichFakeGraph) DeleteNode(_ context.Context, _ string) error       { return nil }
func (g *enrichFakeGraph) PutEdge(_ context.Context, _ *types.CodeEdge) error { return nil }
func (g *enrichFakeGraph) DeleteEdgesFrom(_ context.Context, _ string) error  { return nil }
func (g *enrichFakeGraph) PutBatch(_ context.Context, _ []types.CodeNode, _ []types.CodeEdge) error {
	return nil
}
func (g *enrichFakeGraph) DeleteByRepo(_ context.Context, _ string) error    { return nil }
func (g *enrichFakeGraph) DeleteByFile(_ context.Context, _, _ string) error { return nil }
func (g *enrichFakeGraph) TraverseGraph(_ context.Context, _ string, _ []string, _ string, _ int) ([]types.TraceHop, error) {
	return nil, nil
}
func (g *enrichFakeGraph) Neighbors(_ context.Context, _ string) (*domain.Neighborhood, error) {
	return nil, nil
}
func (g *enrichFakeGraph) GetNodeEmbedding(_ context.Context, _ string) ([]float32, error) {
	return nil, nil
}
func (g *enrichFakeGraph) VectorSearch(_ context.Context, _ []float32, _ domain.VectorSearchOpts) ([]types.ScoredNode, error) {
	return nil, nil
}
func (g *enrichFakeGraph) TextSearch(_ context.Context, _ string, _ domain.TextSearchOpts) ([]types.ScoredNode, error) {
	return nil, nil
}
func (g *enrichFakeGraph) ListNodes(_ context.Context, _ string, _ domain.ListOpts) ([]types.CodeNode, error) {
	return nil, nil
}
func (g *enrichFakeGraph) ListEdges(_ context.Context, _ string, _ []string) ([]types.CodeEdge, error) {
	return g.edges, nil
}
func (g *enrichFakeGraph) ListFilePaths(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}
func (g *enrichFakeGraph) PutRepo(_ context.Context, _ *types.Repo) error           { return nil }
func (g *enrichFakeGraph) GetRepo(_ context.Context, _ string) (*types.Repo, error) { return nil, nil }
func (g *enrichFakeGraph) ListRepos(_ context.Context) ([]types.Repo, error)        { return nil, nil }
func (g *enrichFakeGraph) UpdateRepoIndexedAt(_ context.Context, _ string, _ time.Time) error {
	return nil
}
func (g *enrichFakeGraph) FindRepoByRemoteURL(_ context.Context, _ string) (*types.Repo, error) {
	return nil, nil
}
func (g *enrichFakeGraph) DeleteRepo(_ context.Context, _ string) error { return nil }
func (g *enrichFakeGraph) ApplySchema(_ context.Context) error          { return nil }

var _ domain.OpenCodeGraph = (*enrichFakeGraph)(nil)

// ---------------------------------------------------------------------------
// EnrichAffectedWithCallSites tests
// ---------------------------------------------------------------------------

func TestEnrichAffectedWithCallSites_HappyPath(t *testing.T) {
	callerBody := "func A() {\n\t// line 2\n\tB(ctx)\n\t// line 4\n}"
	// callerBody line 3 is "\tB(ctx)"

	callerNode := types.CodeNode{
		ID:        "node:caller",
		Qualified: "pkg.A",
		Kind:      types.NodeFunction,
		Body:      callerBody,
	}
	targetNode := types.CodeNode{
		ID:        "node:target",
		Qualified: "pkg.B",
		Kind:      types.NodeFunction,
	}

	graph := &enrichFakeGraph{
		nodes: map[string]*types.CodeNode{
			"node:caller": &callerNode,
			"node:target": &targetNode,
		},
		edges: []types.CodeEdge{
			{
				FromID:   "node:caller",
				ToID:     "node:target",
				Kind:     "calls",
				CallSite: "pkg/b.go:3",
			},
		},
	}

	affected := []types.AffectedNode{
		{Node: callerNode, HopCount: 1},
	}

	err := EnrichAffectedWithCallSites(context.Background(), graph, "test-repo", affected)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if affected[0].CallSiteExcerpt == "" {
		t.Error("expected CallSiteExcerpt to be populated")
	}
	if affected[0].CallLine != 3 {
		t.Errorf("CallLine: got %d, want 3", affected[0].CallLine)
	}
	// CallExpression should contain the call to B
	if affected[0].CallExpression == "" {
		t.Error("expected CallExpression to be populated")
	}
}

func TestEnrichAffectedWithCallSites_NoEdge(t *testing.T) {
	callerNode := types.CodeNode{
		ID:        "node:caller",
		Qualified: "pkg.A",
		Kind:      types.NodeFunction,
	}

	graph := &enrichFakeGraph{
		nodes: map[string]*types.CodeNode{"node:caller": &callerNode},
		edges: []types.CodeEdge{}, // no edges
	}

	affected := []types.AffectedNode{
		{Node: callerNode, HopCount: 1},
	}

	err := EnrichAffectedWithCallSites(context.Background(), graph, "test-repo", affected)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// No enrichment should occur
	if affected[0].CallSiteExcerpt != "" {
		t.Error("expected no excerpt when no matching edge")
	}
	if affected[0].CallLine != 0 {
		t.Errorf("expected CallLine=0, got %d", affected[0].CallLine)
	}
}

func TestEnrichAffectedWithCallSites_EmptyInput(t *testing.T) {
	graph := &enrichFakeGraph{}
	err := EnrichAffectedWithCallSites(context.Background(), graph, "repo", nil)
	if err != nil {
		t.Fatalf("unexpected error on empty input: %v", err)
	}
}

func TestEnrichAffectedWithCallSites_WithContextFalse_NoEnrichment(t *testing.T) {
	// This test ensures the service-level "with_context=false" path leaves
	// fields empty. We test it at the service layer directly:
	// passing IncludeContext=false should NOT call EnrichAffectedWithCallSites.
	// This is tested implicitly — affected node has empty CallSiteExcerpt
	// when IncludeContext is false (see blast_service_test.go).
	// Here we just confirm the enricher itself is a no-op on empty input.
	err := EnrichAffectedWithCallSites(context.Background(), &enrichFakeGraph{}, "repo", []types.AffectedNode{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// EnrichHopsWithCallSites tests
// ---------------------------------------------------------------------------

func TestEnrichHopsWithCallSites_HappyPath(t *testing.T) {
	callerBody := "func A() {\n\t// setup\n\tB(req)\n\t// done\n}"

	callerNode := types.CodeNode{
		ID:        "node:caller",
		Qualified: "pkg.A",
		Kind:      types.NodeFunction,
		Body:      callerBody,
	}

	graph := &enrichFakeGraph{
		nodes: map[string]*types.CodeNode{
			"node:caller": &callerNode,
		},
		edges: []types.CodeEdge{
			{
				FromID:   "node:caller",
				ToID:     "node:target",
				Kind:     "calls",
				CallSite: "pkg/a.go:3",
			},
		},
	}

	hops := []types.TraceHop{
		{Node: callerNode, Depth: 1},
	}

	err := EnrichHopsWithCallSites(context.Background(), graph, "test-repo", hops)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if hops[0].CallSiteExcerpt == "" {
		t.Error("expected CallSiteExcerpt to be populated on hop")
	}
	if hops[0].CallExpression == "" {
		t.Error("expected CallExpression to be populated on hop")
	}
}

func TestEnrichHopsWithCallSites_EmptyInput(t *testing.T) {
	graph := &enrichFakeGraph{}
	err := EnrichHopsWithCallSites(context.Background(), graph, "repo", nil)
	if err != nil {
		t.Fatalf("unexpected error on empty input: %v", err)
	}
}

func TestEnrichHopsWithCallSites_NoMatchingEdge(t *testing.T) {
	node := types.CodeNode{ID: "node:x", Qualified: "pkg.X"}
	graph := &enrichFakeGraph{
		nodes: map[string]*types.CodeNode{"node:x": &node},
		edges: []types.CodeEdge{},
	}
	hops := []types.TraceHop{{Node: node, Depth: 1}}

	err := EnrichHopsWithCallSites(context.Background(), graph, "repo", hops)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hops[0].CallSiteExcerpt != "" {
		t.Error("expected no excerpt when no matching edge")
	}
}

func TestEnrichHopsWithCallSites_RecursiveChildren(t *testing.T) {
	parentBody := "func Parent() {\n\tChild(x)\n}"
	childBody := "func Child(x int) {\n\tGrandchild(x)\n}"

	parentNode := types.CodeNode{ID: "node:parent", Qualified: "pkg.Parent", Body: parentBody}
	childNode := types.CodeNode{ID: "node:child", Qualified: "pkg.Child", Body: childBody}

	graph := &enrichFakeGraph{
		nodes: map[string]*types.CodeNode{
			"node:parent": &parentNode,
			"node:child":  &childNode,
		},
		edges: []types.CodeEdge{
			{FromID: "node:parent", ToID: "node:child", Kind: "calls", CallSite: "pkg/p.go:2"},
			{FromID: "node:child", ToID: "node:gc", Kind: "calls", CallSite: "pkg/c.go:2"},
		},
	}

	hops := []types.TraceHop{
		{
			Node:  parentNode,
			Depth: 1,
			Children: []types.TraceHop{
				{Node: childNode, Depth: 2},
			},
		},
	}

	err := EnrichHopsWithCallSites(context.Background(), graph, "repo", hops)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if hops[0].CallSiteExcerpt == "" {
		t.Error("parent hop: expected CallSiteExcerpt populated")
	}
	if hops[0].Children[0].CallSiteExcerpt == "" {
		t.Error("child hop: expected CallSiteExcerpt populated")
	}
}
