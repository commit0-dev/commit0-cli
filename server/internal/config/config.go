package config

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"

	"github.com/commit0-dev/commit0/server/internal/domain"
)

// Config holds all application configuration.
type Config struct {
	EmbedProvider string // "gemini" (default), "voyage", or "ollama"
	LLMProvider   string // "gemini" (default), "openrouter", or "ollama"
	EmbedDim      int    // Normalized HNSW dimension for all providers (default 1024, env: EMBED_DIM)
	BatchSize     int    // Embedding batch size for all providers (default 100, env: BATCH_SIZE)
	Surreal       SurrealConfig
	Gemini        GeminiConfig
	Voyage        VoyageConfig
	Ollama        OllamaConfig
	OpenRouter    OpenRouterConfig
	Server     ServerConfig
	Index      IndexConfig
	Query      QueryConfig
	Sync       SyncConfig
}

// SyncConfig holds settings for P2P graph sync.
type SyncConfig struct {
	Passphrase   string // env: SYNC_PASSPHRASE (shared secret for auth)
	QUICPort     int    // env: SYNC_QUIC_PORT (default: 9443)
	AutoDiscover bool   // env: SYNC_AUTO_DISCOVER (mDNS LAN discovery, default: false)
	AutoPull     bool   // env: SYNC_AUTO_PULL (auto-pull on notification, default: false)
	AutoPush     bool   // env: SYNC_AUTO_PUSH (auto-push after index, default: false)
	InstanceName  string // env: SYNC_INSTANCE_NAME (mDNS instance name, default: hostname)
	ConsulAddr    string // env: SYNC_CONSUL_ADDR (default: "127.0.0.1:8500")
	ConsulToken   string // env: SYNC_CONSUL_TOKEN (optional, for Consul ACL)
	DiscoveryMode string // env: SYNC_DISCOVERY_MODE ("consul" or "mdns", default: "consul")
}

// OpenRouterConfig holds settings for OpenRouter API (multi-model gateway).
type OpenRouterConfig struct {
	APIKey    string // env: OPENROUTER_API_KEY
	BaseURL   string // env: OPENROUTER_BASE_URL (default: https://openrouter.ai/api/v1)
	Model     string // env: OPENROUTER_MODEL (default: google/gemini-2.5-flash-preview)
	MaxTokens int    // env: OPENROUTER_MAX_TOKENS (default: 8192)
}

// OllamaConfig holds local Ollama settings for LLM and embeddings.
type OllamaConfig struct {
	URL        string // Ollama API URL (default: http://localhost:11434)
	Model      string // LLM model name (e.g. "gemma3:4b"). If set, uses local LLM.
	EmbedModel string // Embedding model (default: "nomic-embed-text")
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
	URL             string
	User            string
	Pass            string
	Namespace       string
	Database        string
	ConnectTimeoutS int // Max seconds to wait for initial connection (default 30)
	RPCTimeoutS     int // Max seconds per RPC call (default 300)
	StartupRetries  int // Number of retries for connection + schema on startup (default 5)
	ReadPoolSize    int // Number of read connections (default 8, env: SURREAL_READ_POOL)
	WritePoolSize   int // Number of write connections (default 4, env: SURREAL_WRITE_POOL)
}

// GeminiConfig holds Gemini API settings.
type GeminiConfig struct {
	APIKey       string
	EmbedModel   string
	ExplainModel string
}

// VoyageConfig holds Voyage AI API settings.
type VoyageConfig struct {
	APIKey  string
	Model   string
	BaseURL string
}

// IndexConfig holds indexing configuration.
type IndexConfig struct {
	MaxWorkersParse int
	MaxWorkersEmbed int
	MaxWorkersStore int
	MaxFileKB       int
}

// QueryConfig holds query configuration.
type QueryConfig struct {
	DefaultTopK  int
	MinScore     float64
	RRFKConstant int
}

