package app

import (
	"sync"
	"time"

	"github.com/commit0-dev/commit0/pkg/types"
)

const maxErrorsPerStage = 50

// IndexTracker provides thread-safe progress tracking for an index pipeline run.
// The HTTP handler polls Snapshot() to serve the progress API.
type IndexTracker struct {
	mu sync.RWMutex

	jobID     string
	repoSlug  string
	config    types.IndexConfig
	startedAt time.Time

	currentStage types.IndexStage
	stages       map[types.IndexStage]*types.StageProgress

	filesIndexed int
	nodesCreated int
	edgesCreated int
	finalError   string
	finished     bool
	finishedAt   *time.Time

	// Coverage tracking (AST ↔ downstream)
	coverage types.PipelineCoverage
}

// NewIndexTracker creates a tracker for a new index job.
func NewIndexTracker(jobID, repoSlug string, cfg types.IndexConfig) *IndexTracker {
	allStages := []types.IndexStage{
		types.StageWalk, types.StageParse, types.StageSummarize,
		types.StageEmbed, types.StageStore, types.StageReembed,
		types.StageTemporal, types.StageCleanup,
	}
	stages := make(map[types.IndexStage]*types.StageProgress, len(allStages))
	for _, s := range allStages {
		stages[s] = &types.StageProgress{Status: types.StatusPending}
	}

	return &IndexTracker{
		jobID:     jobID,
		repoSlug:  repoSlug,
		config:    cfg,
		startedAt: time.Now(),
		stages:    stages,
	}
}

// StartStage marks a stage as running.
func (t *IndexTracker) StartStage(stage types.IndexStage) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.currentStage = stage
	if sp, ok := t.stages[stage]; ok {
		now := time.Now()
		sp.Status = types.StatusRunning
		sp.StartedAt = &now
	}
}

// CompleteStage marks a stage as completed.
func (t *IndexTracker) CompleteStage(stage types.IndexStage) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if sp, ok := t.stages[stage]; ok {
		now := time.Now()
		sp.Status = types.StatusCompleted
		sp.CompletedAt = &now
		if sp.StartedAt != nil {
			sp.DurationMS = now.Sub(*sp.StartedAt).Milliseconds()
		}
	}
}

// FailStage marks a stage as failed.
func (t *IndexTracker) FailStage(stage types.IndexStage, errMsg string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if sp, ok := t.stages[stage]; ok {
		now := time.Now()
		sp.Status = types.StatusFailed
		sp.CompletedAt = &now
		if sp.StartedAt != nil {
			sp.DurationMS = now.Sub(*sp.StartedAt).Milliseconds()
		}
		t.addStageErrorLocked(stage, "", "", errMsg)
	}
}

// SkipStage marks a stage as skipped (e.g., summarize in --fast mode).
func (t *IndexTracker) SkipStage(stage types.IndexStage) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if sp, ok := t.stages[stage]; ok {
		sp.Status = types.StatusSkipped
	}
}

// SetStageTotal sets the expected total for a stage (if known).
func (t *IndexTracker) SetStageTotal(stage types.IndexStage, total int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if sp, ok := t.stages[stage]; ok {
		sp.ItemsTotal = total
	}
}

// IncrStage increments the items_done counter for a stage.
func (t *IndexTracker) IncrStage(stage types.IndexStage, n int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if sp, ok := t.stages[stage]; ok {
		sp.ItemsDone += n
	}
}

// AddStageError records an error in a specific stage (capped at maxErrorsPerStage).
func (t *IndexTracker) AddStageError(stage types.IndexStage, file, node, msg string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.addStageErrorLocked(stage, file, node, msg)
}

func (t *IndexTracker) addStageErrorLocked(stage types.IndexStage, file, node, msg string) {
	sp, ok := t.stages[stage]
	if !ok {
		return
	}
	sp.ErrorCount++
	if len(sp.Errors) < maxErrorsPerStage {
		sp.Errors = append(sp.Errors, types.StageError{
			File:    file,
			Node:    node,
			Message: msg,
			Stage:   string(stage),
		})
	}
}

