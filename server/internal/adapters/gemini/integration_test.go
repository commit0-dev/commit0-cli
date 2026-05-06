//go:build integration

package gemini_test

import (
	"context"
	"os"
	"testing"

	"github.com/commit0-dev/commit0/server/internal/adapters/gemini"
	"github.com/commit0-dev/commit0/server/internal/config"
	"github.com/commit0-dev/commit0/server/internal/domain"
	"log/slog"
)

// geminiCfg reads Gemini settings from the environment.
// Tests are skipped (not failed) when GEMINI_API_KEY is unset.
func geminiCfg(t *testing.T) *config.GeminiConfig {
	t.Helper()
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		t.Skip("GEMINI_API_KEY not set — skipping Gemini integration tests")
	}
	return &config.GeminiConfig{
		APIKey:         apiKey,
		EmbedModel:     "gemini-embedding-2-preview",
		ExplainModel:   "gemini-2.0-flash",
		EmbedDimension: 768, // smaller dim for faster tests
		MaxBatchSize:   100,
	}
}

func TestGeminiEmbedderEmbedQuery(t *testing.T) {
	ctx := context.Background()
	cfg := geminiCfg(t)

	client, err := gemini.NewGeminiClient(ctx, cfg)
	if err != nil {
		t.Fatalf("NewGeminiClient: %v", err)
	}

	embedder, err := gemini.NewGeminiEmbedder(client, cfg, slog.Default())
	if err != nil {
		t.Fatalf("NewGeminiEmbedder: %v", err)
	}

	vec, err := embedder.EmbedQuery(ctx, "where is the authentication logic?")
	if err != nil {
		t.Fatalf("EmbedQuery: %v", err)
	}
	if len(vec) == 0 {
		t.Error("EmbedQuery returned empty vector")
	}
	t.Logf("EmbedQuery returned vector of dimension %d", len(vec))
}

func TestGeminiEmbedderEmbedBatch(t *testing.T) {
	ctx := context.Background()
	cfg := geminiCfg(t)

	client, err := gemini.NewGeminiClient(ctx, cfg)
	if err != nil {
		t.Fatalf("NewGeminiClient: %v", err)
	}

	embedder, err := gemini.NewGeminiEmbedder(client, cfg, slog.Default())
	if err != nil {
		t.Fatalf("NewGeminiEmbedder: %v", err)
	}

	inputs := []domain.EmbedInput{
		{ID: "node1", Text: "func Handler(w http.ResponseWriter, r *http.Request) {}"},
		{ID: "node2", Text: "func validateToken(token string) error { return nil }"},
	}

	results, err := embedder.EmbedBatch(ctx, inputs)
	if err != nil {
		t.Fatalf("EmbedBatch: %v", err)
	}
	if len(results) != len(inputs) {
		t.Fatalf("EmbedBatch: got %d results, want %d", len(results), len(inputs))
	}
	for i, r := range results {
		if r.ID != inputs[i].ID {
			t.Errorf("result[%d].ID = %q, want %q", i, r.ID, inputs[i].ID)
		}
		if len(r.Vector) == 0 {
			t.Errorf("result[%d].Vector is empty", i)
		}
	}
	t.Logf("EmbedBatch returned %d vectors of dimension %d", len(results), len(results[0].Vector))
}

func TestGeminiEmbedderEmptyQueryError(t *testing.T) {
	ctx := context.Background()
	cfg := geminiCfg(t)

	client, err := gemini.NewGeminiClient(ctx, cfg)
	if err != nil {
		t.Fatalf("NewGeminiClient: %v", err)
	}

	embedder, err := gemini.NewGeminiEmbedder(client, cfg, slog.Default())
	if err != nil {
		t.Fatalf("NewGeminiEmbedder: %v", err)
	}

	_, err = embedder.EmbedQuery(ctx, "   ")
	if err == nil {
		t.Error("expected error for empty query, got nil")
	}
}

func TestGeminiExplainerExplain(t *testing.T) {
	ctx := context.Background()
	cfg := geminiCfg(t)

	client, err := gemini.NewGeminiClient(ctx, cfg)
	if err != nil {
		t.Fatalf("NewGeminiClient: %v", err)
	}

	explainer, err := gemini.NewGeminiExplainer(client, cfg, slog.Default())
	if err != nil {
		t.Fatalf("NewGeminiExplainer: %v", err)
	}

	req := domain.ExplainRequest{
		UserQuery: "What does the Handler function do?",
		QueryType: "search",
		CodeContext: []domain.CodeExcerpt{
			{
				Qualified: "pkg.Handler",
				FilePath:  "main.go",
				Lines:     "10-20",
				Snippet:   "func Handler(w http.ResponseWriter, r *http.Request) {\n\tw.WriteHeader(200)\n}",
				Score:     0.95,
			},
		},
	}

	ch, err := explainer.Explain(ctx, req)
	if err != nil {
		t.Fatalf("Explain: %v", err)
	}

	var fullText string
	for chunk := range ch {
		if chunk.Error != nil {
			t.Fatalf("Explain chunk error: %v", chunk.Error)
		}
		fullText += chunk.Text
		if chunk.Done {
			break
		}
	}

	if fullText == "" {
		t.Error("Explain returned no text")
	}
	t.Logf("Explain response (%d chars): %s...", len(fullText), truncate(fullText, 100))
}

func TestGeminiExplainerEmptyQueryError(t *testing.T) {
	ctx := context.Background()
	cfg := geminiCfg(t)

	client, err := gemini.NewGeminiClient(ctx, cfg)
	if err != nil {
		t.Fatalf("NewGeminiClient: %v", err)
	}

	explainer, err := gemini.NewGeminiExplainer(client, cfg, slog.Default())
	if err != nil {
		t.Fatalf("NewGeminiExplainer: %v", err)
	}

	_, err = explainer.Explain(ctx, domain.ExplainRequest{UserQuery: ""})
	if err == nil {
		t.Error("expected error for empty UserQuery, got nil")
	}
}

// truncate returns s truncated to at most n runes.
func truncate(s string, n int) string {
	count := 0
	for i := range s {
		if count == n {
			return s[:i] + "..."
		}
		count++
	}
	return s
}
