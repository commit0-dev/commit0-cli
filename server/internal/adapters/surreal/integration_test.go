//go:build integration

package surreal_test

import (
	"context"
	"os"
	"testing"

	"github.com/commit0-dev/commit0/pkg/types"
	"github.com/commit0-dev/commit0/server/internal/adapters/surreal"
	"github.com/commit0-dev/commit0/server/internal/config"
	"github.com/commit0-dev/commit0/server/internal/domain"
)

// surrealCfg reads connection settings from environment variables, defaulting
// to the standard local dev settings produced by `commit0 db start`.
func surrealCfg() *config.SurrealConfig {
	get := func(key, def string) string {
		if v := os.Getenv(key); v != "" {
			return v
		}
		return def
	}
	return &config.SurrealConfig{
		URL:       get("SURREAL_URL", "ws://localhost:8000"),
		User:      get("SURREAL_USER", "root"),
		Pass:      get("SURREAL_PASS", "root"),
		Namespace: get("SURREAL_NAMESPACE", "commit0_test"),
		Database:  get("SURREAL_DATABASE", "integration"),
	}
}

func TestSurrealAdapterConnect(t *testing.T) {
	ctx := context.Background()
	cfg := surrealCfg()

	adapter, err := surreal.NewSurrealAdapter(ctx, cfg, 3072)
	if err != nil {
		t.Skipf("SurrealDB not available at %s: %v", cfg.URL, err)
	}
	defer adapter.Close(ctx)

	if err := adapter.Ping(ctx); err != nil {
		t.Fatalf("Ping failed: %v", err)
	}
}

func TestSurrealAdapterApplySchema(t *testing.T) {
	ctx := context.Background()
	cfg := surrealCfg()

	adapter, err := surreal.NewSurrealAdapter(ctx, cfg, 3072)
	if err != nil {
		t.Skipf("SurrealDB not available: %v", err)
	}
	defer adapter.Close(ctx)

	if err := adapter.ApplySchema(ctx); err != nil {
		t.Fatalf("ApplySchema failed: %v", err)
	}

	version, err := adapter.GetSchemaVersion(ctx)
	if err != nil {
		t.Fatalf("GetSchemaVersion failed: %v", err)
	}
	if version <= 0 {
		t.Errorf("expected schema version > 0, got %d", version)
	}
}

func TestSurrealAdapterRepoCRUD(t *testing.T) {
	ctx := context.Background()
	cfg := surrealCfg()

	adapter, err := surreal.NewSurrealAdapter(ctx, cfg, 3072)
	if err != nil {
		t.Skipf("SurrealDB not available: %v", err)
	}
	defer adapter.Close(ctx)

	if err := adapter.ApplySchema(ctx); err != nil {
		t.Fatalf("ApplySchema: %v", err)
	}

	repo := &types.Repo{
		Slug: "test-repo-crud",
		Path: "/tmp/test-repo",
	}

	// Upsert
	if err := adapter.UpsertRepo(ctx, repo); err != nil {
		t.Fatalf("UpsertRepo: %v", err)
	}

	// Get
	got, err := adapter.GetRepo(ctx, repo.Slug)
	if err != nil {
		t.Fatalf("GetRepo: %v", err)
	}
	if got.Slug != repo.Slug {
		t.Errorf("Slug = %q, want %q", got.Slug, repo.Slug)
	}

	// List
	repos, err := adapter.ListRepos(ctx)
	if err != nil {
		t.Fatalf("ListRepos: %v", err)
	}
	found := false
	for _, r := range repos {
		if r.Slug == repo.Slug {
			found = true
			break
		}
	}
	if !found {
		t.Error("ListRepos: test repo not in results")
	}
}

