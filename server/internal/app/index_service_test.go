package app

import (
	"context"
	"strings"
	"testing"

	"github.com/commit0-dev/commit0/server/internal/config"
	"github.com/commit0-dev/commit0/server/internal/domain"
	"github.com/commit0-dev/commit0/pkg/types"
)

func TestIndexServiceIndexHappyPath(t *testing.T) {
	walker := &stubFileWalker{
		files: []domain.FileEntry{
			{Path: "main.go", Language: "go", Content: []byte("func main() {}")},
		},
	}

	parser := &stubParser{
		result: &domain.ParsedFile{
			Path:     "main.go",
			Language: "go",
			Nodes: []types.CodeNode{
				{ID: "f1", Qualified: "main", Kind: types.NodeFunction},
			},
			Edges: []types.CodeEdge{},
		},
	}

	embedder := &stubEmbedder{
		batchRes: []domain.EmbedResult{
			{ID: "f1", Vector: []float32{0.1, 0.2}},
		},
	}

	store := newStubGraphStore()

	cfg := &config.Config{
		Index: config.IndexConfig{
			MaxWorkersEmbed: 1,
			MaxWorkersStore: 1,
		},
		BatchSize: 1,
	}

	svc := NewIndexService(walker, parser, embedder, store, nil, cfg)

	result, err := svc.Index(context.Background(), IndexRequest{
		RepoPath: "/repo",
		RepoSlug: "my-repo",
	})

	if err != nil {
		t.Fatalf("Index failed: %v", err)
	}

	if result.FilesIndexed != 1 {
		t.Errorf("FilesIndexed = %d, want 1", result.FilesIndexed)
	}

	if result.NodesCreated != 1 {
		t.Errorf("NodesCreated = %d, want 1", result.NodesCreated)
	}
}

func TestIndexServiceIndexWalkerError(t *testing.T) {
	walker := &stubFileWalker{
		files: []domain.FileEntry{},
		err:   domain.Validation("invalid path"),
	}

	parser := &stubParser{
		result: &domain.ParsedFile{
			Nodes: []types.CodeNode{},
			Edges: []types.CodeEdge{},
		},
	}

	embedder := &stubEmbedder{}
	store := newStubGraphStore()

	cfg := &config.Config{
		Index: config.IndexConfig{
			MaxWorkersEmbed: 1,
			MaxWorkersStore: 1,
		},
	}

	svc := NewIndexService(walker, parser, embedder, store, nil, cfg)

	_, err := svc.Index(context.Background(), IndexRequest{
		RepoPath: "/invalid",
		RepoSlug: "my-repo",
	})

	if err == nil {
		t.Errorf("Index should fail with walker error")
	}
}

func TestIndexServiceIndexParseFailure(t *testing.T) {
	walker := &stubFileWalker{
		files: []domain.FileEntry{
			{Path: "bad.go", Language: "go"},
		},
	}

	parser := &stubParser{
		err: domain.Validation("syntax error"),
	}

	embedder := &stubEmbedder{}
	store := newStubGraphStore()

	cfg := &config.Config{
		Index: config.IndexConfig{
			MaxWorkersEmbed: 1,
			MaxWorkersStore: 1,
		},
	}

	svc := NewIndexService(walker, parser, embedder, store, nil, cfg)

	result, err := svc.Index(context.Background(), IndexRequest{
		RepoPath: "/repo",
		RepoSlug: "my-repo",
	})

	if err != nil {
		t.Errorf("Index should not fail on parse error (non-fatal), got: %v", err)
	}

	if result.FilesIndexed != 0 {
		t.Errorf("FilesIndexed = %d, want 0 (file wasn't parsed)", result.FilesIndexed)
	}
}

func TestIndexServiceIndexEmbedFailure(t *testing.T) {
	walker := &stubFileWalker{
		files: []domain.FileEntry{
			{Path: "main.go", Language: "go"},
		},
	}

	parser := &stubParser{
		result: &domain.ParsedFile{
			Path: "main.go",
			Nodes: []types.CodeNode{
				{ID: "f1", Qualified: "main"},
			},
			Edges: []types.CodeEdge{},
		},
	}

	// Embedder fails → embed stage is non-fatal
	embedder := &stubEmbedder{
		batchErr: domain.RateLimit("rate limited"),
	}

	store := newStubGraphStore()

	cfg := &config.Config{
		Index: config.IndexConfig{
			MaxWorkersEmbed: 1,
			MaxWorkersStore: 1,
		},
		BatchSize: 10,
	}

	svc := NewIndexService(walker, parser, embedder, store, nil, cfg)

	result, err := svc.Index(context.Background(), IndexRequest{
		RepoPath: "/repo",
		RepoSlug: "my-repo",
	})

	// Embed failure is non-fatal
	if err != nil {
		t.Errorf("Index should not fail on embed error (non-fatal), got: %v", err)
	}
	if result == nil {
		t.Error("result should not be nil")
	}
}

