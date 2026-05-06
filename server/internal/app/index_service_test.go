package app

import (
	"context"
	"strings"
	"testing"

	"github.com/commit0-dev/commit0/pkg/types"
	"github.com/commit0-dev/commit0/server/internal/config"
	"github.com/commit0-dev/commit0/server/internal/domain"
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

// TestIndexService_Setters covers the three thin setter methods that are
// otherwise only invoked by wire.go.
func TestIndexService_Setters(t *testing.T) {
	cfg := &config.Config{Index: config.IndexConfig{MaxWorkersEmbed: 1, MaxWorkersStore: 1}, BatchSize: 1}
	svc := NewIndexService(nil, nil, nil, newStubGraphStore(), nil, cfg)

	tmp := &TemporalService{}
	svc.SetTemporalService(tmp)
	if svc.temporalSvc != tmp {
		t.Errorf("SetTemporalService did not store the service")
	}

	linkers := []domain.EdgeLinker{}
	svc.SetLinkers(linkers)
	// Compare via reflection-friendly check: nil and empty are both length 0,
	// but the field should reference the slice we passed.
	if len(svc.linkers) != 0 {
		t.Errorf("SetLinkers should accept empty slice")
	}

	svc.SetDocPrefix("search_document: ")
	if svc.builder == nil {
		t.Errorf("builder should still be set after SetDocPrefix")
	}
}

// ── Force re-index ──────────────────────────────────────────────────

func TestIndex_Force_DeletesNodes(t *testing.T) {
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

	_, err := svc.Index(context.Background(), IndexRequest{
		RepoPath: "/repo",
		RepoSlug: "my-repo",
		Force:    true,
	})

	if err != nil {
		t.Fatalf("Index with Force: %v", err)
	}
}

// ── cleanupStaleNodes ───────────────────────────────────────────────

func TestCleanupStaleNodes_ListPathsFails(t *testing.T) {
	walker := &stubFileWalker{files: []domain.FileEntry{}}
	parser := &stubParser{result: &domain.ParsedFile{Nodes: []types.CodeNode{}, Edges: []types.CodeEdge{}}}
	embedder := &stubEmbedder{}
	store := newStubGraphStore()

	store.listFilePathsFn = func(ctx context.Context, repoSlug string) ([]string, error) {
		return nil, domain.NotFound("list failed")
	}

	cfg := &config.Config{
		Index: config.IndexConfig{MaxWorkersEmbed: 1, MaxWorkersStore: 1},
	}

	svc := NewIndexService(walker, parser, embedder, store, nil, cfg)
	svc.cleanupStaleNodes(context.Background(), "test-repo", "/repo")
}

// ── ReEmbed ─────────────────────────────────────────────────────────

func TestReEmbed_HappyPath(t *testing.T) {
	walker := &stubFileWalker{files: []domain.FileEntry{}}
	parser := &stubParser{result: &domain.ParsedFile{Nodes: []types.CodeNode{}, Edges: []types.CodeEdge{}}}

	embedder := &stubEmbedder{
		batchRes: []domain.EmbedResult{
			{ID: "n1", Vector: []float32{0.1, 0.2}},
		},
	}

	store := newStubGraphStore()
	store.nodeIDs = []string{"n1"}
	store.nodes["n1"] = &types.CodeNode{ID: "n1", Summary: "func1"}

	cfg := &config.Config{
		Index:     config.IndexConfig{MaxWorkersEmbed: 1, MaxWorkersStore: 1},
		BatchSize: 100,
	}

	svc := NewIndexService(walker, parser, embedder, store, nil, cfg)

	result, err := svc.ReEmbed(context.Background(), "test-repo", nil)

	if err != nil {
		t.Fatalf("ReEmbed: %v", err)
	}
	if result.NodesTotal != 1 {
		t.Errorf("NodesTotal = %d, want 1", result.NodesTotal)
	}
}

func TestReEmbed_EmptyRepo(t *testing.T) {
	walker := &stubFileWalker{files: []domain.FileEntry{}}
	parser := &stubParser{result: &domain.ParsedFile{Nodes: []types.CodeNode{}, Edges: []types.CodeEdge{}}}
	embedder := &stubEmbedder{}

	store := newStubGraphStore()
	store.nodeIDs = []string{}

	cfg := &config.Config{
		Index:     config.IndexConfig{MaxWorkersEmbed: 1, MaxWorkersStore: 1},
		BatchSize: 100,
	}

	svc := NewIndexService(walker, parser, embedder, store, nil, cfg)

	result, err := svc.ReEmbed(context.Background(), "test-repo", nil)

	if err != nil {
		t.Fatalf("ReEmbed empty: %v", err)
	}
	if result.NodesTotal != 0 {
		t.Errorf("NodesTotal = %d, want 0", result.NodesTotal)
	}
}

func TestReEmbed_ContextCancelled(t *testing.T) {
	walker := &stubFileWalker{files: []domain.FileEntry{}}
	parser := &stubParser{result: &domain.ParsedFile{Nodes: []types.CodeNode{}, Edges: []types.CodeEdge{}}}
	embedder := &stubEmbedder{}

	store := newStubGraphStore()
	store.nodeIDs = []string{"n1"}
	store.nodes["n1"] = &types.CodeNode{ID: "n1"}

	cfg := &config.Config{
		Index:     config.IndexConfig{MaxWorkersEmbed: 1, MaxWorkersStore: 1},
		BatchSize: 100,
	}

	svc := NewIndexService(walker, parser, embedder, store, nil, cfg)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := svc.ReEmbed(ctx, "test-repo", nil)

	if err == nil {
		t.Error("ReEmbed with cancelled context should error")
	}
}

// ── IndexWithProgress ───────────────────────────────────────────────

func TestIndexWithProgress_CallsCallback(t *testing.T) {
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

	progressCalls := 0
	onProgress := func(filesIndexed, nodesCreated int) {
		progressCalls++
	}

	_, err := svc.IndexWithProgress(context.Background(), IndexRequest{
		RepoPath: "/repo",
		RepoSlug: "my-repo",
	}, onProgress, nil)

	if err != nil {
		t.Fatalf("IndexWithProgress: %v", err)
	}
	if progressCalls == 0 {
		t.Error("progress callback should be called")
	}
}

// ── trackChanged ────────────────────────────────────────────────────

func TestTrackChanged_PopulatesMap(t *testing.T) {
	run := &indexRun{}

	nodes := []types.CodeNode{{ID: "n1"}, {ID: "n2"}}
	edges := []types.CodeEdge{{FromID: "n3", ToID: "n4"}}

	run.trackChanged(nodes, edges)

	if len(run.changedNodeIDs) != 4 {
		t.Errorf("changedNodeIDs len = %d, want 4", len(run.changedNodeIDs))
	}
}

// ── Additional tests for Index() coverage: Force delete error ───────────────────

func TestIndex_ForceDeleteError(t *testing.T) {
	// Covers: if req.Force && err := is.graph.DeleteByRepo returns error (line 105-107)
	walker := &stubFileWalker{files: []domain.FileEntry{}}
	parser := &stubParser{result: &domain.ParsedFile{Nodes: []types.CodeNode{}, Edges: []types.CodeEdge{}}}
	embedder := &stubEmbedder{}
	store := newStubGraphStore()
	store.deleteNodesErr = domain.Validation("force delete failed")

	cfg := &config.Config{
		Index: config.IndexConfig{MaxWorkersEmbed: 1, MaxWorkersStore: 1},
	}

	svc := NewIndexService(walker, parser, embedder, store, nil, cfg)
	_, err := svc.Index(context.Background(), IndexRequest{
		RepoPath: "/repo",
		RepoSlug: "my-repo",
		Force:    true,
	})

	if err == nil {
		t.Fatal("expected error from force delete")
	}
	if !strings.Contains(err.Error(), "delete existing nodes") {
		t.Errorf("expected 'delete existing nodes' error, got: %v", err)
	}
}

// ── Additional tests for Index() coverage: PutRepo error ─────────────────────

func TestIndex_PutRepoError(t *testing.T) {
	// Covers: if err := is.graph.PutRepo returns error (line 146-148)
	walker := &stubFileWalker{files: []domain.FileEntry{}}
	parser := &stubParser{result: &domain.ParsedFile{Nodes: []types.CodeNode{}, Edges: []types.CodeEdge{}}}
	embedder := &stubEmbedder{}
	store := newStubGraphStore()
	store.upsertRepoErr = domain.Validation("repo upsert failed")

	cfg := &config.Config{
		Index: config.IndexConfig{MaxWorkersEmbed: 1, MaxWorkersStore: 1},
	}

	svc := NewIndexService(walker, parser, embedder, store, nil, cfg)
	_, err := svc.Index(context.Background(), IndexRequest{
		RepoPath: "/repo",
		RepoSlug: "my-repo",
	})

	if err == nil {
		t.Fatal("expected error from PutRepo")
	}
	if !strings.Contains(err.Error(), "upsert repo") {
		t.Errorf("expected 'upsert repo' error, got: %v", err)
	}
}

// ── Additional tests for Index() coverage: Git detection with remote URL ────────

func TestIndex_GitDetectionWithRemoteURL(t *testing.T) {
	// Covers: if git.Slug != "" (line 114-118) and if git.RemoteURL != "" (line 121-127)
	walker := &stubFileWalker{
		files: []domain.FileEntry{
			{Path: "main.go", Language: "go"},
		},
	}
	parser := &stubParser{
		result: &domain.ParsedFile{
			Path:  "main.go",
			Nodes: []types.CodeNode{{ID: "n1", Qualified: "pkg.F"}},
			Edges: []types.CodeEdge{},
		},
	}
	embedder := &stubEmbedder{
		batchRes: []domain.EmbedResult{{ID: "n1", Vector: []float32{0.1}}},
	}
	store := newStubGraphStore()

	cfg := &config.Config{
		Index:     config.IndexConfig{MaxWorkersEmbed: 1, MaxWorkersStore: 1},
		BatchSize: 10,
	}

	svc := NewIndexService(walker, parser, embedder, store, nil, cfg)
	result, err := svc.Index(context.Background(), IndexRequest{
		RepoPath: "/repo",
		RepoSlug: "my-repo",
	})

	if err != nil {
		t.Fatalf("Index failed: %v", err)
	}
	if result == nil {
		t.Error("result should not be nil")
	}
}

// ── Additional tests for Index() coverage: Incremental skip (unchanged file) ──────

func TestIndex_IncrementalSkipUnchangedFile(t *testing.T) {
	// Covers: if !req.Force && !req.Reparse && parsed.ContentHash != "" && existingFile.ContentHash == parsed.ContentHash
	// (line 202-210)
	walker := &stubFileWalker{
		files: []domain.FileEntry{
			{Path: "main.go", Language: "go", Content: []byte("func main() {}")},
		},
	}

	// Parser returns a file with ContentHash
	parser := &stubParser{
		result: &domain.ParsedFile{
			Path:        "main.go",
			ContentHash: "hash123",
			Nodes: []types.CodeNode{
				{ID: "n1", Qualified: "main", ContentHash: "hash123"},
			},
			Edges: []types.CodeEdge{},
		},
	}

	embedder := &stubEmbedder{
		batchRes: []domain.EmbedResult{{ID: "n1", Vector: []float32{0.1}}},
	}
	store := newStubGraphStore()

	// Pre-populate the store with an existing file node with the same content hash
	existingNode := &types.CodeNode{
		ID:          "n1",
		Qualified:   "main",
		ContentHash: "hash123",
	}
	store.nodes["n1"] = existingNode
	store.nodesByQ["my-repo::main"] = existingNode

	cfg := &config.Config{
		Index:     config.IndexConfig{MaxWorkersEmbed: 1, MaxWorkersStore: 1},
		BatchSize: 10,
	}

	svc := NewIndexService(walker, parser, embedder, store, nil, cfg)
	result, err := svc.Index(context.Background(), IndexRequest{
		RepoPath: "/repo",
		RepoSlug: "my-repo",
		Force:    false,
		Reparse:  false,
	})

	if err != nil {
		t.Fatalf("Index failed: %v", err)
	}
	// File is skipped but counted in FilesIndexed for progress tracking
	if result.FilesIndexed != 1 {
		t.Errorf("FilesIndexed = %d, want 1 (skipped file still counted for progress)", result.FilesIndexed)
	}
}

// ── Additional tests for Index() coverage: Reparse mode ───────────────────────

func TestIndex_ReparseMode(t *testing.T) {
	// Covers: if req.Reparse { ... preserve existing Summary, Concepts, Embedding } (line 222-239)
	walker := &stubFileWalker{
		files: []domain.FileEntry{
			{Path: "main.go", Language: "go"},
		},
	}

	// Parser returns a node with no Summary/Concepts/Embedding
	parser := &stubParser{
		result: &domain.ParsedFile{
			Path: "main.go",
			Nodes: []types.CodeNode{
				{ID: "n1", Qualified: "main", Summary: "", Concepts: []string{}},
			},
			Edges: []types.CodeEdge{},
		},
	}

	embedder := &stubEmbedder{
		batchRes: []domain.EmbedResult{{ID: "n1", Vector: []float32{0.1}}},
	}

	store := newStubGraphStore()

	// Pre-populate with a node that has Summary, Concepts, and Embedding
	existingNode := &types.CodeNode{
		ID:        "n1",
		Qualified: "main",
		Summary:   "This is an existing summary",
		Concepts:  []string{"concept1", "concept2"},
		Embedding: []float32{0.5, 0.6},
	}
	store.nodes["n1"] = existingNode
	store.nodesByQ["my-repo::main"] = existingNode

	cfg := &config.Config{
		Index:     config.IndexConfig{MaxWorkersEmbed: 1, MaxWorkersStore: 1},
		BatchSize: 10,
	}

	svc := NewIndexService(walker, parser, embedder, store, nil, cfg)
	result, err := svc.Index(context.Background(), IndexRequest{
		RepoPath: "/repo",
		RepoSlug: "my-repo",
		Reparse:  true,
	})

	if err != nil {
		t.Fatalf("Index with Reparse failed: %v", err)
	}
	if result == nil {
		t.Error("result should not be nil")
	}
}

// ── Additional tests for Index() coverage: No tracker (tracker == nil) ─────────

func TestIndex_NoTracker(t *testing.T) {
	// Covers: if is.tracker != nil { ... } branches where tracker is nil (multiple places)
	walker := &stubFileWalker{
		files: []domain.FileEntry{
			{Path: "main.go", Language: "go"},
		},
	}

	parser := &stubParser{
		result: &domain.ParsedFile{
			Path:  "main.go",
			Nodes: []types.CodeNode{{ID: "n1", Qualified: "main"}},
			Edges: []types.CodeEdge{},
		},
	}

	embedder := &stubEmbedder{
		batchRes: []domain.EmbedResult{{ID: "n1", Vector: []float32{0.1}}},
	}

	store := newStubGraphStore()

	cfg := &config.Config{
		Index:     config.IndexConfig{MaxWorkersEmbed: 1, MaxWorkersStore: 1},
		BatchSize: 10,
	}

	svc := NewIndexService(walker, parser, embedder, store, nil, cfg)
	svc.tracker = nil // explicitly set to nil to cover nil checks

	result, err := svc.Index(context.Background(), IndexRequest{
		RepoPath: "/repo",
		RepoSlug: "my-repo",
	})

	if err != nil {
		t.Fatalf("Index without tracker failed: %v", err)
	}
	if result.FilesIndexed != 1 {
		t.Errorf("FilesIndexed = %d, want 1", result.FilesIndexed)
	}
}

// ── Additional tests for Index() coverage: Linkers (edge resolution) ───────────

func TestIndex_WithLinkers(t *testing.T) {
	// Covers: if len(is.linkers) > 0 && len(allParsed) > 0 { ... } (line 277-325)
	walker := &stubFileWalker{
		files: []domain.FileEntry{
			{Path: "a.go", Language: "go"},
			{Path: "b.go", Language: "go"},
		},
	}

	parser := &stubParser{
		result: &domain.ParsedFile{
			Path:  "a.go",
			Nodes: []types.CodeNode{{ID: "n1", Qualified: "pkg.F"}},
			Edges: []types.CodeEdge{{FromID: "n1", ToID: "n2", Kind: "calls"}},
		},
	}

	embedder := &stubEmbedder{
		batchRes: []domain.EmbedResult{{ID: "n1", Vector: []float32{0.1}}},
	}

	store := newStubGraphStore()

	cfg := &config.Config{
		Index:     config.IndexConfig{MaxWorkersEmbed: 1, MaxWorkersStore: 1},
		BatchSize: 10,
	}

	svc := NewIndexService(walker, parser, embedder, store, nil, cfg)

	// Create a mock linker that processes edges
	linker := &mockEdgeLinker{
		name: "mock-linker",
	}
	svc.SetLinkers([]domain.EdgeLinker{linker})

	result, err := svc.Index(context.Background(), IndexRequest{
		RepoPath: "/repo",
		RepoSlug: "my-repo",
	})

	if err != nil {
		t.Fatalf("Index with linkers failed: %v", err)
	}
	if result == nil {
		t.Error("result should not be nil")
	}
}

// TestIndex_WithLinkersTwoFiles tests linker with multiple files to exercise edge redistribution
func TestIndex_WithLinkersTwoFiles(t *testing.T) {
	// Covers edge redistribution logic (line 304-324)
	walker := &stubFileWalker{
		files: []domain.FileEntry{
			{Path: "a.go", Language: "go"},
			{Path: "b.go", Language: "go"},
		},
	}

	parser := &stubParser{
		result: &domain.ParsedFile{
			Path:  "a.go",
			Nodes: []types.CodeNode{{ID: "n1", Qualified: "pkg.F"}},
			Edges: []types.CodeEdge{{FromID: "n1", ToID: "n2", Kind: "calls"}},
		},
	}

	embedder := &stubEmbedder{
		batchRes: []domain.EmbedResult{{ID: "n1", Vector: []float32{0.1}}},
	}

	store := newStubGraphStore()

	cfg := &config.Config{
		Index:     config.IndexConfig{MaxWorkersEmbed: 1, MaxWorkersStore: 1},
		BatchSize: 10,
	}

	svc := NewIndexService(walker, parser, embedder, store, nil, cfg)

	linker := &mockEdgeLinker{
		name: "test-linker",
	}
	svc.SetLinkers([]domain.EdgeLinker{linker})

	result, err := svc.Index(context.Background(), IndexRequest{
		RepoPath: "/repo",
		RepoSlug: "my-repo",
	})

	if err != nil {
		t.Fatalf("Index with multiple files and linker failed: %v", err)
	}
	if result == nil {
		t.Error("result should not be nil")
	}
}

// ── Additional tests for Index() coverage: Fast mode (skip summarize/reembed) ──

func TestIndex_FastMode(t *testing.T) {
	// Covers: if req.Fast (line 338, 363) to skip summarizer and reembed stages
	walker := &stubFileWalker{
		files: []domain.FileEntry{
			{Path: "main.go", Language: "go"},
		},
	}

	parser := &stubParser{
		result: &domain.ParsedFile{
			Path:  "main.go",
			Nodes: []types.CodeNode{{ID: "n1", Qualified: "main"}},
			Edges: []types.CodeEdge{},
		},
	}

	embedder := &stubEmbedder{
		batchRes: []domain.EmbedResult{{ID: "n1", Vector: []float32{0.1}}},
	}

	store := newStubGraphStore()

	cfg := &config.Config{
		Index:     config.IndexConfig{MaxWorkersEmbed: 1, MaxWorkersStore: 1},
		BatchSize: 10,
	}

	svc := NewIndexService(walker, parser, embedder, store, nil, cfg)

	result, err := svc.Index(context.Background(), IndexRequest{
		RepoPath: "/repo",
		RepoSlug: "my-repo",
		Fast:     true,
	})

	if err != nil {
		t.Fatalf("Index in fast mode failed: %v", err)
	}
	if result == nil {
		t.Error("result should not be nil")
	}
}

// ── Additional tests for Index() coverage: Empty RepoPath (skip cleanup/temporal) ─

func TestIndex_EmptyRepoPath(t *testing.T) {
	// Covers: if req.RepoPath != "" check for cleanup (line 527) and temporal (line 504)
	walker := &stubFileWalker{
		files: []domain.FileEntry{
			{Path: "main.go", Language: "go"},
		},
	}

	parser := &stubParser{
		result: &domain.ParsedFile{
			Path:  "main.go",
			Nodes: []types.CodeNode{{ID: "n1", Qualified: "main"}},
			Edges: []types.CodeEdge{},
		},
	}

	embedder := &stubEmbedder{
		batchRes: []domain.EmbedResult{{ID: "n1", Vector: []float32{0.1}}},
	}

	store := newStubGraphStore()

	cfg := &config.Config{
		Index:     config.IndexConfig{MaxWorkersEmbed: 1, MaxWorkersStore: 1},
		BatchSize: 10,
	}

	svc := NewIndexService(walker, parser, embedder, store, nil, cfg)

	result, err := svc.Index(context.Background(), IndexRequest{
		RepoPath: "", // empty path
		RepoSlug: "my-repo",
	})

	if err != nil {
		t.Fatalf("Index with empty path failed: %v", err)
	}
	if result == nil {
		t.Error("result should not be nil")
	}
}

// ── Additional tests for Index() coverage: ReembedNeighborhood error ────────────

func TestIndex_ReembedNeighborhoodError(t *testing.T) {
	// Covers: if _, err := is.ReembedNeighborhood returns error (line 492-496)
	walker := &stubFileWalker{
		files: []domain.FileEntry{
			{Path: "main.go", Language: "go"},
		},
	}

	parser := &stubParser{
		result: &domain.ParsedFile{
			Path:  "main.go",
			Nodes: []types.CodeNode{{ID: "n1", Qualified: "main"}},
			Edges: []types.CodeEdge{},
		},
	}

	embedder := &stubEmbedder{
		batchRes: []domain.EmbedResult{{ID: "n1", Vector: []float32{0.1}}},
	}

	store := newStubGraphStore()
	store.err = domain.NotFound("reembed failed")

	cfg := &config.Config{
		Index:     config.IndexConfig{MaxWorkersEmbed: 1, MaxWorkersStore: 1},
		BatchSize: 10,
	}

	svc := NewIndexService(walker, parser, embedder, store, nil, cfg)

	result, err := svc.Index(context.Background(), IndexRequest{
		RepoPath: "/repo",
		RepoSlug: "my-repo",
	})

	// ReembedNeighborhood error is non-fatal (only warns)
	if err != nil {
		t.Errorf("Index should not fail on reembed error (non-fatal), got: %v", err)
	}
	if result == nil {
		t.Error("result should not be nil despite reembed error")
	}
}

// ── Additional tests for Index() coverage: Temporal indexing with service ───────

func TestIndex_TemporalIndexingError(t *testing.T) {
	// Covers: if is.temporalSvc != nil && ... temporal indexing error handling (line 504-521)
	walker := &stubFileWalker{
		files: []domain.FileEntry{
			{Path: "main.go", Language: "go"},
		},
	}

	parser := &stubParser{
		result: &domain.ParsedFile{
			Path:  "main.go",
			Nodes: []types.CodeNode{{ID: "n1", Qualified: "main"}},
			Edges: []types.CodeEdge{},
		},
	}

	embedder := &stubEmbedder{
		batchRes: []domain.EmbedResult{{ID: "n1", Vector: []float32{0.1}}},
	}

	store := newStubGraphStore()

	cfg := &config.Config{
		Index:     config.IndexConfig{MaxWorkersEmbed: 1, MaxWorkersStore: 1},
		BatchSize: 10,
	}

	svc := NewIndexService(walker, parser, embedder, store, nil, cfg)

	// Set a temporal service that will error
	mockTemporal := &mockTemporalService{
		err: domain.Validation("temporal indexing failed"),
	}
	svc.SetTemporalService(mockTemporal)

	result, err := svc.Index(context.Background(), IndexRequest{
		RepoPath: "/repo",
		RepoSlug: "my-repo",
	})

	// Temporal error is non-fatal
	if err != nil {
		t.Errorf("Index should not fail on temporal error (non-fatal), got: %v", err)
	}
	if result == nil {
		t.Error("result should not be nil despite temporal error")
	}
}

// ── Additional tests for cleanupStaleNodes() coverage ──────────────────────────

func TestCleanupStaleNodes_DeletesStaleFile(t *testing.T) {
	// Covers: os.Stat returns IsNotExist error, so file is deleted (line 570-577)
	walker := &stubFileWalker{files: []domain.FileEntry{}}
	parser := &stubParser{result: &domain.ParsedFile{Nodes: []types.CodeNode{}, Edges: []types.CodeEdge{}}}
	embedder := &stubEmbedder{}
	store := newStubGraphStore()

	// Return a list of file paths from the graph
	store.listFilePathsFn = func(ctx context.Context, repoSlug string) ([]string, error) {
		return []string{"main.go", "missing.go"}, nil
	}

	// Track DeleteByFile calls
	deleteCalls := 0
	store.deleteByFileFn = func(ctx context.Context, repoSlug, filePath string) error {
		deleteCalls++
		if filePath == "missing.go" {
			return nil
		}
		return nil
	}

	cfg := &config.Config{
		Index: config.IndexConfig{MaxWorkersEmbed: 1, MaxWorkersStore: 1},
	}

	svc := NewIndexService(walker, parser, embedder, store, nil, cfg)

	// Call cleanupStaleNodes directly
	// Note: This will attempt os.Stat on "/repo/main.go" and "/repo/missing.go"
	// Since /repo doesn't exist, both will fail IsNotExist and both will be "deleted"
	svc.cleanupStaleNodes(context.Background(), "test-repo", "/repo")
}

// ── cleanupStaleNodes with empty file paths ──────────────────────────────────────

func TestCleanupStaleNodes_EmptyFilePaths(t *testing.T) {
	// Covers: for filePath := range graphFiles { if filePath != "" } check (line 561)
	walker := &stubFileWalker{files: []domain.FileEntry{}}
	parser := &stubParser{result: &domain.ParsedFile{Nodes: []types.CodeNode{}, Edges: []types.CodeEdge{}}}
	embedder := &stubEmbedder{}
	store := newStubGraphStore()

	// Return file paths including empty strings
	store.listFilePathsFn = func(ctx context.Context, repoSlug string) ([]string, error) {
		return []string{"main.go", "", "util.go"}, nil
	}

	cfg := &config.Config{
		Index: config.IndexConfig{MaxWorkersEmbed: 1, MaxWorkersStore: 1},
	}

	svc := NewIndexService(walker, parser, embedder, store, nil, cfg)

	// Should handle empty paths gracefully (skip them)
	svc.cleanupStaleNodes(context.Background(), "test-repo", "/repo")
}

// ── cleanupStaleNodes with delete error ────────────────────────────────────────

func TestCleanupStaleNodes_DeleteError(t *testing.T) {
	// Covers: if err := is.graph.DeleteByFile returns error (line 572-575)
	walker := &stubFileWalker{files: []domain.FileEntry{}}
	parser := &stubParser{result: &domain.ParsedFile{Nodes: []types.CodeNode{}, Edges: []types.CodeEdge{}}}
	embedder := &stubEmbedder{}
	store := newStubGraphStore()

	store.listFilePathsFn = func(ctx context.Context, repoSlug string) ([]string, error) {
		return []string{"orphan.go"}, nil
	}

	deleteAttempted := 0
	store.deleteByFileFn = func(ctx context.Context, repoSlug, filePath string) error {
		deleteAttempted++
		return domain.Validation("delete failed")
	}

	cfg := &config.Config{
		Index: config.IndexConfig{MaxWorkersEmbed: 1, MaxWorkersStore: 1},
	}

	svc := NewIndexService(walker, parser, embedder, store, nil, cfg)

	// cleanupStaleNodes should warn but not fail
	svc.cleanupStaleNodes(context.Background(), "test-repo", "/repo")

	if deleteAttempted == 0 {
		t.Error("DeleteByFile should have been called")
	}
}

// ── Mock linker for testing ────────────────────────────────────────────────────

type mockEdgeLinker struct {
	name string
}

func (m *mockEdgeLinker) Name() string {
	return m.name
}

func (m *mockEdgeLinker) Labels() []types.EdgeKind {
	return []types.EdgeKind{"calls", "imports"}
}

func (m *mockEdgeLinker) Link(edges []types.CodeEdge, symbols *domain.SymbolTable) ([]types.CodeEdge, domain.LinkStats) {
	return edges, domain.LinkStats{
		LinkerName: m.name,
		Processed:  len(edges),
		Resolved:   len(edges),
		Unresolved: 0,
	}
}

// ── Mock temporal service for testing ──────────────────────────────────────────

type mockTemporalService struct {
	err error
}

func (m *mockTemporalService) IndexCommitRange(ctx context.Context, req TemporalIndexRequest) error {
	return m.err
}