// AddFiles increments files indexed counter.
func (t *IndexTracker) AddFiles(n int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.filesIndexed += n
}

// AddNodes increments nodes created counter.
func (t *IndexTracker) AddNodes(n int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.nodesCreated += n
}

// AddEdges increments edges created counter.
func (t *IndexTracker) AddEdges(n int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.edgesCreated += n
}

// ── Coverage tracking ─────────────────────────────────────────────────────

// AddWalked records a file discovered by the walker.
func (t *IndexTracker) AddWalked(n int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.coverage.FilesWalked += n
}

// AddParsed records a file successfully parsed.
func (t *IndexTracker) AddParsed(nodes, edges int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.coverage.FilesParsed++
	t.coverage.NodesExtracted += nodes
	t.coverage.EdgesExtracted += edges
}

// AddSkipped records a file skipped (unchanged ContentHash).
func (t *IndexTracker) AddSkipped() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.coverage.FilesSkipped++
}

// AddSummarized records nodes that received LLM summaries.
func (t *IndexTracker) AddSummarized(n int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.coverage.NodesSummarized += n
}

// AddEmbedded records nodes that received embedding vectors.
func (t *IndexTracker) AddEmbedded(n int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.coverage.NodesEmbedded += n
}

// AddStored records nodes and edges written to the DB.
func (t *IndexTracker) AddStored(nodes, edges int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.coverage.NodesStored += nodes
	t.coverage.EdgesStored += edges
}

// AddCallEdgeResolution records resolved vs unresolved call edges.
func (t *IndexTracker) AddCallEdgeResolution(total, resolved int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.coverage.CallEdgesTotal += total
	t.coverage.CallEdgesResolved += resolved
	t.coverage.CallEdgesUnresolved += (total - resolved)
}

// Finish marks the job as completed or failed.
func (t *IndexTracker) Finish(err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now()
	t.finished = true
	t.finishedAt = &now
	if err != nil {
		t.finalError = err.Error()
	}
}

// Snapshot returns a deep copy of the current progress state.
// Thread-safe: can be called from the HTTP handler goroutine.
func (t *IndexTracker) Snapshot() types.IndexProgress {
	t.mu.RLock()
	defer t.mu.RUnlock()

	status := "indexing"
	if t.finished {
		if t.finalError != "" {
			status = "failed"
		} else {
			status = "completed"
		}
	}

	// Deep copy stages
	stages := make(map[types.IndexStage]*types.StageProgress, len(t.stages))
	totalErrors := 0
	for k, v := range t.stages {
		cp := *v
		if len(v.Errors) > 0 {
			cp.Errors = make([]types.StageError, len(v.Errors))
			copy(cp.Errors, v.Errors)
		}
		stages[k] = &cp
		totalErrors += v.ErrorCount
	}

	elapsed := time.Since(t.startedAt).Milliseconds()

	// Compute coverage percentages
	cov := t.coverage // struct copy
	if cov.NodesExtracted > 0 {
		cov.SummaryCoverage = float64(cov.NodesSummarized) / float64(cov.NodesExtracted) * 100
		cov.EmbedCoverage = float64(cov.NodesEmbedded) / float64(cov.NodesExtracted) * 100
		cov.StoreCoverage = float64(cov.NodesStored) / float64(cov.NodesExtracted) * 100
	}
	if cov.CallEdgesTotal > 0 {
		cov.EdgeResolution = float64(cov.CallEdgesResolved) / float64(cov.CallEdgesTotal) * 100
	}

	return types.IndexProgress{
		JobID:        t.jobID,
		Status:       status,
		RepoSlug:     t.repoSlug,
		Error:        t.finalError,
		FilesIndexed: t.filesIndexed,
		NodesCreated: t.nodesCreated,
		EdgesCreated: t.edgesCreated,
		TotalErrors:  totalErrors,
		StartedAt:    t.startedAt,
		FinishedAt:   t.finishedAt,
		ElapsedMS:    elapsed,
		CurrentStage: t.currentStage,
		Stages:       stages,
		Coverage:     cov,
		Config:       t.config,
	}
}