func TestSurrealAdapterUpsertAndGetNode(t *testing.T) {
	ctx := context.Background()
	cfg := surrealCfg()

	adapter, err := surreal.NewSurrealAdapter(ctx, cfg, 3072)
	if err != nil {
		t.Skipf("SurrealDB not available: %v", err)
	}
	defer adapter.Close(ctx)

	if err := adapter.ApplySchema(ctx); err != nil {
		t.Fatalf("ApplySchema: %v", err)
	}

	node := &types.CodeNode{
		ID:        "function:inttest⋅Handler",
		Kind:      types.NodeFunction,
		Name:      "Handler",
		Qualified: "inttest.Handler",
		FilePath:  "main.go",
		RepoSlug:  "test-repo",
		Language:  "go",
		StartLine: 10,
		EndLine:   20,
		Body:      "func Handler() {}",
		Embedding: make([]float32, 3072),
	}

	if err := adapter.UpsertNode(ctx, node); err != nil {
		t.Fatalf("UpsertNode: %v", err)
	}

	got, err := adapter.GetNode(ctx, node.ID)
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	if got.Qualified != node.Qualified {
		t.Errorf("Qualified = %q, want %q", got.Qualified, node.Qualified)
	}
	if got.Language != node.Language {
		t.Errorf("Language = %q, want %q", got.Language, node.Language)
	}
}

func TestSurrealAdapterUpsertFileBatch(t *testing.T) {
	ctx := context.Background()
	cfg := surrealCfg()

	adapter, err := surreal.NewSurrealAdapter(ctx, cfg, 3072)
	if err != nil {
		t.Skipf("SurrealDB not available: %v", err)
	}
	defer adapter.Close(ctx)

	if err := adapter.ApplySchema(ctx); err != nil {
		t.Fatalf("ApplySchema: %v", err)
	}

	nodes := []types.CodeNode{
		{
			ID:        "function:batch⋅A",
			Kind:      types.NodeFunction,
			Name:      "A",
			Qualified: "batch.A",
			FilePath:  "batch.go",
			RepoSlug:  "test-repo",
			Language:  "go",
			Embedding: make([]float32, 3072),
		},
		{
			ID:        "function:batch⋅B",
			Kind:      types.NodeFunction,
			Name:      "B",
			Qualified: "batch.B",
			FilePath:  "batch.go",
			RepoSlug:  "test-repo",
			Language:  "go",
			Embedding: make([]float32, 3072),
		},
	}
	edges := []types.CodeEdge{
		{
			FromID: "function:batch⋅A",
			ToID:   "function:batch⋅B",
			Kind:   types.EdgeCalls,
		},
	}

	if err := adapter.UpsertFileBatch(ctx, nodes, edges); err != nil {
		t.Fatalf("UpsertFileBatch: %v", err)
	}

	// Verify both nodes are stored
	for _, n := range nodes {
		got, err := adapter.GetNode(ctx, n.ID)
		if err != nil {
			t.Errorf("GetNode(%s): %v", n.ID, err)
			continue
		}
		if got.Qualified != n.Qualified {
			t.Errorf("Qualified = %q, want %q", got.Qualified, n.Qualified)
		}
	}
}

