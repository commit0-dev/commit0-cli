package surreal

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	surrealdb "github.com/surrealdb/surrealdb.go"

	"github.com/commit0-dev/commit0/assets"
)

// schemaVersion is the version number written to the schema_version table
// by the DDL in schema.surql. Bump this when the schema changes.
// Bump when: schema structure changes OR default embed dimension changes.
const schemaVersion = 11

// SchemaVersion returns the current schema version for external callers.
func SchemaVersion() int { return schemaVersion }

// ApplySchema executes the embedded schema.surql DDL against SurrealDB.
// The embedDim parameter sets the HNSW vector index dimension (e.g. 3072
// for Gemini Embedding 2, 1024 for voyage-code-3).
// All DEFINE … IF NOT EXISTS statements are idempotent, so this is safe
// to call on every startup.
func (a *SurrealAdapter) ApplySchema(ctx context.Context) error {
	embedDim := a.embedDim
	if embedDim <= 0 {
		embedDim = 3072 // safe default for Gemini Embedding 2
	}

	// Clear stale embeddings when the vector dimension changes.
	// The HNSW OVERWRITE rebuild rejects rows whose vectors don't match
	// the new dimension, so we must nullify them first.
	if err := a.clearStaleEmbeddings(ctx, embedDim); err != nil {
		a.log.Warn("could not clear stale embeddings", "err", err)
	}

	ddl := strings.ReplaceAll(assets.SchemaSurQL, "{{EMBED_DIM}}", strconv.Itoa(embedDim))

	// Split DDL into individual statements and execute sequentially.
	// Sending one giant multi-statement query causes SurrealDB to time out
	// on large datasets (HNSW index rebuilds are especially slow).
	stmts := splitStatements(ddl)
	a.log.Info("applying schema DDL", "version", schemaVersion, "statements", len(stmts))

	for i, stmt := range stmts {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}

		// Log DEFINE/REMOVE statements for progress visibility
		firstLine := stmt
		if idx := strings.IndexByte(stmt, '\n'); idx > 0 {
			firstLine = stmt[:idx]
		}
		if len(firstLine) > 80 {
			firstLine = firstLine[:80] + "..."
		}
		a.log.Debug("schema statement", "i", i+1, "of", len(stmts), "stmt", firstLine)

		results, err := surrealdb.Query[any](ctx, a.db, stmt+";", nil)
		if err != nil {
			return fmt.Errorf("apply schema statement %d (%s): %w", i+1, firstLine, err)
		}

		if results != nil {
			for _, r := range *results {
				if r.Status == "ERR" {
					errMsg := fmt.Sprintf("%v", r.Error)
					if isSchemaAlreadyExistsErr(errMsg) {
						continue
					}
					return fmt.Errorf("apply schema statement %d: %v", i+1, r.Error)
				}
			}
		}
	}

	a.log.Info("schema applied", "version", schemaVersion, "statements", len(stmts))
	return nil
}

// splitStatements splits a multi-statement SurrealQL string into individual
// statements. Splits on ";\n" boundaries (semicolons followed by newlines)
// to avoid splitting inside string literals or inline expressions.
func splitStatements(ddl string) []string {
	raw := strings.Split(ddl, ";\n")
	stmts := make([]string, 0, len(raw))
	for _, s := range raw {
		s = strings.TrimSpace(s)
		// Skip empty lines and comments
		if s == "" || strings.HasPrefix(s, "--") || strings.HasPrefix(s, "//") {
			continue
		}
		// Strip trailing semicolons (we add them back on execution)
		s = strings.TrimRight(s, ";")
		s = strings.TrimSpace(s)
		if s != "" {
			stmts = append(stmts, s)
		}
	}
	return stmts
}

// isSchemaAlreadyExistsErr returns true if the error is a benign "already exists"
// from DEFINE statements on an existing schema.
func isSchemaAlreadyExistsErr(msg string) bool {
	return strings.Contains(msg, "already exists") ||
		strings.Contains(msg, "Transaction write conflict")
}

// clearStaleEmbeddings nullifies embedding fields on all node tables when
// the stored vectors have a different dimension than what the schema expects.
// This prevents DEFINE INDEX OVERWRITE HNSW from failing during rebuild.
func (a *SurrealAdapter) clearStaleEmbeddings(ctx context.Context, targetDim int) error {
	// Quick probe: read one embedding from the function table and check its length.
	const probe = "SELECT embedding FROM `function` WHERE embedding IS NOT NONE LIMIT 1;"
	type row struct {
		Embedding []float32 `json:"embedding"`
	}
	results, err := surrealdb.Query[[]row](ctx, a.db, probe, nil)
	if err != nil {
		a.log.Debug("embedding probe query failed", "err", err)
		return nil //nolint:nilerr // query fails if table doesn't exist yet — safe to skip
	}
	if results == nil || len(*results) == 0 {
		a.log.Debug("embedding probe: no query results")
		return nil
	}
	rows := (*results)[0].Result
	if len(rows) == 0 || len(rows[0].Embedding) == 0 {
		a.log.Debug("embedding probe: no embeddings found")
		return nil // no embeddings stored yet
	}
	if len(rows[0].Embedding) == targetDim {
		a.log.Debug("embedding probe: dimensions match", "dim", targetDim)
		return nil // dimensions match — no migration needed
	}

	a.log.Info("clearing stale embeddings",
		"old_dim", len(rows[0].Embedding),
		"new_dim", targetDim,
	)

	// Drop the tables entirely so the schema DDL recreates them with clean
	// HNSW indexes at the new dimension. This is a full reset — all code
	// nodes, edges, and embeddings are lost and must be re-indexed.
	// This only runs when the embedding dimension changes (provider switch).
	tables := []string{
		// Edges first (reference foreign keys).
		"calls", "imports", "defines", "inherits", "uses", "data_flow", "reads", "writes",
		// Then node tables.
		"`function`", "class", "file", "module",
	}
	for _, t := range tables {
		q := "REMOVE TABLE IF EXISTS " + t + ";"
		if _, err := surrealdb.Query[any](ctx, a.db, q, nil); err != nil {
			a.log.Warn("could not remove table", "table", t, "err", err)
		}
	}

	return nil
}

// GetSchemaVersion returns the current schema version stored in SurrealDB.
// Returns 0 if no version record exists yet.
func (a *SurrealAdapter) GetSchemaVersion(ctx context.Context) (int, error) {
	const q = `SELECT version FROM schema_version ORDER BY version DESC LIMIT 1;`

	type versionRow struct {
		Version int `json:"version"`
	}

	results, err := surrealdb.Query[[]versionRow](ctx, a.db, q, nil)
	if err != nil {
		return 0, fmt.Errorf("get schema version: %w", err)
	}

	if results == nil || len(*results) == 0 {
		return 0, nil
	}

	rows := (*results)[0].Result
	if len(rows) == 0 {
		return 0, nil
	}

	return rows[0].Version, nil
}
