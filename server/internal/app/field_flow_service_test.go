package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/commit0-dev/commit0/pkg/types"
	"github.com/commit0-dev/commit0/server/internal/config"
	"github.com/commit0-dev/commit0/server/internal/domain"
)

// ── fakeOpenCodeGraph ─────────────────────────────────────────────────────
// Minimal implementation used by FieldFlowService tests.
// stubGraphStore from stubs_test.go is reused where applicable.

type fakeFieldFlowGraph struct {
	findNode     *types.CodeNode
	findErr      error
	listNodes    []types.CodeNode
	listErr      error
	hops         []types.TraceHop
	traverseErr  error
	vectorResult []types.ScoredNode
	vectorErr    error
}

func (g *fakeFieldFlowGraph) PutNode(_ context.Context, _ *types.CodeNode) error { return nil }
func (g *fakeFieldFlowGraph) GetNode(_ context.Context, id string) (*types.CodeNode, error) {
	if g.findErr != nil {
		return nil, g.findErr
	}
	if g.findNode != nil && g.findNode.ID == id {
		return g.findNode, nil
	}
	return nil, domain.NotFound("node not found")
}
func (g *fakeFieldFlowGraph) FindNode(_ context.Context, _, qualified string) (*types.CodeNode, error) {
	if g.findErr != nil {
		return nil, g.findErr
	}
	if g.findNode != nil && (g.findNode.Qualified == qualified || g.findNode.ID == qualified) {
		return g.findNode, nil
	}
	return nil, domain.NotFound("not found")
}
func (g *fakeFieldFlowGraph) DeleteNode(_ context.Context, _ string) error            { return nil }
func (g *fakeFieldFlowGraph) PutEdge(_ context.Context, _ *types.CodeEdge) error      { return nil }
func (g *fakeFieldFlowGraph) DeleteEdgesFrom(_ context.Context, _ string) error       { return nil }
func (g *fakeFieldFlowGraph) PutBatch(_ context.Context, _ []types.CodeNode, _ []types.CodeEdge) error {
	return nil
}
func (g *fakeFieldFlowGraph) DeleteByRepo(_ context.Context, _ string) error            { return nil }
func (g *fakeFieldFlowGraph) DeleteByFile(_ context.Context, _, _ string) error         { return nil }
func (g *fakeFieldFlowGraph) TraverseGraph(_ context.Context, _ string, _ []string, _ string, _ int) ([]types.TraceHop, error) {
	return g.hops, g.traverseErr
}
func (g *fakeFieldFlowGraph) Neighbors(_ context.Context, _ string) (*domain.Neighborhood, error) {
	return &domain.Neighborhood{}, nil
}
func (g *fakeFieldFlowGraph) VectorSearch(_ context.Context, _ []float32, _ domain.VectorSearchOpts) ([]types.ScoredNode, error) {
	return g.vectorResult, g.vectorErr
}
func (g *fakeFieldFlowGraph) TextSearch(_ context.Context, _ string, _ domain.TextSearchOpts) ([]types.ScoredNode, error) {
	return nil, nil
}
func (g *fakeFieldFlowGraph) ListNodes(_ context.Context, _ string, _ domain.ListOpts) ([]types.CodeNode, error) {
	return g.listNodes, g.listErr
}
func (g *fakeFieldFlowGraph) ListEdges(_ context.Context, _ string, _ []string) ([]types.CodeEdge, error) {
	return nil, nil
}
func (g *fakeFieldFlowGraph) ListFilePaths(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}
func (g *fakeFieldFlowGraph) PutRepo(_ context.Context, _ *types.Repo) error            { return nil }
func (g *fakeFieldFlowGraph) GetRepo(_ context.Context, _ string) (*types.Repo, error)  { return nil, nil }
func (g *fakeFieldFlowGraph) ListRepos(_ context.Context) ([]types.Repo, error)         { return nil, nil }
func (g *fakeFieldFlowGraph) DeleteRepo(_ context.Context, _ string) error              { return nil }
func (g *fakeFieldFlowGraph) FindRepoByRemoteURL(_ context.Context, _ string) (*types.Repo, error) {
	return nil, nil
}
func (g *fakeFieldFlowGraph) UpdateRepoIndexedAt(_ context.Context, _ string, _ time.Time) error {
	return nil
}
func (g *fakeFieldFlowGraph) ApplySchema(_ context.Context) error { return nil }

// Compile-time check: fakeFieldFlowGraph implements OpenCodeGraph.
var _ domain.OpenCodeGraph = (*fakeFieldFlowGraph)(nil)

// ── helper ────────────────────────────────────────────────────────────────

func newFieldFlowService(g domain.OpenCodeGraph) *FieldFlowService {
	return NewFieldFlowService(g, nil, nil, &config.Config{})
}

// ── TraceFieldFlow validation ─────────────────────────────────────────────

