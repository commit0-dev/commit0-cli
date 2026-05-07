package app

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/commit0-dev/commit0/pkg/types"
)

func newTestTracker() *IndexTracker {
	return NewIndexTracker("job-1", "owner/repo", types.IndexConfig{})
}

// ── NewIndexTracker ───────────────────────────────────────────────────────

func TestNewIndexTracker_Fields(t *testing.T) {
	tr := NewIndexTracker("j1", "org/repo", types.IndexConfig{})
	if tr.jobID != "j1" {
		t.Errorf("jobID = %q, want %q", tr.jobID, "j1")
	}
	if tr.repoSlug != "org/repo" {
		t.Errorf("repoSlug = %q", tr.repoSlug)
	}
	if len(tr.stages) != 8 {
		t.Errorf("expected 8 stages, got %d", len(tr.stages))
	}
	for _, sp := range tr.stages {
		if sp.Status != types.StatusPending {
			t.Errorf("initial stage status should be pending, got %q", sp.Status)
		}
	}
}

// ── StartStage / CompleteStage ────────────────────────────────────────────

func TestStartStage_SetsRunning(t *testing.T) {
	tr := newTestTracker()
	tr.StartStage(types.StageWalk)
	sp := tr.stages[types.StageWalk]
	if sp.Status != types.StatusRunning {
		t.Errorf("status = %q, want running", sp.Status)
	}
	if sp.StartedAt == nil {
		t.Error("StartedAt should be set")
	}
	if tr.currentStage != types.StageWalk {
		t.Errorf("currentStage = %q", tr.currentStage)
	}
}

func TestCompleteStage_SetsCompleted(t *testing.T) {
	tr := newTestTracker()
	tr.StartStage(types.StageParse)
	time.Sleep(time.Millisecond) // ensure non-zero duration
	tr.CompleteStage(types.StageParse)
	sp := tr.stages[types.StageParse]
	if sp.Status != types.StatusCompleted {
		t.Errorf("status = %q, want completed", sp.Status)
	}
	if sp.CompletedAt == nil {
		t.Error("CompletedAt should be set")
	}
	if sp.DurationMS < 0 {
		t.Errorf("DurationMS = %d, should be >= 0", sp.DurationMS)
	}
}

func TestCompleteStage_WithoutStart_NoNilPanic(t *testing.T) {
	tr := newTestTracker()
	// CompleteStage before StartStage: DurationMS stays 0 (StartedAt is nil).
	tr.CompleteStage(types.StageParse)
	sp := tr.stages[types.StageParse]
	if sp.Status != types.StatusCompleted {
		t.Errorf("status should still be completed: %q", sp.Status)
	}
}

// ── FailStage ─────────────────────────────────────────────────────────────

func TestFailStage_SetsFailedAndRecordsError(t *testing.T) {
	tr := newTestTracker()
	tr.StartStage(types.StageEmbed)
	tr.FailStage(types.StageEmbed, "embedding timeout")
	sp := tr.stages[types.StageEmbed]
	if sp.Status != types.StatusFailed {
		t.Errorf("status = %q, want failed", sp.Status)
	}
	if sp.ErrorCount != 1 {
		t.Errorf("ErrorCount = %d", sp.ErrorCount)
	}
	if len(sp.Errors) == 0 || !strings.Contains(sp.Errors[0].Message, "embedding timeout") {
		t.Errorf("error message not recorded: %v", sp.Errors)
	}
}

// ── SkipStage ─────────────────────────────────────────────────────────────

func TestSkipStage_SetsSkipped(t *testing.T) {
	tr := newTestTracker()
	tr.SkipStage(types.StageSummarize)
	if tr.stages[types.StageSummarize].Status != types.StatusSkipped {
		t.Error("SkipStage should set status to skipped")
	}
}

// ── SetStageTotal / IncrStage ─────────────────────────────────────────────

