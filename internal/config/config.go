package config

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"

	"github.com/commit0-dev/commit0/internal/domain"
)

// Config holds all application configuration.
type Config struct {
	EmbedProvider string // "gemini" (default) or "voyage"
	Surreal       SurrealConfig
	Gemini        GeminiConfig
	Voyage        VoyageConfig
	Server        ServerConfig
	Index         IndexConfig
	Query         QueryConfig
}

// ServerConfig holds HTTP server settings.
type ServerConfig struct {
	CORSOrigins     []string
	Port            int
	ReadTimeoutSec  int
	WriteTimeoutSec int
}

// SurrealConfig holds SurrealDB connection settings.
type SurrealConfig struct {
	URL              string
	User             string
	Pass             string
	Namespace        string
	Database         string
	ConnectTimeoutS  int // Max seconds to wait for initial connection (default 30)
	RPCTimeoutS      int // Max seconds per RPC call (default 300)
	StartupRetries   int // Number of retries for connection + schema on startup (default 5)
}

// GeminiConfig holds Gemini API settings.
type GeminiConfig struct {
	APIKey         string
	EmbedModel     string
	ExplainModel   string
	EmbedDimension int
	MaxBatchSize   int
}

// VoyageConfig holds Voyage AI API settings.
type VoyageConfig struct {
	APIKey         string
	Model          string
	BaseURL        string
	EmbedDimension int
	MaxBatchSize   int
}

// IndexConfig holds indexing configuration.
type IndexConfig struct {
	MaxWorkersParse int
	MaxWorkersEmbed int
	MaxWorkersStore int
	MaxFileKB       int
	BatchSize       int
}

// QueryConfig holds query configuration.
type QueryConfig struct {
	DefaultTopK  int
	MinScore     float64
	RRFKConstant int
}

// Load reads configuration via Viper (env vars, optional config file).
// If cfgPath is non-empty, that file is loaded (YAML, JSON, TOML supported).
// A .env file is auto-discovered by walking up from the working directory;
// real environment variables always take precedence over .env values.
func Load(cfgPath string) (*Config, error) {
	LoadDotEnv()

	v := viper.New()

	// --- Key bindings and defaults ---
	setDefaults(v)

	// Env vars: automatic binding with the same key names.
	// Viper normalises keys to lowercase; env vars are uppercased via SetEnvKeyReplacer.
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// Explicit env bindings (Viper key → env var name).
	bindEnvs(v)

	// --- Optional config file ---
	if cfgPath != "" {
		v.SetConfigFile(cfgPath)
		if err := v.ReadInConfig(); err != nil {
			return nil, fmt.Errorf("read config file %q: %w", cfgPath, err)
		}
	}

	cfg := &Config{
		Surreal: SurrealConfig{
			URL:       v.GetString("surreal.url"),
			User:      v.GetString("surreal.user"),
			Pass:      v.GetString("surreal.pass"),
			Namespace: v.GetString("surreal.namespace"),
			Database:  v.GetString("surreal.database"),
		},
		EmbedProvider: v.GetString("embed.provider"),
		Gemini: GeminiConfig{
			APIKey:         v.GetString("gemini.api_key"),
			EmbedModel:     v.GetString("gemini.embed_model"),
			ExplainModel:   v.GetString("gemini.explain_model"),
			EmbedDimension: v.GetInt("gemini.embed_dimension"),
			MaxBatchSize:   v.GetInt("gemini.max_batch_size"),
		},
		Voyage: VoyageConfig{
			APIKey:         v.GetString("voyage.api_key"),
			Model:          v.GetString("voyage.model"),
			EmbedDimension: v.GetInt("voyage.embed_dimension"),
			MaxBatchSize:   v.GetInt("voyage.max_batch_size"),
			BaseURL:        v.GetString("voyage.base_url"),
		},
		Index: IndexConfig{
			MaxWorkersParse: v.GetInt("index.max_workers_parse"),
			MaxWorkersEmbed: v.GetInt("index.max_workers_embed"),
			MaxWorkersStore: v.GetInt("index.max_workers_store"),
			MaxFileKB:       v.GetInt("index.max_file_kb"),
			BatchSize:       v.GetInt("index.batch_size"),
		},
		Query: QueryConfig{
			DefaultTopK:  v.GetInt("query.default_top_k"),
			MinScore:     v.GetFloat64("query.min_score"),
			RRFKConstant: v.GetInt("query.rrf_k_constant"),
		},
		Server: ServerConfig{
			Port:            v.GetInt("server.port"),
			CORSOrigins:     v.GetStringSlice("server.cors_origins"),
			ReadTimeoutSec:  v.GetInt("server.read_timeout_sec"),
			WriteTimeoutSec: v.GetInt("server.write_timeout_sec"),
		},
	}

	// Validate API key for the selected provider.
	switch cfg.EmbedProvider {
	case "voyage":
		if cfg.Voyage.APIKey == "" {
			return nil, domain.Validation("VOYAGE_API_KEY is required when EMBED_PROVIDER=voyage")
		}
	default:
		// "gemini" or empty (default)
		cfg.EmbedProvider = "gemini"
		if cfg.Gemini.APIKey == "" {
			return nil, domain.Validation("GEMINI_API_KEY is required")
		}
	}

	return cfg, nil
}

