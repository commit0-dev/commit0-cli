package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/commit0-dev/commit0/pkg/types"
	"github.com/commit0-dev/commit0/server/internal/domain"
)

// stubKnowledgeGraph extends the package-shared stubGraphStore with extra
// hooks for KnowledgeService tests: edge tracking, put-node error
// injection, list error injection. Reuses the parent's full interface
// compliance via embedding.
type stubKnowledgeGraph struct {
	*stubGraphStore
	edges      []types.CodeEdge
	putNodeErr error
	listErr    error
}

func newStubKnowledgeGraph() *stubKnowledgeGraph {
	return &stubKnowledgeGraph{stubGraphStore: newStubGraphStore()}
}

// PutNode overrides the embedded version to honor putNodeErr and to
// route writes through the shared `nodes` map.
func (s *stubKnowledgeGraph) PutNode(ctx context.Context, node *types.CodeNode) error {
	if s.putNodeErr != nil {
		return s.putNodeErr
	}
	return s.stubGraphStore.PutNode(ctx, node)
}

// PutEdge captures edge writes for assertion.
func (s *stubKnowledgeGraph) PutEdge(_ context.Context, e *types.CodeEdge) error {
	s.edges = append(s.edges, *e)
	return nil
}

// ListNodes overrides to iterate the in-memory nodes map (the embedded
// stub returns nil unless IDsOnly is set, which is the wrong shape for
// knowledge-service tests).
func (s *stubKnowledgeGraph) ListNodes(_ context.Context, repoSlug string, _ domain.ListOpts) ([]types.CodeNode, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	out := make([]types.CodeNode, 0, len(s.nodes))
	for _, n := range s.nodes {
		if repoSlug != "" && n.RepoSlug != "" && n.RepoSlug != repoSlug {
			continue
		}
		out = append(out, *n)
	}
	return out, nil
}

// FindNode overrides to look in the `nodes` map by ID (mirrors GetNode).
// The embedded GetNodeByQualified path uses nodesByQ which is keyed
// differently; our knowledge tests only need ID-based lookup.
func (s *stubKnowledgeGraph) FindNode(_ context.Context, _, id string) (*types.CodeNode, error) {
	n, ok := s.nodes[id]
	if !ok {
		return nil, domain.NotFound("not found")
	}
	cp := *n
	return &cp, nil
}

// stubKnowledgeEmbedder produces a fixed vector.
type stubKnowledgeEmbedder struct {
	vec []float32
	err error
}

func (s *stubKnowledgeEmbedder) EmbedBatch(_ context.Context, _ []domain.EmbedInput) ([]domain.EmbedResult, error) {
	return nil, s.err
}
func (s *stubKnowledgeEmbedder) EmbedQuery(_ context.Context, _ string) ([]float32, error) {
	return s.vec, s.err
}

// ─── CreateNode ───────────────────────────────────────────────────────────

func TestKnowledgeServiceCreateNode(t *testing.T) {
	graph := newStubKnowledgeGraph()
	embedder := &stubKnowledgeEmbedder{vec: []float32{0.1, 0.2}}
	svc := NewKnowledgeService(graph, embedder)

	node := &types.KnowledgeNode{
		Kind:     types.NodeDecision,
		RepoSlug: "myrepo",
		Title:    "Use SurrealDB for graph storage",
		Body:     "We chose SurrealDB because…",
		Tags:     []string{"database", "graph"},
		Author:   "alice",
		Status:   types.StatusAccepted,
	}
	if err := svc.CreateNode(context.Background(), node); err != nil {
		t.Fatalf("CreateNode: %v", err)
	}
	if node.ID == "" {
		t.Errorf("ID not generated")
	}
	if node.AccessScope != types.AccessScopePublic {
		t.Errorf("AccessScope default = %s, want public", node.AccessScope)
	}
	if node.CreatedAt.IsZero() || node.UpdatedAt.IsZero() {
		t.Errorf("timestamps not stamped")
	}

	// Verify persisted node + embedding.
	persisted := graph.nodes[node.ID]
	if persisted == nil {
		t.Fatal("not persisted")
	}
	if len(persisted.Embedding) != 2 {
		t.Errorf("embedding not attached: %v", persisted.Embedding)
	}
	if persisted.Kind != types.NodeDecision {
		t.Errorf("kind wrong: %s", persisted.Kind)
	}
	if persisted.Body != node.Body {
		t.Errorf("body lost in round-trip")
	}
	if !strings.Contains(persisted.Summary, "author=alice") {
		t.Errorf("metadata not packed: %s", persisted.Summary)
	}
}

