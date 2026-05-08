package http

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/commit0-dev/commit0/pkg/types"
	"github.com/commit0-dev/commit0/server/internal/app"
)

// indexTrackerStore holds active and completed IndexTrackers for polling.
type indexTrackerStore struct {
	trackers map[string]*app.IndexTracker
	mu       sync.RWMutex
}

func newIndexTrackerStore() *indexTrackerStore {
	return &indexTrackerStore{trackers: make(map[string]*app.IndexTracker)}
}

func (s *indexTrackerStore) set(jobID string, t *app.IndexTracker) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.trackers[jobID] = t
}

func (s *indexTrackerStore) get(jobID string) (*app.IndexTracker, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.trackers[jobID]
	return t, ok
}

// newJobID generates a random hex job ID using crypto/rand.
func newJobID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

type startIndexRequest struct {
	RepoPath  string   `json:"repo_path"`
	RepoSlug  string   `json:"repo_slug"`
	Languages []string `json:"languages"`
	Exclude   []string `json:"exclude"`
	Force     bool     `json:"force"`
	Reparse   bool     `json:"reparse"`
	Fast      bool     `json:"fast"`
}

// handleStartIndex handles POST /api/v1/index.
func (s *Server) handleStartIndex(c *gin.Context) {
	var req startIndexRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid request body"})
		return
	}
	if req.RepoPath == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "repo_path is required"})
		return
	}
	if req.RepoSlug == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "repo_slug is required"})
		return
	}

	jobID, err := newJobID()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "failed to generate job ID"})
		return
	}

	// Build config snapshot for debugging.
	indexCfg := types.IndexConfig{
		Fast:    req.Fast,
		Reparse: req.Reparse,
		Force:   req.Force,
	}
	if s.fullCfg != nil {
		indexCfg.EmbedProvider = s.fullCfg.EmbedProvider
		indexCfg.LLMProvider = s.fullCfg.LLMProvider
		indexCfg.EmbedDim = s.fullCfg.EmbedDim
		indexCfg.BatchSize = s.fullCfg.BatchSize
		switch s.fullCfg.EmbedProvider {
		case "gemini":
			indexCfg.EmbedModel = s.fullCfg.Gemini.EmbedModel
		case "voyage":
			indexCfg.EmbedModel = s.fullCfg.Voyage.Model
		case "ollama":
			indexCfg.EmbedModel = s.fullCfg.Ollama.EmbedModel
		}
		switch s.fullCfg.LLMProvider {
		case "gemini":
			indexCfg.LLMModel = s.fullCfg.Gemini.ExplainModel
		case "openrouter":
			indexCfg.LLMModel = s.fullCfg.OpenRouter.Model
		case "ollama":
			indexCfg.LLMModel = s.fullCfg.Ollama.Model
		}
	}

	tracker := app.NewIndexTracker(jobID, req.RepoSlug, indexCfg)
	s.trackers.set(jobID, tracker)

	go func() {
		// Legacy progress callback (for backward compat with indexRun counters).
		onProgress := func(filesIndexed, nodesCreated int) {}

		_, indexErr := s.indexSvc.IndexWithProgress(context.Background(), app.IndexRequest{
			RepoPath:  req.RepoPath,
			RepoSlug:  req.RepoSlug,
			Languages: req.Languages,
			Force:     req.Force,
			Reparse:   req.Reparse,
			Fast:      req.Fast,
		}, onProgress, tracker)

		tracker.Finish(indexErr)
	}()

	c.JSON(http.StatusAccepted, gin.H{"job_id": jobID})
}

// handleIndexStatus handles GET /api/v1/index/:job_id.
func (s *Server) handleIndexStatus(c *gin.Context) {
	jobID := c.Param("job_id")
	tracker, ok := s.trackers.get(jobID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"message": "job not found"})
		return
	}
	c.JSON(http.StatusOK, tracker.Snapshot())
}

// handleIndexStream handles GET /api/v1/index/:job_id/stream — SSE stream of
// indexing events: stage_start, graph_delta (nodes + edges), stage_done,
// progress, done, error. Coexists with the poll endpoint (handleIndexStatus);
// the indexer keeps running independently of any single consumer.
//
// Late attachers get a synthetic `progress` snapshot first (so the UI has
// non-empty state immediately) and the terminal `done` event when the job
// finishes — but they do NOT see graph_delta events that fired before they
// connected. The poll endpoint is the source of truth for fully-attached
// state; the stream is for live deltas.
func (s *Server) handleIndexStream(c *gin.Context) {
	jobID := c.Param("job_id")
	tracker, ok := s.trackers.get(jobID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"message": "job not found"})
		return
	}

	// Switch the response to SSE before subscribing so any error path above
	// doesn't leave a half-open stream.
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)

	// SSE responses are long-lived; clear the per-write deadline so the
	// pipeline can hold the connection open until the job finishes or the
	// client disconnects. (Same pattern as handleAgentChat / handleTrace.)
	if ctrl := http.NewResponseController(c.Writer); ctrl != nil {
		_ = ctrl.SetWriteDeadline(time.Time{})
	}

	events, unsub := tracker.Subscribe()
	defer unsub()

	// Catch-up snapshot: emit one progress event so a late attacher sees the
	// current state immediately rather than waiting for the next tick.
	snap := tracker.Snapshot()
	writeSSE(c, string(types.IndexEventProgress), types.IndexEvent{
		Type: types.IndexEventProgress,
		Progress: &types.ProgressSnapshot{
			FilesIndexed: snap.FilesIndexed,
			NodesCreated: snap.NodesCreated,
			EdgesCreated: snap.EdgesCreated,
			ElapsedMS:    snap.ElapsedMS,
			CurrentStage: snap.CurrentStage,
		},
		EmittedAt: time.Now(),
	})

	// Already-finished job: emit a synthetic `done` and exit cleanly. This
	// covers the case where a client polls the stream after completion to
	// fetch the final summary in the same uniform way live consumers do.
	if snap.Status == "completed" || snap.Status == "failed" {
		writeSSE(c, string(types.IndexEventDone), types.IndexEvent{
			Type:      types.IndexEventDone,
			Done:      &snap,
			EmittedAt: time.Now(),
		})
		return
	}

	ctx := c.Request.Context()
	for {
		select {
		case evt, open := <-events:
			if !open {
				return
			}
			writeSSE(c, string(evt.Type), evt)
		case <-ctx.Done():
			// Client disconnected mid-stream — unsub via defer and let the
			// indexer keep running (its lifetime is decoupled from this req).
			return
		}
	}
}