func TestSetStageTotal(t *testing.T) {
	tr := newTestTracker()
	tr.SetStageTotal(types.StageStore, 42)
	if tr.stages[types.StageStore].ItemsTotal != 42 {
		t.Errorf("ItemsTotal = %d", tr.stages[types.StageStore].ItemsTotal)
	}
}

func TestIncrStage(t *testing.T) {
	tr := newTestTracker()
	tr.IncrStage(types.StageStore, 5)
	tr.IncrStage(types.StageStore, 3)
	if tr.stages[types.StageStore].ItemsDone != 8 {
		t.Errorf("ItemsDone = %d, want 8", tr.stages[types.StageStore].ItemsDone)
	}
}

// ── AddStageError ─────────────────────────────────────────────────────────

func TestAddStageError_Accumulates(t *testing.T) {
	tr := newTestTracker()
	for i := 0; i < 3; i++ {
		tr.AddStageError(types.StageWalk, "file.go", "node", "error msg")
	}
	sp := tr.stages[types.StageWalk]
	if sp.ErrorCount != 3 {
		t.Errorf("ErrorCount = %d", sp.ErrorCount)
	}
	if len(sp.Errors) != 3 {
		t.Errorf("Errors len = %d", len(sp.Errors))
	}
}

func TestAddStageError_CappedAtMaxErrorsPerStage(t *testing.T) {
	tr := newTestTracker()
	for i := 0; i < maxErrorsPerStage+10; i++ {
		tr.AddStageError(types.StageWalk, "f.go", "n", "err")
	}
	sp := tr.stages[types.StageWalk]
	if len(sp.Errors) > maxErrorsPerStage {
		t.Errorf("Errors should be capped at %d, got %d", maxErrorsPerStage, len(sp.Errors))
	}
	if sp.ErrorCount != maxErrorsPerStage+10 {
		t.Errorf("ErrorCount = %d, want %d", sp.ErrorCount, maxErrorsPerStage+10)
	}
}

func TestAddStageError_UnknownStage_NoPanic(t *testing.T) {
	tr := newTestTracker()
	tr.AddStageError("nonexistent", "f.go", "n", "err") // must not panic
}

// ── Counter methods ───────────────────────────────────────────────────────

func TestAddFiles(t *testing.T) {
	tr := newTestTracker()
	tr.AddFiles(10)
	tr.AddFiles(5)
	if tr.filesIndexed != 15 {
		t.Errorf("filesIndexed = %d", tr.filesIndexed)
	}
}

func TestAddNodes(t *testing.T) {
	tr := newTestTracker()
	tr.AddNodes(7)
	if tr.nodesCreated != 7 {
		t.Errorf("nodesCreated = %d", tr.nodesCreated)
	}
}

func TestAddEdges(t *testing.T) {
	tr := newTestTracker()
	tr.AddEdges(3)
	if tr.edgesCreated != 3 {
		t.Errorf("edgesCreated = %d", tr.edgesCreated)
	}
}

// ── Coverage tracking ─────────────────────────────────────────────────────

func TestAddWalked(t *testing.T) {
	tr := newTestTracker()
	tr.AddWalked(12)
	if tr.coverage.FilesWalked != 12 {
		t.Errorf("FilesWalked = %d", tr.coverage.FilesWalked)
	}
}

func TestAddParsed(t *testing.T) {
	tr := newTestTracker()
	tr.AddParsed(100, 50)
	if tr.coverage.FilesParsed != 1 {
		t.Errorf("FilesParsed = %d", tr.coverage.FilesParsed)
	}
	if tr.coverage.NodesExtracted != 100 {
		t.Errorf("NodesExtracted = %d", tr.coverage.NodesExtracted)
	}
	if tr.coverage.EdgesExtracted != 50 {
		t.Errorf("EdgesExtracted = %d", tr.coverage.EdgesExtracted)
	}
}

