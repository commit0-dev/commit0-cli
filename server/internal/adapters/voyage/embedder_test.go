package voyage

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"resty.dev/v3"

	"github.com/commit0-dev/commit0/server/internal/domain"
)

// ---------------------------------------------------------------------------
// NewVoyageEmbedder — constructor validation
// ---------------------------------------------------------------------------

func TestNewVoyageEmbedderEmptyAPIKey(t *testing.T) {
	_, err := NewVoyageEmbedder("", "", "", 0, 0, slog.Default())
	if err == nil {
		t.Fatal("expected error for empty API key, got nil")
	}
	var de *domain.DomainError
	if !errors.As(err, &de) {
		t.Fatalf("expected *domain.DomainError, got %T: %v", err, err)
	}
	if de.Code != domain.ErrValidation {
		t.Fatalf("expected code %q, got %q", domain.ErrValidation, de.Code)
	}
}

func TestNewVoyageEmbedderDefaultModel(t *testing.T) {
	emb, err := NewVoyageEmbedder("test-key", "", "", 0, 0, slog.Default())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if emb.model != "voyage-code-3" {
		t.Errorf("expected default model %q, got %q", "voyage-code-3", emb.model)
	}
}

func TestNewVoyageEmbedderDefaultDim(t *testing.T) {
	emb, err := NewVoyageEmbedder("test-key", "", "", 0, 0, slog.Default())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if emb.dim != 1024 {
		t.Errorf("expected default dim 1024, got %d", emb.dim)
	}
}

func TestNewVoyageEmbedderDefaultBatch(t *testing.T) {
	emb, err := NewVoyageEmbedder("test-key", "", "", 1024, 0, slog.Default())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if emb.batch != 128 {
		t.Errorf("expected default batch 128, got %d", emb.batch)
	}
}

func TestNewVoyageEmbedderOversizedBatchClamped(t *testing.T) {
	emb, err := NewVoyageEmbedder("test-key", "", "", 1024, 500, slog.Default())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if emb.batch != 128 {
		t.Errorf("expected batch clamped to 128, got %d", emb.batch)
	}
}

func TestNewVoyageEmbedderCustomValues(t *testing.T) {
	emb, err := NewVoyageEmbedder("test-key", "voyage-code-2", "https://custom.api.com/v1/", 512, 64, slog.Default())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if emb.model != "voyage-code-2" {
		t.Errorf("model = %q; want %q", emb.model, "voyage-code-2")
	}
	if emb.dim != 512 {
		t.Errorf("dim = %d; want 512", emb.dim)
	}
	if emb.batch != 64 {
		t.Errorf("batch = %d; want 64", emb.batch)
	}
	// baseURL is stored in the Resty client, not the struct — verify via the client.
	if got := emb.rc.BaseURL(); got != "https://custom.api.com/v1" {
		t.Errorf("baseURL = %q; want trailing slash stripped", got)
	}
}

// ---------------------------------------------------------------------------
// EmbedBatch — input validation
// ---------------------------------------------------------------------------

func stubEmbedder(batchSize int) *VoyageEmbedder {
	return &VoyageEmbedder{
		rc:    resty.New().SetBaseURL("http://localhost").SetAuthToken("test-key"),
		model: "voyage-code-3",
		dim:   1024,
		batch: batchSize,
		log:   slog.Default(),
	}
}

func TestEmbedBatchEmptyNil(t *testing.T) {
	emb := stubEmbedder(128)
	results, err := emb.EmbedBatch(context.Background(), nil)
	if err != nil {
		t.Fatalf("expected nil error for nil input, got %v", err)
	}
	if results != nil {
		t.Fatalf("expected nil results for nil input, got %v", results)
	}
}

func TestEmbedBatchEmptySlice(t *testing.T) {
	emb := stubEmbedder(128)
	results, err := emb.EmbedBatch(context.Background(), []domain.EmbedInput{})
	if err != nil {
		t.Fatalf("expected nil error for empty slice, got %v", err)
	}
	if results != nil {
		t.Fatalf("expected nil results for empty slice, got %v", results)
	}
}

func TestEmbedBatchOverLimit(t *testing.T) {
	emb := stubEmbedder(3)
	inputs := make([]domain.EmbedInput, 4)
	_, err := emb.EmbedBatch(context.Background(), inputs)
	if err == nil {
		t.Fatal("expected validation error for oversized batch, got nil")
	}
	var de *domain.DomainError
	if !errors.As(err, &de) {
		t.Fatalf("expected *domain.DomainError, got %T: %v", err, err)
	}
	if de.Code != domain.ErrValidation {
		t.Fatalf("expected code %q, got %q", domain.ErrValidation, de.Code)
	}
}