func TestKnowledgeServiceCreateNodeNoEmbedder(t *testing.T) {
	graph := newStubKnowledgeGraph()
	svc := NewKnowledgeService(graph, nil)
	node := &types.KnowledgeNode{Kind: types.NodeRunbook, RepoSlug: "r", Title: "Restart prod"}
	if err := svc.CreateNode(context.Background(), node); err != nil {
		t.Fatalf("CreateNode: %v", err)
	}
	if persisted := graph.nodes[node.ID]; persisted != nil && len(persisted.Embedding) != 0 {
		t.Errorf("no embedder should yield empty embedding")
	}
}

func TestKnowledgeServiceCreateNodeEmbedderFailureNonFatal(t *testing.T) {
	graph := newStubKnowledgeGraph()
	embedder := &stubKnowledgeEmbedder{err: errors.New("provider down")}
	svc := NewKnowledgeService(graph, embedder)
	node := &types.KnowledgeNode{Kind: types.NodeIncident, RepoSlug: "r", Title: "Outage", Body: "down"}
	if err := svc.CreateNode(context.Background(), node); err != nil {
		t.Errorf("embedder failure should be non-fatal, got %v", err)
	}
	if persisted := graph.nodes[node.ID]; persisted == nil {
		t.Errorf("node should persist despite embed failure")
	}
}

func TestKnowledgeServiceCreateNodeValidation(t *testing.T) {
	svc := NewKnowledgeService(newStubKnowledgeGraph(), nil)
	cases := []struct {
		name string
		node *types.KnowledgeNode
	}{
		{"nil", nil},
		{"non-knowledge kind", &types.KnowledgeNode{Kind: types.NodeFunction, RepoSlug: "r", Title: "x"}},
		{"missing title", &types.KnowledgeNode{Kind: types.NodeDecision, RepoSlug: "r"}},
		{"missing repo", &types.KnowledgeNode{Kind: types.NodeDecision, Title: "x"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := svc.CreateNode(context.Background(), tc.node); err == nil {
				t.Errorf("expected validation error")
			}
		})
	}
}

func TestKnowledgeServiceCreateNodeNilGraph(t *testing.T) {
	svc := NewKnowledgeService(nil, nil)
	node := &types.KnowledgeNode{Kind: types.NodeDecision, RepoSlug: "r", Title: "x"}
	if err := svc.CreateNode(context.Background(), node); err == nil {
		t.Errorf("nil graph should error")
	}
}

func TestKnowledgeServiceCreateNodePersistError(t *testing.T) {
	graph := newStubKnowledgeGraph()
	graph.putNodeErr = errors.New("db down")
	svc := NewKnowledgeService(graph, nil)
	node := &types.KnowledgeNode{Kind: types.NodeDecision, RepoSlug: "r", Title: "x"}
	if err := svc.CreateNode(context.Background(), node); err == nil {
		t.Errorf("persist error should propagate")
	}
}

// ─── GetNode ──────────────────────────────────────────────────────────────

func TestKnowledgeServiceGetNode(t *testing.T) {
	graph := newStubKnowledgeGraph()
	graph.nodes["decision:abc"] = &types.CodeNode{
		ID:       "decision:abc",
		Kind:     types.NodeDecision,
		Name:     "Use SurrealDB",
		RepoSlug: "myrepo",
		Body:     "rationale",
		Summary:  "author=alice;status=accepted;",
		Concepts: []string{"db"},
	}
	svc := NewKnowledgeService(graph, nil)

	got, err := svc.GetNode(context.Background(), "decision:abc")
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	if got.Title != "Use SurrealDB" || got.Author != "alice" || got.Status != "accepted" {
		t.Errorf("decoded fields wrong: %+v", got)
	}
}

func TestKnowledgeServiceGetNodeNotKnowledgeKind(t *testing.T) {
	graph := newStubKnowledgeGraph()
	graph.nodes["fn:x"] = &types.CodeNode{ID: "fn:x", Kind: types.NodeFunction, Name: "x"}
	svc := NewKnowledgeService(graph, nil)
	if _, err := svc.GetNode(context.Background(), "fn:x"); err == nil {
		t.Errorf("non-knowledge node should be NotFound")
	}
}

func TestKnowledgeServiceGetNodeMissing(t *testing.T) {
	svc := NewKnowledgeService(newStubKnowledgeGraph(), nil)
	if _, err := svc.GetNode(context.Background(), "missing"); err == nil {
		t.Errorf("missing should error")
	}
}

func TestKnowledgeServiceGetNodeNilGraph(t *testing.T) {
	svc := NewKnowledgeService(nil, nil)
	if _, err := svc.GetNode(context.Background(), "x"); err == nil {
		t.Errorf("nil graph should error")
	}
}