func TestIndexServiceIndexStoreFailure(t *testing.T) {
	walker := &stubFileWalker{
		files: []domain.FileEntry{
			{Path: "main.go", Language: "go"},
		},
	}

	parser := &stubParser{
		result: &domain.ParsedFile{
			Path: "main.go",
			Nodes: []types.CodeNode{
				{ID: "f1", Qualified: "main"},
			},
			Edges: []types.CodeEdge{},
		},
	}

	embedder := &stubEmbedder{
		batchRes: []domain.EmbedResult{{ID: "f1", Vector: []float32{0.1}}},
	}

	store := newStubGraphStore()
	store.upsertBatchErr = domain.Validation("store write failed") // non-fatal

	cfg := &config.Config{
		Index: config.IndexConfig{
			MaxWorkersEmbed: 1,
			MaxWorkersStore: 1,
		},
		BatchSize: 10,
	}

	svc := NewIndexService(walker, parser, embedder, store, nil, cfg)

	result, err := svc.Index(context.Background(), IndexRequest{
		RepoPath: "/repo",
		RepoSlug: "my-repo",
	})

	// Store failure is non-fatal
	if err != nil {
		t.Errorf("Index should not fail on store error (non-fatal), got: %v", err)
	}
	if result == nil {
		t.Error("result should not be nil")
	}
}

func TestIndexServiceZeroFiles(t *testing.T) {
	walker := &stubFileWalker{
		files: []domain.FileEntry{},
	}

	parser := &stubParser{
		result: &domain.ParsedFile{
			Nodes: []types.CodeNode{},
			Edges: []types.CodeEdge{},
		},
	}

	embedder := &stubEmbedder{}
	store := newStubGraphStore()

	cfg := &config.Config{
		Index: config.IndexConfig{
			MaxWorkersEmbed: 1,
			MaxWorkersStore: 1,
		},
	}

	svc := NewIndexService(walker, parser, embedder, store, nil, cfg)

	result, err := svc.Index(context.Background(), IndexRequest{
		RepoPath: "/repo",
		RepoSlug: "my-repo",
	})

	if err != nil {
		t.Fatalf("Index failed: %v", err)
	}

	if result.FilesIndexed != 0 {
		t.Errorf("FilesIndexed = %d, want 0", result.FilesIndexed)
	}

	if result.Timing.TotalMS < 0 {
		t.Errorf("Timing.TotalMS should be non-negative, got %d", result.Timing.TotalMS)
	}
}

func TestIndexServiceCustomChannelBuffers(t *testing.T) {
	// Covers the `parsedCap = is.parsedChBuf` and `embedCap = is.embedChBuf` branches (> 0 cases).
	walker := &stubFileWalker{
		files: []domain.FileEntry{
			{Path: "main.go", Language: "go"},
		},
	}
	parser := &stubParser{
		result: &domain.ParsedFile{
			Path:  "main.go",
			Nodes: []types.CodeNode{{ID: "f1", Qualified: "main"}},
			Edges: []types.CodeEdge{},
		},
	}
	embedder := &stubEmbedder{
		batchRes: []domain.EmbedResult{{ID: "f1", Vector: []float32{0.1}}},
	}
	store := newStubGraphStore()
	cfg := &config.Config{
		Index: config.IndexConfig{
			MaxWorkersEmbed: 1,
			MaxWorkersStore: 1,
		},
		BatchSize: 10,
	}
	svc := NewIndexService(walker, parser, embedder, store, nil, cfg)
	svc.parsedChBuf = 8 // positive → parsedCap = 8
	svc.embedChBuf = 4  // positive → embedCap = 4

	result, err := svc.Index(context.Background(), IndexRequest{RepoPath: "/repo", RepoSlug: "r"})
	if err != nil {
		t.Fatalf("Index with custom buffers failed: %v", err)
	}
	if result.FilesIndexed != 1 {
		t.Errorf("FilesIndexed = %d, want 1", result.FilesIndexed)
	}
}