func TestAddSkipped(t *testing.T) {
	tr := newTestTracker()
	tr.AddSkipped()
	tr.AddSkipped()
	if tr.coverage.FilesSkipped != 2 {
		t.Errorf("FilesSkipped = %d", tr.coverage.FilesSkipped)
	}
}

func TestAddSummarized(t *testing.T) {
	tr := newTestTracker()
	tr.AddSummarized(5)
	if tr.coverage.NodesSummarized != 5 {
		t.Errorf("NodesSummarized = %d", tr.coverage.NodesSummarized)
	}
}

func TestAddEmbedded(t *testing.T) {
	tr := newTestTracker()
	tr.AddEmbedded(8)
	if tr.coverage.NodesEmbedded != 8 {
		t.Errorf("NodesEmbedded = %d", tr.coverage.NodesEmbedded)
	}
}

func TestAddStored(t *testing.T) {
	tr := newTestTracker()
	tr.AddStored(10, 5)
	if tr.coverage.NodesStored != 10 || tr.coverage.EdgesStored != 5 {
		t.Errorf("NodesStored=%d EdgesStored=%d", tr.coverage.NodesStored, tr.coverage.EdgesStored)
	}
}

func TestAddCallEdgeResolution(t *testing.T) {
	tr := newTestTracker()
	tr.AddCallEdgeResolution(10, 7)
	if tr.coverage.CallEdgesTotal != 10 {
		t.Errorf("CallEdgesTotal = %d", tr.coverage.CallEdgesTotal)
	}
	if tr.coverage.CallEdgesResolved != 7 {
		t.Errorf("CallEdgesResolved = %d", tr.coverage.CallEdgesResolved)
	}
	if tr.coverage.CallEdgesUnresolved != 3 {
		t.Errorf("CallEdgesUnresolved = %d", tr.coverage.CallEdgesUnresolved)
	}
}

// ── Finish ────────────────────────────────────────────────────────────────

func TestFinish_Success(t *testing.T) {
	tr := newTestTracker()
	tr.Finish(nil)
	if !tr.finished {
		t.Error("finished should be true")
	}
	if tr.finalError != "" {
		t.Errorf("finalError = %q, want empty", tr.finalError)
	}
	if tr.finishedAt == nil {
		t.Error("finishedAt should be set")
	}
}

func TestFinish_WithError(t *testing.T) {
	tr := newTestTracker()
	tr.Finish(errors.New("index failed"))
	if tr.finalError != "index failed" {
		t.Errorf("finalError = %q", tr.finalError)
	}
}

// ── Snapshot ──────────────────────────────────────────────────────────────

func TestSnapshot_StatusIndexing(t *testing.T) {
	tr := newTestTracker()
	snap := tr.Snapshot()
	if snap.Status != "indexing" {
		t.Errorf("status = %q, want indexing", snap.Status)
	}
}

func TestSnapshot_StatusCompleted(t *testing.T) {
	tr := newTestTracker()
	tr.Finish(nil)
	snap := tr.Snapshot()
	if snap.Status != "completed" {
		t.Errorf("status = %q, want completed", snap.Status)
	}
}

func TestSnapshot_StatusFailed(t *testing.T) {
	tr := newTestTracker()
	tr.Finish(errors.New("boom"))
	snap := tr.Snapshot()
	if snap.Status != "failed" {
		t.Errorf("status = %q, want failed", snap.Status)
	}
	if snap.Error == "" {
		t.Error("Error should be set in snapshot")
	}
}

func TestSnapshot_DeepCopiesStages(t *testing.T) {
	tr := newTestTracker()
	tr.AddStageError(types.StageWalk, "f.go", "n", "msg")
	snap := tr.Snapshot()

	// Mutate the original
	tr.stages[types.StageWalk].Errors[0].Message = "mutated"

	// Snapshot should be unchanged
	if snap.Stages[types.StageWalk].Errors[0].Message == "mutated" {
		t.Error("Snapshot should return a deep copy of stage errors")
	}
}