// ─── ListNodes ────────────────────────────────────────────────────────────

func TestKnowledgeServiceListNodes(t *testing.T) {
	graph := newStubKnowledgeGraph()
	graph.nodes["d1"] = &types.CodeNode{ID: "d1", Kind: types.NodeDecision, RepoSlug: "r", Name: "D1"}
	graph.nodes["d2"] = &types.CodeNode{ID: "d2", Kind: types.NodeDecision, RepoSlug: "r", Name: "D2"}
	graph.nodes["i1"] = &types.CodeNode{ID: "i1", Kind: types.NodeIncident, RepoSlug: "r", Name: "I1"}
	graph.nodes["fn"] = &types.CodeNode{ID: "fn", Kind: types.NodeFunction, RepoSlug: "r", Name: "fn"}
	svc := NewKnowledgeService(graph, nil)

	all, err := svc.ListNodes(context.Background(), "r", "")
	if err != nil {
		t.Fatalf("ListNodes: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("ListNodes all = %d, want 3 (no fn)", len(all))
	}

	decisions, err := svc.ListNodes(context.Background(), "r", types.NodeDecision)
	if err != nil {
		t.Fatalf("ListNodes filtered: %v", err)
	}
	if len(decisions) != 2 {
		t.Errorf("decisions = %d, want 2", len(decisions))
	}

	if _, err := svc.ListNodes(context.Background(), "", ""); err == nil {
		t.Errorf("empty repoSlug should error")
	}

	// Nil graph returns nil.
	got, err := NewKnowledgeService(nil, nil).ListNodes(context.Background(), "r", "")
	if err != nil || got != nil {
		t.Errorf("nil graph: got %+v, %v", got, err)
	}

	// Propagate list error.
	graph.listErr = errors.New("oops")
	if _, err := svc.ListNodes(context.Background(), "r", ""); err == nil {
		t.Errorf("list error should propagate")
	}
}

// ─── DeleteNode ───────────────────────────────────────────────────────────

func TestKnowledgeServiceDeleteNode(t *testing.T) {
	graph := newStubKnowledgeGraph()
	graph.nodes["d1"] = &types.CodeNode{ID: "d1", Kind: types.NodeDecision, RepoSlug: "r"}
	svc := NewKnowledgeService(graph, nil)
	if err := svc.DeleteNode(context.Background(), "d1"); err != nil {
		t.Fatalf("DeleteNode: %v", err)
	}

	// Refuse to delete a non-knowledge node.
	graph.nodes["fn"] = &types.CodeNode{ID: "fn", Kind: types.NodeFunction}
	if err := svc.DeleteNode(context.Background(), "fn"); err == nil {
		t.Errorf("should refuse to delete non-knowledge node")
	}

	// Nil graph errors.
	if err := NewKnowledgeService(nil, nil).DeleteNode(context.Background(), "x"); err == nil {
		t.Errorf("nil graph should error")
	}
}

// ─── LinkNodes ────────────────────────────────────────────────────────────

func TestKnowledgeServiceLinkNodes(t *testing.T) {
	graph := newStubKnowledgeGraph()
	svc := NewKnowledgeService(graph, nil)
	if err := svc.LinkNodes(context.Background(), "alice", "fn:x", types.EdgeOwns, nil); err != nil {
		t.Fatalf("LinkNodes: %v", err)
	}
	if len(graph.edges) != 1 {
		t.Fatalf("edge not persisted")
	}
	if graph.edges[0].Kind != types.EdgeOwns {
		t.Errorf("edge kind wrong: %s", graph.edges[0].Kind)
	}
	if graph.edges[0].Metadata["created_at"] == "" {
		t.Errorf("created_at should be auto-stamped")
	}

	// Validation paths.
	cases := []struct {
		name               string
		from, to, edgeKind string
	}{
		{"empty from", "", "to", types.EdgeOwns},
		{"empty to", "from", "", types.EdgeOwns},
		{"non-knowledge edge", "from", "to", types.EdgeCalls},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := svc.LinkNodes(context.Background(), tc.from, tc.to, tc.edgeKind, nil); err == nil {
				t.Errorf("expected validation error")
			}
		})
	}

	// Nil graph errors.
	if err := NewKnowledgeService(nil, nil).LinkNodes(context.Background(), "a", "b", types.EdgeOwns, nil); err == nil {
		t.Errorf("nil graph should error")
	}

	// Custom metadata preserved.
	meta := map[string]string{"since": "2026-01-01"}
	if err := svc.LinkNodes(context.Background(), "alice", "fn:y", types.EdgeOwns, meta); err != nil {
		t.Fatalf("LinkNodes with metadata: %v", err)
	}
	last := graph.edges[len(graph.edges)-1]
	if last.Metadata["since"] != "2026-01-01" {
		t.Errorf("metadata lost: %+v", last.Metadata)
	}
}

