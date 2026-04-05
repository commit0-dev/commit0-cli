package surreal

import (
	"context"
	"fmt"
	"strings"

	surrealdb "github.com/surrealdb/surrealdb.go"

	"github.com/commit0-dev/commit0/assets"
)

// schemaVersion is the version number written to the schema_version table
// by the DDL in schema.surql. Bump this when the schema changes.
const schemaVersion = 5

// SchemaVersion returns the current schema version for external callers.
func SchemaVersion() int { return schemaVersion }

// ApplySchema executes the embedded schema.surql DDL against SurrealDB.
// All DEFINE … IF NOT EXISTS statements are idempotent, so this is safe
// to call on every startup.
func (a *SurrealAdapter) ApplySchema(ctx context.Context) error {
	results, err := surrealdb.Query[any](ctx, a.db, assets.SchemaSurQL, nil)
	if err != nil {
		return fmt.Errorf("apply schema: %w", err)
	}

	// Surface any per-statement errors from the multi-statement DDL.
	// "already exists" errors are expected when re-applying — skip them.
	if results != nil {
		for i, r := range *results {
			if r.Status == "ERR" {
				errMsg := fmt.Sprintf("%v", r.Error)
				if isSchemaAlreadyExistsErr(errMsg) {
					continue
				}
				return fmt.Errorf("apply schema statement %d: %v", i, r.Error)
			}
		}
	}

	a.log.Info("schema applied", "version", schemaVersion)
	return nil
}

// isSchemaAlreadyExistsErr returns true if the error is a benign "already exists"
// from DEFINE statements on an existing schema.
func isSchemaAlreadyExistsErr(msg string) bool {
	return strings.Contains(msg, "already exists") ||
		strings.Contains(msg, "Transaction write conflict")
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
