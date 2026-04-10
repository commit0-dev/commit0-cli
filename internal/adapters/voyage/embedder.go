package voyage

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"resty.dev/v3"

	"github.com/commit0-dev/commit0/internal/domain"
)

const (
	defaultModel     = "voyage-code-3"
	defaultDim       = 1024
	defaultBatchSize = 128
	maxBatchSize     = 128
	defaultBaseURL   = "https://api.voyageai.com/v1"
)

// VoyageEmbedder implements domain.Embedder using the Voyage AI REST API.
type VoyageEmbedder struct {
	rc    *resty.Client
	log   *slog.Logger
	model string
	dim   int
	batch int
}

// Compile-time interface check.
var _ domain.Embedder = (*VoyageEmbedder)(nil)

// NewVoyageEmbedder constructs a VoyageEmbedder with the given parameters.
func NewVoyageEmbedder(apiKey, model, baseURL string, dim, batch int, log *slog.Logger) (*VoyageEmbedder, error) {
	if apiKey == "" {
		return nil, domain.Validation("VoyageEmbedder: API key must not be empty")
	}
	if model == "" {
		model = defaultModel
	}
	if dim <= 0 {
		dim = defaultDim
	}
	if batch <= 0 || batch > maxBatchSize {
		batch = defaultBatchSize
	}
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	baseURL = strings.TrimRight(baseURL, "/")

	rc := resty.New().
		SetBaseURL(baseURL).
		SetAuthToken(apiKey).
		SetTimeout(30 * time.Second).
		SetRetryCount(3).
		SetRetryWaitTime(100 * time.Millisecond).
		SetRetryMaxWaitTime(2 * time.Second).
		SetAllowNonIdempotentRetry(true)

	return &VoyageEmbedder{
		rc:    rc,
		log:   log,
		model: model,
		dim:   dim,
		batch: batch,
	}, nil
}

// embeddingRequest is the Voyage AI /embeddings request body.
type embeddingRequest struct {
	OutputDimension *int     `json:"output_dimension,omitempty"`
	Model           string   `json:"model"`
	InputType       string   `json:"input_type"`
	Input           []string `json:"input"`
}

// embeddingResponse is the Voyage AI /embeddings response body.
type embeddingResponse struct {
	Usage *usageInfo      `json:"usage,omitempty"`
	Data  []embeddingData `json:"data"`
}

type embeddingData struct {
	Embedding []float32 `json:"embedding"`
	Index     int       `json:"index"`
}

type usageInfo struct {
	TotalTokens int `json:"total_tokens"`
}

// apiError is the Voyage AI error response body.
type apiError struct {
	Detail string `json:"detail"`
}

// EmbedBatch embeds up to 128 inputs per call using input_type "document".
func (e *VoyageEmbedder) EmbedBatch(ctx context.Context, inputs []domain.EmbedInput) ([]domain.EmbedResult, error) {
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
		texts[i] = inp.Text
	}

	embeddings, err := e.callAPI(ctx, texts, "document")
	if err != nil {
		return nil, fmt.Errorf("voyage: EmbedBatch: %w", err)
	}

	if len(embeddings) != len(inputs) {
		return nil, fmt.Errorf(
			"voyage: EmbedBatch: expected %d embeddings, got %d",
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

// EmbedQuery embeds a single query string using input_type "query".
func (e *VoyageEmbedder) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
	if strings.TrimSpace(query) == "" {
		return nil, domain.Validation("EmbedQuery: query must not be empty")
	}

	embeddings, err := e.callAPI(ctx, []string{query}, "query")
	if err != nil {
		return nil, fmt.Errorf("voyage: EmbedQuery: %w", err)
	}

	if len(embeddings) == 0 {
		return nil, fmt.Errorf("voyage: EmbedQuery: empty embedding response")
	}

	e.log.DebugContext(ctx, "EmbedQuery complete",
		slog.String("model", e.model),
		slog.Int("dim", len(embeddings[0])),
	)
	return embeddings[0], nil
}

// callAPI makes a POST request to the Voyage AI embeddings endpoint.
// Resty handles retry with exponential backoff for 429/500+ responses.
func (e *VoyageEmbedder) callAPI(ctx context.Context, texts []string, inputType string) ([][]float32, error) {
	var respBody embeddingResponse
	resp, err := e.rc.R().
		SetContext(ctx).
		SetBody(embeddingRequest{
			Model:           e.model,
			Input:           texts,
			InputType:       inputType,
			OutputDimension: &e.dim,
		}).
		SetResult(&respBody).
		Post("/embeddings")
	if err != nil {
		return nil, classifyError(err)
	}

	if resp.IsError() {
		return nil, classifyHTTPError(resp.StatusCode(), resp.Bytes())
	}

	// Sort by index to guarantee order matches input.
	embeddings := make([][]float32, len(respBody.Data))
	for _, d := range respBody.Data {
		if d.Index < 0 || d.Index >= len(embeddings) {
			return nil, fmt.Errorf("invalid embedding index %d for %d inputs", d.Index, len(texts))
		}
		embeddings[d.Index] = d.Embedding
	}

	return embeddings, nil
}

// classifyError maps transport-level errors to domain error types.
func classifyError(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "context deadline exceeded"):
		return domain.Timeout(msg, err)
	case strings.Contains(msg, "context canceled"):
		return err // not retryable
	default:
		return err
	}
}

// classifyHTTPError maps HTTP status codes to domain error types.
func classifyHTTPError(status int, body []byte) error {
	detail := string(body)
	// Try to extract detail from JSON error response.
	var apiErr apiError
	if json.Unmarshal(body, &apiErr) == nil && apiErr.Detail != "" {
		detail = apiErr.Detail
	}

	msg := fmt.Sprintf("HTTP %d: %s", status, detail)
	switch {
	case status == http.StatusTooManyRequests:
		return domain.RateLimit(msg)
	case status == http.StatusRequestTimeout || status == http.StatusGatewayTimeout:
		return domain.Timeout(msg, nil)
	default:
		return fmt.Errorf("voyage API: %s", msg)
	}
}