// ─── IngestMarkdown ───────────────────────────────────────────────────────

func TestKnowledgeServiceIngestMarkdown(t *testing.T) {
	graph := newStubKnowledgeGraph()
	svc := NewKnowledgeService(graph, nil)
	body := "# Adopt commit0\n\nWe choose commit0 because…"
	got, err := svc.IngestMarkdown(context.Background(), "r", types.NodeDecision, "docs/decisions/0001.md", body)
	if err != nil {
		t.Fatalf("IngestMarkdown: %v", err)
	}
	if got.Title != "Adopt commit0" {
		t.Errorf("title from H1 = %s, want 'Adopt commit0'", got.Title)
	}
	if got.URL != "docs/decisions/0001.md" {
		t.Errorf("URL not preserved: %s", got.URL)
	}
}

func TestKnowledgeServiceIngestMarkdownTitleFallbackToFilename(t *testing.T) {
	svc := NewKnowledgeService(newStubKnowledgeGraph(), nil)
	got, err := svc.IngestMarkdown(context.Background(), "r", types.NodeRunbook, "docs/oncall.md", "no heading here")
	if err != nil {
		t.Fatalf("IngestMarkdown: %v", err)
	}
	if got.Title != "oncall" {
		t.Errorf("title from filename = %s, want oncall", got.Title)
	}
}

func TestKnowledgeServiceIngestMarkdownTitleEmptySource(t *testing.T) {
	svc := NewKnowledgeService(newStubKnowledgeGraph(), nil)
	got, err := svc.IngestMarkdown(context.Background(), "r", types.NodeRunbook, "", "no heading")
	if err != nil {
		t.Fatalf("IngestMarkdown: %v", err)
	}
	if got.Title != "untitled" {
		t.Errorf("empty source title = %s, want 'untitled'", got.Title)
	}
}

func TestKnowledgeServiceIngestMarkdownInvalidKind(t *testing.T) {
	svc := NewKnowledgeService(newStubKnowledgeGraph(), nil)
	if _, err := svc.IngestMarkdown(context.Background(), "r", types.NodeFunction, "x.md", "body"); err == nil {
		t.Errorf("should reject non-knowledge kind")
	}
}

// ─── helpers ──────────────────────────────────────────────────────────────

func TestExtractMarkdownTitle(t *testing.T) {
	if got := extractMarkdownTitle("# Hello", "x"); got != "Hello" {
		t.Errorf("H1 extraction = %s, want Hello", got)
	}
	if got := extractMarkdownTitle("body", "docs/notes.md"); got != "notes" {
		t.Errorf("filename fallback = %s, want notes", got)
	}
	if got := extractMarkdownTitle("", "/abs/path/decision.md"); got != "decision" {
		t.Errorf("absolute path fallback = %s, want decision", got)
	}
	if got := extractMarkdownTitle("", ""); got != "untitled" {
		t.Errorf("both empty = %s, want untitled", got)
	}
}

func TestKnowledgeMetadataPackUnpack(t *testing.T) {
	n := &types.KnowledgeNode{Author: "alice", Status: "accepted", URL: "x"}
	packed := packKnowledgeMetadata(n)
	out := &types.KnowledgeNode{}
	unpackKnowledgeMetadata(packed, out)
	if out.Author != "alice" || out.Status != "accepted" || out.URL != "x" {
		t.Errorf("round-trip lost data: %+v", out)
	}

	// Robust to malformed entries.
	out = &types.KnowledgeNode{}
	unpackKnowledgeMetadata("malformed; ;=novalue;key=value;", out)
	// Should not panic; unknown keys ignored.
}

func TestIsKnowledgeKindAndEdge(t *testing.T) {
	if !types.IsKnowledgeKind(types.NodeDecision) {
		t.Errorf("decision should be knowledge")
	}
	if types.IsKnowledgeKind(types.NodeFunction) {
		t.Errorf("function should not be knowledge")
	}
	if !types.IsKnowledgeEdge(types.EdgeOwns) {
		t.Errorf("owns should be knowledge edge")
	}
	if types.IsKnowledgeEdge(types.EdgeCalls) {
		t.Errorf("calls should not be knowledge edge")
	}
	if got := types.AllKnowledgeKinds(); len(got) != 6 {
		t.Errorf("expected 6 kinds, got %d", len(got))
	}
}
