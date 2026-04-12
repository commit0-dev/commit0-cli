package http

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"

	"github.com/commit0-dev/commit0/server/internal/app"
	"github.com/commit0-dev/commit0/pkg/types"
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
