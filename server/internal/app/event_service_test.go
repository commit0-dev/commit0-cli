package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/commit0-dev/commit0/pkg/types"
)

// stubEventStore implements domain.EventStore for tests.
type stubEventStore struct {
	appended      []types.Event
	appendBatch   [][]types.Event
	rangeResult   []types.Event
	rangeErr      error
	subscribeChan chan types.Event
	subscribeErr  error
	unsubChan     <-chan types.Event
	unsubErr      error
	compactCount  int64
	compactErr    error
	countResult   int64
	countErr      error
	appendErr     error
}

func (s *stubEventStore) Append(_ context.Context, e types.Event) error {
	if s.appendErr != nil {
		return s.appendErr
	}
	s.appended = append(s.appended, e)
	return nil
}
func (s *stubEventStore) AppendBatch(_ context.Context, events []types.Event) error {
	if s.appendErr != nil {
		return s.appendErr
	}
	s.appendBatch = append(s.appendBatch, events)
	return nil
}
func (s *stubEventStore) Range(_ context.Context, _ types.EventFilter) ([]types.Event, error) {
	return s.rangeResult, s.rangeErr
}
func (s *stubEventStore) Subscribe(_ context.Context, _ types.EventFilter) (<-chan types.Event, error) {
	if s.subscribeErr != nil {
		return nil, s.subscribeErr
	}
	if s.subscribeChan == nil {
		s.subscribeChan = make(chan types.Event, 1)
	}
	return s.subscribeChan, nil
}
func (s *stubEventStore) Unsubscribe(_ context.Context, ch <-chan types.Event) error {
	s.unsubChan = ch
	return s.unsubErr
}
func (s *stubEventStore) Compact(_ context.Context, _ time.Time) (int64, error) {
	return s.compactCount, s.compactErr
}
func (s *stubEventStore) Count(_ context.Context, _ types.EventFilter) (int64, error) {
	return s.countResult, s.countErr
}

func TestEventServiceEmit(t *testing.T) {
	store := &stubEventStore{}
	svc := NewEventService(store)

	err := svc.Emit(context.Background(), types.EventNodeCreated, "my-repo", "indexer", map[string]any{"id": "n1"})
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	if len(store.appended) != 1 {
		t.Fatalf("appended = %d, want 1", len(store.appended))
	}
	got := store.appended[0]
	if got.Type != types.EventNodeCreated {
		t.Errorf("Type = %v, want %v", got.Type, types.EventNodeCreated)
	}
	if got.RepoSlug != "my-repo" {
		t.Errorf("RepoSlug = %s, want my-repo", got.RepoSlug)
	}
	if got.Source != "indexer" {
		t.Errorf("Source = %s, want indexer", got.Source)
	}
	if got.AuthorID != "system" {
		t.Errorf("AuthorID = %s, want system (default)", got.AuthorID)
	}
	if got.Timestamp.IsZero() {
		t.Errorf("Timestamp not stamped")
	}
	if got.Payload["id"] != "n1" {
		t.Errorf("Payload missing")
	}
}

func TestEventServiceEmitNilStore(t *testing.T) {
	svc := NewEventService(nil)
	if err := svc.Emit(context.Background(), types.EventNodeCreated, "r", "s", nil); err != nil {
		t.Errorf("Emit with nil store should succeed, got %v", err)
	}
}

func TestEventServiceEmitStoreError(t *testing.T) {
	store := &stubEventStore{appendErr: errors.New("boom")}
	svc := NewEventService(store)
	if err := svc.Emit(context.Background(), types.EventNodeCreated, "r", "s", nil); err == nil {
		t.Errorf("Emit should propagate store error")
	}
}

func TestEventServiceEmitBatch(t *testing.T) {
	store := &stubEventStore{}
	svc := NewEventService(store)

	now := time.Now()
	events := []types.Event{
		{Type: types.EventNodeCreated, RepoSlug: "r", Timestamp: now, AuthorID: "alice"},
		{Type: types.EventEdgeCreated, RepoSlug: "r"}, // missing timestamp + author → defaulted
	}
	if err := svc.EmitBatch(context.Background(), events); err != nil {
		t.Fatalf("EmitBatch: %v", err)
	}
	if len(store.appendBatch) != 1 || len(store.appendBatch[0]) != 2 {
		t.Fatalf("expected 1 batch of 2 events, got %+v", store.appendBatch)
	}
	got := store.appendBatch[0]
	if !got[0].Timestamp.Equal(now) {
		t.Errorf("explicit timestamp overwritten")
	}
	if got[0].AuthorID != "alice" {
		t.Errorf("explicit AuthorID overwritten: %s", got[0].AuthorID)
	}
	if got[1].Timestamp.IsZero() {
		t.Errorf("missing timestamp not defaulted")
	}
	if got[1].AuthorID != "system" {
		t.Errorf("missing AuthorID not defaulted: %s", got[1].AuthorID)
	}
}

