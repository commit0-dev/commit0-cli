package surreal

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	surrealdb "github.com/surrealdb/surrealdb.go"

	"github.com/commit0-dev/commit0/pkg/types"
	"github.com/commit0-dev/commit0/server/internal/domain"
	"github.com/commit0-dev/commit0/server/internal/infra/retry"
)

type eventRow struct {
	ID        string         `json:"id"`
	Type      string         `json:"type"`
	RepoSlug  string         `json:"repo_slug"`
	AuthorID  string         `json:"author_id"`
	Timestamp time.Time      `json:"timestamp"`
	Payload   map[string]any `json:"payload"`
	Source    string         `json:"source"`
}

// eventSubscriber tracks an active subscription.
type eventSubscriber struct {
	filter types.EventFilter
	ch     chan types.Event
}

// Compile-time interface check.
var _ interface {
	Append(context.Context, types.Event) error
} = (*SurrealAdapter)(nil)

// subscriptionsMu protects subscribers and the fan-out broadcast.
var subscriptionsMu sync.RWMutex

// subscribers is keyed by channel pointer, contains the subscription record.
var subscribers = make(map[<-chan types.Event]*eventSubscriber)

// ---------------------------------------------------------------------------
// Append and AppendBatch
// ---------------------------------------------------------------------------

func (a *SurrealAdapter) Append(ctx context.Context, event types.Event) error {
	return a.AppendBatch(ctx, []types.Event{event})
}

