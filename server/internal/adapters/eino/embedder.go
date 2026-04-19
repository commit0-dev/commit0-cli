// Package eino implements adapters bridging CloudWeGo Eino components to commit0 domain interfaces.
package eino

import (
	"context"
	"fmt"
	"log/slog"
	"math"

	"github.com/cloudwego/eino/components/embedding"

	"github.com/commit0-dev/commit0/server/internal/domain"
)

// EinoEmbedder adapts an Eino embedding.Embedder to commit0's domain.Embedder.
// It bridges the type gap: Eino returns [][]float64, domain expects []float32.
type EinoEmbedder struct {
	inner embedding.Embedder
	dim   int
	log   *slog.Logger
}

// NewEinoEmbedder wraps an Eino Embedder for use with commit0's domain.Embedder interface.
func NewEinoEmbedder(inner embedding.Embedder, dim int, log *slog.Logger) *EinoEmbedder {
	return &EinoEmbedder{inner: inner, dim: dim, log: log}
}

// EmbedBatch implements domain.Embedder.
func (e *EinoEmbedder) EmbedBatch(ctx context.Context, inputs []domain.EmbedInput) ([]domain.EmbedResult, error) {
	if len(inputs) == 0 {
		return nil, nil
	}

	texts := make([]string, len(inputs))
	for i, inp := range inputs {
		texts[i] = inp.Text
	}

	vecs, err := e.inner.EmbedStrings(ctx, texts)
	if err != nil {
		return nil, fmt.Errorf("eino embed batch: %w", err)
	}

	results := make([]domain.EmbedResult, len(inputs))
	for i, inp := range inputs {
		if i >= len(vecs) {
			break
		}
		results[i] = domain.EmbedResult{
			ID:     inp.ID,
			Vector: normalizeVec(toFloat32(vecs[i]), e.dim),
		}
	}
	return results, nil
}

// EmbedQuery implements domain.Embedder.
func (e *EinoEmbedder) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
	if query == "" {
		return nil, fmt.Errorf("eino embed query: empty query")
	}

	vecs, err := e.inner.EmbedStrings(ctx, []string{query})
	if err != nil {
		return nil, fmt.Errorf("eino embed query: %w", err)
	}
	if len(vecs) == 0 {
		return nil, fmt.Errorf("eino embed query: no embedding returned")
	}

	return normalizeVec(toFloat32(vecs[0]), e.dim), nil
}

// compile-time check.
var _ domain.Embedder = (*EinoEmbedder)(nil)

// toFloat32 converts []float64 to []float32.
func toFloat32(f64 []float64) []float32 {
	out := make([]float32, len(f64))
	for i, v := range f64 {
		out[i] = float32(v)
	}
	return out
}

// normalizeVec truncates or zero-pads to targetDim, then L2-normalizes.
// Same logic as local/embedder.go normalizeVector — reused here to avoid
// cross-adapter import.
func normalizeVec(vec []float32, targetDim int) []float32 {
	if len(vec) == targetDim {
		return vec
	}
	if len(vec) > targetDim {
		vec = vec[:targetDim]
	} else {
		padded := make([]float32, targetDim)
		copy(padded, vec)
		vec = padded
	}
	var norm float32
	for _, v := range vec {
		norm += v * v
	}
	if norm > 0 {
		norm = float32(math.Sqrt(float64(norm)))
		for i := range vec {
			vec[i] /= norm
		}
	}
	return vec
}
