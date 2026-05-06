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
