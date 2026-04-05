package config

import (
	"os"
	"strconv"

	"github.com/commit0-dev/commit0/internal/domain"
)

// Config holds all application configuration
type Config struct {
	Surreal SurrealConfig
	Gemini  GeminiConfig
	Index   IndexConfig
	Query   QueryConfig
	Server  ServerConfig
}

// ServerConfig holds HTTP server settings
type ServerConfig struct {
	Port           int
	CORSOrigins    []string
	ReadTimeoutSec int
	WriteTimeoutSec int
}

// SurrealConfig holds SurrealDB connection settings
type SurrealConfig struct {
	URL       string `mapstructure:"SURREAL_URL"`
	User      string `mapstructure:"SURREAL_USER"`
	Pass      string `mapstructure:"SURREAL_PASS"`
	Namespace string `mapstructure:"SURREAL_NAMESPACE"`
	Database  string `mapstructure:"SURREAL_DATABASE"`
}

// GeminiConfig holds Gemini API settings
type GeminiConfig struct {
	APIKey         string `mapstructure:"GEMINI_API_KEY"`
	EmbedModel     string `mapstructure:"GEMINI_EMBED_MODEL"`
	ExplainModel   string `mapstructure:"GEMINI_EXPLAIN_MODEL"`
	EmbedDimension int    `mapstructure:"GEMINI_EMBED_DIM"`
	MaxBatchSize   int    `mapstructure:"GEMINI_BATCH_SIZE"`
}

// IndexConfig holds indexing configuration
type IndexConfig struct {
	MaxWorkersParse  int
	MaxWorkersEmbed  int
	MaxWorkersStore  int
	MaxFileKB        int
	BatchSize        int
}

// QueryConfig holds query configuration
type QueryConfig struct {
	DefaultTopK       int
	MinScore          float64
	RRFKConstant      int
}

// Load loads configuration from environment variables with defaults
func Load() (*Config, error) {
	cfg := &Config{
		Surreal: SurrealConfig{
			URL:       getEnv("SURREAL_URL", "ws://localhost:8000"),
			User:      getEnv("SURREAL_USER", "root"),
			Pass:      getEnv("SURREAL_PASS", "root"),
			Namespace: getEnv("SURREAL_NAMESPACE", "commit0"),
			Database:  getEnv("SURREAL_DATABASE", "codebase"),
		},
		Gemini: GeminiConfig{
			APIKey:         getEnv("GEMINI_API_KEY", ""),
			EmbedModel:     getEnv("GEMINI_EMBED_MODEL", "gemini-embedding-2-preview"),
			ExplainModel:   getEnv("GEMINI_EXPLAIN_MODEL", "gemini-2.0-flash"),
			EmbedDimension: getEnvInt("GEMINI_EMBED_DIM", 3072),
			MaxBatchSize:   getEnvInt("GEMINI_BATCH_SIZE", 100),
		},
		Index: IndexConfig{
			MaxWorkersParse: getEnvInt("INDEX_WORKERS_PARSE", 0), // 0 = GOMAXPROCS
			MaxWorkersEmbed: getEnvInt("INDEX_WORKERS_EMBED", 4),
			MaxWorkersStore: getEnvInt("INDEX_WORKERS_STORE", 8),
			MaxFileKB:       getEnvInt("INDEX_MAX_FILE_KB", 10000),
			BatchSize:       getEnvInt("INDEX_BATCH_SIZE", 100),
		},
		Query: QueryConfig{
			DefaultTopK:  getEnvInt("QUERY_DEFAULT_TOP_K", 10),
			MinScore:     getEnvFloat("QUERY_MIN_SCORE", 0.5),
			RRFKConstant: getEnvInt("QUERY_RRF_K", 60),
		},
		Server: ServerConfig{
			Port:            getEnvInt("SERVER_PORT", 8080),
			CORSOrigins:     []string{getEnv("SERVER_CORS_ORIGINS", "*")},
			ReadTimeoutSec:  getEnvInt("SERVER_READ_TIMEOUT", 30),
			WriteTimeoutSec: getEnvInt("SERVER_WRITE_TIMEOUT", 120),
		},
	}

	// Validate required fields
	if cfg.Gemini.APIKey == "" {
		return nil, domain.Validation("GEMINI_API_KEY is required")
	}

	return cfg, nil
}

// getEnv retrieves an environment variable or returns a default value
func getEnv(key, defaultVal string) string {
	if val, ok := os.LookupEnv(key); ok {
		return val
	}
	return defaultVal
}

// getEnvInt retrieves an environment variable as integer or returns a default value
func getEnvInt(key string, defaultVal int) int {
	if val, ok := os.LookupEnv(key); ok {
		if intVal, err := strconv.Atoi(val); err == nil {
			return intVal
		}
	}
	return defaultVal
}

// getEnvFloat retrieves an environment variable as float or returns a default value
func getEnvFloat(key string, defaultVal float64) float64 {
	if val, ok := os.LookupEnv(key); ok {
		if floatVal, err := strconv.ParseFloat(val, 64); err == nil {
			return floatVal
		}
	}
	return defaultVal
}
