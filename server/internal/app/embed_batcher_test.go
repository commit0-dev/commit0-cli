package app

import (
	"context"
	"testing"

	"github.com/commit0-dev/commit0/server/internal/domain"
	"github.com/commit0-dev/commit0/pkg/types"
)

func TestEmbedBatcherDedup(t *testing.T) {
	embedder := &stubEmbedder{
		batchRes: []domain.EmbedResult{
			{ID: "1", Vector: []float32{0.1, 0.2}, Cached: false},
		},
	}

	batcher := NewEmbedBatcher(embedder, 100)
	ctx := context.Background()

	ok1, err1 := batcher.Add(ctx, domain.EmbedInput{ID: "1", Text: "hello"})
	if err1 != nil || !ok1 {
		t.Fatalf("first Add failed: %v", err1)
	}

	ok2, err2 := batcher.Add(ctx, domain.EmbedInput{ID: "1", Text: "hello"})
	if err2 != nil || ok2 {
		t.Errorf("second Add should be deduped, got ok=%v err=%v", ok2, err2)
	}

	batcher.Flush(ctx)

	if embedder.callCount != 1 {
		t.Errorf("embedder called %d times, want 1", embedder.callCount)
	}
}

func TestEmbedBatcherAutoFlush(t *testing.T) {
	embedder := &stubEmbedder{
		batchRes: []domain.EmbedResult{
			{ID: "1", Vector: []float32{0.1}, Cached: false},
			{ID: "2", Vector: []float32{0.2}, Cached: false},
			{ID: "3", Vector: []float32{0.3}, Cached: false},
		},
	}

	batcher := NewEmbedBatcher(embedder, 3) // batch size 3
	ctx := context.Background()

	batcher.Add(ctx, domain.EmbedInput{ID: "1", Text: "a"})
	batcher.Add(ctx, domain.EmbedInput{ID: "2", Text: "b"})

	if embedder.callCount != 0 {
		t.Errorf("should not auto-flush before reaching batch size, got %d calls", embedder.callCount)
	}

	// 3rd item triggers auto-flush
	batcher.Add(ctx, domain.EmbedInput{ID: "3", Text: "c"})

	if embedder.callCount != 1 {
		t.Errorf("should auto-flush at batch size, got %d calls", embedder.callCount)
	}
}

func TestEmbedBatcherAutoFlushError(t *testing.T) {
	// Auto-flush triggered by batchSize; embedder fails → Add returns the error
	embedder := &stubEmbedder{
		batchErr: domain.RateLimit("rate limited"),
	}

	batcher := NewEmbedBatcher(embedder, 2) // small batch size
	ctx := context.Background()

	batcher.Add(ctx, domain.EmbedInput{ID: "1", Text: "a"})
	_, err := batcher.Add(ctx, domain.EmbedInput{ID: "2", Text: "b"}) // triggers flush

	if err == nil {
		t.Errorf("Add should propagate embedder error on auto-flush")
	}
}

func TestEmbedBatcherManualFlush(t *testing.T) {
	embedder := &stubEmbedder{
		batchRes: []domain.EmbedResult{
			{ID: "1", Vector: []float32{0.1}, Cached: false},
			{ID: "2", Vector: []float32{0.2}, Cached: false},
		},
	}

	batcher := NewEmbedBatcher(embedder, 100)
	ctx := context.Background()

	batcher.Add(ctx, domain.EmbedInput{ID: "1", Text: "a"})
	batcher.Add(ctx, domain.EmbedInput{ID: "2", Text: "b"})

	if embedder.callCount != 0 {
		t.Errorf("should not auto-flush, got %d calls", embedder.callCount)
	}

	results, err := batcher.Flush(ctx)
	if err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	if len(results) != 2 {
		t.Errorf("Flush returned %d results, want 2", len(results))
	}

	if embedder.callCount != 1 {
		t.Errorf("Flush should call embedder once, got %d calls", embedder.callCount)
	}
}

func TestEmbedBatcherEmptyFlush(t *testing.T) {
	embedder := &stubEmbedder{}
	batcher := NewEmbedBatcher(embedder, 100)
	ctx := context.Background()

	results, err := batcher.Flush(ctx)
	if err != nil {
		t.Fatalf("empty Flush failed: %v", err)
	}

	if len(results) != 0 {
		t.Errorf("empty Flush should return 0 results, got %d", len(results))
	}

	if embedder.callCount != 0 {
		t.Errorf("empty Flush should not call embedder, got %d calls", embedder.callCount)
	}
}

