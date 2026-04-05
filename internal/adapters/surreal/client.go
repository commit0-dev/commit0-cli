package surreal

import (
	"context"
	"fmt"
	"log/slog"

	surrealdb "github.com/surrealdb/surrealdb.go"
	"github.com/surrealdb/surrealdb.go/pkg/models"

	"github.com/commit0-dev/commit0/internal/config"
	"github.com/commit0-dev/commit0/internal/domain"
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
	db     *surrealdb.DB
	log    *slog.Logger
	ns     string
	dbName string
}

// NewSurrealAdapter dials SurrealDB, authenticates, and selects the
// namespace/database specified in cfg. The returned adapter is ready to use.
func NewSurrealAdapter(ctx context.Context, cfg *config.SurrealConfig) (*SurrealAdapter, error) {
	if cfg.URL == "" {
		return nil, domain.Validation("surreal URL is required")
	}

	db, err := surrealdb.FromEndpointURLString(ctx, cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("surreal dial %s: %w", cfg.URL, err)
	}

	if _, err := db.SignIn(ctx, map[string]any{
		"user": cfg.User,
		"pass": cfg.Pass,
	}); err != nil {
		_ = db.Close(ctx)
		return nil, fmt.Errorf("surreal signin: %w", err)
	}

	if err := db.Use(ctx, cfg.Namespace, cfg.Database); err != nil {
		_ = db.Close(ctx)
		return nil, fmt.Errorf("surreal USE %s/%s: %w", cfg.Namespace, cfg.Database, err)
	}

	log := slog.Default().With("adapter", "surreal", "ns", cfg.Namespace, "db", cfg.Database)
	log.Info("connected", "url", cfg.URL)

	return &SurrealAdapter{
		db:     db,
		ns:     cfg.Namespace,
		dbName: cfg.Database,
		log:    log,
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
