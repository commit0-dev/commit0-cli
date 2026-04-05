package config

import (
	"os"
	"testing"

	"github.com/commit0-dev/commit0/internal/domain"
)

func TestLoadDefaults(t *testing.T) {
	// Set required env var
	t.Setenv("GEMINI_API_KEY", "test-key")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.Surreal.URL != "ws://localhost:8000" {
		t.Errorf("Surreal.URL = %s, want ws://localhost:8000", cfg.Surreal.URL)
	}

	if cfg.Gemini.EmbedModel != "gemini-embedding-2-preview" {
		t.Errorf("Gemini.EmbedModel = %s, want gemini-embedding-2-preview", cfg.Gemini.EmbedModel)
	}

	if cfg.Gemini.EmbedDimension != 3072 {
		t.Errorf("Gemini.EmbedDimension = %d, want 3072", cfg.Gemini.EmbedDimension)
	}

	if cfg.Index.MaxWorkersEmbed != 4 {
		t.Errorf("Index.MaxWorkersEmbed = %d, want 4", cfg.Index.MaxWorkersEmbed)
	}
}

func TestLoadEnvOverrides(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "my-key")
	t.Setenv("SURREAL_URL", "wss://custom:8001")
	t.Setenv("GEMINI_EMBED_DIM", "1536")
	t.Setenv("INDEX_WORKERS_EMBED", "8")
	t.Setenv("QUERY_MIN_SCORE", "0.7")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.Surreal.URL != "wss://custom:8001" {
		t.Errorf("Surreal.URL = %s, want wss://custom:8001", cfg.Surreal.URL)
	}

	if cfg.Gemini.EmbedDimension != 1536 {
		t.Errorf("Gemini.EmbedDimension = %d, want 1536", cfg.Gemini.EmbedDimension)
	}

	if cfg.Index.MaxWorkersEmbed != 8 {
		t.Errorf("Index.MaxWorkersEmbed = %d, want 8", cfg.Index.MaxWorkersEmbed)
	}

	if cfg.Query.MinScore != 0.7 {
		t.Errorf("Query.MinScore = %.1f, want 0.7", cfg.Query.MinScore)
	}
}

func TestLoadMissingAPIKey(t *testing.T) {
	// Unset GEMINI_API_KEY
	os.Unsetenv("GEMINI_API_KEY")

	_, err := Load()
	if err == nil {
		t.Errorf("Load should fail with missing GEMINI_API_KEY")
	}

	domErr, ok := err.(*domain.DomainError)
	if !ok {
		t.Errorf("error type = %T, want *DomainError", err)
	}

	if domErr.Code != domain.ErrValidation {
		t.Errorf("error code = %s, want validation", domErr.Code)
	}
}

func TestLoadInvalidInt(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "test-key")
	t.Setenv("GEMINI_EMBED_DIM", "not-a-number")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load should use default for invalid int, got: %v", err)
	}

	// Should use default value for invalid int
	if cfg.Gemini.EmbedDimension != 3072 {
		t.Errorf("Gemini.EmbedDimension = %d, want 3072 (default)", cfg.Gemini.EmbedDimension)
	}
}

func TestLoadInvalidFloat(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "test-key")
	t.Setenv("QUERY_MIN_SCORE", "not-a-float")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load should use default for invalid float, got: %v", err)
	}

	if cfg.Query.MinScore != 0.5 {
		t.Errorf("Query.MinScore = %.1f, want 0.5 (default)", cfg.Query.MinScore)
	}
}

func TestConfigStructFields(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "test-key")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// Verify all config sections are populated
	if cfg.Surreal.URL == "" {
		t.Error("Surreal config not populated")
	}

	if cfg.Gemini.APIKey == "" {
		t.Error("Gemini APIKey not set")
	}

	if cfg.Index.MaxWorkersEmbed == 0 {
		t.Error("Index config not populated")
	}

	if cfg.Query.DefaultTopK == 0 {
		t.Error("Query config not populated")
	}
}
