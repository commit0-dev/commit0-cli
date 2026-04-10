package surreal

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"time"

	surrealdb "github.com/surrealdb/surrealdb.go"
	"github.com/surrealdb/surrealdb.go/pkg/connection"
	"github.com/surrealdb/surrealdb.go/pkg/connection/gorillaws"
	"github.com/surrealdb/surrealdb.go/pkg/models"

	"github.com/commit0-dev/commit0/server/internal/config"
	"github.com/commit0-dev/commit0/server/internal/domain"
)

// Compile-time interface checks — fail fast at build time if we drift.
var (
	_ domain.GraphStore  = (*SurrealAdapter)(nil)
	_ domain.VectorIndex = (*VectorAdapter)(nil)
	_ domain.TextIndex   = (*TextAdapter)(nil)
)

// VectorAdapter wraps SurrealAdapter to implement domain.VectorIndex.
// Go does not allow two methods with the same name but different signatures
// on the same type, so we use thin delegate wrappers.
type VectorAdapter struct{ *SurrealAdapter }

// TextAdapter wraps SurrealAdapter to implement domain.TextIndex.
type TextAdapter struct{ *SurrealAdapter }

// AsVectorIndex returns a domain.VectorIndex view of this adapter.
func (a *SurrealAdapter) AsVectorIndex() domain.VectorIndex { return &VectorAdapter{a} }

// AsTextIndex returns a domain.TextIndex view of this adapter.
func (a *SurrealAdapter) AsTextIndex() domain.TextIndex { return &TextAdapter{a} }

// SurrealAdapter implements GraphStore, VectorIndex, and TextIndex
// using SurrealDB 3.0 via WebSocket.
type SurrealAdapter struct {
	db       *surrealdb.DB
	log      *slog.Logger
	ns       string
	dbName   string
	embedDim int // HNSW vector index dimension (e.g. 3072, 1024)
}

// defaultRPCTimeout is the fallback if config doesn't specify one.
const defaultRPCTimeout = 5 * time.Minute

// NewSurrealAdapter dials SurrealDB, authenticates, and selects the
// namespace/database specified in cfg. embedDim sets the HNSW vector index
// dimension used in ApplySchema (e.g. 3072 for Gemini, 1024 for Voyage).
//
// The connection uses cfg.ConnectTimeoutS for the initial dial and
// cfg.RPCTimeoutS for per-operation timeouts.
func NewSurrealAdapter(ctx context.Context, cfg *config.SurrealConfig, embedDim int) (*SurrealAdapter, error) {
	if cfg.URL == "" {
		return nil, domain.Validation("surreal URL is required")
	}

	u, err := url.ParseRequestURI(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("surreal: invalid URL %q: %w", cfg.URL, err)
	}

	connCfg := connection.NewConfig(u)
	if cfgErr := connCfg.Validate(); cfgErr != nil {
		return nil, fmt.Errorf("surreal: invalid connection config: %w", cfgErr)
	}

	// Configurable RPC timeout
	rpcTimeout := defaultRPCTimeout
	if cfg.RPCTimeoutS > 0 {
		rpcTimeout = time.Duration(cfg.RPCTimeoutS) * time.Second
	}
	ws := gorillaws.New(connCfg).SetTimeOut(rpcTimeout)

	// Apply connect timeout
	connectTimeout := 30 * time.Second
	if cfg.ConnectTimeoutS > 0 {
		connectTimeout = time.Duration(cfg.ConnectTimeoutS) * time.Second
	}
	connCtx, connCancel := context.WithTimeout(ctx, connectTimeout)
	defer connCancel()

	log := slog.Default().With("adapter", "surreal", "ns", cfg.Namespace, "db", cfg.Database)
	log.Info("connecting", "url", cfg.URL, "connect_timeout", connectTimeout, "rpc_timeout", rpcTimeout)

	db, err := surrealdb.FromConnection(connCtx, ws)
	if err != nil {
		return nil, fmt.Errorf("surreal dial %s: %w", cfg.URL, err)
	}

	if _, err := db.SignIn(connCtx, map[string]any{
		"user": cfg.User,
		"pass": cfg.Pass,
	}); err != nil {
		_ = db.Close(ctx)
		return nil, fmt.Errorf("surreal signin: %w", err)
	}

	if err := db.Use(connCtx, cfg.Namespace, cfg.Database); err != nil {
		_ = db.Close(ctx)
		return nil, fmt.Errorf("surreal USE %s/%s: %w", cfg.Namespace, cfg.Database, err)
	}

	// Verify connectivity with a ping
	if err := func() error {
		pingCtx, pingCancel := context.WithTimeout(ctx, 5*time.Second)
		defer pingCancel()
		_, err := db.Version(pingCtx)
		return err
	}(); err != nil {
		_ = db.Close(ctx)
		return nil, fmt.Errorf("surreal ping after connect: %w", err)
	}

	log.Info("connected", "url", cfg.URL)

	return &SurrealAdapter{
		db:       db,
		ns:       cfg.Namespace,
		dbName:   cfg.Database,
		embedDim: embedDim,
		log:      log,
	}, nil
}

// Close shuts down the underlying WebSocket connection.
func (a *SurrealAdapter) Close(ctx context.Context) {
	if err := a.db.Close(ctx); err != nil {
		a.log.Warn("close error", "err", err)
	}
}

// Ping issues a lightweight Version RPC to verify connectivity.
func (a *SurrealAdapter) Ping(ctx context.Context) error {
	if _, err := a.db.Version(ctx); err != nil {
		return fmt.Errorf("surreal ping: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// nodeTable maps a NodeKind to its SurrealDB table name.
// NodeKind constants are already lowercase strings matching the table names.
func nodeTable(kind string) string {
	switch kind {
	case "function":
		return "function"
	case "class":
		return "class"
	case "file":
		return "file"
	case "module":
		return "module"
	default:
		return "function" // safe fallback
	}
}

// selectCols returns the SELECT column list for search queries.
// The module table lacks "qualified", "body", "signature", "centrality" —
// we alias "path" so it maps to the same struct fields as other tables.
func selectCols(table string) string {
	if table == "module" {
		return "id, name, path AS qualified, path AS file_path, repo_slug, language, docstring, 0 AS start_line, 0 AS end_line, 0 AS centrality"
	}
	return "id, name, qualified, file_path, repo_slug, language, body, signature, docstring, start_line, end_line, centrality"
}

// recordID builds a models.RecordID from a table + opaque ID string.
// SurrealDB stores IDs like "function:myPkg.Handler.ServeHTTP".
func recordID(table, id string) models.RecordID {
	return models.NewRecordID(table, id)
}

// splitRecordID splits a "table:id" string into (table, id).
// If the input has no colon the whole string is treated as the ID
// and table is returned empty.
func splitRecordID(full string) (table, id string) {
	for i, ch := range full {
		if ch == ':' {
			return full[:i], full[i+1:]
		}
	}
	return "", full
}
