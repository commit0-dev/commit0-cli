package gemini

import (
	"context"
	"errors"
	"testing"

	"google.golang.org/genai"

	"log/slog"

	"github.com/commit0-dev/commit0/internal/config"
	"github.com/commit0-dev/commit0/internal/domain"
)

// ---------------------------------------------------------------------------
// ptr helper
// ---------------------------------------------------------------------------

func TestPtr_Int(t *testing.T) {
	p := ptr(42)
	if p == nil {
		t.Fatal("ptr(42) returned nil")
	}
	if *p != 42 {
		t.Fatalf("expected *p == 42, got %d", *p)
	}
}

func TestPtr_String(t *testing.T) {
	p := ptr("hello")
	if p == nil {
		t.Fatal("ptr(\"hello\") returned nil")
	}
	if *p != "hello" {
		t.Fatalf("expected *p == \"hello\", got %q", *p)
	}
}

// ---------------------------------------------------------------------------
// NewGeminiClient — nil/empty API key guard (no network call happens)
// ---------------------------------------------------------------------------

func TestNewGeminiClientNilAPIKey(t *testing.T) {
	ctx := context.Background()
	_, err := NewGeminiClient(ctx, &config.GeminiConfig{APIKey: ""})
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

// ---------------------------------------------------------------------------
// NewGeminiEmbedder — nil client guard
// ---------------------------------------------------------------------------

func TestNewGeminiEmbedderNilClient(t *testing.T) {
	cfg := &config.GeminiConfig{}
	_, err := NewGeminiEmbedder(nil, cfg, slog.Default())
	if err == nil {
		t.Fatal("expected error for nil client, got nil")
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
// NewGeminiExplainer — nil client guard
// ---------------------------------------------------------------------------

func TestNewGeminiExplainerNilClient(t *testing.T) {
	cfg := &config.GeminiConfig{}
	_, err := NewGeminiExplainer(nil, cfg, slog.Default())
	if err == nil {
		t.Fatal("expected error for nil client, got nil")
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
// classifyError
// ---------------------------------------------------------------------------

func TestClassifyError_Nil(t *testing.T) {
	if got := classifyError(nil); got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestClassifyError_RateLimit_429(t *testing.T) {
	err := classifyError(errors.New("server returned 429 Too Many Requests"))
	var de *domain.DomainError
	if !errors.As(err, &de) {
		t.Fatalf("expected *domain.DomainError, got %T", err)
	}
	if de.Code != domain.ErrRateLimit {
		t.Fatalf("expected %q, got %q", domain.ErrRateLimit, de.Code)
	}
}

func TestClassifyError_RateLimit_ResourceExhausted(t *testing.T) {
	err := classifyError(errors.New("RESOURCE_EXHAUSTED: quota exceeded"))
	var de *domain.DomainError
	if !errors.As(err, &de) {
		t.Fatalf("expected *domain.DomainError, got %T", err)
	}
	if de.Code != domain.ErrRateLimit {
		t.Fatalf("expected %q, got %q", domain.ErrRateLimit, de.Code)
	}
}

func TestClassifyError_RateLimit_RateLimitExceeded(t *testing.T) {
	err := classifyError(errors.New("rateLimitExceeded for project"))
	var de *domain.DomainError
	if !errors.As(err, &de) {
		t.Fatalf("expected *domain.DomainError, got %T", err)
	}
	if de.Code != domain.ErrRateLimit {
		t.Fatalf("expected %q, got %q", domain.ErrRateLimit, de.Code)
	}
}

func TestClassifyError_Timeout_DeadlineExceeded(t *testing.T) {
	err := classifyError(errors.New("DEADLINE_EXCEEDED: operation timed out"))
	var de *domain.DomainError
	if !errors.As(err, &de) {
		t.Fatalf("expected *domain.DomainError, got %T", err)
	}
	if de.Code != domain.ErrTimeout {
		t.Fatalf("expected %q, got %q", domain.ErrTimeout, de.Code)
	}
}

func TestClassifyError_Timeout_ContextDeadlineExceeded(t *testing.T) {
	err := classifyError(errors.New("context deadline exceeded"))
	var de *domain.DomainError
	if !errors.As(err, &de) {
		t.Fatalf("expected *domain.DomainError, got %T", err)
	}
	if de.Code != domain.ErrTimeout {
		t.Fatalf("expected %q, got %q", domain.ErrTimeout, de.Code)
	}
}

func TestClassifyError_Timeout_DeadlineExceededCamelCase(t *testing.T) {
	err := classifyError(errors.New("DeadlineExceeded"))
	var de *domain.DomainError
	if !errors.As(err, &de) {
		t.Fatalf("expected *domain.DomainError, got %T", err)
	}
	if de.Code != domain.ErrTimeout {
		t.Fatalf("expected %q, got %q", domain.ErrTimeout, de.Code)
	}
}

func TestClassifyError_Generic(t *testing.T) {
	original := errors.New("some unrecognized error")
	got := classifyError(original)
	// Should be returned as-is, not wrapped in a DomainError.
	if got != original {
		t.Fatalf("expected the original error to be returned unchanged, got %v", got)
	}
	var de *domain.DomainError
	if errors.As(got, &de) {
		t.Fatalf("did not expect *domain.DomainError for generic error, got code %q", de.Code)
	}
}

// ---------------------------------------------------------------------------
// EmbedBatch / EmbedQuery — input-validation paths only.
// We use a zero-value *genai.Client (non-nil pointer) so the constructor
// passes the nil-check, and we only exercise guards that return BEFORE the
// API call is made.
// ---------------------------------------------------------------------------

// zeroClient returns a non-nil *genai.Client without dialing the network.
func zeroClient() *genai.Client {
	return &genai.Client{}
}

func stubEmbedder(batchSize int) *GeminiEmbedder {
	return &GeminiEmbedder{
		client: zeroClient(),
		model:  "gemini-embedding-2-preview",
		dim:    3072,
		batch:  batchSize,
		log:    slog.Default(),
	}
}

func TestEmbedBatchEmptyNil(t *testing.T) {
	emb := stubEmbedder(100)
	results, err := emb.EmbedBatch(context.Background(), nil)
	if err != nil {
		t.Fatalf("expected nil error for nil input, got %v", err)
	}
	if results != nil {
		t.Fatalf("expected nil results for nil input, got %v", results)
	}
}

func TestEmbedBatchEmptySlice(t *testing.T) {
	emb := stubEmbedder(100)
	results, err := emb.EmbedBatch(context.Background(), []domain.EmbedInput{})
	if err != nil {
		t.Fatalf("expected nil error for empty slice, got %v", err)
	}
	if results != nil {
		t.Fatalf("expected nil results for empty slice, got %v", results)
	}
}

func TestEmbedBatchOverLimit(t *testing.T) {
	emb := stubEmbedder(3) // batch size is 3
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

func TestEmbedQueryEmptyString(t *testing.T) {
	emb := stubEmbedder(100)
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
// Explain — empty UserQuery guard (white-box struct construction)
// ---------------------------------------------------------------------------

func TestExplainEmptyUserQuery(t *testing.T) {
	e := &GeminiExplainer{
		client: zeroClient(),
		model:  "gemini-2.0-flash",
		log:    slog.Default(),
	}
	_, err := e.Explain(context.Background(), domain.ExplainRequest{UserQuery: ""})
	if err == nil {
		t.Fatal("expected validation error for empty UserQuery, got nil")
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
// EmbedBatch — canceled context covers content-building + error return paths
// ---------------------------------------------------------------------------

func TestEmbedBatchCancelledContext(t *testing.T) {
	emb := stubEmbedder(100)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel

	inputs := []domain.EmbedInput{
		{ID: "a", Text: "some text"},
	}
	_, err := emb.EmbedBatch(ctx, inputs)
	if err == nil {
		t.Fatal("expected error for canceled context, got nil")
	}
}

func TestEmbedBatchCancelledContextWithImages(t *testing.T) {
	emb := stubEmbedder(100)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Include image data to cover the image-attachment branch.
	inputs := []domain.EmbedInput{
		{
			ID:         "b",
			Text:       "image doc",
			Images:     [][]byte{[]byte("fakeimage")},
			ImageMIMEs: []string{"image/png"},
		},
	}
	_, err := emb.EmbedBatch(ctx, inputs)
	if err == nil {
		t.Fatal("expected error for canceled context, got nil")
	}
}

func TestEmbedBatchCancelledContextImageDefaultMIME(t *testing.T) {
	emb := stubEmbedder(100)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// ImageMIMEs slice shorter than Images → uses default mime "image/jpeg"
	inputs := []domain.EmbedInput{
		{
			ID:     "c",
			Text:   "another",
			Images: [][]byte{[]byte("data1"), []byte("data2")},
			// ImageMIMEs intentionally omitted / short
		},
	}
	_, err := emb.EmbedBatch(ctx, inputs)
	if err == nil {
		t.Fatal("expected error for canceled context, got nil")
	}
}

// ---------------------------------------------------------------------------
// EmbedQuery — canceled context covers content-building + error return paths
// ---------------------------------------------------------------------------

func TestEmbedQueryCancelledContext(t *testing.T) {
	emb := stubEmbedder(100)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := emb.EmbedQuery(ctx, "valid query")
	if err == nil {
		t.Fatal("expected error for canceled context, got nil")
	}
}

// ---------------------------------------------------------------------------
// NewGeminiEmbedder — default / custom model and batch size defaults
// ---------------------------------------------------------------------------

func TestNewGeminiEmbedderDefaultModel(t *testing.T) {
	cfg := &config.GeminiConfig{EmbedModel: ""}
	emb, err := NewGeminiEmbedder(zeroClient(), cfg, slog.Default())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if emb.model != "gemini-embedding-2-preview" {
		t.Errorf("expected default model, got %q", emb.model)
	}
}

func TestNewGeminiEmbedderDefaultDim(t *testing.T) {
	cfg := &config.GeminiConfig{EmbedDimension: 0}
	emb, err := NewGeminiEmbedder(zeroClient(), cfg, slog.Default())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if emb.dim != 3072 {
		t.Errorf("expected default dim 3072, got %d", emb.dim)
	}
}

func TestNewGeminiEmbedderDefaultBatch(t *testing.T) {
	cfg := &config.GeminiConfig{MaxBatchSize: 0}
	emb, err := NewGeminiEmbedder(zeroClient(), cfg, slog.Default())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if emb.batch != 100 {
		t.Errorf("expected default batch 100, got %d", emb.batch)
	}
}

func TestNewGeminiEmbedderOversizedBatchClamped(t *testing.T) {
	cfg := &config.GeminiConfig{MaxBatchSize: 200}
	emb, err := NewGeminiEmbedder(zeroClient(), cfg, slog.Default())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if emb.batch != 100 {
		t.Errorf("expected batch clamped to 100, got %d", emb.batch)
	}
}

func TestNewGeminiEmbedderCustomValues(t *testing.T) {
	cfg := &config.GeminiConfig{
		EmbedModel:     "custom-model",
		EmbedDimension: 512,
		MaxBatchSize:   50,
	}
	emb, err := NewGeminiEmbedder(zeroClient(), cfg, slog.Default())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if emb.model != "custom-model" {
		t.Errorf("model = %q; want %q", emb.model, "custom-model")
	}
	if emb.dim != 512 {
		t.Errorf("dim = %d; want 512", emb.dim)
	}
	if emb.batch != 50 {
		t.Errorf("batch = %d; want 50", emb.batch)
	}
}
