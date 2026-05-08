package app

import (
	"sync"
	"testing"
	"time"

	"github.com/commit0-dev/commit0/pkg/types"
)

// newTrackerForTest builds a minimal IndexTracker keyed by jobID for use in
// registry tests. We do not exercise the SSE fan-out here.
func newTrackerForTest(jobID string) *IndexTracker {
	return NewIndexTracker(jobID, "test/repo", types.IndexConfig{})
}

// TestRegisterTracker_LookupReturnsRunningTracker checks the basic register +
// GetTracker round trip while the job is still running (finishedAt zero).
func TestRegisterTracker_LookupReturnsRunningTracker(t *testing.T) {
	t.Parallel()

	is := &IndexService{}
	tracker := newTrackerForTest("job-running")

	is.registerTracker(tracker)

	got, ok := is.GetTracker("job-running")
	if !ok {
		t.Fatalf("GetTracker(job-running): want ok=true, got false")
	}
	if got != tracker {
		t.Fatalf("GetTracker(job-running): want same tracker pointer, got different")
	}
}

// TestRegisterTracker_NilTrackerIsNoOp guards the "nil tracker" branch in
// IndexWithProgress where progress reporting is disabled.
func TestRegisterTracker_NilTrackerIsNoOp(t *testing.T) {
	t.Parallel()

	is := &IndexService{}
	is.registerTracker(nil)

	// Registry must remain empty: a Range walk should see zero entries.
	count := 0
	is.trackerRegistry.Range(func(_, _ any) bool {
		count++
		return true
	})
	if count != 0 {
		t.Fatalf("registerTracker(nil): want empty registry, got %d entries", count)
	}
}

// TestMarkTrackerFinished_StaysQueryableInsideTTL verifies that marking a
// tracker finished does NOT immediately evict it; clients can still fetch the
// final snapshot via GetTracker for the full trackerEvictAfter window.
func TestMarkTrackerFinished_StaysQueryableInsideTTL(t *testing.T) {
	t.Parallel()

	is := &IndexService{}
	tracker := newTrackerForTest("job-finished")
	is.registerTracker(tracker)

	is.markTrackerFinished("job-finished")

	got, ok := is.GetTracker("job-finished")
	if !ok {
		t.Fatalf("GetTracker after Finish (within TTL): want ok=true, got false")
	}
	if got != tracker {
		t.Fatalf("GetTracker: want same tracker pointer, got different")
	}
}

// TestGetTracker_EvictsAfterTTL forces a finishedAt timestamp older than
// trackerEvictAfter and confirms the next GetTracker call returns (nil, false)
// and that the registry entry is gone.
func TestGetTracker_EvictsAfterTTL(t *testing.T) {
	t.Parallel()

	is := &IndexService{}
	tracker := newTrackerForTest("job-expired")
	is.registerTracker(tracker)

	// Force the entry's finishedAt into the past, beyond the eviction window.
	expired := time.Now().Add(-(trackerEvictAfter + time.Minute))
	is.trackerRegistry.Store("job-expired", trackerEntry{
		tracker:    tracker,
		finishedAt: expired,
	})

	got, ok := is.GetTracker("job-expired")
	if ok {
		t.Fatalf("GetTracker after TTL: want ok=false, got true (tracker=%v)", got)
	}

	// The lazy sweep should have removed the entry from the underlying map.
	if _, present := is.trackerRegistry.Load("job-expired"); present {
		t.Fatalf("entry still present in registry after TTL sweep")
	}
}

// TestGetTracker_UnknownJobIDReturnsFalse covers the lookup-miss path.
func TestGetTracker_UnknownJobIDReturnsFalse(t *testing.T) {
	t.Parallel()

	is := &IndexService{}
	got, ok := is.GetTracker("nope")
	if ok {
		t.Fatalf("GetTracker(nope): want ok=false, got true (tracker=%v)", got)
	}
}

// TestRegisterTracker_RegisterAfterFinishResetsWindow models a retry: the
// caller registers a fresh tracker under the same jobID after the previous
// run finished. The new entry's finishedAt must be zero (running again).
func TestRegisterTracker_RegisterAfterFinishResetsWindow(t *testing.T) {
	t.Parallel()

	is := &IndexService{}
	first := newTrackerForTest("job-retry")
	is.registerTracker(first)
	is.markTrackerFinished("job-retry")

	second := newTrackerForTest("job-retry")
	is.registerTracker(second)

	value, ok := is.trackerRegistry.Load("job-retry")
	if !ok {
		t.Fatalf("registerTracker after retry: entry missing")
	}
	entry, ok := value.(trackerEntry)
	if !ok {
		t.Fatalf("registry value has unexpected type: %T", value)
	}
	if entry.tracker != second {
		t.Fatalf("retry: registry still points at first tracker")
	}
	if !entry.finishedAt.IsZero() {
		t.Fatalf("retry: finishedAt should be zero after re-register, got %v", entry.finishedAt)
	}
}

// TestTrackerRegistry_ConcurrentRegisterAndLookup is a small race-detector
// smoke test: many goroutines register and read trackers in parallel and the
// test must complete without a data race or panic.
//
// Run with: go test -race ./internal/app/... -run TestTrackerRegistry_Concurrent
func TestTrackerRegistry_ConcurrentRegisterAndLookup(t *testing.T) {
	t.Parallel()

	is := &IndexService{}

	const writers = 8
	const readers = 8
	const opsPerGoroutine = 200

	var wg sync.WaitGroup
	wg.Add(writers + readers)

	for w := 0; w < writers; w++ {
		go func(w int) {
			defer wg.Done()
			for i := 0; i < opsPerGoroutine; i++ {
				jobID := writerJobID(w, i)
				tracker := newTrackerForTest(jobID)
				is.registerTracker(tracker)
				if i%4 == 0 {
					is.markTrackerFinished(jobID)
				}
			}
		}(w)
	}

	for r := 0; r < readers; r++ {
		go func() {
			defer wg.Done()
			for i := 0; i < opsPerGoroutine; i++ {
				// Lookup will frequently miss (different jobIDs) — that's fine.
				_, _ = is.GetTracker("any")
			}
		}()
	}

	wg.Wait()
}

// writerJobID is a tiny deterministic key generator so we don't import
// fmt.Sprintf in a hot loop.
func writerJobID(worker, iter int) string {
	const digits = "0123456789"
	buf := []byte{'j', '-', byte('A' + worker), '-'}
	for iter > 0 || len(buf) == 4 {
		buf = append(buf, digits[iter%10])
		iter /= 10
		if iter == 0 {
			break
		}
	}
	return string(buf)
}