func TestEventServiceEmitBatchNilStore(t *testing.T) {
	svc := NewEventService(nil)
	if err := svc.EmitBatch(context.Background(), []types.Event{{}}); err != nil {
		t.Errorf("EmitBatch with nil store should succeed, got %v", err)
	}
}

func TestEventServiceQuery(t *testing.T) {
	want := []types.Event{{Type: types.EventNodeCreated}}
	store := &stubEventStore{rangeResult: want}
	svc := NewEventService(store)

	got, err := svc.Query(context.Background(), types.EventFilter{RepoSlug: "r"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 1 || got[0].Type != types.EventNodeCreated {
		t.Errorf("Query result = %+v, want %+v", got, want)
	}
}

func TestEventServiceQueryNilStore(t *testing.T) {
	svc := NewEventService(nil)
	got, err := svc.Query(context.Background(), types.EventFilter{})
	if err != nil {
		t.Errorf("Query with nil store should succeed, got %v", err)
	}
	if got != nil {
		t.Errorf("Query with nil store should return nil events, got %+v", got)
	}
}

func TestEventServiceSubscribe(t *testing.T) {
	ch := make(chan types.Event, 1)
	store := &stubEventStore{subscribeChan: ch}
	svc := NewEventService(store)

	got, err := svc.Subscribe(context.Background(), types.EventFilter{})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if got == nil {
		t.Fatal("Subscribe returned nil channel")
	}
}

func TestEventServiceSubscribeNilStore(t *testing.T) {
	svc := NewEventService(nil)
	ch, err := svc.Subscribe(context.Background(), types.EventFilter{})
	if err != nil {
		t.Errorf("Subscribe with nil store should succeed, got %v", err)
	}
	// Channel should be closed immediately.
	select {
	case _, ok := <-ch:
		if ok {
			t.Errorf("nil-store Subscribe should return closed channel")
		}
	case <-time.After(100 * time.Millisecond):
		t.Errorf("nil-store Subscribe channel did not close")
	}
}

func TestEventServiceUnsubscribe(t *testing.T) {
	store := &stubEventStore{}
	svc := NewEventService(store)
	ch := make(chan types.Event)
	if err := svc.Unsubscribe(context.Background(), ch); err != nil {
		t.Errorf("Unsubscribe: %v", err)
	}
	if store.unsubChan == nil {
		t.Errorf("unsubscribe channel not forwarded to store")
	}
}

func TestEventServiceUnsubscribeNilStore(t *testing.T) {
	svc := NewEventService(nil)
	if err := svc.Unsubscribe(context.Background(), nil); err != nil {
		t.Errorf("Unsubscribe with nil store should succeed, got %v", err)
	}
}

func TestEventServiceCompact(t *testing.T) {
	store := &stubEventStore{compactCount: 42}
	svc := NewEventService(store)
	got, err := svc.Compact(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if got != 42 {
		t.Errorf("Compact count = %d, want 42", got)
	}
}

func TestEventServiceCompactError(t *testing.T) {
	store := &stubEventStore{compactErr: errors.New("compact failed")}
	svc := NewEventService(store)
	got, err := svc.Compact(context.Background(), time.Now())
	if err == nil {
		t.Errorf("Compact should propagate error")
	}
	if got != 0 {
		t.Errorf("Compact count on error = %d, want 0", got)
	}
}

func TestEventServiceCompactNilStore(t *testing.T) {
	svc := NewEventService(nil)
	got, err := svc.Compact(context.Background(), time.Now())
	if err != nil {
		t.Errorf("Compact with nil store should succeed, got %v", err)
	}
	if got != 0 {
		t.Errorf("Compact with nil store count = %d, want 0", got)
	}
}

func TestEventServiceCount(t *testing.T) {
	store := &stubEventStore{countResult: 7}
	svc := NewEventService(store)
	got, err := svc.Count(context.Background(), types.EventFilter{})
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if got != 7 {
		t.Errorf("Count = %d, want 7", got)
	}
}

func TestEventServiceCountNilStore(t *testing.T) {
	svc := NewEventService(nil)
	got, err := svc.Count(context.Background(), types.EventFilter{})
	if err != nil {
		t.Errorf("Count with nil store should succeed, got %v", err)
	}
	if got != 0 {
		t.Errorf("Count with nil store = %d, want 0", got)
	}
}