func TestSnapshot_CoveragePercentages(t *testing.T) {
	tr := newTestTracker()
	tr.AddParsed(100, 0) // NodesExtracted = 100
	tr.AddSummarized(50)
	tr.AddEmbedded(80)
	tr.AddStored(90, 0)

	snap := tr.Snapshot()
	cov := snap.Coverage

	if cov.SummaryCoverage != 50.0 {
		t.Errorf("SummaryCoverage = %.1f, want 50.0", cov.SummaryCoverage)
	}
	if cov.EmbedCoverage != 80.0 {
		t.Errorf("EmbedCoverage = %.1f, want 80.0", cov.EmbedCoverage)
	}
	if cov.StoreCoverage != 90.0 {
		t.Errorf("StoreCoverage = %.1f, want 90.0", cov.StoreCoverage)
	}
}

func TestSnapshot_EdgeResolutionPercentage(t *testing.T) {
	tr := newTestTracker()
	tr.AddCallEdgeResolution(100, 75)
	snap := tr.Snapshot()
	if snap.Coverage.EdgeResolution != 75.0 {
		t.Errorf("EdgeResolution = %.1f, want 75.0", snap.Coverage.EdgeResolution)
	}
}

func TestSnapshot_ZeroNodesExtracted_NoPanic(t *testing.T) {
	tr := newTestTracker()
	// No nodes extracted → coverage computations should be zero / not divide by zero
	snap := tr.Snapshot()
	if snap.Coverage.SummaryCoverage != 0 {
		t.Errorf("SummaryCoverage should be 0 when no nodes extracted")
	}
}

func TestSnapshot_TotalErrors(t *testing.T) {
	tr := newTestTracker()
	tr.AddStageError(types.StageWalk, "f.go", "n", "err1")
	tr.AddStageError(types.StageWalk, "g.go", "n", "err2")
	tr.AddStageError(types.StageParse, "h.go", "n", "err3")
	snap := tr.Snapshot()
	if snap.TotalErrors != 3 {
		t.Errorf("TotalErrors = %d, want 3", snap.TotalErrors)
	}
}

func TestSnapshot_Fields(t *testing.T) {
	tr := newTestTracker()
	tr.AddFiles(5)
	tr.AddNodes(10)
	tr.AddEdges(3)
	snap := tr.Snapshot()
	if snap.JobID != "job-1" {
		t.Errorf("JobID = %q", snap.JobID)
	}
	if snap.RepoSlug != "owner/repo" {
		t.Errorf("RepoSlug = %q", snap.RepoSlug)
	}
	if snap.FilesIndexed != 5 {
		t.Errorf("FilesIndexed = %d", snap.FilesIndexed)
	}
	if snap.NodesCreated != 10 {
		t.Errorf("NodesCreated = %d", snap.NodesCreated)
	}
	if snap.EdgesCreated != 3 {
		t.Errorf("EdgesCreated = %d", snap.EdgesCreated)
	}
}

// ── Streaming events (SSE) ────────────────────────────────────────────────

// TestSubscribe_ReceivesStageStartAndDone confirms StartStage and CompleteStage
// fan out as IndexEvents to a live subscriber.
func TestSubscribe_ReceivesStageStartAndDone(t *testing.T) {
	tr := newTestTracker()
	ch, unsub := tr.Subscribe()
	defer unsub()

	tr.StartStage(types.StageWalk)
	tr.CompleteStage(types.StageWalk)

	got := drain(t, ch, 2, 500*time.Millisecond)
	if len(got) != 2 {
		t.Fatalf("expected 2 events, got %d", len(got))
	}
	if got[0].Type != types.IndexEventStageStart || got[0].Stage != types.StageWalk {
		t.Errorf("event 0: %+v", got[0])
	}
	if got[1].Type != types.IndexEventStageDone || got[1].Stage != types.StageWalk {
		t.Errorf("event 1: %+v", got[1])
	}
}

