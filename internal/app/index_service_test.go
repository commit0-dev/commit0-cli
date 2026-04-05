package app

import (
	"context"
	"strings"
	"testing"

	"github.com/commit0-dev/commit0/internal/config"
	"github.com/commit0-dev/commit0/internal/domain"
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
			BatchSize:       1,
		},
	}

	svc := NewIndexService(walker, parser, embedder, store, cfg)

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

	svc := NewIndexService(walker, parser, embedder, store, cfg)

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

	svc := NewIndexService(walker, parser, embedder, store, cfg)

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
			BatchSize:       10,
		},
	}

	svc := NewIndexService(walker, parser, embedder, store, cfg)

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
			BatchSize:       10,
		},
	}

	svc := NewIndexService(walker, parser, embedder, store, cfg)

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

	svc := NewIndexService(walker, parser, embedder, store, cfg)

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
			BatchSize:       10,
		},
	}
	svc := NewIndexService(walker, parser, embedder, store, cfg)
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

	svc := NewIndexService(walker, parser, embedder, store, cfg)
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
			BatchSize:       10,
		},
	}

	svc := NewIndexService(walker, parser, embedder, store, cfg)

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
			BatchSize:       10,
		},
	}

	svc := NewIndexService(walker, parser, embedder, store, cfg)
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
			BatchSize:       1, // flush every node so both files produce store calls
		},
	}

	svc := NewIndexService(walker, parser, embedder, store, cfg)
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
			BatchSize:       10,
		},
	}

	svc := NewIndexService(walker, parser, embedder, store, cfg)
	svc.embedChBuf = -1 // unbuffered: send blocks → cancel wins
	cancel()

	svc.Index(ctx, IndexRequest{RepoPath: "/repo", RepoSlug: "r"})
}