func TestSurrealAdapterTraceForwardReverse(t *testing.T) {
	ctx := context.Background()
	cfg := surrealCfg()

	adapter, err := surreal.NewSurrealAdapter(ctx, cfg, 3072)
	if err != nil {
		t.Skipf("SurrealDB not available: %v", err)
	}
	defer adapter.Close(ctx)

	if err := adapter.ApplySchema(ctx); err != nil {
		t.Fatalf("ApplySchema: %v", err)
	}

	// Set up two nodes with a calls edge
	caller := &types.CodeNode{
		ID: "function:trace⋅Caller", Kind: types.NodeFunction,
		Name: "Caller", Qualified: "trace.Caller",
		FilePath: "trace.go", RepoSlug: "test-repo", Language: "go",
		Embedding: make([]float32, 3072),
	}
	callee := &types.CodeNode{
		ID: "function:trace⋅Callee", Kind: types.NodeFunction,
		Name: "Callee", Qualified: "trace.Callee",
		FilePath: "trace.go", RepoSlug: "test-repo", Language: "go",
		Embedding: make([]float32, 3072),
	}

	if err := adapter.UpsertNode(ctx, caller); err != nil {
		t.Fatalf("UpsertNode caller: %v", err)
	}
	if err := adapter.UpsertNode(ctx, callee); err != nil {
		t.Fatalf("UpsertNode callee: %v", err)
	}

	edge := &types.CodeEdge{
		FromID: caller.ID,
		ToID:   callee.ID,
		Kind:   types.EdgeCalls,
	}
	if err := adapter.UpsertEdge(ctx, edge); err != nil {
		t.Fatalf("UpsertEdge: %v", err)
	}

	// TraceForward from caller should reach callee
	hops, err := adapter.TraceForward(ctx, caller.ID, 1)
	if err != nil {
		t.Fatalf("TraceForward: %v", err)
	}
	found := false
	for _, h := range hops {
		if h.Node.Qualified == callee.Qualified {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("TraceForward: callee %q not in hops %v", callee.Qualified, hops)
	}

	// TraceReverse from callee should reach caller
	hops, err = adapter.TraceReverse(ctx, callee.ID, 1)
	if err != nil {
		t.Fatalf("TraceReverse: %v", err)
	}
	found = false
	for _, h := range hops {
		if h.Node.Qualified == caller.Qualified {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("TraceReverse: caller %q not in hops %v", caller.Qualified, hops)
	}
}

func TestSurrealAdapterVectorSearch(t *testing.T) {
	ctx := context.Background()
	cfg := surrealCfg()

	adapter, err := surreal.NewSurrealAdapter(ctx, cfg, 3072)
	if err != nil {
		t.Skipf("SurrealDB not available: %v", err)
	}
	defer adapter.Close(ctx)

	if err := adapter.ApplySchema(ctx); err != nil {
		t.Fatalf("ApplySchema: %v", err)
	}

	// Store a node with a non-zero embedding
	embedding := make([]float32, 3072)
	embedding[0] = 1.0 // unit-ish vector

	node := &types.CodeNode{
		ID:        "function:vec⋅SearchMe",
		Kind:      types.NodeFunction,
		Name:      "SearchMe",
		Qualified: "vec.SearchMe",
		FilePath:  "vec.go",
		RepoSlug:  "test-repo",
		Language:  "go",
		Embedding: embedding,
	}
	if err := adapter.UpsertNode(ctx, node); err != nil {
		t.Fatalf("UpsertNode: %v", err)
	}

	// Search with the same vector — should return the node
	vi := adapter.AsVectorIndex()
	results, err := vi.Search(ctx, embedding, domain.VectorSearchOpts{TopK: 5})
	if err != nil {
		t.Fatalf("VectorIndex.Search: %v", err)
	}

	found := false
	for _, r := range results {
		if r.Node.Qualified == node.Qualified {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("VectorSearch: %q not in results (got %d results)", node.Qualified, len(results))
	}
}

func TestSurrealAdapterDeleteNode(t *testing.T) {
	ctx := context.Background()
	cfg := surrealCfg()

	adapter, err := surreal.NewSurrealAdapter(ctx, cfg, 3072)
	if err != nil {
		t.Skipf("SurrealDB not available: %v", err)
	}
	defer adapter.Close(ctx)

	if err := adapter.ApplySchema(ctx); err != nil {
		t.Fatalf("ApplySchema: %v", err)
	}

	node := &types.CodeNode{
		ID:        "function:del⋅DeleteMe",
		Kind:      types.NodeFunction,
		Name:      "DeleteMe",
		Qualified: "del.DeleteMe",
		FilePath:  "del.go",
		RepoSlug:  "test-repo",
		Language:  "go",
		Embedding: make([]float32, 3072),
	}
	if err := adapter.UpsertNode(ctx, node); err != nil {
		t.Fatalf("UpsertNode: %v", err)
	}

	if err := adapter.DeleteNode(ctx, node.ID); err != nil {
		t.Fatalf("DeleteNode: %v", err)
	}

	_, err = adapter.GetNode(ctx, node.ID)
	if err == nil {
		t.Error("expected error after DeleteNode, got nil")
	}
}
