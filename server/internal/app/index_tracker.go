package app

import (
	"sync"
	"time"

	"github.com/commit0-dev/commit0/pkg/types"
)

const maxErrorsPerStage = 50

// streamSubBufSize bounds each SSE consumer's per-subscription buffer. Picked
// for the indexing rate of a typical run (~1500 nodes total, bursty but well
// under 256 events between two consumer reads). When full, we mark the
// subscription "lagged" and stop sending to it; the indexer never blocks.
const streamSubBufSize = 256

// indexSub is one SSE consumer attached to a tracker. The producer fans events
// out to every live sub; a sub that lags is marked and skipped from then on.
type indexSub struct {
	ch     chan types.IndexEvent
	lagged bool
}

// IndexTracker provides thread-safe progress tracking for an index pipeline run.
// The HTTP handler polls Snapshot() to serve the progress API, and SSE handlers
// call Subscribe() to receive a live event stream.
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

	// Streaming fan-out. subMu guards subs and subsClosed; do NOT take t.mu
	// while holding subMu (and vice versa) — keep the locks disjoint.
	subMu      sync.Mutex
	subs       map[*indexSub]struct{}
	subsClosed bool
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
		subs:      make(map[*indexSub]struct{}),
	}
}

// ── Subscriber model ──────────────────────────────────────────────────────

// Subscribe attaches a new SSE consumer. The returned channel receives live
// IndexEvents until the tracker is finished (channel is closed) or the caller
// invokes the returned unsubscribe func. The channel is buffered; if a
// consumer falls behind, an `error` event is queued and subsequent payloads
// are dropped silently — the indexer never blocks on a slow consumer.
//
// If the tracker has already been finished, Subscribe returns nil channel and
// a no-op unsubscribe; callers should fall back to Snapshot().
func (t *IndexTracker) Subscribe() (<-chan types.IndexEvent, func()) {
	t.subMu.Lock()
	defer t.subMu.Unlock()

	if t.subsClosed {
		// Late attacher: surface a closed channel so the SSE loop ends cleanly.
		ch := make(chan types.IndexEvent)
		close(ch)
		return ch, func() {}
	}

	sub := &indexSub{ch: make(chan types.IndexEvent, streamSubBufSize)}
	t.subs[sub] = struct{}{}

	unsub := func() {
		t.subMu.Lock()
		defer t.subMu.Unlock()
		if _, ok := t.subs[sub]; !ok {
			return
		}
		delete(t.subs, sub)
		close(sub.ch)
	}
	return sub.ch, unsub
}

// emit fans an event out to every live subscriber. Non-blocking per sub: if a
// sub's buffer is full, mark it lagged, deliver one final `error` event, and
// stop sending to it. Producer never blocks.
func (t *IndexTracker) emit(evt types.IndexEvent) {
	if evt.EmittedAt.IsZero() {
		evt.EmittedAt = time.Now()
	}
	t.subMu.Lock()
	defer t.subMu.Unlock()
	if t.subsClosed || len(t.subs) == 0 {
		return
	}
	for sub := range t.subs {
		if sub.lagged {
			continue
		}
		select {
		case sub.ch <- evt:
		default:
			sub.lagged = true
			// Best-effort lag notice; if even this can't enqueue, drop it.
			lag := types.IndexEvent{
				Type:      types.IndexEventError,
				Message:   "stream consumer lagged; reconnect for current state",
				EmittedAt: time.Now(),
			}
			select {
			case sub.ch <- lag:
			default:
			}
		}
	}
}

// closeSubscribers closes every subscriber's channel exactly once. Called from
// Finish() after the terminal `done` event has been emitted.
func (t *IndexTracker) closeSubscribers() {
	t.subMu.Lock()
	defer t.subMu.Unlock()
	if t.subsClosed {
		return
	}
	t.subsClosed = true
	for sub := range t.subs {
		close(sub.ch)
		delete(t.subs, sub)
	}
}

// StartStage marks a stage as running.
func (t *IndexTracker) StartStage(stage types.IndexStage) {
	t.mu.Lock()
	t.currentStage = stage
	var total int
	if sp, ok := t.stages[stage]; ok {
		now := time.Now()
		sp.Status = types.StatusRunning
		sp.StartedAt = &now
		total = sp.ItemsTotal
	}
	t.mu.Unlock()
	t.emit(types.IndexEvent{
		Type:        types.IndexEventStageStart,
		Stage:       stage,
		ItemsTotal:  total,
		StageStatus: types.StatusRunning,
	})
}