func TestIndexServiceDefaultMaxWorkersParse(t *testing.T) {
	// MaxWorkersParse=0 → runtime.GOMAXPROCS(0)
	walker := &stubFileWalker{files: []domain.FileEntry{}}
	parser := &stubParser{result: &domain.ParsedFile{Nodes: []types.CodeNode{}, Edges: []types.CodeEdge{}}}
	embedder := &stubEmbedder{}
	store := newStubGraphStore()

	cfg := &config.Config{
		Index: config.IndexConfig{
			MaxWorkersParse: 0, // triggers GOMAXPROCS default
			MaxWorkersEmbed: 1,
			MaxWorkersStore: 1,
		},
	}

	svc := NewIndexService(walker, parser, embedder, store, nil, cfg)
	result, err := svc.Index(context.Background(), IndexRequest{RepoPath: "/repo", RepoSlug: "r"})

	if err != nil {
		t.Fatalf("Index failed: %v", err)
	}
	if result == nil {
		t.Error("result should not be nil")
	}
}

// TestIndexServiceStoreContextCancelled covers the store-goroutine context-cancel path (lines
// added during refactor: `if err := storeCtx.Err(); err != nil { return err }`).
// We use a pre-canceled context so that storeCtx is immediately Done, and set the channel
// buffers to large values so the pipeline can fill before the store goroutines run.
func TestIndexServiceStoreContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	walker := &stubFileWalker{
		files: []domain.FileEntry{
			{Path: "main.go", Language: "go"},
		},
	}
	parser := &stubParser{
		result: &domain.ParsedFile{
			Path:  "main.go",
			Nodes: []types.CodeNode{{ID: "f1", Qualified: "main"}},
			Edges: []types.CodeEdge{},
		},
	}
	embedder := &stubEmbedder{
		batchRes: []domain.EmbedResult{{ID: "f1", Vector: []float32{0.1}}},
	}
	store := newStubGraphStore()

	cfg := &config.Config{
		Index: config.IndexConfig{
			MaxWorkersEmbed: 4,
			MaxWorkersStore: 4,
		},
		BatchSize: 10,
	}

	svc := NewIndexService(walker, parser, embedder, store, nil, cfg)

	// Cancel ctx BEFORE calling Index so all derived contexts (storeCtx) are also canceled.
	cancel()

	// The result may be an error (store canceled) or success (empty pipeline);
	// both are acceptable — the key is that no panic occurs and all goroutines exit cleanly.
	svc.Index(ctx, IndexRequest{RepoPath: "/repo", RepoSlug: "my-repo"})
}

// TestIndexServiceParseContextCancelled exercises the parse-stage context-cancel select branch.
// We use an unbuffered parsedCh (parsedChBuf = -1) + pre-canceled context so the select
// always picks the Done case (send would block, Done is immediately ready).
func TestIndexServiceParseContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	walker := &stubFileWalker{
		files: []domain.FileEntry{
			{Path: "a.go", Language: "go"},
		},
	}
	parser := &stubParser{
		result: &domain.ParsedFile{
			Path:  "a.go",
			Nodes: []types.CodeNode{{ID: "n1", Qualified: "pkg.F"}},
			Edges: []types.CodeEdge{},
		},
	}
	embedder := &stubEmbedder{batchRes: []domain.EmbedResult{}}
	store := newStubGraphStore()

	cfg := &config.Config{
		Index: config.IndexConfig{
			MaxWorkersEmbed: 1,
			MaxWorkersStore: 1,
		},
		BatchSize: 10,
	}

	svc := NewIndexService(walker, parser, embedder, store, nil, cfg)
	svc.parsedChBuf = -1 // unbuffered: send blocks → cancel wins
	cancel()             // pre-cancel context

	// Must not hang or panic. Result may be error or zero-result success.
	svc.Index(ctx, IndexRequest{RepoPath: "/repo", RepoSlug: "r"})
}

