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

// Compile-time interface check: openCodeGraphAdapter (see open_code_graph.go)
// is the single graph interface. SurrealAdapter methods are implementation details.

// SurrealAdapter implements GraphStore, VectorIndex, and TextIndex
// using SurrealDB 3.0 via WebSocket with dual connection pools.
// Read pool handles queries/trace/blast (never blocked by writes).
// Write pool handles index upserts (concurrent batch writes).
type SurrealAdapter struct {
	db        *surrealdb.DB // primary connection (schema ops)
	readPool  *ConnPool     // nil = use db (single-conn fallback)
	writePool *ConnPool     // nil = use db (single-conn fallback)
	log       *slog.Logger
	ns        string
	dbName    string
	embedDim  int
}

// readDB returns a connection for read operations (query, trace, blast).
func (a *SurrealAdapter) readDB() *surrealdb.DB {
	if a.readPool != nil {
		return a.readPool.Acquire()
	}
	return a.db
}

// writeDB returns a connection for write operations (upsert, delete).
func (a *SurrealAdapter) writeDB() *surrealdb.DB {
	if a.writePool != nil {
		return a.writePool.Acquire()
	}
	return a.db
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

	adapter := &SurrealAdapter{
		db:       db,
		ns:       cfg.Namespace,
		dbName:   cfg.Database,
		embedDim: embedDim,
		log:      log,
	}

	// Initialize connection pools for concurrent read/write access.
	// Read pool: queries, traces, blasts (default 8 connections).
	// Write pool: index upserts, deletes (default 4 connections).
	readSize := cfg.ReadPoolSize
	if readSize <= 0 {
		readSize = 8
	}
	writeSize := cfg.WritePoolSize
	if writeSize <= 0 {
		writeSize = 4
	}

	readPool, err := NewConnPool(ctx, cfg, readSize, "read")
	if err != nil {
		log.Warn("read pool init failed, using single connection", "err", err)
	} else {
		adapter.readPool = readPool
	}

	writePool, err := NewConnPool(ctx, cfg, writeSize, "write")
	if err != nil {
		log.Warn("write pool init failed, using single connection", "err", err)
	} else {
		adapter.writePool = writePool
	}

	return adapter, nil
}

// Close shuts down all connections and pools.
func (a *SurrealAdapter) Close(ctx context.Context) {
	if a.readPool != nil {
		a.readPool.Close(ctx)
	}
	if a.writePool != nil {
		a.writePool.Close(ctx)
	}
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