func TestTraceFieldFlow_EmptySymbol_ValidationError(t *testing.T) {
	g := &fakeFieldFlowGraph{}
	svc := newFieldFlowService(g)
	_, err := svc.TraceFieldFlow(context.Background(), FieldFlowRequest{Symbol: ""})
	if err == nil {
		t.Fatal("expected validation error for empty symbol")
	}
}

func TestTraceFieldFlow_DefaultDirection(t *testing.T) {
	node := &types.CodeNode{ID: "function:pkg.Foo", Qualified: "pkg.Foo"}
	g := &fakeFieldFlowGraph{findNode: node, hops: nil}
	svc := newFieldFlowService(g)
	result, err := svc.TraceFieldFlow(context.Background(), FieldFlowRequest{
		Symbol:   "pkg.Foo",
		RepoSlug: "r",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Direction != "forward" {
		t.Errorf("default direction should be forward, got %q", result.Direction)
	}
}

func TestTraceFieldFlow_DefaultDepth(t *testing.T) {
	node := &types.CodeNode{ID: "function:pkg.Foo", Qualified: "pkg.Foo"}
	g := &fakeFieldFlowGraph{findNode: node}
	svc := newFieldFlowService(g)
	result, err := svc.TraceFieldFlow(context.Background(), FieldFlowRequest{
		Symbol:   "pkg.Foo",
		RepoSlug: "r",
		Depth:    0, // triggers default
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = result
}

func TestTraceFieldFlow_SymbolNotFound(t *testing.T) {
	g := &fakeFieldFlowGraph{findErr: domain.NotFound("symbol not found")}
	svc := newFieldFlowService(g)
	_, err := svc.TraceFieldFlow(context.Background(), FieldFlowRequest{
		Symbol:   "pkg.Missing",
		RepoSlug: "r",
	})
	if err == nil {
		t.Error("expected not-found error when symbol cannot be resolved")
	}
}

func TestTraceFieldFlow_TraverseError(t *testing.T) {
	node := &types.CodeNode{ID: "function:pkg.Foo", Qualified: "pkg.Foo"}
	g := &fakeFieldFlowGraph{findNode: node, traverseErr: errors.New("traverse fail")}
	svc := newFieldFlowService(g)
	_, err := svc.TraceFieldFlow(context.Background(), FieldFlowRequest{
		Symbol:   "pkg.Foo",
		RepoSlug: "r",
	})
	if err == nil {
		t.Error("expected traverse error")
	}
}

func TestTraceFieldFlow_EmptyHops_EmptyChains(t *testing.T) {
	node := &types.CodeNode{ID: "function:pkg.Foo", Qualified: "pkg.Foo"}
	g := &fakeFieldFlowGraph{findNode: node, hops: nil}
	svc := newFieldFlowService(g)
	result, err := svc.TraceFieldFlow(context.Background(), FieldFlowRequest{
		Symbol:   "pkg.Foo",
		RepoSlug: "r",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Chains) != 0 {
		t.Errorf("empty hops should produce empty chains, got %d", len(result.Chains))
	}
}

func TestTraceFieldFlow_WithHops_BuildsChains(t *testing.T) {
	node := &types.CodeNode{ID: "function:pkg.Foo", Qualified: "pkg.Foo"}
	edge := types.CodeEdge{
		Kind:     types.EdgeDataFlow,
		FromID:   "function:pkg.Foo",
		ToID:     "function:pkg.Bar",
		Metadata: map[string]string{"field_path": "user.Email"},
	}
	hops := []types.TraceHop{
		{
			Node:  types.CodeNode{ID: "function:pkg.Bar", Qualified: "pkg.Bar"},
			Edge:  edge,
			Depth: 1,
		},
	}
	g := &fakeFieldFlowGraph{findNode: node, hops: hops}
	svc := newFieldFlowService(g)
	result, err := svc.TraceFieldFlow(context.Background(), FieldFlowRequest{
		Symbol:   "pkg.Foo",
		RepoSlug: "r",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Chains) == 0 {
		t.Error("expected at least one chain from hops")
	}
}

func TestTraceFieldFlow_ShowMutations_FilteredWhenNoMutations(t *testing.T) {
	node := &types.CodeNode{ID: "function:pkg.Foo", Qualified: "pkg.Foo"}
	edge := types.CodeEdge{
		Metadata: map[string]string{"field_path": "x.Y"},
	}
	hops := []types.TraceHop{
		{Node: types.CodeNode{ID: "n1"}, Edge: edge, Depth: 1},
	}
	g := &fakeFieldFlowGraph{findNode: node, hops: hops}
	svc := newFieldFlowService(g)
	result, err := svc.TraceFieldFlow(context.Background(), FieldFlowRequest{
		Symbol:        "pkg.Foo",
		RepoSlug:      "r",
		ShowMutations: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// All chains without mutations should be filtered.
	if len(result.Chains) != 0 {
		t.Errorf("expected 0 chains after mutation filter, got %d", len(result.Chains))
	}
}

// ── buildFieldFlowChains ───────────────────────────────────────────────────

func TestBuildFieldFlowChains_EmptyHops(t *testing.T) {
	g := &fakeFieldFlowGraph{}
	svc := newFieldFlowService(g)
	chains := svc.buildFieldFlowChains(nil, "")
	if chains != nil {
		t.Error("empty hops should return nil chains")
	}
}

func TestBuildFieldFlowChains_FieldFromMetadata(t *testing.T) {
	g := &fakeFieldFlowGraph{}
	svc := newFieldFlowService(g)
	hops := []types.TraceHop{
		{
			Node:  types.CodeNode{Qualified: "pkg.A"},
			Edge:  types.CodeEdge{Metadata: map[string]string{"field": "foo.Bar"}},
			Depth: 1,
		},
	}
	chains := svc.buildFieldFlowChains(hops, "")
	if len(chains) == 0 {
		t.Error("expected at least one chain")
	}
	if chains[0].FieldPath != "foo.Bar" {
		t.Errorf("FieldPath = %q, want foo.Bar", chains[0].FieldPath)
	}
}

func TestBuildFieldFlowChains_DefaultFieldPath(t *testing.T) {
	g := &fakeFieldFlowGraph{}
	svc := newFieldFlowService(g)
	// No field_path or field in metadata → uses "_default"
	hops := []types.TraceHop{
		{
			Node:  types.CodeNode{Qualified: "pkg.A"},
			Edge:  types.CodeEdge{Metadata: map[string]string{}},
			Depth: 1,
		},
	}
	chains := svc.buildFieldFlowChains(hops, "")
	if len(chains) == 0 {
		t.Error("expected chain even with default field path")
	}
	if chains[0].FieldPath != "_default" {
		t.Errorf("FieldPath = %q, want _default", chains[0].FieldPath)
	}
}

func TestBuildFieldFlowChains_FilterByField(t *testing.T) {
	g := &fakeFieldFlowGraph{}
	svc := newFieldFlowService(g)
	hops := []types.TraceHop{
		{
			Node:  types.CodeNode{Qualified: "pkg.A"},
			Edge:  types.CodeEdge{Metadata: map[string]string{"field_path": "x.Email"}},
			Depth: 1,
		},
		{
			Node:  types.CodeNode{Qualified: "pkg.B"},
			Edge:  types.CodeEdge{Metadata: map[string]string{"field_path": "x.Password"}},
			Depth: 1,
		},
	}
	chains := svc.buildFieldFlowChains(hops, "x.Email")
	if len(chains) != 1 {
		t.Errorf("expected 1 chain after field filter, got %d", len(chains))
	}
	if chains[0].FieldPath != "x.Email" {
		t.Errorf("unexpected chain field: %q", chains[0].FieldPath)
	}
}

func TestBuildFieldFlowChains_MutationMetadata(t *testing.T) {
	g := &fakeFieldFlowGraph{}
	svc := newFieldFlowService(g)
	hops := []types.TraceHop{
		{
			Node: types.CodeNode{Qualified: "pkg.Mutate"},
			Edge: types.CodeEdge{
				Metadata: map[string]string{
					"field_path":    "user.Email",
					"mutation_type": "assign",
					"mutation_expr": "user.Email = req.Email",
					"mutation_line": "42",
					"param_name":    "email",
					"arg_expr":      "req.Email",
				},
			},
			Depth: 1,
		},
	}
	chains := svc.buildFieldFlowChains(hops, "")
	if len(chains) == 0 {
		t.Fatal("expected chain")
	}
	chain := chains[0]
	if len(chain.Mutations) == 0 {
		t.Error("expected mutation recorded")
	}
	if chain.TaintPoint == nil {
		t.Error("expected taint point to be set for first mutation")
	}
	if chain.Mutations[0].MutationType != "assign" {
		t.Errorf("MutationType = %q, want assign", chain.Mutations[0].MutationType)
	}
	if chain.Mutations[0].MutationLine != 42 {
		t.Errorf("MutationLine = %d, want 42", chain.Mutations[0].MutationLine)
	}
}

func TestBuildFieldFlowChains_RecursiveChildren(t *testing.T) {
	g := &fakeFieldFlowGraph{}
	svc := newFieldFlowService(g)

	child := types.TraceHop{
		Node:  types.CodeNode{Qualified: "pkg.Child"},
		Edge:  types.CodeEdge{Metadata: map[string]string{"field_path": "x.Y"}},
		Depth: 2,
	}
	parent := types.TraceHop{
		Node:     types.CodeNode{Qualified: "pkg.Parent"},
		Edge:     types.CodeEdge{Metadata: map[string]string{"field_path": "x.Y"}},
		Depth:    1,
		Children: []types.TraceHop{child},
	}
	chains := svc.buildFieldFlowChains([]types.TraceHop{parent}, "")
	if len(chains) == 0 {
		t.Fatal("expected chain")
	}
	if len(chains[0].Hops) != 2 {
		t.Errorf("expected 2 hops (parent + child), got %d", len(chains[0].Hops))
	}
}
