package app

import (
	"context"
	"testing"

	"github.com/commit0-dev/commit0/internal/domain"
)

func TestEmbedBatcherDedup(t *testing.T) {
	embedder := &stubEmbedder{
		batchRes: []domain.EmbedResult{
			{ID: "1", Vector: []float32{0.1, 0.2}, Cached: false},
		},
	}

	batcher := NewEmbedBatcher(embedder, 100)
	ctx := context.Background()

	// Add same input twice
	ok1, err1 := batcher.Add(ctx, domain.EmbedInput{ID: "1", Text: "hello"})
	if err1 != nil || !ok1 {
		t.Fatalf("first Add failed: %v", err1)
	}

	// Second add should be deduped (return false)
	ok2, err2 := batcher.Add(ctx, domain.EmbedInput{ID: "1", Text: "hello"})
	if err2 != nil || ok2 {
		t.Errorf("second Add should be deduped, got ok=%v err=%v", ok2, err2)
	}

	// Flush to trigger embedder call
	batcher.Flush(ctx)

	// Should have called embedder only once (only one unique input)
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

	// Add 2 items
	batcher.Add(ctx, domain.EmbedInput{ID: "1", Text: "a"})
	batcher.Add(ctx, domain.EmbedInput{ID: "2", Text: "b"})

	if embedder.callCount != 0 {
		t.Errorf("should not auto-flush before reaching batch size, got %d calls", embedder.callCount)
	}

	// Add 3rd item - should trigger auto-flush
	batcher.Add(ctx, domain.EmbedInput{ID: "3", Text: "c"})

	if embedder.callCount != 1 {
		t.Errorf("should auto-flush at batch size, got %d calls", embedder.callCount)
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

	// Add items without reaching batch size
	batcher.Add(ctx, domain.EmbedInput{ID: "1", Text: "a"})
	batcher.Add(ctx, domain.EmbedInput{ID: "2", Text: "b"})

	if embedder.callCount != 0 {
		t.Errorf("should not auto-flush, got %d calls", embedder.callCount)
	}

	// Manual flush
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