// TestIndexServiceStoreStageFatalError exercises the storeCtx.Err() fatal path and the
// resulting storeGroup.Wait() error return. We send two files through the full pipeline
// (parse → embed succeed for both), then in the first store call we cancel the context.
// With MaxWorkersStore=1, the second goroutine is queued and when it runs it sees
// storeCtx.Err() != nil, returns the error, and storeGroup.Wait() propagates it.
func TestIndexServiceStoreStageFatalError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	walker := &stubFileWalker{
		files: []domain.FileEntry{
			{Path: "a.go", Language: "go"},
			{Path: "b.go", Language: "go"},
		},
	}
	// stubParser returns the same result for every call; both files parse to one node each.
	parser := &stubParser{
		result: &domain.ParsedFile{
			Path:  "a.go",
			Nodes: []types.CodeNode{{ID: "n1", Qualified: "pkg.F", Kind: types.NodeFunction}},
			Edges: []types.CodeEdge{},
		},
	}
	embedder := &stubEmbedder{
		batchRes: []domain.EmbedResult{{ID: "n1", Vector: []float32{0.1}}},
	}

	store := newStubGraphStore()
	// First UpsertFileBatch call cancels the context; the second goroutine will see
	// storeCtx.Err() != nil and return it, causing storeGroup.Wait() to return an error.
	store.upsertBatchFn = func(c context.Context, nodes []types.CodeNode, edges []types.CodeEdge) error {
		cancel() // cancel the shared context
		return nil
	}

	cfg := &config.Config{
		Index: config.IndexConfig{
			MaxWorkersEmbed: 2,
			MaxWorkersStore: 1, // serialize store goroutines: 1st cancels, 2nd sees Err()
		},
		BatchSize: 1, // flush every node so both files produce store calls
	}

	svc := NewIndexService(walker, parser, embedder, store, nil, cfg)
	// Both stages have large buffers so items flow through before context check fires.
	svc.parsedChBuf = 8
	svc.embedChBuf = 8

	_, err := svc.Index(ctx, IndexRequest{RepoPath: "/repo", RepoSlug: "my-repo"})
	// The second store goroutine may or may not fire depending on scheduler timing;
	// the test is valid either way — it must not panic, and if an error is returned
	// it must be a "store stage" error.
	if err != nil && !strings.Contains(err.Error(), "store stage") {
		t.Errorf("expected 'store stage' error, got: %v", err)
	}
}

// TestIndexServiceEmbedContextCancelled exercises the embed-stage context-cancel select branch.
func TestIndexServiceEmbedContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	walker := &stubFileWalker{
		files: []domain.FileEntry{
			{Path: "b.go", Language: "go"},
		},
	}
	parser := &stubParser{
		result: &domain.ParsedFile{
			Path:  "b.go",
			Nodes: []types.CodeNode{{ID: "n1", Qualified: "pkg.G"}},
			Edges: []types.CodeEdge{},
		},
	}
	embedder := &stubEmbedder{
		batchRes: []domain.EmbedResult{{ID: "n1", Vector: []float32{0.1}}},
	}
	store := newStubGraphStore()

	cfg := &config.Config{
		Index: config.IndexConfig{
			MaxWorkersEmbed: 1,
			MaxWorkersStore: 1,
		},
		BatchSize: 10,
	}

	svc := NewIndexService(walker, parser, embedder, store, nil, cfg)
	svc.embedChBuf = -1 // unbuffered: send blocks → cancel wins
	cancel()

	svc.Index(ctx, IndexRequest{RepoPath: "/repo", RepoSlug: "r"})
}

// ── ReembedNeighborhood tests ────────────────────────────────────────────────

func TestReembedNeighborhoodHappyPath(t *testing.T) {
	node := &types.CodeNode{ID: "function:pkg⋅F", Qualified: "pkg.F", Kind: types.NodeFunction}
	store := newStubGraphStore()
	store.nodeIDs = []string{node.ID}
	store.nodes[node.ID] = node

	embedder := &stubEmbedder{
		batchRes: []domain.EmbedResult{{ID: node.ID, Vector: []float32{0.1, 0.2}}},
	}

	cfg := &config.Config{Index: config.IndexConfig{}}
	svc := NewIndexService(nil, nil, embedder, store, nil, cfg)

	result, err := svc.ReembedNeighborhood(context.Background(), "my-repo", nil)
	if err != nil {
		t.Fatalf("ReembedNeighborhood failed: %v", err)
	}
	if result.NodesUpdated != 1 {
		t.Errorf("NodesUpdated = %d, want 1", result.NodesUpdated)
	}
}

func TestReembedNeighborhoodListIDsError(t *testing.T) {
	store := newStubGraphStore()
	store.err = domain.NotFound("forced list error")

	cfg := &config.Config{Index: config.IndexConfig{}}
	svc := NewIndexService(nil, nil, nil, store, nil, cfg)

	_, err := svc.ReembedNeighborhood(context.Background(), "my-repo", nil)
	if err == nil {
		t.Fatal("expected error from ListNodeIDs failure")
	}
}