// Load reads configuration via Viper (env vars, optional config file).
func Load(cfgPath string) (*Config, error) {
	LoadDotEnv()

	v := viper.New()

	setDefaults(v)

	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	bindEnvs(v)

	if cfgPath != "" {
		v.SetConfigFile(cfgPath)
		if err := v.ReadInConfig(); err != nil {
			return nil, fmt.Errorf("read config file %q: %w", cfgPath, err)
		}
	}

	cfg := &Config{
		Surreal: SurrealConfig{
			URL:           v.GetString("surreal.url"),
			User:          v.GetString("surreal.user"),
			Pass:          v.GetString("surreal.pass"),
			Namespace:     v.GetString("surreal.namespace"),
			Database:      v.GetString("surreal.database"),
			ReadPoolSize:  v.GetInt("surreal.read_pool_size"),
			WritePoolSize: v.GetInt("surreal.write_pool_size"),
		},
		EmbedProvider: v.GetString("embed.provider"),
		LLMProvider:   v.GetString("llm.provider"),
		EmbedDim:      v.GetInt("embed.dim"),
		BatchSize:     v.GetInt("embed.batch_size"),
		OpenRouter: OpenRouterConfig{
			APIKey:    v.GetString("openrouter.api_key"),
			BaseURL:   v.GetString("openrouter.base_url"),
			Model:     v.GetString("openrouter.model"),
			MaxTokens: v.GetInt("openrouter.max_tokens"),
		},
		Ollama: OllamaConfig{
			URL:        v.GetString("ollama.url"),
			Model:      v.GetString("ollama.model"),
			EmbedModel: v.GetString("ollama.embed_model"),
		},
		Gemini: GeminiConfig{
			APIKey:       v.GetString("gemini.api_key"),
			EmbedModel:   v.GetString("gemini.embed_model"),
			ExplainModel: v.GetString("gemini.explain_model"),
		},
		Voyage: VoyageConfig{
			APIKey:  v.GetString("voyage.api_key"),
			Model:   v.GetString("voyage.model"),
			BaseURL: v.GetString("voyage.base_url"),
		},
		Index: IndexConfig{
			MaxWorkersParse: v.GetInt("index.max_workers_parse"),
			MaxWorkersEmbed: v.GetInt("index.max_workers_embed"),
			MaxWorkersStore: v.GetInt("index.max_workers_store"),
			MaxFileKB:       v.GetInt("index.max_file_kb"),
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
		Sync: SyncConfig{
			Passphrase:   v.GetString("sync.passphrase"),
			QUICPort:     v.GetInt("sync.quic_port"),
			AutoDiscover: v.GetBool("sync.auto_discover"),
			AutoPull:     v.GetBool("sync.auto_pull"),
			AutoPush:     v.GetBool("sync.auto_push"),
			InstanceName:  v.GetString("sync.instance_name"),
			ConsulAddr:    v.GetString("sync.consul_addr"),
			ConsulToken:   v.GetString("sync.consul_token"),
			DiscoveryMode: v.GetString("sync.discovery_mode"),
		},
	}

	if cfg.Sync.QUICPort == 0 {
		cfg.Sync.QUICPort = 9443
	}

	// Validate API key for the selected provider.
	switch cfg.EmbedProvider {
	case "ollama":
		// Fully local — no cloud API keys needed for embeddings.
	case "voyage":
		if cfg.Voyage.APIKey == "" {
			return nil, domain.Validation("VOYAGE_API_KEY is required when EMBED_PROVIDER=voyage")
		}
	default:
		cfg.EmbedProvider = "gemini"
		if cfg.Gemini.APIKey == "" {
			if cfg.Ollama.Model != "" {
				return nil, domain.Validation("GEMINI_API_KEY is required for embeddings (or set EMBED_PROVIDER=ollama for fully local mode)")
			}
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
	v.SetDefault("embed.dim", 1024)
	v.SetDefault("embed.batch_size", 100)
	v.SetDefault("llm.provider", "gemini")

	v.SetDefault("openrouter.base_url", "https://openrouter.ai/api/v1")
	v.SetDefault("openrouter.model", "google/gemini-2.5-flash-preview")
	v.SetDefault("openrouter.max_tokens", 8192)

	v.SetDefault("ollama.url", "http://localhost:11434")
	v.SetDefault("ollama.model", "")
	v.SetDefault("ollama.embed_model", "nomic-embed-text")

	v.SetDefault("voyage.model", "voyage-code-3")
	v.SetDefault("voyage.base_url", "https://api.voyageai.com/v1")

	v.SetDefault("gemini.embed_model", "gemini-embedding-2-preview")
	v.SetDefault("gemini.explain_model", "gemini-2.5-flash")

	v.SetDefault("index.max_workers_parse", 0)
	v.SetDefault("index.max_workers_embed", 4)
	v.SetDefault("index.max_workers_store", 4)
	v.SetDefault("index.max_file_kb", 10000)

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
		"surreal.database":        "SURREAL_DATABASE",
		"surreal.read_pool_size":  "SURREAL_READ_POOL",
		"surreal.write_pool_size": "SURREAL_WRITE_POOL",

		"embed.provider":   "EMBED_PROVIDER",
		"embed.dim":        "EMBED_DIM",
		"embed.batch_size": "BATCH_SIZE",
		"llm.provider":     "LLM_PROVIDER",

		"openrouter.api_key":    "OPENROUTER_API_KEY",
		"openrouter.base_url":   "OPENROUTER_BASE_URL",
		"openrouter.model":      "OPENROUTER_MODEL",
		"openrouter.max_tokens": "OPENROUTER_MAX_TOKENS",

		"ollama.url":         "OLLAMA_URL",
		"ollama.model":       "OLLAMA_MODEL",
		"ollama.embed_model": "OLLAMA_EMBED_MODEL",

		"voyage.api_key": "VOYAGE_API_KEY",
		"voyage.model":   "VOYAGE_MODEL",
		"voyage.base_url": "VOYAGE_BASE_URL",

		"gemini.api_key":       "GEMINI_API_KEY",
		"gemini.embed_model":   "GEMINI_EMBED_MODEL",
		"gemini.explain_model": "GEMINI_EXPLAIN_MODEL",

		"index.max_workers_parse": "INDEX_WORKERS_PARSE",
		"index.max_workers_embed": "INDEX_WORKERS_EMBED",
		"index.max_workers_store": "INDEX_WORKERS_STORE",
		"index.max_file_kb":       "INDEX_MAX_FILE_KB",

		"query.default_top_k":  "QUERY_DEFAULT_TOP_K",
		"query.min_score":      "QUERY_MIN_SCORE",
		"query.rrf_k_constant": "QUERY_RRF_K",

		"server.port":              "SERVER_PORT",
		"server.cors_origins":      "SERVER_CORS_ORIGINS",
		"server.read_timeout_sec":  "SERVER_READ_TIMEOUT",
		"server.write_timeout_sec": "SERVER_WRITE_TIMEOUT",

		"sync.passphrase":    "SYNC_PASSPHRASE",
		"sync.quic_port":     "SYNC_QUIC_PORT",
		"sync.auto_discover": "SYNC_AUTO_DISCOVER",
		"sync.auto_pull":     "SYNC_AUTO_PULL",
		"sync.auto_push":     "SYNC_AUTO_PUSH",
		"sync.instance_name":  "SYNC_INSTANCE_NAME",
		"sync.consul_addr":    "SYNC_CONSUL_ADDR",
		"sync.consul_token":   "SYNC_CONSUL_TOKEN",
		"sync.discovery_mode": "SYNC_DISCOVERY_MODE",
	}
	for key, env := range envMap {
		v.MustBindEnv(key, env)
	}
}
