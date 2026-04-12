package surreal

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	surrealdb "github.com/surrealdb/surrealdb.go"

	"github.com/commit0-dev/commit0/server/assets"
)

// schemaVersion is the version number written to the schema_version table
// by the DDL in schema.surql. Bump this when the schema changes.
// Bump when: schema structure changes OR default embed dimension changes.
const schemaVersion = 17 // 16: OpenCodeGraph — batch-drop edge tables + recreate as SCHEMALESS

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

	// Non-destructive migration: NULL embeddings when dimension changes
	// instead of dropping tables. Graph structure is preserved.
	if err := a.migrateEmbeddings(ctx, embedDim); err != nil {
		a.log.Warn("could not migrate embeddings", "err", err)
	}

	// OpenCodeGraph migration: drop old SCHEMAFULL edge tables in one batch
	// so the field definitions are fully removed before SCHEMALESS recreation.
	// Must be a single multi-statement query (not split) so SurrealDB commits
	// the REMOVE before the DEFINE in the main DDL.
	// Pre-create FTS analyzers before DDL runs — the splitStatements approach
	// sometimes fails to create the code_analyzer (SurrealDB parser issue with
	// TOKENIZERS class keyword in standalone statements). Create them explicitly.
	analyzerDDL := `DEFINE ANALYZER IF NOT EXISTS code_analyzer TOKENIZERS class, blank FILTERS lowercase, ascii;
DEFINE ANALYZER IF NOT EXISTS nl_analyzer TOKENIZERS blank FILTERS lowercase, ascii, snowball(english);`
	if _, err := surrealdb.Query[any](ctx, a.db, analyzerDDL, nil); err != nil {
		a.log.Warn("pre-create analyzers failed", "err", err)
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

	// Verify HNSW indexes are operational on this connection.
	// SurrealDB WebSocket connections may not see indexes created in the
	// same session until a query forces a refresh.
	if err := a.verifyHNSWIndexes(ctx, embedDim); err != nil {
		a.log.Warn("HNSW index verification failed", "err", err)
	}

	// Stamp the actual code version into the DB so the next startup skips DDL.
	versionQ := fmt.Sprintf("INSERT INTO schema_version (version) VALUES (%d) ON DUPLICATE KEY UPDATE version = %d;", schemaVersion, schemaVersion)
	if _, err := surrealdb.Query[any](ctx, a.db, versionQ, nil); err != nil {
		a.log.Warn("failed to update schema_version", "err", err)
	}

	a.log.Info("schema applied", "version", schemaVersion, "statements", len(stmts))
	return nil
}

// verifyHNSWIndexes confirms each HNSW vector index is operational by
// running a probe ANN query. If a probe fails, the index is recreated
// on the current connection.
func (a *SurrealAdapter) verifyHNSWIndexes(ctx context.Context, embedDim int) error {
	tables := []struct {
		name string
		idx  string
		efc  int
	}{
		{"function", "fn_vec_idx", 200},
		{"class", "cls_vec_idx", 200},
		{"file", "file_vec_idx", 150},
		{"module", "mod_vec_idx", 150},
	}

	zeroVec := make([]float32, embedDim)

	for _, t := range tables {
		probe := fmt.Sprintf("SELECT id FROM `%s` WHERE embedding <|1,40|> $q LIMIT 1;", t.name)
		_, err := surrealdb.Query[any](ctx, a.db, probe, map[string]any{"q": zeroVec})
		if err == nil {
			continue
		}

		a.log.Warn("HNSW index not operational, recreating", "table", t.name, "err", err)
		recreate := fmt.Sprintf(
			"DEFINE INDEX OVERWRITE %s ON `%s` FIELDS embedding HNSW DIMENSION %d DIST COSINE TYPE F32 EFC %d M 16;",
			t.idx, t.name, embedDim, t.efc)
		if _, rErr := surrealdb.Query[any](ctx, a.db, recreate, nil); rErr != nil {
			return fmt.Errorf("recreate HNSW %s: %w", t.name, rErr)
		}
	}

	a.log.Info("HNSW indexes verified", "tables", len(tables))
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

// migrateEmbeddings handles dimension changes non-destructively.
// Instead of dropping all tables (which destroys the entire code graph),
// it NULLs out embedding fields and lets HNSW indexes rebuild at the new
// dimension via DEFINE INDEX OVERWRITE in the schema DDL.
// All nodes, edges, FTS indexes, and graph structure are preserved.
func (a *SurrealAdapter) migrateEmbeddings(ctx context.Context, targetDim int) error {
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
		return nil
	}
	rows := (*results)[0].Result
	if len(rows) == 0 || len(rows[0].Embedding) == 0 {
		return nil // no embeddings stored yet
	}
	if len(rows[0].Embedding) == targetDim {
		a.log.Debug("embedding dimensions match", "dim", targetDim)
		return nil
	}

	a.log.Info("migrating embeddings (non-destructive)",
		"old_dim", len(rows[0].Embedding),
		"new_dim", targetDim,
	)

	// NULL out embedding fields on all node tables.
	// Graph structure (nodes, edges, FTS) is fully preserved.
	nodeTables := []string{"`function`", "class", "file", "module"}
	for _, t := range nodeTables {
		q := fmt.Sprintf("UPDATE %s SET embedding = NONE, embed_provider = NONE WHERE embedding IS NOT NONE;", t)
		if _, err := surrealdb.Query[any](ctx, a.db, q, nil); err != nil {
			a.log.Warn("could not clear embeddings", "table", t, "err", err)
		}
	}

	// HNSW indexes will be rebuilt with new dimension by DEFINE INDEX OVERWRITE
	// in the schema DDL (executed after this function returns).
	a.log.Info("embeddings cleared — graph preserved, re-embedding required")
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