func setDefaults(v *viper.Viper) {
	v.SetDefault("surreal.url", "ws://localhost:8000")
	v.SetDefault("surreal.user", "root")
	v.SetDefault("surreal.pass", "root")
	v.SetDefault("surreal.namespace", "commit0")
	v.SetDefault("surreal.database", "codebase")
	v.SetDefault("surreal.connect_timeout_s", 30)
	v.SetDefault("surreal.rpc_timeout_s", 300)
	v.SetDefault("surreal.startup_retries", 5)

	v.SetDefault("embed.provider", "gemini")

	v.SetDefault("voyage.model", "voyage-code-3")
	v.SetDefault("voyage.embed_dimension", 1024)
	v.SetDefault("voyage.max_batch_size", 128)
	v.SetDefault("voyage.base_url", "https://api.voyageai.com/v1")

	v.SetDefault("gemini.embed_model", "gemini-embedding-2-preview")
	v.SetDefault("gemini.explain_model", "gemini-2.5-flash")
	v.SetDefault("gemini.embed_dimension", 3072)
	v.SetDefault("gemini.max_batch_size", 100)

	v.SetDefault("index.max_workers_parse", 0) // 0 = GOMAXPROCS
	v.SetDefault("index.max_workers_embed", 4)
	v.SetDefault("index.max_workers_store", 4)
	v.SetDefault("index.max_file_kb", 10000)
	v.SetDefault("index.batch_size", 100)

	v.SetDefault("query.default_top_k", 10)
	v.SetDefault("query.min_score", 0.5)
	v.SetDefault("query.rrf_k_constant", 60)

	v.SetDefault("server.port", 8080)
	v.SetDefault("server.cors_origins", []string{"*"})
	v.SetDefault("server.read_timeout_sec", 30)
	v.SetDefault("server.write_timeout_sec", 120)
}

// bindEnvs maps Viper keys to their canonical environment variable names.
func bindEnvs(v *viper.Viper) {
	envMap := map[string]string{
		"surreal.url":       "SURREAL_URL",
		"surreal.user":      "SURREAL_USER",
		"surreal.pass":      "SURREAL_PASS",
		"surreal.namespace": "SURREAL_NAMESPACE",
		"surreal.database":  "SURREAL_DATABASE",

		"embed.provider": "EMBED_PROVIDER",

		"voyage.api_key":         "VOYAGE_API_KEY",
		"voyage.model":           "VOYAGE_MODEL",
		"voyage.embed_dimension": "VOYAGE_EMBED_DIM",
		"voyage.max_batch_size":  "VOYAGE_BATCH_SIZE",
		"voyage.base_url":        "VOYAGE_BASE_URL",

		"gemini.api_key":         "GEMINI_API_KEY",
		"gemini.embed_model":     "GEMINI_EMBED_MODEL",
		"gemini.explain_model":   "GEMINI_EXPLAIN_MODEL",
		"gemini.embed_dimension": "GEMINI_EMBED_DIM",
		"gemini.max_batch_size":  "GEMINI_BATCH_SIZE",

		"index.max_workers_parse": "INDEX_WORKERS_PARSE",
		"index.max_workers_embed": "INDEX_WORKERS_EMBED",
		"index.max_workers_store": "INDEX_WORKERS_STORE",
		"index.max_file_kb":       "INDEX_MAX_FILE_KB",
		"index.batch_size":        "INDEX_BATCH_SIZE",

		"query.default_top_k":  "QUERY_DEFAULT_TOP_K",
		"query.min_score":      "QUERY_MIN_SCORE",
		"query.rrf_k_constant": "QUERY_RRF_K",

		"server.port":              "SERVER_PORT",
		"server.cors_origins":      "SERVER_CORS_ORIGINS",
		"server.read_timeout_sec":  "SERVER_READ_TIMEOUT",
		"server.write_timeout_sec": "SERVER_WRITE_TIMEOUT",
	}
	for key, env := range envMap {
		v.MustBindEnv(key, env)
	}
}