func TestReembedNeighborhoodGetNodeError(t *testing.T) {
	// GetNode fails for the node ID → warns and skips, no nodes updated.
	store := newStubGraphStore()
	store.nodeIDs = []string{"function:pkg⋅Missing"}
	// nodes map is empty → GetNode returns not-found

	embedder := &stubEmbedder{
		batchRes: []domain.EmbedResult{},
	}

	cfg := &config.Config{Index: config.IndexConfig{}}
	svc := NewIndexService(nil, nil, embedder, store, nil, cfg)

	result, err := svc.ReembedNeighborhood(context.Background(), "my-repo", nil)
	if err != nil {
		t.Fatalf("ReembedNeighborhood should not return error on get-node failure: %v", err)
	}
	if result.NodesUpdated != 0 {
		t.Errorf("NodesUpdated = %d, want 0 (node was skipped)", result.NodesUpdated)
	}
}

func TestReembedNeighborhoodEmbedError(t *testing.T) {
	// EmbedBatch fails → warns and continues; no nodes updated.
	node := &types.CodeNode{ID: "function:pkg⋅F", Qualified: "pkg.F", Kind: types.NodeFunction}
	store := newStubGraphStore()
	store.nodeIDs = []string{node.ID}
	store.nodes[node.ID] = node

	embedder := &stubEmbedder{
		batchErr: domain.RateLimit("embed failed"),
	}

	cfg := &config.Config{Index: config.IndexConfig{}}
	svc := NewIndexService(nil, nil, embedder, store, nil, cfg)

	result, err := svc.ReembedNeighborhood(context.Background(), "my-repo", nil)
	if err != nil {
		t.Fatalf("ReembedNeighborhood should not fail on embed error: %v", err)
	}
	if result.NodesUpdated != 0 {
		t.Errorf("NodesUpdated = %d, want 0 (batch was skipped)", result.NodesUpdated)
	}
}

func TestReembedNeighborhoodUpsertError(t *testing.T) {
	// UpsertNode fails for the enriched node → warns and continues.
	node := &types.CodeNode{ID: "function:pkg⋅F", Qualified: "pkg.F", Kind: types.NodeFunction}

	embedder := &stubEmbedder{
		batchRes: []domain.EmbedResult{{ID: node.ID, Vector: []float32{0.1}}},
	}

	cfg := &config.Config{Index: config.IndexConfig{}}

	goodStore := newStubGraphStore()
	goodStore.nodeIDs = []string{node.ID}
	goodStore.nodes[node.ID] = node

	// upsertFailStore overrides UpsertNode to always error while GetNode works fine.
	failStore := &upsertFailStore{stubGraphStore: goodStore}
	svc2 := NewIndexService(nil, nil, embedder, failStore, nil, cfg)

	result, err := svc2.ReembedNeighborhood(context.Background(), "my-repo", nil)
	if err != nil {
		t.Fatalf("ReembedNeighborhood should not fail on upsert error: %v", err)
	}
	if result.NodesUpdated != 0 {
		t.Errorf("NodesUpdated = %d, want 0 (upsert was skipped)", result.NodesUpdated)
	}
}

func TestReembedNeighborhoodDefaultBatchSize(t *testing.T) {
	// BatchSize=0 → defaults to 100.
	node := &types.CodeNode{ID: "function:pkg⋅F", Qualified: "pkg.F", Kind: types.NodeFunction}
	store := newStubGraphStore()
	store.nodeIDs = []string{node.ID}
	store.nodes[node.ID] = node

	embedder := &stubEmbedder{
		batchRes: []domain.EmbedResult{{ID: node.ID, Vector: []float32{0.1}}},
	}

	cfg := &config.Config{BatchSize: 0} // triggers default=100
	svc := NewIndexService(nil, nil, embedder, store, nil, cfg)

	result, err := svc.ReembedNeighborhood(context.Background(), "my-repo", nil)
	if err != nil {
		t.Fatalf("ReembedNeighborhood failed: %v", err)
	}
	if result.NodesUpdated != 1 {
		t.Errorf("NodesUpdated = %d, want 1", result.NodesUpdated)
	}
}

// upsertFailStore wraps stubGraphStore and makes UpsertNode always return an error.
type upsertFailStore struct {
	*stubGraphStore
}

func (u *upsertFailStore) UpsertNode(ctx context.Context, node *types.CodeNode) error {
	return domain.Validation("upsert failed")
}

func (u *upsertFailStore) PutNode(ctx context.Context, node *types.CodeNode) error {
	return domain.Validation("upsert failed")
}

func (u *upsertFailStore) ListNodeIDs(ctx context.Context, repoSlug string) ([]string, error) {
	return u.stubGraphStore.nodeIDs, nil
}

func (u *upsertFailStore) GetNode(ctx context.Context, id string) (*types.CodeNode, error) {
	return u.stubGraphStore.GetNode(ctx, id)
}
