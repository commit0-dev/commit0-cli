package gemini

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"google.golang.org/genai"

	"github.com/commit0-dev/commit0/internal/config"
	"github.com/commit0-dev/commit0/internal/domain"
	"github.com/commit0-dev/commit0/internal/infra/retry"
)

const (
	// queryPrefix uses the Gemini Embedding 2 code-retrieval task type, which is
	// optimized for matching natural-language questions to source code.
	// Document text uses the "title: … | text: …" convention and is produced by
	// ContextBuilder — no additional prefix is added at embed time.
	queryPrefix = "task: code retrieval | query: "

	maxRetryAttempts = 3
)

// ptr returns a pointer to the given value. Used for optional API fields.
func ptr[T any](v T) *T { return &v }

// NewGeminiClient creates a shared *genai.Client from the provided config.
// The caller is responsible for the lifetime of the returned client; it may be
// shared between GeminiEmbedder and GeminiExplainer.
func NewGeminiClient(ctx context.Context, cfg *config.GeminiConfig) (*genai.Client, error) {
	if cfg.APIKey == "" {
		return nil, domain.Validation("GeminiConfig.APIKey must not be empty")
	}
	client, err := genai.NewClient(ctx, &genai.ClientConfig{APIKey: cfg.APIKey})
	if err != nil {
		return nil, fmt.Errorf("gemini: create client: %w", err)
	}
	return client, nil
}

// GeminiEmbedder implements domain.Embedder using the Gemini Embedding 2 model.
type GeminiEmbedder struct {
	client *genai.Client
	log    *slog.Logger
	model  string
	dim    int
	batch  int
}

// Compile-time interface check.
var _ domain.Embedder = (*GeminiEmbedder)(nil)

// NewGeminiEmbedder constructs a GeminiEmbedder with the given parameters.
func NewGeminiEmbedder(client *genai.Client, model string, dim, batch int, log *slog.Logger) (*GeminiEmbedder, error) {
	if client == nil {
		return nil, domain.Validation("GeminiEmbedder: client must not be nil")
	}
	if model == "" {
		model = "gemini-embedding-2-preview"
	}
	if dim <= 0 {
		dim = 1024
	}
	if batch <= 0 || batch > 100 {
		batch = 100
	}
	return &GeminiEmbedder{
		client: client,
		model:  model,
		dim:    dim,
		batch:  batch,
		log:    log,
	}, nil
}

// EmbedBatch embeds up to 100 inputs per call. Inputs are sent as a single
// batched EmbedContent request; the index prefix is prepended to each text.
// Each input may optionally include image data (via Images + ImageMIMEs).
func (e *GeminiEmbedder) EmbedBatch(ctx context.Context, inputs []domain.EmbedInput) ([]domain.EmbedResult, error) {
	if len(inputs) == 0 {
		return nil, nil
	}
	if len(inputs) > e.batch {
		return nil, domain.Validation(fmt.Sprintf(
			"EmbedBatch: input count %d exceeds maximum batch size %d", len(inputs), e.batch,
		))
	}

	contents := make([]*genai.Content, len(inputs))
	for i, inp := range inputs {
		parts := []*genai.Part{
			genai.NewPartFromText(inp.Text),
		}
		// Attach inline images when present.
		for j, imgBytes := range inp.Images {
			mime := "image/jpeg"
			if j < len(inp.ImageMIMEs) && inp.ImageMIMEs[j] != "" {
				mime = inp.ImageMIMEs[j]
			}
			parts = append(parts, genai.NewPartFromBytes(imgBytes, mime))
		}
		contents[i] = &genai.Content{Parts: parts}
	}

	cfg := &genai.EmbedContentConfig{
		OutputDimensionality: ptr(int32(e.dim)),
	}

	var resp *genai.EmbedContentResponse
	err := retry.WithRetry(ctx, maxRetryAttempts, func() error {
		var apiErr error
		resp, apiErr = e.client.Models.EmbedContent(ctx, e.model, contents, cfg)
		if apiErr != nil {
			return classifyError(apiErr)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("gemini: EmbedBatch: %w", err)
	}

	if len(resp.Embeddings) != len(inputs) {
		return nil, fmt.Errorf(
			"gemini: EmbedBatch: expected %d embeddings, got %d",
			len(inputs), len(resp.Embeddings),
		)
	}

	results := make([]domain.EmbedResult, len(inputs))
	for i, emb := range resp.Embeddings {
		results[i] = domain.EmbedResult{
			ID:     inputs[i].ID,
			Vector: emb.Values,
		}
	}

	e.log.DebugContext(ctx, "EmbedBatch complete",
		slog.Int("count", len(results)),
		slog.String("model", e.model),
	)
	return results, nil
}

// EmbedQuery embeds a single query string using the query-time task prefix.
func (e *GeminiEmbedder) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
	if strings.TrimSpace(query) == "" {
		return nil, domain.Validation("EmbedQuery: query must not be empty")
	}

	content := &genai.Content{
		Parts: []*genai.Part{genai.NewPartFromText(queryPrefix + query)},
	}
	cfg := &genai.EmbedContentConfig{
		OutputDimensionality: ptr(int32(e.dim)),
	}

	var resp *genai.EmbedContentResponse
	err := retry.WithRetry(ctx, maxRetryAttempts, func() error {
		var apiErr error
		resp, apiErr = e.client.Models.EmbedContent(ctx, e.model, []*genai.Content{content}, cfg)
		if apiErr != nil {
			return classifyError(apiErr)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("gemini: EmbedQuery: %w", err)
	}

	if len(resp.Embeddings) == 0 || resp.Embeddings[0] == nil {
		return nil, fmt.Errorf("gemini: EmbedQuery: empty embedding response")
	}

	e.log.DebugContext(ctx, "EmbedQuery complete",
		slog.String("model", e.model),
		slog.Int("dim", len(resp.Embeddings[0].Values)),
	)
	return resp.Embeddings[0].Values, nil
}

// classifyError maps Gemini API errors to domain error types.
// Rate-limit (429) and deadline-exceeded errors are wrapped so that the retry
// layer knows they are transient.
func classifyError(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "429") ||
		strings.Contains(msg, "RESOURCE_EXHAUSTED") ||
		strings.Contains(msg, "rateLimitExceeded"):
		return domain.RateLimit(msg)
	case strings.Contains(msg, "DEADLINE_EXCEEDED") ||
		strings.Contains(msg, "context deadline exceeded") ||
		strings.Contains(msg, "DeadlineExceeded"):
		return domain.Timeout(msg, err)
	default:
		return err
	}
}
