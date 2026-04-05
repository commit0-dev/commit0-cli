package voyage

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/commit0-dev/commit0/internal/config"
	"github.com/commit0-dev/commit0/internal/domain"
	"github.com/commit0-dev/commit0/internal/infra/retry"
)

const (
	defaultModel     = "voyage-code-3"
	defaultDim       = 1024
	defaultBatchSize = 128
	maxBatchSize     = 128
	defaultBaseURL   = "https://api.voyageai.com/v1"
	maxRetryAttempts = 3
)

// VoyageEmbedder implements domain.Embedder using the Voyage AI REST API.
type VoyageEmbedder struct {
	apiKey  string
	baseURL string
	client  *http.Client
	log     *slog.Logger
	model   string
	dim     int
	batch   int
}

// Compile-time interface check.
var _ domain.Embedder = (*VoyageEmbedder)(nil)

// NewVoyageEmbedder constructs a VoyageEmbedder from config.
func NewVoyageEmbedder(cfg *config.VoyageConfig, log *slog.Logger) (*VoyageEmbedder, error) {
	if cfg.APIKey == "" {
		return nil, domain.Validation("VoyageEmbedder: API key must not be empty")
	}

	model := cfg.Model
	if model == "" {
		model = defaultModel
	}
	dim := cfg.EmbedDimension
	if dim <= 0 {
		dim = defaultDim
	}
	batch := cfg.MaxBatchSize
	if batch <= 0 || batch > maxBatchSize {
		batch = defaultBatchSize
	}
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	baseURL = strings.TrimRight(baseURL, "/")

	return &VoyageEmbedder{
		apiKey:  cfg.APIKey,
		baseURL: baseURL,
		client:  &http.Client{},
		log:     log,
		model:   model,
		dim:     dim,
		batch:   batch,
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

// callAPI makes a POST request to the Voyage AI embeddings endpoint with retry.
func (e *VoyageEmbedder) callAPI(ctx context.Context, texts []string, inputType string) ([][]float32, error) {
	reqBody := embeddingRequest{
		Model:           e.model,
		Input:           texts,
		InputType:       inputType,
		OutputDimension: &e.dim,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	var respBody embeddingResponse
	err = retry.WithRetry(ctx, maxRetryAttempts, func() error {
		req, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+"/embeddings", bytes.NewReader(bodyBytes))
		if reqErr != nil {
			return fmt.Errorf("create request: %w", reqErr)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+e.apiKey)

		resp, doErr := e.client.Do(req)
		if doErr != nil {
			return classifyError(doErr)
		}
		defer resp.Body.Close()

		respBytes, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return fmt.Errorf("read response: %w", readErr)
		}

		if resp.StatusCode != http.StatusOK {
			return classifyHTTPError(resp.StatusCode, respBytes)
		}

		if jsonErr := json.Unmarshal(respBytes, &respBody); jsonErr != nil {
			return fmt.Errorf("unmarshal response: %w", jsonErr)
		}
		return nil
	})
	if err != nil {
		return nil, err
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