// ---------------------------------------------------------------------------
// EmbedQuery — input validation
// ---------------------------------------------------------------------------

func TestEmbedQueryEmptyString(t *testing.T) {
	emb := stubEmbedder(128)
	_, err := emb.EmbedQuery(context.Background(), "   ")
	if err == nil {
		t.Fatal("expected validation error for whitespace-only query, got nil")
	}
	var de *domain.DomainError
	if !errors.As(err, &de) {
		t.Fatalf("expected *domain.DomainError, got %T: %v", err, err)
	}
	if de.Code != domain.ErrValidation {
		t.Fatalf("expected code %q, got %q", domain.ErrValidation, de.Code)
	}
}

// ---------------------------------------------------------------------------
// EmbedBatch / EmbedQuery — canceled context
// ---------------------------------------------------------------------------

func TestEmbedBatchCancelledContext(t *testing.T) {
	emb := stubEmbedder(128)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	inputs := []domain.EmbedInput{
		{ID: "a", Text: "some text"},
	}
	_, err := emb.EmbedBatch(ctx, inputs)
	if err == nil {
		t.Fatal("expected error for canceled context, got nil")
	}
}

func TestEmbedQueryCancelledContext(t *testing.T) {
	emb := stubEmbedder(128)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := emb.EmbedQuery(ctx, "valid query")
	if err == nil {
		t.Fatal("expected error for canceled context, got nil")
	}
}

// ---------------------------------------------------------------------------
// classifyHTTPError
// ---------------------------------------------------------------------------

func TestClassifyHTTPError_RateLimit(t *testing.T) {
	err := classifyHTTPError(429, []byte(`{"detail":"rate limit exceeded"}`))
	var de *domain.DomainError
	if !errors.As(err, &de) {
		t.Fatalf("expected *domain.DomainError, got %T", err)
	}
	if de.Code != domain.ErrRateLimit {
		t.Fatalf("expected %q, got %q", domain.ErrRateLimit, de.Code)
	}
}

func TestClassifyHTTPError_Timeout(t *testing.T) {
	err := classifyHTTPError(408, []byte(`{}`))
	var de *domain.DomainError
	if !errors.As(err, &de) {
		t.Fatalf("expected *domain.DomainError, got %T", err)
	}
	if de.Code != domain.ErrTimeout {
		t.Fatalf("expected %q, got %q", domain.ErrTimeout, de.Code)
	}
}

func TestClassifyHTTPError_GatewayTimeout(t *testing.T) {
	err := classifyHTTPError(504, []byte(`gateway timeout`))
	var de *domain.DomainError
	if !errors.As(err, &de) {
		t.Fatalf("expected *domain.DomainError, got %T", err)
	}
	if de.Code != domain.ErrTimeout {
		t.Fatalf("expected %q, got %q", domain.ErrTimeout, de.Code)
	}
}

func TestClassifyHTTPError_Generic(t *testing.T) {
	err := classifyHTTPError(500, []byte(`internal server error`))
	var de *domain.DomainError
	if errors.As(err, &de) {
		t.Fatalf("did not expect *domain.DomainError for 500, got code %q", de.Code)
	}
}

// ---------------------------------------------------------------------------
// classifyError
// ---------------------------------------------------------------------------