// TestEmitGraphDelta_NodesBeforeEdges asserts the node/edge ordering contract
// the frontend relies on: within a single delta, every node precedes every
// edge, and the wire shape is the trimmed delta DTO (not the full CodeNode).
func TestEmitGraphDelta_NodesBeforeEdges(t *testing.T) {
	tr := newTestTracker()
	ch, unsub := tr.Subscribe()
	defer unsub()

	nodes := []types.CodeNode{
		{ID: "function:a", Qualified: "pkg.A", Kind: types.NodeFunction, FilePath: "a.go"},
		{ID: "function:b", Qualified: "pkg.B", Kind: types.NodeFunction, FilePath: "a.go"},
	}
	edges := []types.CodeEdge{
		{FromID: "function:a", ToID: "function:b", Kind: types.EdgeCalls},
	}
	tr.EmitGraphDelta(nodes, edges)

	got := drain(t, ch, 1, 500*time.Millisecond)
	if len(got) != 1 || got[0].Type != types.IndexEventGraphDelta {
		t.Fatalf("expected 1 graph_delta event, got %+v", got)
	}
	evt := got[0]
	if len(evt.Nodes) != 2 {
		t.Errorf("nodes len = %d, want 2", len(evt.Nodes))
	}
	if len(evt.Edges) != 1 {
		t.Errorf("edges len = %d, want 1", len(evt.Edges))
	}
	if evt.Nodes[0].ID != "function:a" || evt.Nodes[1].ID != "function:b" {
		t.Errorf("node ordering not preserved: %+v", evt.Nodes)
	}
	if evt.Edges[0].FromID != "function:a" {
		t.Errorf("edge from ID wrong: %+v", evt.Edges[0])
	}
}

// TestEmitGraphDelta_Empty_NoEvent confirms zero-length deltas are dropped at
// the source so consumers never see useless empty events.
func TestEmitGraphDelta_Empty_NoEvent(t *testing.T) {
	tr := newTestTracker()
	ch, unsub := tr.Subscribe()
	defer unsub()

	tr.EmitGraphDelta(nil, nil)

	select {
	case e := <-ch:
		t.Errorf("expected no event for empty delta, got %+v", e)
	case <-time.After(50 * time.Millisecond):
		// Pass: no event produced.
	}
}

