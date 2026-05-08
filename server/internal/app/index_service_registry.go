package app

import (
	"time"
)

// trackerEvictAfter is how long a finished IndexTracker remains queryable via
// GetTracker before being lazily evicted from the registry. Long enough that an
// MCP client can fetch a final commit0_index_status snapshot without racing
// the SSE stream's terminal `done` event; short enough that completed jobs do
// not accumulate indefinitely on long-lived servers.
const trackerEvictAfter = 30 * time.Minute

// trackerEntry is one row in IndexService.trackerRegistry.
//
// finishedAt is the zero value while the job is running and is stamped with
// the wall-clock time when IndexWithProgress returns. Eviction is lazy:
// GetTracker walks the registry on each call and removes any entry whose
// finished window has expired.
type trackerEntry struct {
	tracker    *IndexTracker
	finishedAt time.Time
}

// registerTracker records a tracker in the per-service registry, keyed by its
// JobID. Called from IndexWithProgress on entry. Replacing an entry with the
// same jobID is allowed (e.g. a retry) and resets the finished window.
func (is *IndexService) registerTracker(tracker *IndexTracker) {
	if tracker == nil {
		return
	}
	is.trackerRegistry.Store(tracker.JobID(), trackerEntry{tracker: tracker})
}

// markTrackerFinished stamps the finishedAt field of the registry entry for
// jobID. Called from IndexWithProgress's defer block after Index returns. The
// tracker stays queryable for trackerEvictAfter, then is swept on the next
// GetTracker call.
//
// No-op if jobID is unknown (tracker may have been evicted between
// registration and Finish — extremely unlikely in practice).
func (is *IndexService) markTrackerFinished(jobID string) {
	value, ok := is.trackerRegistry.Load(jobID)
	if !ok {
		return
	}
	entry, ok := value.(trackerEntry)
	if !ok {
		return
	}
	entry.finishedAt = time.Now()
	is.trackerRegistry.Store(jobID, entry)
}

// GetTracker returns the IndexTracker registered for jobID, or (nil, false) if
// no entry exists or the entry has been evicted by TTL.
//
// Each call performs a lazy sweep: any entry whose finishedAt is non-zero and
// older than trackerEvictAfter is deleted. This keeps the registry bounded
// without a background goroutine.
func (is *IndexService) GetTracker(jobID string) (*IndexTracker, bool) {
	now := time.Now()
	is.sweepExpiredTrackersLocked(now)

	value, ok := is.trackerRegistry.Load(jobID)
	if !ok {
		return nil, false
	}
	entry, ok := value.(trackerEntry)
	if !ok {
		return nil, false
	}
	return entry.tracker, true
}

// RegisterTrackerForTest seeds the registry directly without running an
// indexing pipeline. Exposed so tests outside package app (notably the MCP
// adapter layer) can verify the GetTracker behavior through the public API
// without spinning up a full IndexService dependency graph.
//
// Production code must not call this — IndexWithProgress is the only
// supported registration path so the finishedAt accounting stays correct.
func (is *IndexService) RegisterTrackerForTest(tracker *IndexTracker) {
	is.registerTracker(tracker)
}

// sweepExpiredTrackersLocked removes any registry entry whose finished window
// has expired. The "Locked" suffix is conventional: sync.Map provides its own
// locking, so callers do not need to hold an external lock.
func (is *IndexService) sweepExpiredTrackersLocked(now time.Time) {
	is.trackerRegistry.Range(func(key, value any) bool {
		entry, ok := value.(trackerEntry)
		if !ok {
			is.trackerRegistry.Delete(key)
			return true
		}
		if !entry.finishedAt.IsZero() && now.Sub(entry.finishedAt) > trackerEvictAfter {
			is.trackerRegistry.Delete(key)
		}
		return true
	})
}
