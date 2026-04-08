package config

import (
	"testing"

	"github.com/commit0-dev/commit0/internal/domain"
)

func TestLoadDefaults(t *testing.T) {
	// Set required env var
	t.Setenv("GEMINI_API_KEY", "test-key")

	cfg, err := Load("")
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

	cfg, err := Load("")
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
	// Ensure no API keys or local provider bypasses are set.
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("EMBED_PROVIDER", "gemini")
	t.Setenv("OLLAMA_MODEL", "")
	t.Setenv("VOYAGE_API_KEY", "")

	_, err := Load("")
	if err == nil {
		t.Errorf("Load should fail with missing GEMINI_API_KEY")
	}

	domErr, ok := err.(*domain.DomainError)
	if !ok {
		t.Fatalf("error type = %T, want *DomainError", err)
	}

	if domErr.Code != domain.ErrValidation {
		t.Errorf("error code = %s, want validation", domErr.Code)
	}
}

func TestLoadInvalidInt(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "test-key")
	t.Setenv("GEMINI_EMBED_DIM", "not-a-number")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// Viper returns 0 for unparseable numeric env vars (no silent fallback to default).
	if cfg.Gemini.EmbedDimension != 0 {
		t.Errorf("Gemini.EmbedDimension = %d, want 0 for invalid value", cfg.Gemini.EmbedDimension)
	}
}

func TestLoadInvalidFloat(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "test-key")
	t.Setenv("QUERY_MIN_SCORE", "not-a-float")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// Viper returns 0 for unparseable numeric env vars (no silent fallback to default).
	if cfg.Query.MinScore != 0 {
		t.Errorf("Query.MinScore = %f, want 0 for invalid value", cfg.Query.MinScore)
	}
}

func TestConfigStructFields(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "test-key")
	cfg, err := Load("")
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

func TestServerConfigDefaults(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "test-key")
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.Server.Port != 8080 {
		t.Errorf("Server.Port = %d, want 8080", cfg.Server.Port)
	}

	if cfg.Server.ReadTimeoutSec != 30 {
		t.Errorf("Server.ReadTimeoutSec = %d, want 30", cfg.Server.ReadTimeoutSec)
	}

	if cfg.Server.WriteTimeoutSec != 120 {
		t.Errorf("Server.WriteTimeoutSec = %d, want 120", cfg.Server.WriteTimeoutSec)
	}

	if len(cfg.Server.CORSOrigins) == 0 {
		t.Error("Server.CORSOrigins should have at least one entry")
	}
}

func TestServerConfigEnvOverride(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "test-key")
	t.Setenv("SERVER_PORT", "9090")
	t.Setenv("SERVER_READ_TIMEOUT", "60")
	t.Setenv("SERVER_WRITE_TIMEOUT", "300")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.Server.Port != 9090 {
		t.Errorf("Server.Port = %d, want 9090", cfg.Server.Port)
	}

	if cfg.Server.ReadTimeoutSec != 60 {
		t.Errorf("Server.ReadTimeoutSec = %d, want 60", cfg.Server.ReadTimeoutSec)
	}

	if cfg.Server.WriteTimeoutSec != 300 {
		t.Errorf("Server.WriteTimeoutSec = %d, want 300", cfg.Server.WriteTimeoutSec)
	}
}