func TestEmbedBatcherEmbedderFailure(t *testing.T) {
	embedder := &stubEmbedder{
		err: domain.RateLimit("too fast"),
	}

	batcher := NewEmbedBatcher(embedder, 100)
	ctx := context.Background()

	batcher.Add(ctx, domain.EmbedInput{ID: "1", Text: "a"})

	_, err := batcher.Flush(ctx)
	if err == nil {
		t.Errorf("Flush should propagate embedder error, got nil")
	}

	if _, ok := err.(*domain.DomainError); !ok {
		t.Errorf("error type = %T, want *DomainError", err)
	}
}

func TestEmbedBatcherResetAfterFlush(t *testing.T) {
	embedder := &stubEmbedder{
		batchRes: []domain.EmbedResult{
			{ID: "1", Vector: []float32{0.1}, Cached: false},
		},
	}

	batcher := NewEmbedBatcher(embedder, 100)
	ctx := context.Background()

	batcher.Add(ctx, domain.EmbedInput{ID: "1", Text: "a"})
	batcher.Flush(ctx)

	// Add again after flush should work (not deduped)
	ok, err := batcher.Add(ctx, domain.EmbedInput{ID: "1", Text: "a"})
	if err != nil || !ok {
		t.Errorf("Add after Flush failed: ok=%v err=%v", ok, err)
	}
}

func TestEmbedBatcherDefaultBatchSize(t *testing.T) {
	// batchSize <= 0 should default to 100
	embedder := &stubEmbedder{}
	batcher := NewEmbedBatcher(embedder, 0)
	if batcher.batchSize != 100 {
		t.Errorf("batchSize = %d, want 100 for 0 input", batcher.batchSize)
	}

	batcherNeg := NewEmbedBatcher(embedder, -5)
	if batcherNeg.batchSize != 100 {
		t.Errorf("batchSize = %d, want 100 for negative input", batcherNeg.batchSize)
	}
}

func TestEmbedBatcherProcessResultMapping(t *testing.T) {
	// Results with matching IDs → node embeddings are set
	embedder := &stubEmbedder{
		batchRes: []domain.EmbedResult{
			{ID: "node1", Vector: []float32{0.1, 0.2, 0.3}},
			{ID: "node2", Vector: []float32{0.4, 0.5, 0.6}},
		},
	}

	batcher := NewEmbedBatcher(embedder, 100)
	cb := NewContextBuilder(1000)
	ctx := context.Background()

	nodes := []types.CodeNode{
		{ID: "node1", Kind: types.NodeFunction, Qualified: "pkg.F1", Language: "go"},
		{ID: "node2", Kind: types.NodeFunction, Qualified: "pkg.F2", Language: "go"},
	}

	result, err := batcher.Process(ctx, nodes, cb)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	if len(result) != 2 {
		t.Fatalf("Process returned %d nodes, want 2", len(result))
	}

	// Verify embeddings were applied
	for _, n := range result {
		if len(n.Embedding) == 0 {
			t.Errorf("node %s has empty embedding after Process", n.ID)
		}
	}
}

func TestEmbedBatcherProcessEmptyNodes(t *testing.T) {
	embedder := &stubEmbedder{}
	batcher := NewEmbedBatcher(embedder, 100)
	cb := NewContextBuilder(1000)

	result, err := batcher.Process(context.Background(), []types.CodeNode{}, cb)
	if err != nil {
		t.Fatalf("Process with empty nodes failed: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("Process with empty nodes returned %d nodes, want 0", len(result))
	}
}

func TestEmbedBatcherProcessFlushError(t *testing.T) {
	// Flush fails → Process returns error
	embedder := &stubEmbedder{
		batchErr: domain.RateLimit("flush rate limited"),
	}

	batcher := NewEmbedBatcher(embedder, 100)
	cb := NewContextBuilder(1000)
	ctx := context.Background()

	nodes := []types.CodeNode{
		{ID: "n1", Kind: types.NodeFunction, Qualified: "pkg.F", Language: "go"},
	}

	_, err := batcher.Process(ctx, nodes, cb)
	if err == nil {
		t.Errorf("Process should fail when flush fails")
	}
}

func TestEmbedBatcherProcessAddError(t *testing.T) {
	// batchSize=1 means the first Add immediately triggers auto-flush.
	// Embedder fails → auto-flush fails → Add returns error → Process returns "add input" error.
	embedder := &stubEmbedder{
		batchErr: domain.RateLimit("add flush rate limited"),
	}

	batcher := NewEmbedBatcher(embedder, 1) // size 1 → auto-flush on first add
	cb := NewContextBuilder(1000)
	ctx := context.Background()

	nodes := []types.CodeNode{
		{ID: "n1", Kind: types.NodeFunction, Qualified: "pkg.F", Language: "go"},
	}

	_, err := batcher.Process(ctx, nodes, cb)
	if err == nil {
		t.Errorf("Process should fail when Add auto-flush fails")
	}
}
