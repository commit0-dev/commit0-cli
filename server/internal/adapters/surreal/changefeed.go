package surreal

import (
	"context"
	"fmt"
	"time"

	surrealdb "github.com/surrealdb/surrealdb.go"
)

// ChangefeedEntry represents a single change from SurrealDB's SHOW CHANGES command.
type ChangefeedEntry struct {
	Versionstamp int    `json:"versionstamp"`
	Changes      []any  `json:"changes"`
}

// ReadChangefeed returns changes to a table since the given timestamp.
// Uses SurrealDB's CHANGEFEED feature (requires CHANGEFEED on the table definition).
// Returns nil, nil if no changes or changefeeds are unavailable.
func (a *SurrealAdapter) ReadChangefeed(ctx context.Context, table string, since time.Time) ([]ChangefeedEntry, error) {
	q := fmt.Sprintf("SHOW CHANGES FOR TABLE %s SINCE '%s' LIMIT 1000;", table, since.UTC().Format(time.RFC3339))
	results, err := surrealdb.Query[[]ChangefeedEntry](ctx, a.readDB(), q, nil)
	if err != nil {
		// Changefeeds may not be available (data older than 7d, or table recreated).
		a.log.Debug("changefeed unavailable", "table", table, "since", since, "err", err)
		return nil, nil
	}
	if results == nil || len(*results) == 0 {
		return nil, nil
	}
	return (*results)[0].Result, nil
}

// HasChangefeedSince checks if changefeed data is available for a table since the given time.
func (a *SurrealAdapter) HasChangefeedSince(ctx context.Context, table string, since time.Time) bool {
	entries, _ := a.ReadChangefeed(ctx, table, since)
	return entries != nil
}