// CompleteStage marks a stage as completed.
func (t *IndexTracker) CompleteStage(stage types.IndexStage) {
	t.mu.Lock()
	var (
		dur      int64
		done     int
		errCount int
	)
	if sp, ok := t.stages[stage]; ok {
		now := time.Now()
		sp.Status = types.StatusCompleted
		sp.CompletedAt = &now
		if sp.StartedAt != nil {
			sp.DurationMS = now.Sub(*sp.StartedAt).Milliseconds()
		}
		dur = sp.DurationMS
		done = sp.ItemsDone
		errCount = sp.ErrorCount
	}
	t.mu.Unlock()
	t.emit(types.IndexEvent{
		Type:        types.IndexEventStageDone,
		Stage:       stage,
		ItemsDone:   done,
		ErrorCount:  errCount,
		DurationMS:  dur,
		StageStatus: types.StatusCompleted,
	})
}

// FailStage marks a stage as failed.
func (t *IndexTracker) FailStage(stage types.IndexStage, errMsg string) {
	t.mu.Lock()
	var (
		dur      int64
		done     int
		errCount int
	)
	if sp, ok := t.stages[stage]; ok {
		now := time.Now()
		sp.Status = types.StatusFailed
		sp.CompletedAt = &now
		if sp.StartedAt != nil {
			sp.DurationMS = now.Sub(*sp.StartedAt).Milliseconds()
		}
		t.addStageErrorLocked(stage, "", "", errMsg)
		dur = sp.DurationMS
		done = sp.ItemsDone
		errCount = sp.ErrorCount
	}
	t.mu.Unlock()
	t.emit(types.IndexEvent{
		Type:        types.IndexEventStageDone,
		Stage:       stage,
		ItemsDone:   done,
		ErrorCount:  errCount,
		DurationMS:  dur,
		StageStatus: types.StatusFailed,
		Message:     errMsg,
	})
}

// SkipStage marks a stage as skipped (e.g., summarize in --fast mode).
func (t *IndexTracker) SkipStage(stage types.IndexStage) {
	t.mu.Lock()
	if sp, ok := t.stages[stage]; ok {
		sp.Status = types.StatusSkipped
	}
	t.mu.Unlock()
	t.emit(types.IndexEvent{
		Type:        types.IndexEventStageDone,
		Stage:       stage,
		StageStatus: types.StatusSkipped,
	})
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

// Finish marks the job as completed or failed and emits the terminal `done`
// event before closing every live subscriber's channel. Safe to call once.
func (t *IndexTracker) Finish(err error) {
	t.mu.Lock()
	now := time.Now()
	t.finished = true
	t.finishedAt = &now
	if err != nil {
		t.finalError = err.Error()
	}
	t.mu.Unlock()

	// Emit the terminal `done` event with the full snapshot, then close subs.
	snap := t.Snapshot()
	t.emit(types.IndexEvent{
		Type: types.IndexEventDone,
		Done: &snap,
	})
	t.closeSubscribers()
}

// EmitGraphDelta enqueues a graph_delta event carrying a batch of newly
// stored nodes and edges. Called from the store stage AFTER graph.PutBatch
// succeeds so consumers only see persisted graph state.
//
// The slice arguments are read-only — copy into delta DTOs and discard.
func (t *IndexTracker) EmitGraphDelta(nodes []types.CodeNode, edges []types.CodeEdge) {
	if len(nodes) == 0 && len(edges) == 0 {
		return
	}
	nd := make([]types.GraphNodeDelta, 0, len(nodes))
	for _, n := range nodes {
		nd = append(nd, types.GraphNodeDelta{
			ID:        n.ID,
			Qualified: n.Qualified,
			Name:      n.Name,
			Kind:      n.Kind,
			FilePath:  n.FilePath,
			Language:  n.Language,
			RepoSlug:  n.RepoSlug,
			StartLine: n.StartLine,
			EndLine:   n.EndLine,
			Signature: n.Signature,
		})
	}
	ed := make([]types.GraphEdgeDelta, 0, len(edges))
	for _, e := range edges {
		ed = append(ed, types.GraphEdgeDelta{
			FromID:   e.FromID,
			ToID:     e.ToID,
			Kind:     e.Kind,
			CallSite: e.CallSite,
		})
	}
	t.emit(types.IndexEvent{
		Type:  types.IndexEventGraphDelta,
		Nodes: nd,
		Edges: ed,
	})
}

// EmitProgress enqueues a periodic progress event. Producer-side throttling
// is the caller's responsibility — IndexService runs a single ticker.
func (t *IndexTracker) EmitProgress() {
	t.mu.RLock()
	snap := types.ProgressSnapshot{
		FilesIndexed: t.filesIndexed,
		NodesCreated: t.nodesCreated,
		EdgesCreated: t.edgesCreated,
		ElapsedMS:    time.Since(t.startedAt).Milliseconds(),
		CurrentStage: t.currentStage,
	}
	t.mu.RUnlock()
	t.emit(types.IndexEvent{
		Type:     types.IndexEventProgress,
		Progress: &snap,
	})
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
