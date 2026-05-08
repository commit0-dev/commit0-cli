package domain

import (
	"context"
	"time"

	"github.com/commit0-dev/commit0/pkg/types"
)

// ---------------------------------------------------------------------------
// Event Store — Append-only event log for platform pivot
// ---------------------------------------------------------------------------

// EventStore manages the append-only event log for code graph evolution.
// All graph mutations (node/edge create/update/delete), index operations,
// and repository changes emit events for audit, time-travel queries,
// and P2P sync.
type EventStore interface {
	Append(ctx context.Context, event types.Event) error
	AppendBatch(ctx context.Context, events []types.Event) error
	Range(ctx context.Context, filter types.EventFilter) ([]types.Event, error)
	Subscribe(ctx context.Context, filter types.EventFilter) (<-chan types.Event, error)
	Unsubscribe(ctx context.Context, ch <-chan types.Event) error
	Compact(ctx context.Context, before time.Time) (int64, error)
	Count(ctx context.Context, filter types.EventFilter) (int64, error)
}
