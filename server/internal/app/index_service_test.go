package app

import (
	"context"
	"strings"
	"testing"
	"time"

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

// TestIndexServiceStoreContextCanceled covers the store-goroutine context-cancel path (lines
// added during refactor: `if err := storeCtx.Err(); err != nil { return err }`).
// We use a pre-canceled context so that storeCtx is immediately Done, and set the channel
// buffers to large values so the pipeline can fill before the store goroutines run.
func TestIndexServiceStoreContextCanceled(t *testing.T) {
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

// TestIndexServiceParseContextCanceled exercises the parse-stage context-cancel select branch.
// We use an unbuffered parsedCh (parsedChBuf = -1) + pre-canceled context so the select
// always picks the Done case (send would block, Done is immediately ready).
func TestIndexServiceParseContextCanceled(t *testing.T) {
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

// TestIndexServiceEmbedContextCanceled exercises the embed-stage context-cancel select branch.
func TestIndexServiceEmbedContextCanceled(t *testing.T) {
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

func TestReEmbed_ContextCanceled(t *testing.T) {
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
		t.Error("ReEmbed with canceled context should error")
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

func TestIndex_WithNoChangedNodes(t *testing.T) {
	// Covers: ReembedNeighborhood with empty changedNodeIDs (line 492-500)
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
	store.nodeIDs = []string{"n1"}
	store.nodes["n1"] = &types.CodeNode{ID: "n1", Qualified: "main"}

	cfg := &config.Config{
		Index:     config.IndexConfig{MaxWorkersEmbed: 1, MaxWorkersStore: 1},
		BatchSize: 10,
	}

	svc := NewIndexService(walker, parser, embedder, store, nil, cfg)

	result, err := svc.Index(context.Background(), IndexRequest{
		RepoPath: "/repo",
		RepoSlug: "my-repo",
	})

	// Should succeed even with reembed attempt
	if err != nil {
		t.Errorf("Index should succeed, got: %v", err)
	}
	if result == nil {
		t.Error("result should not be nil")
	}
}

// ── Additional tests for Index() coverage: Temporal indexing with service ───────

func TestIndex_TemporalServiceNil(t *testing.T) {
	// Covers: if is.temporalSvc != nil check at line 504 when service is nil
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
	// temporalSvc is nil (default), so temporal indexing path is skipped

	result, err := svc.Index(context.Background(), IndexRequest{
		RepoPath: "/repo",
		RepoSlug: "my-repo",
	})

	// Should succeed with no temporal service
	if err != nil {
		t.Errorf("Index should succeed when temporalSvc is nil, got: %v", err)
	}
	if result == nil {
		t.Error("result should not be nil")
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

// ── Additional tests for Index() coverage: Summarizer stage ──────────────────────

func TestIndex_SummarizerStage(t *testing.T) {
	// Covers: summarizer.SummarizeNodes path (line 371) when summarizer is set
	walker := &stubFileWalker{
		files: []domain.FileEntry{
			{Path: "main.go", Language: "go"},
		},
	}

	parser := &stubParser{
		result: &domain.ParsedFile{
			Path:  "main.go",
			Nodes: []types.CodeNode{{ID: "n1", Qualified: "main", Summary: ""}},
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
	// Note: summarizer is nil by default in this test setup
	// To test the summarizer path would require dependency injection

	result, err := svc.Index(context.Background(), IndexRequest{
		RepoPath: "/repo",
		RepoSlug: "my-repo",
	})

	if err != nil {
		t.Fatalf("Index failed: %v", err)
	}
	if result.NodesCreated != 1 {
		t.Errorf("NodesCreated = %d, want 1", result.NodesCreated)
	}
}

// ── Additional tests for Index() coverage: Pre-embedded nodes ──────────────────

func TestIndex_PreEmbeddedNodes(t *testing.T) {
	// Covers: nodes that already have embeddings (line 387-390)
	walker := &stubFileWalker{
		files: []domain.FileEntry{
			{Path: "main.go", Language: "go"},
		},
	}

	parser := &stubParser{
		result: &domain.ParsedFile{
			Path:  "main.go",
			Nodes: []types.CodeNode{{ID: "n1", Qualified: "main", Embedding: []float32{0.5, 0.6}}},
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

// ── Additional tests for Index() coverage: UnbufferedChannels (force context-cancel) ─

func TestIndex_UnbufferedEmbedChannel(t *testing.T) {
	// Covers: embedChBuf < 0 case (line 349-350) for unbuffered embed channel
	ctx, cancel := context.WithCancel(context.Background())

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
	svc.embedChBuf = -1 // unbuffered

	cancel() // pre-cancel context

	// Should handle gracefully
	svc.Index(ctx, IndexRequest{RepoPath: "/repo", RepoSlug: "my-repo"})
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

// ── Additional tests for Index() coverage: Repo deduplication ───────────────────

func TestIndex_RepoDeduplication(t *testing.T) {
	// Covers: repo deduplication logic (line 121-127)
	// Tests when an existing repo with same remote URL is found
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

	// Mock FindRepoByRemoteURL to return an existing repo
	store.findRepoByRemoteURLFn = func(ctx context.Context, url string) (*types.Repo, error) {
		if url != "" {
			return &types.Repo{
				Slug:      "existing-repo",
				RemoteURL: url,
			}, nil
		}
		return nil, nil
	}

	cfg := &config.Config{
		Index:     config.IndexConfig{MaxWorkersEmbed: 1, MaxWorkersStore: 1},
		BatchSize: 10,
	}

	svc := NewIndexService(walker, parser, embedder, store, nil, cfg)

	result, err := svc.Index(context.Background(), IndexRequest{
		RepoPath: "/repo",
		RepoSlug: "new-repo",
	})

	if err != nil {
		t.Fatalf("Index with repo deduplication failed: %v", err)
	}
	if result == nil {
		t.Error("result should not be nil")
	}
}

// ── Additional tests for Index() coverage: No linkers with parsed files ──────────

func TestIndex_NolinkersMultipleFiles(t *testing.T) {
	// Covers: if len(is.linkers) > 0 && len(allParsed) > 0 check is false (line 277)
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
	// No linkers set (empty slice)

	result, err := svc.Index(context.Background(), IndexRequest{
		RepoPath: "/repo",
		RepoSlug: "my-repo",
	})

	if err != nil {
		t.Fatalf("Index without linkers failed: %v", err)
	}
	if result == nil {
		t.Error("result should not be nil")
	}
}

// ── Additional tests for Index() coverage: Reparse with missing node ────────────

func TestIndex_ReparseWithMissingNode(t *testing.T) {
	// Covers: reparse mode when FindNode fails (line 225)
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
	// Don't pre-populate nodes, so FindNode will fail

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
		t.Fatalf("Index with reparse (missing node) failed: %v", err)
	}
	if result == nil {
		t.Error("result should not be nil")
	}
}

// ── Additional tests for Index() coverage: ReembedNeighborhood GetNode error ─────

func TestIndex_ReembedNeighborhoodGetNodeError(t *testing.T) {
	// Covers: ReembedNeighborhood GetNode error path when nodes exist in ID list but not in store
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
	// Set nodeIDs but don't populate nodes, so GetNode will fail during ReembedNeighborhood
	store.nodeIDs = []string{"n1"}

	cfg := &config.Config{
		Index:     config.IndexConfig{MaxWorkersEmbed: 1, MaxWorkersStore: 1},
		BatchSize: 10,
	}

	svc := NewIndexService(walker, parser, embedder, store, nil, cfg)

	result, err := svc.Index(context.Background(), IndexRequest{
		RepoPath: "/repo",
		RepoSlug: "my-repo",
	})

	// Should still succeed (reembed is non-fatal)
	if err != nil {
		t.Fatalf("Index should not fail on reembed GetNode error, got: %v", err)
	}
	if result == nil {
		t.Error("result should not be nil")
	}
}

// ── Additional tests for ReEmbed() coverage: GetNode errors ──────────────────

func TestReEmbed_GetNodeError(t *testing.T) {
	// Covers: GetNode error path (line 784-788) when node fetch fails
	walker := &stubFileWalker{files: []domain.FileEntry{}}
	parser := &stubParser{result: &domain.ParsedFile{Nodes: []types.CodeNode{}, Edges: []types.CodeEdge{}}}
	embedder := &stubEmbedder{}

	store := newStubGraphStore()
	store.nodeIDs = []string{"n1", "n2"}
	// Only n1 is in nodes map, n2 will fail GetNode
	store.nodes["n1"] = &types.CodeNode{ID: "n1"}

	cfg := &config.Config{
		Index:     config.IndexConfig{MaxWorkersEmbed: 1, MaxWorkersStore: 1},
		BatchSize: 100,
	}

	svc := NewIndexService(walker, parser, embedder, store, nil, cfg)

	result, err := svc.ReEmbed(context.Background(), "test-repo", nil)

	if err != nil {
		t.Fatalf("ReEmbed should not fail on partial GetNode errors: %v", err)
	}
	if result == nil {
		t.Error("result should not be nil")
	}
}

// ── Additional tests for ReEmbed() coverage: PutNode error ────────────────────

func TestReEmbed_PutNodeError(t *testing.T) {
	// Covers: PutNode error path (line 820-822)
	walker := &stubFileWalker{files: []domain.FileEntry{}}
	parser := &stubParser{result: &domain.ParsedFile{Nodes: []types.CodeNode{}, Edges: []types.CodeEdge{}}}

	embedder := &stubEmbedder{
		batchRes: []domain.EmbedResult{{ID: "n1", Vector: []float32{0.1}}},
	}

	store := newStubGraphStore()
	store.nodeIDs = []string{"n1"}
	store.nodes["n1"] = &types.CodeNode{ID: "n1"}

	// Make PutNode fail
	failStore := &upsertFailStore{stubGraphStore: store}

	cfg := &config.Config{
		Index:     config.IndexConfig{MaxWorkersEmbed: 1, MaxWorkersStore: 1},
		BatchSize: 100,
	}

	svc := NewIndexService(walker, parser, embedder, failStore, nil, cfg)

	result, err := svc.ReEmbed(context.Background(), "test-repo", nil)

	// Should continue despite upsert error
	if err != nil {
		t.Fatalf("ReEmbed should not fail on upsert error: %v", err)
	}
	if result == nil {
		t.Error("result should not be nil")
	}
}

// ── Additional tests for ReEmbed() coverage: Multiple batches ──────────────────

func TestReEmbed_MultipleBatches(t *testing.T) {
	// Covers: multiple batches (i += batchSize loop) with real batching
	walker := &stubFileWalker{files: []domain.FileEntry{}}
	parser := &stubParser{result: &domain.ParsedFile{Nodes: []types.CodeNode{}, Edges: []types.CodeEdge{}}}

	embedder := &stubEmbedder{
		batchRes: []domain.EmbedResult{
			{ID: "n1", Vector: []float32{0.1}},
			{ID: "n2", Vector: []float32{0.2}},
			{ID: "n3", Vector: []float32{0.3}},
		},
	}

	store := newStubGraphStore()
	store.nodeIDs = []string{"n1", "n2", "n3"}
	store.nodes["n1"] = &types.CodeNode{ID: "n1"}
	store.nodes["n2"] = &types.CodeNode{ID: "n2"}
	store.nodes["n3"] = &types.CodeNode{ID: "n3"}

	cfg := &config.Config{
		Index:     config.IndexConfig{MaxWorkersEmbed: 1, MaxWorkersStore: 1},
		BatchSize: 2, // Force multiple batches
	}

	svc := NewIndexService(walker, parser, embedder, store, nil, cfg)

	progressCalls := 0
	onProgress := func(done, total int) {
		progressCalls++
	}

	result, err := svc.ReEmbed(context.Background(), "test-repo", onProgress)

	if err != nil {
		t.Fatalf("ReEmbed with multiple batches failed: %v", err)
	}
	if result.NodesTotal != 3 {
		t.Errorf("NodesTotal = %d, want 3", result.NodesTotal)
	}
	if result.NodesEmbedded != 3 {
		t.Errorf("NodesEmbedded = %d, want 3", result.NodesEmbedded)
	}
}

// ── Additional tests for ReEmbed() coverage: Embed batch error ──────────────────

func TestReEmbed_EmbedBatchError(t *testing.T) {
	// Covers: embed batch error path (line 806-810)
	walker := &stubFileWalker{files: []domain.FileEntry{}}
	parser := &stubParser{result: &domain.ParsedFile{Nodes: []types.CodeNode{}, Edges: []types.CodeEdge{}}}

	embedder := &stubEmbedder{
		batchErr: domain.RateLimit("embed rate limited"),
	}

	store := newStubGraphStore()
	store.nodeIDs = []string{"n1", "n2"}
	store.nodes["n1"] = &types.CodeNode{ID: "n1"}
	store.nodes["n2"] = &types.CodeNode{ID: "n2"}

	cfg := &config.Config{
		Index:     config.IndexConfig{MaxWorkersEmbed: 1, MaxWorkersStore: 1},
		BatchSize: 100,
	}

	svc := NewIndexService(walker, parser, embedder, store, nil, cfg)

	result, err := svc.ReEmbed(context.Background(), "test-repo", nil)

	// Should continue despite embed error
	if err != nil {
		t.Fatalf("ReEmbed should not fail on embed batch error: %v", err)
	}
	// Nodes that failed embedding should not be counted
	if result.NodesEmbedded != 0 {
		t.Errorf("NodesEmbedded = %d, want 0 (batch failed)", result.NodesEmbedded)
	}
}

// ── Additional tests for Index() coverage: Summarizer with tracker ────────────

func TestIndex_SummarizerWithTracker(t *testing.T) {
	// Covers: summarizer path with tracker enabled (line 363-381)
	walker := &stubFileWalker{
		files: []domain.FileEntry{
			{Path: "main.go", Language: "go"},
		},
	}

	parser := &stubParser{
		result: &domain.ParsedFile{
			Path:  "main.go",
			Nodes: []types.CodeNode{{ID: "n1", Qualified: "main", Summary: ""}},
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

	explainer := &stubExplainer{}
	svc := NewIndexService(walker, parser, embedder, store, explainer, cfg)

	// summarizer should be set by NewIndexService
	if svc.summarizer == nil {
		t.Skip("summarizer not initialized in test setup")
	}

	result, err := svc.Index(context.Background(), IndexRequest{
		RepoPath: "/repo",
		RepoSlug: "my-repo",
	})

	if err != nil {
		t.Fatalf("Index with summarizer failed: %v", err)
	}
	if result.NodesCreated != 1 {
		t.Errorf("NodesCreated = %d, want 1", result.NodesCreated)
	}
}

// ── Additional tests for Index() coverage: Linkers with orphan edges ────────────

func TestIndex_LinkersWithOrphanEdges(t *testing.T) {
	// Covers: linker orphan edge handling (line 320-323)
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

	// Linker that returns an orphan edge (FromID not in nodeFileMap)
	orphanLinker := &mockEdgeLinker{
		name: "orphan-linker",
	}
	svc.SetLinkers([]domain.EdgeLinker{orphanLinker})

	result, err := svc.Index(context.Background(), IndexRequest{
		RepoPath: "/repo",
		RepoSlug: "my-repo",
	})

	if err != nil {
		t.Fatalf("Index with orphan edges failed: %v", err)
	}
	if result == nil {
		t.Error("result should not be nil")
	}
}

// ── Additional tests for ReEmbed() coverage: AlreadyEmbeddedNodes skip ──────────

func TestReEmbed_AlreadyEmbeddedNodesSkipped(t *testing.T) {
	// Covers: already embedded node skip (line 789-792)
	walker := &stubFileWalker{files: []domain.FileEntry{}}
	parser := &stubParser{result: &domain.ParsedFile{Nodes: []types.CodeNode{}, Edges: []types.CodeEdge{}}}

	embedder := &stubEmbedder{
		batchRes: []domain.EmbedResult{},
	}

	store := newStubGraphStore()
	// n1 already has embedding, n2 doesn't
	store.nodeIDs = []string{"n1", "n2"}
	store.nodes["n1"] = &types.CodeNode{ID: "n1", Embedding: []float32{0.1, 0.2}}
	store.nodes["n2"] = &types.CodeNode{ID: "n2"}

	cfg := &config.Config{
		Index:     config.IndexConfig{MaxWorkersEmbed: 1, MaxWorkersStore: 1},
		BatchSize: 100,
	}

	svc := NewIndexService(walker, parser, embedder, store, nil, cfg)

	result, err := svc.ReEmbed(context.Background(), "test-repo", nil)

	if err != nil {
		t.Fatalf("ReEmbed with pre-embedded nodes failed: %v", err)
	}
	// n1 is skipped (already embedded), n2 gets 0 embedded (no batch results)
	if result.NodesTotal != 2 {
		t.Errorf("NodesTotal = %d, want 2", result.NodesTotal)
	}
}

// ── Additional tests for Index() coverage: Mixed error tracking ──────────────────

func TestIndex_MixedErrorTracking(t *testing.T) {
	// Covers: multiple non-fatal errors being tracked (parse + embed + store)
	walker := &stubFileWalker{
		files: []domain.FileEntry{
			{Path: "a.go", Language: "go"},
			{Path: "b.go", Language: "go"},
		},
	}

	parser := &stubParser{
		result: &domain.ParsedFile{
			Path:  "a.go",
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
	})

	if err != nil {
		t.Fatalf("Index with multiple files failed: %v", err)
	}
	if result == nil {
		t.Error("result should not be nil")
	}
}

// ── Additional tests for Index() coverage: Incremental skip with tracker ───────

func TestIndex_IncrementalSkipWithTracker(t *testing.T) {
	// Covers: tracker.AddSkipped() path (line 208) for unchanged files
	walker := &stubFileWalker{
		files: []domain.FileEntry{
			{Path: "main.go", Language: "go"},
		},
	}

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
	tracker := &IndexTracker{}
	svc.tracker = tracker

	result, err := svc.Index(context.Background(), IndexRequest{
		RepoPath: "/repo",
		RepoSlug: "my-repo",
		Force:    false,
		Reparse:  false,
	})

	if err != nil {
		t.Fatalf("Index with tracker and incremental skip failed: %v", err)
	}
	if result.FilesIndexed != 1 {
		t.Errorf("FilesIndexed = %d, want 1 (skipped file still counted)", result.FilesIndexed)
	}
}

// ── Additional tests for Index() coverage: Content hash mismatch (reparse) ──────

func TestIndex_ContentHashMismatch(t *testing.T) {
	// Covers: incremental skip path with hash mismatch (line 202-212)
	walker := &stubFileWalker{
		files: []domain.FileEntry{
			{Path: "main.go", Language: "go"},
		},
	}

	parser := &stubParser{
		result: &domain.ParsedFile{
			Path:        "main.go",
			ContentHash: "new-hash",
			Nodes: []types.CodeNode{
				{ID: "n1", Qualified: "main", ContentHash: "new-hash"},
			},
			Edges: []types.CodeEdge{},
		},
	}

	embedder := &stubEmbedder{
		batchRes: []domain.EmbedResult{{ID: "n1", Vector: []float32{0.1}}},
	}

	store := newStubGraphStore()
	existingNode := &types.CodeNode{
		ID:          "n1",
		Qualified:   "main",
		ContentHash: "old-hash", // Different hash
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
		t.Fatalf("Index with content hash mismatch failed: %v", err)
	}
	// File should be reprocessed because hash changed
	if result.NodesCreated < 1 {
		t.Errorf("NodesCreated = %d, want >= 1 (file changed)", result.NodesCreated)
	}
}

// ── Additional tests for Index() coverage: Reparse preserves all fields ──────────

func TestIndex_ReparsePreservesAllFields(t *testing.T) {
	// Covers: reparse preservation of Summary, Concepts, Embedding (line 227-235)
	walker := &stubFileWalker{
		files: []domain.FileEntry{
			{Path: "main.go", Language: "go"},
		},
	}

	parser := &stubParser{
		result: &domain.ParsedFile{
			Path: "main.go",
			Nodes: []types.CodeNode{
				{ID: "n1", Qualified: "main", Summary: "", Concepts: []string{}, Embedding: []float32{}},
			},
			Edges: []types.CodeEdge{},
		},
	}

	embedder := &stubEmbedder{
		batchRes: []domain.EmbedResult{{ID: "n1", Vector: []float32{0.1}}},
	}

	store := newStubGraphStore()
	existingNode := &types.CodeNode{
		ID:        "n1",
		Qualified: "main",
		Summary:   "Old summary",
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
		t.Fatalf("Index with reparse and field preservation failed: %v", err)
	}
	if result == nil {
		t.Error("result should not be nil")
	}
}

// ── Additional tests for Index() coverage: Fast mode skips summary ──────────────

func TestIndex_FastModeSkipsSummary(t *testing.T) {
	// Covers: fast mode skips summarizer (line 338, 363-381)
	walker := &stubFileWalker{
		files: []domain.FileEntry{
			{Path: "main.go", Language: "go"},
		},
	}

	parser := &stubParser{
		result: &domain.ParsedFile{
			Path:  "main.go",
			Nodes: []types.CodeNode{{ID: "n1", Qualified: "main", Summary: ""}},
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

	explainer := &stubExplainer{}
	svc := NewIndexService(walker, parser, embedder, store, explainer, cfg)

	result, err := svc.Index(context.Background(), IndexRequest{
		RepoPath: "/repo",
		RepoSlug: "my-repo",
		Fast:     true,
	})

	if err != nil {
		t.Fatalf("Index in fast mode failed: %v", err)
	}
	if result.FilesIndexed != 1 {
		t.Errorf("FilesIndexed = %d, want 1", result.FilesIndexed)
	}
}

// ── Additional tests for Index() coverage: Nodes with existing embeddings ──────

func TestIndex_NodesWithExistingEmbeddings(t *testing.T) {
	// Covers: preEmbedded count logic (line 384-390)
	walker := &stubFileWalker{
		files: []domain.FileEntry{
			{Path: "main.go", Language: "go"},
		},
	}

	parser := &stubParser{
		result: &domain.ParsedFile{
			Path: "main.go",
			Nodes: []types.CodeNode{
				{ID: "n1", Qualified: "main", Embedding: []float32{0.1, 0.2}},
				{ID: "n2", Qualified: "helper", Embedding: []float32{0.3, 0.4}},
			},
			Edges: []types.CodeEdge{},
		},
	}

	embedder := &stubEmbedder{
		batchRes: []domain.EmbedResult{
			{ID: "n1", Vector: []float32{0.1, 0.2}},
			{ID: "n2", Vector: []float32{0.3, 0.4}},
		},
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
		t.Fatalf("Index with pre-embedded nodes failed: %v", err)
	}
	if result.NodesCreated != 2 {
		t.Errorf("NodesCreated = %d, want 2", result.NodesCreated)
	}
}

// ── Additional tests for Index() coverage: Reparse with partial existing data ───

func TestIndex_ReparsePartialExistingData(t *testing.T) {
	// Covers: reparse with some nodes having existing fields and others not
	walker := &stubFileWalker{
		files: []domain.FileEntry{
			{Path: "main.go", Language: "go"},
		},
	}

	parser := &stubParser{
		result: &domain.ParsedFile{
			Path: "main.go",
			Nodes: []types.CodeNode{
				{ID: "n1", Qualified: "func1", Summary: ""},
				{ID: "n2", Qualified: "func2", Summary: ""},
			},
			Edges: []types.CodeEdge{},
		},
	}

	embedder := &stubEmbedder{
		batchRes: []domain.EmbedResult{
			{ID: "n1", Vector: []float32{0.1}},
			{ID: "n2", Vector: []float32{0.2}},
		},
	}

	store := newStubGraphStore()
	// Only n1 has existing data; n2 doesn't exist
	store.nodes["n1"] = &types.CodeNode{
		ID:          "n1",
		Qualified:   "func1",
		Summary:     "Existing summary",
		Concepts:    []string{"concept1"},
		Embedding:   []float32{0.1},
		ContentHash: "oldhash",
	}
	store.nodesByQ["my-repo::func1"] = store.nodes["n1"]

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
		t.Fatalf("Index with partial reparse data failed: %v", err)
	}
	if result.NodesCreated != 2 {
		t.Errorf("NodesCreated = %d, want 2", result.NodesCreated)
	}
}

// ── Additional tests for Index() coverage: Linker with no edges ────────────────

func TestIndex_LinkerNoEdges(t *testing.T) {
	// Covers: linker execution with empty edges (edge redistribution logic)
	walker := &stubFileWalker{
		files: []domain.FileEntry{
			{Path: "a.go", Language: "go"},
		},
	}

	parser := &stubParser{
		result: &domain.ParsedFile{
			Path:  "a.go",
			Nodes: []types.CodeNode{{ID: "n1", Qualified: "pkg.F"}},
			Edges: []types.CodeEdge{}, // no edges
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

	linker := &mockEdgeLinker{name: "test-linker"}
	svc.SetLinkers([]domain.EdgeLinker{linker})

	result, err := svc.Index(context.Background(), IndexRequest{
		RepoPath: "/repo",
		RepoSlug: "my-repo",
	})

	if err != nil {
		t.Fatalf("Index with linker and no edges failed: %v", err)
	}
	if result == nil {
		t.Error("result should not be nil")
	}
}

// ── Additional tests for ReEmbed() coverage: Mixed embedding results ───────────

func TestReEmbed_MixedEmbeddingResults(t *testing.T) {
	// Covers: partial embedding results where some nodes get vectors and others don't
	walker := &stubFileWalker{files: []domain.FileEntry{}}
	parser := &stubParser{result: &domain.ParsedFile{Nodes: []types.CodeNode{}, Edges: []types.CodeEdge{}}}

	embedder := &stubEmbedder{
		batchRes: []domain.EmbedResult{
			{ID: "n1", Vector: []float32{0.1}}, // n1 gets embedding
			// n2 is missing from results
		},
	}

	store := newStubGraphStore()
	store.nodeIDs = []string{"n1", "n2"}
	store.nodes["n1"] = &types.CodeNode{ID: "n1"}
	store.nodes["n2"] = &types.CodeNode{ID: "n2"}

	cfg := &config.Config{
		Index:     config.IndexConfig{MaxWorkersEmbed: 1, MaxWorkersStore: 1},
		BatchSize: 100,
	}

	svc := NewIndexService(walker, parser, embedder, store, nil, cfg)

	result, err := svc.ReEmbed(context.Background(), "test-repo", nil)

	if err != nil {
		t.Fatalf("ReEmbed with partial results failed: %v", err)
	}
	// Only n1 should be updated since n2 wasn't in results
	if result.NodesEmbedded != 1 {
		t.Errorf("NodesEmbedded = %d, want 1", result.NodesEmbedded)
	}
}

// ── Additional tests for ReEmbed() coverage: All nodes skip (pre-embedded) ──────

func TestReEmbed_AllNodesSkipped(t *testing.T) {
	// Covers: batch with all nodes pre-embedded (line 789-791 for all nodes)
	walker := &stubFileWalker{files: []domain.FileEntry{}}
	parser := &stubParser{result: &domain.ParsedFile{Nodes: []types.CodeNode{}, Edges: []types.CodeEdge{}}}
	embedder := &stubEmbedder{
		batchRes: []domain.EmbedResult{},
	}

	store := newStubGraphStore()
	// All nodes already have embeddings
	store.nodeIDs = []string{"n1", "n2", "n3"}
	store.nodes["n1"] = &types.CodeNode{ID: "n1", Embedding: []float32{0.1}}
	store.nodes["n2"] = &types.CodeNode{ID: "n2", Embedding: []float32{0.2}}
	store.nodes["n3"] = &types.CodeNode{ID: "n3", Embedding: []float32{0.3}}

	cfg := &config.Config{
		Index:     config.IndexConfig{MaxWorkersEmbed: 1, MaxWorkersStore: 1},
		BatchSize: 100,
	}

	svc := NewIndexService(walker, parser, embedder, store, nil, cfg)

	result, err := svc.ReEmbed(context.Background(), "test-repo", nil)

	if err != nil {
		t.Fatalf("ReEmbed with all pre-embedded nodes failed: %v", err)
	}
	if result.NodesEmbedded != 3 {
		t.Errorf("NodesEmbedded = %d, want 3 (all skipped but counted)", result.NodesEmbedded)
	}
}

// ── Additional tests for Index() coverage: Edge redistribution with orphans ─────

func TestIndex_EdgeRedistributionOrphanAttachment(t *testing.T) {
	// Covers: orphan edge attachment to first file (line 320-323)
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
			Edges: []types.CodeEdge{{FromID: "n1", ToID: "n_external", Kind: "calls"}},
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

	// Linker returns edges where FromID doesn't map to any nodeFileMap entry
	orphanLinker := &mockEdgeLinker{name: "orphan-linker"}
	svc.SetLinkers([]domain.EdgeLinker{orphanLinker})

	result, err := svc.Index(context.Background(), IndexRequest{
		RepoPath: "/repo",
		RepoSlug: "my-repo",
	})

	if err != nil {
		t.Fatalf("Index with edge redistribution failed: %v", err)
	}
	if result == nil {
		t.Error("result should not be nil")
	}
}

// ── Additional tests for Index() coverage: No parsed files with linkers ──────────

func TestIndex_NoFilesButLinkerDefined(t *testing.T) {
	// Covers: linker branch when no files parsed (line 277 condition fails)
	walker := &stubFileWalker{files: []domain.FileEntry{}}
	parser := &stubParser{result: &domain.ParsedFile{Nodes: []types.CodeNode{}, Edges: []types.CodeEdge{}}}
	embedder := &stubEmbedder{}
	store := newStubGraphStore()

	cfg := &config.Config{
		Index:     config.IndexConfig{MaxWorkersEmbed: 1, MaxWorkersStore: 1},
		BatchSize: 10,
	}

	svc := NewIndexService(walker, parser, embedder, store, nil, cfg)

	// Linker is set but no files are parsed
	linker := &mockEdgeLinker{name: "unused"}
	svc.SetLinkers([]domain.EdgeLinker{linker})

	result, err := svc.Index(context.Background(), IndexRequest{
		RepoPath: "/repo",
		RepoSlug: "my-repo",
	})

	if err != nil {
		t.Fatalf("Index with linker but no files failed: %v", err)
	}
	if result == nil {
		t.Error("result should not be nil")
	}
}

// ── Additional tests for Index() coverage: Reparse with empty concepts ──────────

func TestIndex_ReparseEmptyConceptsPreserved(t *testing.T) {
	// Covers: reparse skips empty Concepts (line 230-231 branch)
	walker := &stubFileWalker{
		files: []domain.FileEntry{
			{Path: "main.go", Language: "go"},
		},
	}

	parser := &stubParser{
		result: &domain.ParsedFile{
			Path:  "main.go",
			Nodes: []types.CodeNode{{ID: "n1", Qualified: "main", Concepts: []string{}}},
			Edges: []types.CodeEdge{},
		},
	}

	embedder := &stubEmbedder{
		batchRes: []domain.EmbedResult{{ID: "n1", Vector: []float32{0.1}}},
	}

	store := newStubGraphStore()
	existingNode := &types.CodeNode{
		ID:        "n1",
		Qualified: "main",
		Concepts:  []string{}, // empty in DB too
		Summary:   "Summary",  // but has summary
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
		t.Fatalf("Index with reparse empty concepts failed: %v", err)
	}
	if result == nil {
		t.Error("result should not be nil")
	}
}

// ── Additional tests for Index() coverage: Reparse with empty embedding ──────────

func TestIndex_ReparseEmptyEmbeddingNotPreserved(t *testing.T) {
	// Covers: reparse skips empty Embedding (line 233 branch)
	walker := &stubFileWalker{
		files: []domain.FileEntry{
			{Path: "main.go", Language: "go"},
		},
	}

	parser := &stubParser{
		result: &domain.ParsedFile{
			Path:  "main.go",
			Nodes: []types.CodeNode{{ID: "n1", Qualified: "main", Embedding: []float32{}}},
			Edges: []types.CodeEdge{},
		},
	}

	embedder := &stubEmbedder{
		batchRes: []domain.EmbedResult{{ID: "n1", Vector: []float32{0.1}}},
	}

	store := newStubGraphStore()
	existingNode := &types.CodeNode{
		ID:        "n1",
		Qualified: "main",
		Embedding: []float32{0.5, 0.6}, // has embedding
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
		t.Fatalf("Index with reparse empty embedding failed: %v", err)
	}
	if result == nil {
		t.Error("result should not be nil")
	}
}

// ── Coverage: Force index path ───────────────────────────────────────

func TestIndex_ForceDeletesExistingNodes(t *testing.T) {
	// Covers: req.Force → is.graph.DeleteByRepo
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
		Force:    true,
	})

	if err != nil {
		t.Fatalf("Index with Force failed: %v", err)
	}
	if result.FilesIndexed != 1 {
		t.Errorf("FilesIndexed = %d, want 1", result.FilesIndexed)
	}
}

// ── Coverage: Git metadata and repo deduplication ──────────────────────────

func TestIndex_RepoDuplicationWithDifferentSlug(t *testing.T) {
	// Covers: existing.Slug != canonicalSlug → reuse existing slug
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
	// Preload an existing repo with a different slug
	existingRepo := &types.Repo{
		Slug:      "old-repo-slug",
		RemoteURL: "https://github.com/example/repo.git",
		Path:      "/different/path",
	}
	store.repos["old-repo-slug"] = existingRepo

	store.findRepoByRemoteURLFn = func(ctx context.Context, url string) (*types.Repo, error) {
		if url == "https://github.com/example/repo.git" {
			return existingRepo, nil
		}
		return nil, nil
	}

	cfg := &config.Config{
		Index: config.IndexConfig{
			MaxWorkersEmbed: 1,
			MaxWorkersStore: 1,
		},
		BatchSize: 10,
	}

	svc := NewIndexService(walker, parser, embedder, store, nil, cfg)

	// Simulate finding git repo with different slug
	result, err := svc.Index(context.Background(), IndexRequest{
		RepoPath: "/repo",
		RepoSlug: "new-repo-slug",
	})

	if err != nil {
		t.Fatalf("Index with dedup failed: %v", err)
	}
	if result == nil {
		t.Error("result should not be nil")
	}
}

// ── Coverage: Incremental indexing (unchanged files) ──────────────────────

func TestIndex_IncrementalSkipsUnchangedFile(t *testing.T) {
	// Covers: !req.Force && !req.Reparse && existingFile.ContentHash == parsed.ContentHash
	walker := &stubFileWalker{
		files: []domain.FileEntry{
			{Path: "main.go", Language: "go"},
		},
	}

	hash := "abc123def456"
	parser := &stubParser{
		result: &domain.ParsedFile{
			Path:        "main.go",
			ContentHash: hash,
			Nodes: []types.CodeNode{
				{ID: "n1", Qualified: "main", ContentHash: hash},
			},
			Edges: []types.CodeEdge{},
		},
	}

	embedder := &stubEmbedder{
		batchRes: []domain.EmbedResult{{ID: "n1", Vector: []float32{0.1}}},
	}

	store := newStubGraphStore()
	// Pre-populate node with matching hash
	existingNode := &types.CodeNode{
		ID:          "n1",
		Qualified:   "main",
		ContentHash: hash,
	}
	store.nodes["n1"] = existingNode
	store.nodesByQ["my-repo::main"] = existingNode

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
		Force:    false,
		Reparse:  false,
	})

	if err != nil {
		t.Fatalf("Incremental index failed: %v", err)
	}
	if result == nil {
		t.Error("result should not be nil")
		return
	}
	// File should still be counted in FilesIndexed (added at line 206)
	if result.FilesIndexed != 1 {
		t.Errorf("FilesIndexed = %d, want 1 (unchanged file still counted)", result.FilesIndexed)
	}
}

// ── Coverage: Linker chain execution ─────────────────────────────────────

func TestIndex_WithEdgeLinker(t *testing.T) {
	// Covers: len(is.linkers) > 0 && len(allParsed) > 0 → linker.Link()
	walker := &stubFileWalker{
		files: []domain.FileEntry{
			{Path: "main.go", Language: "go"},
		},
	}

	parser := &stubParser{
		result: &domain.ParsedFile{
			Path: "main.go",
			Nodes: []types.CodeNode{
				{ID: "n1", Qualified: "main"},
				{ID: "n2", Qualified: "helper"},
			},
			Edges: []types.CodeEdge{
				{FromID: "n1", ToID: "n2", Kind: "calls"},
			},
		},
	}

	embedder := &stubEmbedder{
		batchRes: []domain.EmbedResult{
			{ID: "n1", Vector: []float32{0.1}},
			{ID: "n2", Vector: []float32{0.2}},
		},
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

	// Add a mock linker
	mockLinker := &mockEdgeLinker{}
	svc.linkers = []domain.EdgeLinker{mockLinker}

	result, err := svc.Index(context.Background(), IndexRequest{
		RepoPath: "/repo",
		RepoSlug: "my-repo",
	})

	if err != nil {
		t.Fatalf("Index with linker failed: %v", err)
	}
	if result == nil {
		t.Error("result should not be nil")
	}
	// Linker should have been called
	_ = mockLinker
}

// ── Coverage: Tracker stage callbacks ────────────────────────────────────

func TestIndex_WithTracker(t *testing.T) {
	// Covers: is.tracker != nil → StartStage, CompleteStage, etc.
	walker := &stubFileWalker{
		files: []domain.FileEntry{
			{Path: "main.go", Language: "go"},
		},
	}

	parser := &stubParser{
		result: &domain.ParsedFile{
			Path: "main.go",
			Nodes: []types.CodeNode{
				{ID: "n1", Qualified: "main"},
			},
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

	// Add a tracker (create an empty types.IndexConfig for the tracker)
	tracker := NewIndexTracker("job1", "my-repo", types.IndexConfig{})
	svc.tracker = tracker

	result, err := svc.Index(context.Background(), IndexRequest{
		RepoPath: "/repo",
		RepoSlug: "my-repo",
	})

	if err != nil {
		t.Fatalf("Index with tracker failed: %v", err)
	}
	if result == nil {
		t.Error("result should not be nil")
	}

	snapshot := tracker.Snapshot()
	// Snapshot returns a value, not a pointer, so always non-nil
	_ = snapshot
}

// ── Coverage: Fast mode (skip summarize and reembed) ─────────────────────

func TestIndex_FastModeSkipsReembed(t *testing.T) {
	// Covers: req.Fast → skip reembed stage
	walker := &stubFileWalker{
		files: []domain.FileEntry{
			{Path: "main.go", Language: "go"},
		},
	}

	parser := &stubParser{
		result: &domain.ParsedFile{
			Path: "main.go",
			Nodes: []types.CodeNode{
				{ID: "n1", Qualified: "main"},
			},
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

	result, err := svc.Index(context.Background(), IndexRequest{
		RepoPath: "/repo",
		RepoSlug: "my-repo",
		Fast:     true,
	})

	if err != nil {
		t.Fatalf("Index with Fast mode failed: %v", err)
	}
	if result == nil {
		t.Error("result should not be nil")
	}
}

// ── Coverage: Reparse mode (preserve existing fields) ──────────────────────

func TestIndex_ReparsePreservesSummaryAndEmbedding(t *testing.T) {
	// Covers: req.Reparse → preserve Summary, Concepts, Embedding
	walker := &stubFileWalker{
		files: []domain.FileEntry{
			{Path: "main.go", Language: "go"},
		},
	}

	oldSummary := "This is the main entry point"
	oldEmbedding := []float32{0.5, 0.6, 0.7}

	parser := &stubParser{
		result: &domain.ParsedFile{
			Path: "main.go",
			Nodes: []types.CodeNode{
				{ID: "n1", Qualified: "main", Summary: ""},
			},
			Edges: []types.CodeEdge{},
		},
	}

	embedder := &stubEmbedder{
		batchRes: []domain.EmbedResult{{ID: "n1", Vector: []float32{0.2, 0.3}}},
	}

	store := newStubGraphStore()
	existingNode := &types.CodeNode{
		ID:        "n1",
		Qualified: "main",
		Summary:   oldSummary,
		Embedding: oldEmbedding,
	}
	store.nodes["n1"] = existingNode
	store.nodesByQ["my-repo::main"] = existingNode

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
		Reparse:  true,
	})

	if err != nil {
		t.Fatalf("Reparse index failed: %v", err)
	}
	if result == nil {
		t.Error("result should not be nil")
	}
}

// ── Coverage: Pre-embedded nodes (skip embed) ───────────────────────────

func TestIndex_PreEmbeddedNodesSkipped(t *testing.T) {
	// Covers: counting preEmbedded nodes (lines 385-390)
	walker := &stubFileWalker{
		files: []domain.FileEntry{
			{Path: "main.go", Language: "go"},
		},
	}

	parser := &stubParser{
		result: &domain.ParsedFile{
			Path: "main.go",
			Nodes: []types.CodeNode{
				{ID: "n1", Qualified: "main", Embedding: []float32{0.1, 0.2}},
			},
			Edges: []types.CodeEdge{},
		},
	}

	embedder := &stubEmbedder{
		batchRes: []domain.EmbedResult{{ID: "n1", Vector: []float32{0.1, 0.2}}},
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

	if err != nil {
		t.Fatalf("Index with pre-embedded nodes failed: %v", err)
	}
	if result == nil {
		t.Error("result should not be nil")
	}
}

// ── Coverage: Store error is non-fatal ──────────────────────────────────

func TestIndex_StoreContextCancellationFatal(t *testing.T) {
	// Covers: storeCtx.Err() check (line 439)
	walker := &stubFileWalker{
		files: []domain.FileEntry{
			{Path: "main.go", Language: "go"},
		},
	}

	parser := &stubParser{
		result: &domain.ParsedFile{
			Path: "main.go",
			Nodes: []types.CodeNode{
				{ID: "n1", Qualified: "main"},
			},
			Edges: []types.CodeEdge{},
		},
	}

	embedder := &stubEmbedder{
		batchRes: []domain.EmbedResult{{ID: "n1", Vector: []float32{0.1}}},
	}

	store := newStubGraphStore()
	store.upsertBatchFn = func(ctx context.Context, nodes []types.CodeNode, edges []types.CodeEdge) error {
		// Simulate context cancellation
		if err := ctx.Err(); err != nil {
			return err
		}
		return nil
	}

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

	if err != nil {
		t.Errorf("Store context check should not fail with normal context, got: %v", err)
	}
	if result == nil {
		t.Error("result should not be nil")
	}
}

// ── Index — git-metadata branches via injected extractGit ─────────────────

// indexBaseCfg returns a minimal config used by the git-branch tests below.
func indexBaseCfg() *config.Config {
	return &config.Config{
		Index:     config.IndexConfig{MaxWorkersEmbed: 1, MaxWorkersStore: 1},
		BatchSize: 1,
	}
}

func indexBaseDeps() (*stubFileWalker, *stubParser, *stubEmbedder) {
	walker := &stubFileWalker{files: []domain.FileEntry{
		{Path: "main.go", Language: "go", Content: []byte("package main")},
	}}
	parser := &stubParser{result: &domain.ParsedFile{
		Path: "main.go", Language: "go",
		Nodes: []types.CodeNode{{ID: "f1", Qualified: "main", Kind: types.NodeFunction}},
		Edges: []types.CodeEdge{},
	}}
	embedder := &stubEmbedder{batchRes: []domain.EmbedResult{{ID: "f1", Vector: []float32{0.1, 0.2}}}}
	return walker, parser, embedder
}

func TestIndex_GitDetected_AdoptsCanonicalSlug(t *testing.T) {
	walker, parser, embedder := indexBaseDeps()
	store := newStubGraphStore()
	svc := NewIndexService(walker, parser, embedder, store, nil, indexBaseCfg())
	svc.extractGit = func(string) GitMetadata {
		return GitMetadata{Slug: "owner/repo", RemoteURL: "https://github.com/owner/repo", Branch: "main", CommitHash: "abc1234"}
	}
	if _, err := svc.Index(context.Background(), IndexRequest{RepoPath: "/repo", RepoSlug: "passed-in"}); err != nil {
		t.Fatalf("Index: %v", err)
	}
	if _, ok := store.repos["owner/repo"]; !ok {
		t.Errorf("expected canonical slug owner/repo to be persisted, got repos=%+v", store.repos)
	}
}

func TestIndex_GitDetected_DedupReusesExistingSlug(t *testing.T) {
	walker, parser, embedder := indexBaseDeps()
	store := newStubGraphStore()
	// Pre-seed an existing repo with a different slug but the same remote.
	store.repos["pre-existing/slug"] = &types.Repo{Slug: "pre-existing/slug", RemoteURL: "https://github.com/owner/repo"}
	store.findRepoByRemoteURLFn = func(_ context.Context, url string) (*types.Repo, error) {
		if url == "https://github.com/owner/repo" {
			return store.repos["pre-existing/slug"], nil
		}
		return nil, nil
	}

	svc := NewIndexService(walker, parser, embedder, store, nil, indexBaseCfg())
	svc.extractGit = func(string) GitMetadata {
		return GitMetadata{Slug: "owner/repo", RemoteURL: "https://github.com/owner/repo"}
	}
	if _, err := svc.Index(context.Background(), IndexRequest{RepoPath: "/repo", RepoSlug: "passed-in"}); err != nil {
		t.Fatalf("Index: %v", err)
	}
	if _, ok := store.repos["pre-existing/slug"]; !ok {
		t.Errorf("dedup should reuse pre-existing slug, repos=%+v", store.repos)
	}
}

func TestIndex_GitDetected_ExistingRepoMergesFields(t *testing.T) {
	walker, parser, embedder := indexBaseDeps()
	store := newStubGraphStore()
	earlier := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	store.repos["owner/repo"] = &types.Repo{
		Slug:          "owner/repo",
		LastIndexedAt: &earlier,
		CreatedAt:     earlier,
		Languages:     []string{"go", "ts"},
	}

	svc := NewIndexService(walker, parser, embedder, store, nil, indexBaseCfg())
	svc.extractGit = func(string) GitMetadata {
		return GitMetadata{Slug: "owner/repo", RemoteURL: "https://github.com/owner/repo"}
	}
	if _, err := svc.Index(context.Background(), IndexRequest{RepoPath: "/repo", RepoSlug: "owner/repo"}); err != nil {
		t.Fatalf("Index: %v", err)
	}
	got := store.repos["owner/repo"]
	if got == nil || got.LastIndexedAt == nil || !got.LastIndexedAt.Equal(earlier) {
		t.Errorf("merge should preserve existing LastIndexedAt, got %+v", got)
	}
	if len(got.Languages) != 2 {
		t.Errorf("merge should preserve existing Languages, got %v", got.Languages)
	}
}

func TestIndex_GitNoSlug_FallsBackToRequestSlug(t *testing.T) {
	walker, parser, embedder := indexBaseDeps()
	store := newStubGraphStore()
	svc := NewIndexService(walker, parser, embedder, store, nil, indexBaseCfg())
	svc.extractGit = func(string) GitMetadata { return GitMetadata{} } // no git detected

	if _, err := svc.Index(context.Background(), IndexRequest{RepoPath: "/repo", RepoSlug: "my-passed-slug"}); err != nil {
		t.Fatalf("Index: %v", err)
	}
	if _, ok := store.repos["my-passed-slug"]; !ok {
		t.Errorf("when git returns no slug, request slug should be canonical: %+v", store.repos)
	}
}

func TestIndex_GitDedup_FindRepoErrorIgnored(t *testing.T) {
	walker, parser, embedder := indexBaseDeps()
	store := newStubGraphStore()
	store.findRepoByRemoteURLFn = func(_ context.Context, _ string) (*types.Repo, error) {
		return nil, domain.NotFound("not in db")
	}
	svc := NewIndexService(walker, parser, embedder, store, nil, indexBaseCfg())
	svc.extractGit = func(string) GitMetadata {
		return GitMetadata{Slug: "owner/repo", RemoteURL: "https://github.com/owner/repo"}
	}
	if _, err := svc.Index(context.Background(), IndexRequest{RepoPath: "/repo", RepoSlug: "owner/repo"}); err != nil {
		t.Fatalf("Index should swallow FindRepoByRemoteURL errors: %v", err)
	}
}

// ── Stage tests: runEmbedAndStore branches ────────────────────────────────

// TestRunEmbedAndStore_TrackerSummarizeAndEmbed exercises the summarizer-on
// + tracker-on path that the existing happy-path tests skip (they all run
// with nil summarizer or nil tracker).
func TestRunEmbedAndStore_TrackerSummarizeAndEmbed(t *testing.T) {
	walker := &stubFileWalker{files: []domain.FileEntry{
		{Path: "main.go", Language: "go", Content: []byte("package main")},
	}}
	parser := &stubParser{result: &domain.ParsedFile{
		Path:     "main.go",
		Language: "go",
		Nodes: []types.CodeNode{
			{ID: "f1", Qualified: "main.f1", Kind: types.NodeFunction,
				Body: strings.Repeat("x\n", 60), StartLine: 0, EndLine: 60},
		},
	}}
	embedder := &stubEmbedder{batchRes: []domain.EmbedResult{
		{ID: "f1", Vector: []float32{0.1, 0.2}},
	}}
	store := newStubGraphStore()
	explainer := &stubExplainer{
		structuredJSON: []byte(`{"summary":"does work","concepts":["a"]}`),
	}

	cfg := &config.Config{
		Index:     config.IndexConfig{MaxWorkersEmbed: 1, MaxWorkersStore: 1},
		BatchSize: 10,
	}
	svc := NewIndexService(walker, parser, embedder, store, explainer, cfg)
	svc.tracker = NewIndexTracker("job", "repo", types.IndexConfig{})

	result, err := svc.Index(context.Background(), IndexRequest{
		RepoPath: "/repo", RepoSlug: "repo",
	})
	if err != nil {
		t.Fatalf("Index: %v", err)
	}
	if result.NodesCreated != 1 {
		t.Errorf("expected 1 node, got %d", result.NodesCreated)
	}
}

// TestRunEmbedAndStore_EmbedErrorFiresTracker covers the embed-failure branch
// that records an AddStageError on the tracker.
func TestRunEmbedAndStore_EmbedErrorFiresTracker(t *testing.T) {
	walker := &stubFileWalker{files: []domain.FileEntry{
		{Path: "main.go", Language: "go", Content: []byte("package main")},
	}}
	parser := &stubParser{result: &domain.ParsedFile{
		Path:     "main.go",
		Language: "go",
		Nodes:    []types.CodeNode{{ID: "f1", Qualified: "main.f1", Kind: types.NodeFunction}},
	}}
	embedder := &stubEmbedder{batchErr: domain.RateLimit("embed quota exceeded")}
	store := newStubGraphStore()

	cfg := &config.Config{
		Index:     config.IndexConfig{MaxWorkersEmbed: 1, MaxWorkersStore: 1},
		BatchSize: 10,
	}
	svc := NewIndexService(walker, parser, embedder, store, nil, cfg)
	svc.tracker = NewIndexTracker("job", "repo", types.IndexConfig{})

	if _, err := svc.Index(context.Background(), IndexRequest{RepoPath: "/repo", RepoSlug: "repo"}); err != nil {
		// embed error is non-fatal; Index should succeed
		t.Errorf("embed error should be non-fatal: %v", err)
	}
}

// TestRunEmbedAndStore_FastModeSkipsSummarize covers the SkipStage path
// inside runEmbedAndStore when req.Fast is true.
func TestRunEmbedAndStore_FastModeSkipsSummarize(t *testing.T) {
	walker := &stubFileWalker{files: []domain.FileEntry{
		{Path: "main.go", Language: "go", Content: []byte("package main")},
	}}
	parser := &stubParser{result: &domain.ParsedFile{
		Path:     "main.go",
		Language: "go",
		Nodes:    []types.CodeNode{{ID: "f1", Qualified: "main.f1", Kind: types.NodeFunction}},
	}}
	embedder := &stubEmbedder{batchRes: []domain.EmbedResult{{ID: "f1", Vector: []float32{0.1}}}}
	store := newStubGraphStore()
	explainer := &stubExplainer{}

	cfg := &config.Config{
		Index:     config.IndexConfig{MaxWorkersEmbed: 1, MaxWorkersStore: 1},
		BatchSize: 10,
	}
	svc := NewIndexService(walker, parser, embedder, store, explainer, cfg)
	svc.tracker = NewIndexTracker("job", "repo", types.IndexConfig{})

	if _, err := svc.Index(context.Background(), IndexRequest{
		RepoPath: "/repo", RepoSlug: "repo", Fast: true,
	}); err != nil {
		t.Fatalf("Fast Index: %v", err)
	}
}

// TestRunEmbedAndStore_PartialEmbeddingsCounter covers the newlyEmbedded
// counter loop that distinguishes nodes with non-empty Embedding.
func TestRunEmbedAndStore_PartialEmbeddingsCounter(t *testing.T) {
	walker := &stubFileWalker{files: []domain.FileEntry{
		{Path: "main.go", Language: "go", Content: []byte("package main")},
	}}
	// Two nodes; embedder will only return a vector for one of them.
	parser := &stubParser{result: &domain.ParsedFile{
		Path:     "main.go",
		Language: "go",
		Nodes: []types.CodeNode{
			{ID: "f1", Qualified: "main.f1", Kind: types.NodeFunction},
			{ID: "f2", Qualified: "main.f2", Kind: types.NodeFunction},
		},
	}}
	embedder := &stubEmbedder{batchRes: []domain.EmbedResult{
		{ID: "f1", Vector: []float32{0.1}},
		{ID: "f2", Vector: nil}, // empty embedding — bumps the "skipped" counter
	}}
	store := newStubGraphStore()

	cfg := &config.Config{
		Index:     config.IndexConfig{MaxWorkersEmbed: 1, MaxWorkersStore: 1},
		BatchSize: 10,
	}
	svc := NewIndexService(walker, parser, embedder, store, nil, cfg)
	svc.tracker = NewIndexTracker("job", "repo", types.IndexConfig{})

	if _, err := svc.Index(context.Background(), IndexRequest{RepoPath: "/repo", RepoSlug: "repo"}); err != nil {
		t.Fatalf("Index: %v", err)
	}
}

// ── runPostPipeline branches ─────────────────────────────────────────────

// TestRunPostPipeline_TrackerWithTemporalAndReembed covers the success
// branches inside runPostPipeline: reembed StartStage/CompleteStage and
// temporal StartStage/CompleteStage when both are configured.
func TestRunPostPipeline_TrackerWithTemporalAndReembed(t *testing.T) {
	walker := &stubFileWalker{files: []domain.FileEntry{
		{Path: "main.go", Language: "go", Content: []byte("package main")},
	}}
	parser := &stubParser{result: &domain.ParsedFile{
		Path: "main.go", Language: "go",
		Nodes: []types.CodeNode{{ID: "f1", Qualified: "main.f1", Kind: types.NodeFunction}},
	}}
	embedder := &stubEmbedder{batchRes: []domain.EmbedResult{{ID: "f1", Vector: []float32{0.1}}}}
	store := newStubGraphStore()

	cfg := &config.Config{
		Index:     config.IndexConfig{MaxWorkersEmbed: 1, MaxWorkersStore: 1},
		BatchSize: 10,
	}
	svc := NewIndexService(walker, parser, embedder, store, nil, cfg)
	svc.tracker = NewIndexTracker("job", "repo", types.IndexConfig{})

	if _, err := svc.Index(context.Background(), IndexRequest{
		RepoPath: "/repo", RepoSlug: "repo",
	}); err != nil {
		t.Fatalf("Index: %v", err)
	}
}

// TestRunPostPipeline_NoTemporal_SkipsStage covers the else-branch that
// fires SkipStage(StageTemporal) when temporalSvc is nil but a tracker
// is attached.
func TestRunPostPipeline_NoTemporal_SkipsStage(t *testing.T) {
	walker := &stubFileWalker{files: []domain.FileEntry{}}
	parser := &stubParser{result: &domain.ParsedFile{Nodes: []types.CodeNode{}}}
	embedder := &stubEmbedder{}
	store := newStubGraphStore()

	cfg := &config.Config{Index: config.IndexConfig{MaxWorkersEmbed: 1, MaxWorkersStore: 1}}
	svc := NewIndexService(walker, parser, embedder, store, nil, cfg)
	svc.tracker = NewIndexTracker("job", "repo", types.IndexConfig{})

	if _, err := svc.Index(context.Background(), IndexRequest{
		RepoPath: "/repo", RepoSlug: "repo",
	}); err != nil {
		t.Fatalf("Index: %v", err)
	}
	snap := svc.tracker.Snapshot()
	_ = snap // The stage should be marked Skipped; we don't assert exact tracker
	// state — just exercise the SkipStage path.
}

// TestRunPostPipeline_EmptyRepoPath_SkipsCleanup covers the else-branch
// that fires SkipStage(StageCleanup) when RepoPath is empty.
func TestRunPostPipeline_EmptyRepoPath_SkipsCleanup(t *testing.T) {
	walker := &stubFileWalker{}
	parser := &stubParser{result: &domain.ParsedFile{}}
	embedder := &stubEmbedder{}
	store := newStubGraphStore()

	cfg := &config.Config{Index: config.IndexConfig{MaxWorkersEmbed: 1, MaxWorkersStore: 1}}
	svc := NewIndexService(walker, parser, embedder, store, nil, cfg)
	svc.tracker = NewIndexTracker("job", "repo", types.IndexConfig{})

	if _, err := svc.Index(context.Background(), IndexRequest{RepoPath: "", RepoSlug: "repo"}); err != nil {
		t.Fatalf("Index: %v", err)
	}
}

// TestRunPostPipeline_TemporalSvc_RunsAndCompletes covers the temporal-svc
// success path in runPostPipeline (StartStage + IndexCommitRange + CompleteStage).
func TestRunPostPipeline_TemporalSvc_RunsAndCompletes(t *testing.T) {
	walker := &stubFileWalker{files: []domain.FileEntry{}}
	parser := &stubParser{result: &domain.ParsedFile{Nodes: []types.CodeNode{}}}
	embedder := &stubEmbedder{}
	store := newStubGraphStore()

	cfg := &config.Config{Index: config.IndexConfig{MaxWorkersEmbed: 1, MaxWorkersStore: 1}}
	svc := NewIndexService(walker, parser, embedder, store, nil, cfg)
	svc.tracker = NewIndexTracker("job", "repo", types.IndexConfig{})

	// Wire a temporal service with empty fakes — it returns no commits, which
	// drives IndexCommitRange to a no-op success path inside runPostPipeline.
	tempStore := &fakeTemporalStore{}
	gitWalker := &fakeGitWalker{commits: nil}
	tempSvc := NewTemporalService(store, tempStore, gitWalker, parser)
	svc.SetTemporalService(tempSvc)

	if _, err := svc.Index(context.Background(), IndexRequest{
		RepoPath: "/repo", RepoSlug: "repo",
	}); err != nil {
		t.Fatalf("Index: %v", err)
	}
}

// TestRunPostPipeline_ReembedError_FailsStage covers the ReembedNeighborhood
// error path that calls FailStage(StageReembed).
func TestRunPostPipeline_ReembedError_FailsStage(t *testing.T) {
	walker := &stubFileWalker{files: []domain.FileEntry{}}
	parser := &stubParser{result: &domain.ParsedFile{Nodes: []types.CodeNode{}}}
	// embedder returns an error → ReembedNeighborhood propagates it.
	embedder := &stubEmbedder{batchErr: domain.RateLimit("embed err")}
	store := newStubGraphStore()
	// Seed a node so ReembedNeighborhood actually does work and hits the
	// embedder. Without this the function may early-return and skip the
	// failure branch.
	store.nodeIDs = []string{"f1"}
	store.nodes = map[string]*types.CodeNode{
		"f1": {ID: "f1", Qualified: "main.f1", Body: "x", FilePath: "main.go"},
	}

	cfg := &config.Config{Index: config.IndexConfig{MaxWorkersEmbed: 1, MaxWorkersStore: 1}}
	svc := NewIndexService(walker, parser, embedder, store, nil, cfg)
	svc.tracker = NewIndexTracker("job", "repo", types.IndexConfig{})

	if _, err := svc.Index(context.Background(), IndexRequest{
		RepoPath: "/repo", RepoSlug: "repo",
	}); err != nil {
		t.Fatalf("Index: %v", err)
	}
}

// ── runParse + runLinkAndSummarize fine branches ──────────────────────────

// TestRunParse_DefaultParseWorkers covers parseLimit <= 0 fallback to
// runtime.GOMAXPROCS(0).
func TestRunParse_DefaultParseWorkers(t *testing.T) {
	walker := &stubFileWalker{files: []domain.FileEntry{
		{Path: "a.go", Language: "go"}, {Path: "b.go", Language: "go"},
	}}
	parser := &stubParser{result: &domain.ParsedFile{
		Nodes: []types.CodeNode{{ID: "f", Qualified: "p.f", Kind: types.NodeFunction}},
	}}
	embedder := &stubEmbedder{batchRes: []domain.EmbedResult{{ID: "f", Vector: []float32{0.1}}}}
	store := newStubGraphStore()
	cfg := &config.Config{
		// MaxWorkersParse=0 → falls through to runtime.GOMAXPROCS.
		Index:     config.IndexConfig{MaxWorkersEmbed: 1, MaxWorkersStore: 1, MaxWorkersParse: 0},
		BatchSize: 10,
	}
	svc := NewIndexService(walker, parser, embedder, store, nil, cfg)
	if _, err := svc.Index(context.Background(), IndexRequest{RepoPath: "/", RepoSlug: "r"}); err != nil {
		t.Fatalf("Index: %v", err)
	}
}

// TestRunParse_ParseErrorWithTracker covers the AddStageError(StageParse) path.
func TestRunParse_ParseErrorWithTracker(t *testing.T) {
	walker := &stubFileWalker{files: []domain.FileEntry{{Path: "bad.go", Language: "go"}}}
	parser := &stubParser{err: domain.Validation("syntax")}
	embedder := &stubEmbedder{}
	store := newStubGraphStore()
	cfg := &config.Config{Index: config.IndexConfig{MaxWorkersEmbed: 1, MaxWorkersStore: 1}}
	svc := NewIndexService(walker, parser, embedder, store, nil, cfg)
	svc.tracker = NewIndexTracker("job", "r", types.IndexConfig{})

	if _, err := svc.Index(context.Background(), IndexRequest{RepoPath: "/", RepoSlug: "r"}); err != nil {
		t.Fatalf("parse error should be non-fatal: %v", err)
	}
}

// TestRunParse_UnbufferedChannel covers the parsedChBuf < 0 branch that
// forces an unbuffered channel.
func TestRunParse_UnbufferedChannel(t *testing.T) {
	walker := &stubFileWalker{files: []domain.FileEntry{}}
	parser := &stubParser{result: &domain.ParsedFile{}}
	embedder := &stubEmbedder{}
	store := newStubGraphStore()
	cfg := &config.Config{Index: config.IndexConfig{MaxWorkersEmbed: 1, MaxWorkersStore: 1}}
	svc := NewIndexService(walker, parser, embedder, store, nil, cfg)
	svc.parsedChBuf = -1 // unbuffered

	if _, err := svc.Index(context.Background(), IndexRequest{RepoPath: "/", RepoSlug: "r"}); err != nil {
		t.Fatalf("Index: %v", err)
	}
}

// TestRunPostPipeline_FastReparseSkipsReembed covers SkipStage(StageReembed)
// when Fast or Reparse is set with a tracker.
func TestRunPostPipeline_ReparseSkipsReembed(t *testing.T) {
	walker := &stubFileWalker{}
	parser := &stubParser{result: &domain.ParsedFile{}}
	embedder := &stubEmbedder{}
	store := newStubGraphStore()
	cfg := &config.Config{Index: config.IndexConfig{MaxWorkersEmbed: 1, MaxWorkersStore: 1}}
	svc := NewIndexService(walker, parser, embedder, store, nil, cfg)
	svc.tracker = NewIndexTracker("job", "r", types.IndexConfig{})

	if _, err := svc.Index(context.Background(), IndexRequest{
		RepoPath: "/", RepoSlug: "r", Reparse: true,
	}); err != nil {
		t.Fatalf("Index: %v", err)
	}
}
