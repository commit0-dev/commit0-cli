package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"

	"github.com/commit0-dev/commit0/internal/domain"
	"github.com/commit0-dev/commit0/pkg/types"
)

// EmbedBatcher accumulates embedding inputs and batches API calls.
type EmbedBatcher struct {
	embedder  domain.Embedder
	seen      map[string]bool
	pending   []domain.EmbedInput
	batchSize int
	mu        sync.Mutex
}

// NewEmbedBatcher creates a new embedding batcher.
func NewEmbedBatcher(embedder domain.Embedder, batchSize int) *EmbedBatcher {
	if batchSize <= 0 {
		batchSize = 100
	}
	return &EmbedBatcher{
		embedder:  embedder,
		batchSize: batchSize,
		seen:      make(map[string]bool),
		pending:   make([]domain.EmbedInput, 0, batchSize),
	}
}

// Add adds an input to the batcher, auto-flushing if needed.
func (eb *EmbedBatcher) Add(ctx context.Context, input domain.EmbedInput) (bool, error) {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	// Compute SHA-256 hash if not provided
	if input.ContentHash == "" {
		hash := sha256.Sum256([]byte(input.Text))
		input.ContentHash = hex.EncodeToString(hash[:])
	}

	// Skip if already seen in this batch
	if eb.seen[input.ContentHash] {
		return false, nil
	}

	// Mark as seen
	eb.seen[input.ContentHash] = true

	// Add to pending
	eb.pending = append(eb.pending, input)

	// Auto-flush if batch is full
	if len(eb.pending) >= eb.batchSize {
		_, err := eb.flushLocked(ctx)
		return true, err
	}

	return true, nil
}

// Flush explicitly flushes pending inputs.
func (eb *EmbedBatcher) Flush(ctx context.Context) ([]domain.EmbedResult, error) {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	return eb.flushLocked(ctx)
}

// flushLocked flushes without acquiring lock (caller must hold lock).
func (eb *EmbedBatcher) flushLocked(ctx context.Context) ([]domain.EmbedResult, error) {
	if len(eb.pending) == 0 {
		return []domain.EmbedResult{}, nil
	}

	results, err := eb.embedder.EmbedBatch(ctx, eb.pending)

	// Reset for next batch
	eb.pending = make([]domain.EmbedInput, 0, eb.batchSize)
	eb.seen = make(map[string]bool)

	return results, err
}

// Process is a convenience method to embed a batch of nodes.
func (eb *EmbedBatcher) Process(ctx context.Context, nodes []types.CodeNode, builder *ContextBuilder) ([]types.CodeNode, error) {
	// Build inputs from nodes (use ForNodeCtx for graph-neighborhood enrichment
	// when a GraphStore is attached to the builder; falls back to ForNode otherwise)
	for _, node := range nodes {
		text := builder.ForNodeCtx(ctx, &node)
		hash := sha256.Sum256([]byte(text))

		input := domain.EmbedInput{
			ID:          node.ID,
			Text:        text,
			ContentHash: hex.EncodeToString(hash[:]),
		}

		_, err := eb.Add(ctx, input)
		if err != nil {
			return nil, fmt.Errorf("add input: %w", err)
		}
	}

	// Flush all pending inputs
	results, err := eb.Flush(ctx)
	if err != nil {
		return nil, fmt.Errorf("flush embeddings: %w", err)
	}

	// Apply results back to nodes by ID
	resultMap := make(map[string]domain.EmbedResult)
	for _, r := range results {
		resultMap[r.ID] = r
	}

	for i := range nodes {
		if result, exists := resultMap[nodes[i].ID]; exists {
			nodes[i].Embedding = result.Vector
		}
	}

	return nodes, nil
}
