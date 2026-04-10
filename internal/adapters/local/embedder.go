package local

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"

	"resty.dev/v3"

	"github.com/commit0-dev/commit0/internal/domain"
)

const (
	defaultEmbedModel = "nomic-embed-text"
	defaultEmbedDim   = 768
	defaultEmbedBatch = 50
)

// modelPrefixes maps known Ollama embedding models to their document/query
// prefix conventions. Unknown models default to no prefix.
var modelPrefixes = map[string][2]string{
	"nomic-embed-text":       {"search_document: ", "search_query: "},
	"snowflake-arctic-embed": {"", ""},
	"mxbai-embed-large":      {"", "Represent this sentence for searching relevant passages: "},
	"all-minilm":             {"", ""},
}

// DocPrefixForModel returns the document embedding prefix for a known model.
// Returns empty string for unknown models.
func DocPrefixForModel(model string) string {
	// Strip tag (e.g. "nomic-embed-text:latest" → "nomic-embed-text")
	base := model
	if idx := strings.Index(model, ":"); idx > 0 {
		base = model[:idx]
	}
	if p, ok := modelPrefixes[base]; ok {
		return p[0]
	}
	return ""
}

// queryPrefixForModel returns the query embedding prefix for a known model.
func queryPrefixForModel(model string) string {
	base := model
	if idx := strings.Index(model, ":"); idx > 0 {
		base = model[:idx]
	}
	if p, ok := modelPrefixes[base]; ok {
		return p[1]
	}
	return ""
}

// OllamaEmbedder implements domain.Embedder using Ollama's POST /api/embed endpoint.
type OllamaEmbedder struct {
	rc        *resty.Client
	model     string
	dim       int
	docPrefix string
	qryPrefix string
	batch     int
	log       *slog.Logger
}

// Compile-time interface check.
var _ domain.Embedder = (*OllamaEmbedder)(nil)

// NewOllamaEmbedder creates an embedder backed by a local Ollama instance.
func NewOllamaEmbedder(baseURL, model string, dim int, log *slog.Logger) *OllamaEmbedder {
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	if model == "" {
		model = defaultEmbedModel
	}
	if dim <= 0 {
		dim = defaultEmbedDim
	}

	rc := resty.New().
		SetBaseURL(strings.TrimRight(baseURL, "/")).
		SetTimeout(30 * time.Second).
		SetRetryCount(3).
		SetRetryWaitTime(100 * time.Millisecond).
		SetRetryMaxWaitTime(2 * time.Second).
		SetAllowNonIdempotentRetry(true)

	return &OllamaEmbedder{
		rc:        rc,
		model:     model,
		dim:       dim,
		docPrefix: DocPrefixForModel(model),
		qryPrefix: queryPrefixForModel(model),
		batch:     defaultEmbedBatch,
		log:       log.With("adapter", "ollama-embed", "model", model),
	}
}

// ollamaEmbedRequest is the Ollama /api/embed request body.
type ollamaEmbedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

// ollamaEmbedResponse is the Ollama /api/embed response body.
type ollamaEmbedResponse struct {
	Embeddings [][]float32 `json:"embeddings"`
}

// EmbedBatch embeds a batch of inputs using Ollama's /api/embed endpoint.
func (e *OllamaEmbedder) EmbedBatch(ctx context.Context, inputs []domain.EmbedInput) ([]domain.EmbedResult, error) {
	if len(inputs) == 0 {
		return nil, nil
	}
	if len(inputs) > e.batch {
		return nil, domain.Validation(fmt.Sprintf(
			"EmbedBatch: input count %d exceeds maximum batch size %d", len(inputs), e.batch,
		))
	}

	texts := make([]string, len(inputs))
	for i, inp := range inputs {
		texts[i] = e.docPrefix + inp.Text
	}

	embeddings, err := e.callAPI(ctx, texts)
	if err != nil {
		return nil, fmt.Errorf("ollama embed: EmbedBatch: %w", err)
	}

	if len(embeddings) != len(inputs) {
		return nil, fmt.Errorf(
			"ollama embed: EmbedBatch: expected %d embeddings, got %d",
			len(inputs), len(embeddings),
		)
	}

	results := make([]domain.EmbedResult, len(inputs))
	for i, emb := range embeddings {
		results[i] = domain.EmbedResult{
			ID:     inputs[i].ID,
			Vector: emb,
		}
	}

	e.log.DebugContext(ctx, "EmbedBatch complete",
		slog.Int("count", len(results)),
		slog.String("model", e.model),
	)
	return results, nil
}

// EmbedQuery embeds a single query string using the query-time prefix.
func (e *OllamaEmbedder) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
	if strings.TrimSpace(query) == "" {
		return nil, domain.Validation("EmbedQuery: query must not be empty")
	}

	text := e.qryPrefix + query
	embeddings, err := e.callAPI(ctx, []string{text})
	if err != nil {
		return nil, fmt.Errorf("ollama embed: EmbedQuery: %w", err)
	}

	if len(embeddings) == 0 {
		return nil, fmt.Errorf("ollama embed: EmbedQuery: empty embedding response")
	}

	e.log.DebugContext(ctx, "EmbedQuery complete",
		slog.String("model", e.model),
		slog.Int("dim", len(embeddings[0])),
	)
	return embeddings[0], nil
}

// callAPI makes a POST request to Ollama's /api/embed endpoint.
// Resty handles retry with exponential backoff for transient failures.
func (e *OllamaEmbedder) callAPI(ctx context.Context, texts []string) ([][]float32, error) {
	var respBody ollamaEmbedResponse
	resp, err := e.rc.R().
		SetContext(ctx).
		SetBody(ollamaEmbedRequest{
			Model: e.model,
			Input: texts,
		}).
		SetResult(&respBody).
		Post("/api/embed")
	if err != nil {
		return nil, classifyEmbedError(err)
	}

	if resp.IsError() {
		return nil, fmt.Errorf("ollama embed API: HTTP %d: %s", resp.StatusCode(), resp.String())
	}

	// Normalize to target dimension if model output differs.
	// Zero-pad + L2 normalize for models that don't support dimension control.
	for i, vec := range respBody.Embeddings {
		respBody.Embeddings[i] = normalizeVector(vec, e.dim)
	}

	return respBody.Embeddings, nil
}

// normalizeVector pads or truncates a vector to targetDim, then L2-normalizes.
// For Matryoshka models, truncation preserves quality. For non-MRL models,
// zero-padding preserves direction in the original subspace.
func normalizeVector(vec []float32, targetDim int) []float32 {
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
	// L2 normalize
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

// classifyEmbedError maps transport-level errors to domain error types.
func classifyEmbedError(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "context deadline exceeded"):
		return domain.Timeout(msg, err)
	case strings.Contains(msg, "context canceled"):
		return err
	default:
		return err
	}
}