func TestClassifyError_Nil(t *testing.T) {
	if got := classifyError(nil); got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestClassifyError_ContextDeadlineExceeded(t *testing.T) {
	err := classifyError(errors.New("context deadline exceeded"))
	var de *domain.DomainError
	if !errors.As(err, &de) {
		t.Fatalf("expected *domain.DomainError, got %T", err)
	}
	if de.Code != domain.ErrTimeout {
		t.Fatalf("expected %q, got %q", domain.ErrTimeout, de.Code)
	}
}

func TestClassifyError_ContextCanceled(t *testing.T) {
	original := errors.New("context canceled")
	got := classifyError(original)
	// Should be returned as-is (not retryable).
	if got != original {
		t.Fatalf("expected original error returned unchanged, got %v", got)
	}
}

func TestClassifyError_Generic(t *testing.T) {
	original := errors.New("some unrecognized error")
	got := classifyError(original)
	if got != original {
		t.Fatalf("expected original error returned unchanged, got %v", got)
	}
}

// ---------------------------------------------------------------------------
// EmbedBatch / EmbedQuery — end-to-end with httptest server
// ---------------------------------------------------------------------------

func fakeVoyageServer(t *testing.T, dim int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if r.URL.Path != "/embeddings" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-key" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		var req embeddingRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		data := make([]embeddingData, len(req.Input))
		for i := range req.Input {
			vec := make([]float32, dim)
			for j := range vec {
				vec[j] = float32(i+1) * 0.1
			}
			data[i] = embeddingData{Embedding: vec, Index: i}
		}

		resp := embeddingResponse{
			Data:  data,
			Usage: &usageInfo{TotalTokens: len(req.Input) * 10},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
}

func TestEmbedBatchEndToEnd(t *testing.T) {
	srv := fakeVoyageServer(t, 1024)
	defer srv.Close()

	emb := &VoyageEmbedder{
		rc:    resty.NewWithClient(srv.Client()).SetBaseURL(srv.URL).SetAuthToken("test-key"),
		model: "voyage-code-3",
		dim:   1024,
		batch: 128,
		log:   slog.Default(),
	}

	inputs := []domain.EmbedInput{
		{ID: "node1", Text: "func main() {}"},
		{ID: "node2", Text: "func helper() {}"},
	}

	results, err := emb.EmbedBatch(context.Background(), inputs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].ID != "node1" {
		t.Errorf("results[0].ID = %q; want %q", results[0].ID, "node1")
	}
	if results[1].ID != "node2" {
		t.Errorf("results[1].ID = %q; want %q", results[1].ID, "node2")
	}
	if len(results[0].Vector) != 1024 {
		t.Errorf("expected 1024-dim vector, got %d", len(results[0].Vector))
	}
}

func TestEmbedQueryEndToEnd(t *testing.T) {
	srv := fakeVoyageServer(t, 1024)
	defer srv.Close()

	emb := &VoyageEmbedder{
		rc:    resty.NewWithClient(srv.Client()).SetBaseURL(srv.URL).SetAuthToken("test-key"),
		model: "voyage-code-3",
		dim:   1024,
		batch: 128,
		log:   slog.Default(),
	}

	vec, err := emb.EmbedQuery(context.Background(), "where is JWT validation?")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vec) != 1024 {
		t.Errorf("expected 1024-dim vector, got %d", len(vec))
	}
}

func TestEmbedBatchServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"detail":"internal error"}`, http.StatusInternalServerError)
	}))
	defer srv.Close()

	emb := &VoyageEmbedder{
		rc:    resty.NewWithClient(srv.Client()).SetBaseURL(srv.URL).SetAuthToken("test-key"),
		model: "voyage-code-3",
		dim:   1024,
		batch: 128,
		log:   slog.Default(),
	}

	inputs := []domain.EmbedInput{{ID: "a", Text: "text"}}
	_, err := emb.EmbedBatch(context.Background(), inputs)
	if err == nil {
		t.Fatal("expected error for server 500, got nil")
	}
}

func TestEmbedBatchRateLimitRetry(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls <= 2 {
			http.Error(w, `{"detail":"rate limit"}`, http.StatusTooManyRequests)
			return
		}
		// Succeed on 3rd call.
		var req embeddingRequest
		json.NewDecoder(r.Body).Decode(&req)
		data := make([]embeddingData, len(req.Input))
		for i := range req.Input {
			data[i] = embeddingData{Embedding: make([]float32, 1024), Index: i}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(embeddingResponse{Data: data})
	}))
	defer srv.Close()

	rc := resty.NewWithClient(srv.Client()).
		SetBaseURL(srv.URL).
		SetAuthToken("test-key").
		SetRetryCount(3).
		SetRetryWaitTime(10 * time.Millisecond).
		SetRetryMaxWaitTime(50 * time.Millisecond).
		SetAllowNonIdempotentRetry(true)
	emb := &VoyageEmbedder{
		rc:    rc,
		model: "voyage-code-3",
		dim:   1024,
		batch: 128,
		log:   slog.Default(),
	}

	inputs := []domain.EmbedInput{{ID: "a", Text: "text"}}
	results, err := emb.EmbedBatch(context.Background(), inputs)
	if err != nil {
		t.Fatalf("expected success after retries, got: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if calls != 3 {
		t.Errorf("expected 3 API calls (2 retries + 1 success), got %d", calls)
	}
}
