package app

import (
	"context"
	"log/slog"
	"time"

	"github.com/commit0-dev/commit0/pkg/types"
	"github.com/commit0-dev/commit0/server/internal/domain"
)

// EventService wraps the EventStore port with business logic and sensible defaults.
type EventService struct {
	store domain.EventStore
	log   *slog.Logger
}

// NewEventService constructs an EventService.
func NewEventService(store domain.EventStore) *EventService {
	return &EventService{
		store: store,
		log:   slog.Default().With("service", "event"),
	}
}

// Emit appends a single event with sensible defaults (timestamp, author).
// Gracefully degrades if store is nil.
func (es *EventService) Emit(ctx context.Context, eventType types.EventType, repoSlug, source string, payload map[string]any) error {
	if es.store == nil {
		return nil
	}
	event := types.Event{
		Type:      eventType,
		RepoSlug:  repoSlug,
		AuthorID:  "system",
		Timestamp: time.Now(),
		Payload:   payload,
		Source:    source,
	}
	return es.store.Append(ctx, event)
}

// EmitBatch appends multiple events with sensible defaults.
// Gracefully degrades if store is nil.
func (es *EventService) EmitBatch(ctx context.Context, events []types.Event) error {
	if es.store == nil {
		return nil
	}
	now := time.Now()
	for i := range events {
		if events[i].Timestamp.IsZero() {
			events[i].Timestamp = now
		}
		if events[i].AuthorID == "" {
			events[i].AuthorID = "system"
		}
	}
	return es.store.AppendBatch(ctx, events)
}

// Query returns events matching the filter.
// Gracefully degrades if store is nil.
func (es *EventService) Query(ctx context.Context, filter types.EventFilter) ([]types.Event, error) {
	if es.store == nil {
		return nil, nil
	}
	return es.store.Range(ctx, filter)
}

// Subscribe creates a subscription for matching events.
// Gracefully degrades if store is nil.
func (es *EventService) Subscribe(ctx context.Context, filter types.EventFilter) (<-chan types.Event, error) {
	if es.store == nil {
		ch := make(chan types.Event)
		close(ch)
		return ch, nil
	}
	return es.store.Subscribe(ctx, filter)
}

// Unsubscribe removes a subscription.
// Gracefully degrades if store is nil.
func (es *EventService) Unsubscribe(ctx context.Context, ch <-chan types.Event) error {
	if es.store == nil {
		return nil
	}
	return es.store.Unsubscribe(ctx, ch)
}

// Compact removes events older than the given time.
// Gracefully degrades if store is nil.
func (es *EventService) Compact(ctx context.Context, before time.Time) (int64, error) {
	if es.store == nil {
		return 0, nil
	}
	count, err := es.store.Compact(ctx, before)
	if err != nil {
		return 0, err
	}
	es.log.Info("compacted events", "deleted", count, "before", before)
	return count, nil
}

// Count returns the number of events matching the filter.
// Gracefully degrades if store is nil.
func (es *EventService) Count(ctx context.Context, filter types.EventFilter) (int64, error) {
	if es.store == nil {
		return 0, nil
	}
	return es.store.Count(ctx, filter)
}