func (a *SurrealAdapter) AppendBatch(ctx context.Context, events []types.Event) error {
	for _, event := range events {
		query := `CREATE event_log CONTENT $props;`
		props := map[string]any{
			"type":      event.Type,
			"repo_slug": event.RepoSlug,
			"author_id": event.AuthorID,
			"timestamp": event.Timestamp,
			"payload":   event.Payload,
			"source":    event.Source,
		}
		if event.AuthorID == "" {
			props["author_id"] = "system"
		}

		err := retry.WithRetry(ctx, 3, func() error {
			results, err := surrealdb.Query[any](ctx, a.writeDB(), query, map[string]any{"props": props})
			if err != nil {
				return err
			}
			if results == nil || len(*results) == 0 {
				return fmt.Errorf("no response from event append")
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("append event: %w", err)
		}

		// Fan-out to subscribers.
		a.notifySubscribers(event)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Range — Query events with filters
// ---------------------------------------------------------------------------

func (a *SurrealAdapter) Range(ctx context.Context, filter types.EventFilter) ([]types.Event, error) {
	var conditions []string
	params := map[string]any{}

	if filter.RepoSlug != "" {
		conditions = append(conditions, "repo_slug = $repo_slug")
		params["repo_slug"] = filter.RepoSlug
	}
	if len(filter.Types) > 0 {
		conditions = append(conditions, "type IN $types")
		params["types"] = filter.Types
	}
	if filter.Source != "" {
		conditions = append(conditions, "source = $source")
		params["source"] = filter.Source
	}
	if filter.Since != nil {
		conditions = append(conditions, "timestamp >= $since")
		params["since"] = *filter.Since
	}
	if filter.Until != nil {
		conditions = append(conditions, "timestamp <= $until")
		params["until"] = *filter.Until
	}

	query := "SELECT * FROM event_log"
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY timestamp DESC"

	limit := filter.Limit
	if limit <= 0 {
		limit = 100
	}
	query += fmt.Sprintf(" LIMIT %d;", limit)

	var rows []eventRow
	err := retry.WithRetry(ctx, 3, func() error {
		results, err := surrealdb.Query[[]eventRow](ctx, a.readDB(), query, params)
		if err != nil {
			// Table missing → no events yet on a fresh instance. Return
			// empty rather than 500 so consumers can poll before any
			// event has been appended.
			if strings.Contains(err.Error(), "does not exist") {
				return nil
			}
			return err
		}
		if results == nil || len(*results) == 0 {
			return nil
		}
		if len((*results)[0].Result) > 0 {
			rows = (*results)[0].Result
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("range events: %w", err)
	}

	events := make([]types.Event, len(rows))
	for i, r := range rows {
		events[i] = types.Event{
			ID:        r.ID,
			Type:      r.Type,
			RepoSlug:  r.RepoSlug,
			AuthorID:  r.AuthorID,
			Timestamp: r.Timestamp,
			Payload:   r.Payload,
			Source:    r.Source,
		}
	}
	return events, nil
}

// ---------------------------------------------------------------------------
// Subscribe and Unsubscribe — In-memory fan-out
// ---------------------------------------------------------------------------

func (a *SurrealAdapter) Subscribe(ctx context.Context, filter types.EventFilter) (<-chan types.Event, error) {
	ch := make(chan types.Event, 64)
	sub := &eventSubscriber{
		filter: filter,
		ch:     ch,
	}

	subscriptionsMu.Lock()
	subscribers[ch] = sub
	subscriptionsMu.Unlock()

	return ch, nil
}

func (a *SurrealAdapter) Unsubscribe(ctx context.Context, ch <-chan types.Event) error {
	subscriptionsMu.Lock()
	delete(subscribers, ch)
	subscriptionsMu.Unlock()
	return nil
}

// notifySubscribers fans out an event to all subscribers whose filters match.
func (a *SurrealAdapter) notifySubscribers(event types.Event) {
	subscriptionsMu.RLock()
	defer subscriptionsMu.RUnlock()

	for _, sub := range subscribers {
		if a.matchesFilter(event, sub.filter) {
			select {
			case sub.ch <- event:
			default:
			}
		}
	}
}

// matchesFilter checks if an event matches a subscription filter.
func (a *SurrealAdapter) matchesFilter(event types.Event, filter types.EventFilter) bool {
	if filter.RepoSlug != "" && event.RepoSlug != filter.RepoSlug {
		return false
	}
	if len(filter.Types) > 0 {
		match := false
		for _, t := range filter.Types {
			if event.Type == t {
				match = true
				break
			}
		}
		if !match {
			return false
		}
	}
	if filter.Source != "" && event.Source != filter.Source {
		return false
	}
	if filter.Since != nil && event.Timestamp.Before(*filter.Since) {
		return false
	}
	if filter.Until != nil && event.Timestamp.After(*filter.Until) {
		return false
	}
	return true
}

// ---------------------------------------------------------------------------
// Compact — Delete events before a given timestamp
// ---------------------------------------------------------------------------

func (a *SurrealAdapter) Compact(ctx context.Context, before time.Time) (int64, error) {
	query := `DELETE FROM event_log WHERE timestamp < $before RETURN BEFORE;`

	type deletedRow struct {
		ID string `json:"id"`
	}

	var deleted []deletedRow
	err := retry.WithRetry(ctx, 3, func() error {
		results, err := surrealdb.Query[[]deletedRow](ctx, a.writeDB(), query, map[string]any{"before": before})
		if err != nil {
			return err
		}
		if results != nil && len(*results) > 0 && len((*results)[0].Result) > 0 {
			deleted = (*results)[0].Result
		}
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("compact events: %w", err)
	}
	return int64(len(deleted)), nil
}

// ---------------------------------------------------------------------------
// Count — Count events matching a filter
// ---------------------------------------------------------------------------

func (a *SurrealAdapter) Count(ctx context.Context, filter types.EventFilter) (int64, error) {
	var conditions []string
	params := map[string]any{}

	if filter.RepoSlug != "" {
		conditions = append(conditions, "repo_slug = $repo_slug")
		params["repo_slug"] = filter.RepoSlug
	}
	if len(filter.Types) > 0 {
		conditions = append(conditions, "type IN $types")
		params["types"] = filter.Types
	}
	if filter.Source != "" {
		conditions = append(conditions, "source = $source")
		params["source"] = filter.Source
	}
	if filter.Since != nil {
		conditions = append(conditions, "timestamp >= $since")
		params["since"] = *filter.Since
	}
	if filter.Until != nil {
		conditions = append(conditions, "timestamp <= $until")
		params["until"] = *filter.Until
	}

	query := "SELECT count() as total FROM event_log"
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " GROUP ALL;"

	type countResult struct {
		Total int64 `json:"total"`
	}

	var rows []countResult
	err := retry.WithRetry(ctx, 3, func() error {
		results, err := surrealdb.Query[[]countResult](ctx, a.readDB(), query, params)
		if err != nil {
			// Table missing → 0 (fresh instance).
			if strings.Contains(err.Error(), "does not exist") {
				return nil
			}
			return err
		}
		if results != nil && len(*results) > 0 && len((*results)[0].Result) > 0 {
			rows = (*results)[0].Result
		}
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("count events: %w", err)
	}
	if len(rows) == 0 {
		return 0, nil
	}
	return rows[0].Total, nil
}

// AsEventStore returns the SurrealAdapter as an EventStore interface.
func (a *SurrealAdapter) AsEventStore() domain.EventStore {
	return a
}