// TestFinish_EmitsDoneAndClosesChannel verifies the terminal contract: the
// final event is `done` with a full snapshot, and the channel is then closed
// so consumers exit cleanly.
func TestFinish_EmitsDoneAndClosesChannel(t *testing.T) {
	tr := newTestTracker()
	tr.AddNodes(7)
	ch, unsub := tr.Subscribe()
	defer unsub()

	tr.Finish(nil)

	// First event should be `done` with a non-nil snapshot.
	select {
	case evt, open := <-ch:
		if !open {
			t.Fatalf("channel closed before receiving done event")
		}
		if evt.Type != types.IndexEventDone {
			t.Errorf("expected done event, got %s", evt.Type)
		}
		if evt.Done == nil || evt.Done.NodesCreated != 7 {
			t.Errorf("done snapshot missing or wrong: %+v", evt.Done)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for done event")
	}

	// Channel must close after done is delivered.
	select {
	case _, open := <-ch:
		if open {
			t.Errorf("channel should be closed after done, but received another event")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("channel was not closed after done event")
	}
}

// TestSubscribe_AfterFinish_ReturnsClosedChannel ensures late attachers do not
// hang waiting for events that will never come.
func TestSubscribe_AfterFinish_ReturnsClosedChannel(t *testing.T) {
	tr := newTestTracker()
	tr.Finish(nil)

	ch, unsub := tr.Subscribe()
	defer unsub()

	select {
	case _, open := <-ch:
		if open {
			t.Errorf("late subscriber should get a closed channel, not an event")
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("late subscriber's channel was not closed")
	}
}

// TestEmit_LaggedConsumer_FailsFast simulates a consumer that does not drain
// the channel: once the buffer fills, the producer marks the sub lagged and
// stops sending — and crucially, never blocks the indexer.
func TestEmit_LaggedConsumer_FailsFast(t *testing.T) {
	tr := newTestTracker()
	_, unsub := tr.Subscribe()
	defer unsub()

	// Flood the channel with more events than the buffer holds. Producer must
	// not block; this whole sequence should complete well under the buffer
	// drain timeout.
	done := make(chan struct{})
	go func() {
		for i := 0; i < streamSubBufSize+50; i++ {
			tr.StartStage(types.StageStore) // generates a stage_start event
		}
		close(done)
	}()

	select {
	case <-done:
		// Pass: producer did not block on a slow consumer.
	case <-time.After(2 * time.Second):
		t.Fatal("producer blocked on a lagged consumer")
	}
}

// TestSubscribe_FanOutToMultipleConsumers confirms each subscriber receives
// every event independently (not first-come-first-steal).
func TestSubscribe_FanOutToMultipleConsumers(t *testing.T) {
	tr := newTestTracker()
	ch1, unsub1 := tr.Subscribe()
	defer unsub1()
	ch2, unsub2 := tr.Subscribe()
	defer unsub2()

	tr.StartStage(types.StageWalk)

	for i, ch := range []<-chan types.IndexEvent{ch1, ch2} {
		select {
		case evt := <-ch:
			if evt.Type != types.IndexEventStageStart {
				t.Errorf("ch%d: got %s, want stage_start", i+1, evt.Type)
			}
		case <-time.After(200 * time.Millisecond):
			t.Errorf("ch%d: timed out", i+1)
		}
	}
}

// TestUnsubscribe_StopsDeliveryAndClosesChannel verifies the unsub func cleanly
// detaches a consumer mid-run without panicking the producer.
func TestUnsubscribe_StopsDeliveryAndClosesChannel(t *testing.T) {
	tr := newTestTracker()
	ch, unsub := tr.Subscribe()

	tr.StartStage(types.StageWalk) // delivered before unsubscribe
	got := drain(t, ch, 1, 200*time.Millisecond)
	if len(got) != 1 {
		t.Fatalf("expected initial event, got %d", len(got))
	}

	unsub()

	// Channel must be closed by unsub.
	select {
	case _, open := <-ch:
		if open {
			t.Error("channel should be closed after unsubscribe")
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("channel not closed after unsubscribe")
	}

	// Producer must keep working without panic even though the sub is gone.
	tr.CompleteStage(types.StageWalk)
}

// drain pulls up to n events with a per-event budget. Used by event-ordering
// assertions where we expect a small, fixed number of events.
func drain(t *testing.T, ch <-chan types.IndexEvent, n int, budget time.Duration) []types.IndexEvent {
	t.Helper()
	out := make([]types.IndexEvent, 0, n)
	for i := 0; i < n; i++ {
		select {
		case evt := <-ch:
			out = append(out, evt)
		case <-time.After(budget):
			return out
		}
	}
	return out
}

// ── Concurrency safety ────────────────────────────────────────────────────

func TestIndexTracker_ConcurrencySafe(t *testing.T) {
	tr := newTestTracker()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tr.StartStage(types.StageWalk)
			tr.IncrStage(types.StageWalk, 1)
			tr.AddFiles(1)
			tr.AddNodes(1)
			tr.AddEdges(1)
			tr.AddWalked(1)
			tr.AddParsed(1, 1)
			tr.AddSkipped()
			tr.AddSummarized(1)
			tr.AddEmbedded(1)
			tr.AddStored(1, 1)
			tr.AddCallEdgeResolution(2, 1)
			tr.AddStageError(types.StageWalk, "f", "n", "e")
			_ = tr.Snapshot()
		}()
	}
	wg.Wait()
}
