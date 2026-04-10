package http

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/commit0-dev/commit0/server/internal/app"
)

// ---- Re-embedding ──────────────────────────────────────────────────────

type reembedRequest struct {
	RepoSlug string `json:"repo_slug"`
}

// handleReEmbed handles POST /api/v1/reembed — starts background re-embedding.
func (s *Server) handleReEmbed(c *gin.Context) {
	var req reembedRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid request body"})
		return
	}
	if req.RepoSlug == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "repo_slug is required"})
		return
	}

	go func() {
		_, err := s.indexSvc.ReEmbed(context.Background(), req.RepoSlug, nil)
		if err != nil {
			slog.Error("background re-embed failed", "repo", req.RepoSlug, "err", err)
		}
	}()

	c.JSON(http.StatusAccepted, gin.H{"status": "re-embedding started", "repo_slug": req.RepoSlug})
}

// ---- API Surface ─────────────────────────────────────────────────────────

type apiDiscoverRequest struct {
	RepoSlug string `json:"repo_slug"`
}

// handleAPIDiscover handles POST /api/v1/api/discover.
func (s *Server) handleAPIDiscover(c *gin.Context) {
	if s.apiSurfSvc == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"message": "API surface service not available"})
		return
	}
	var req apiDiscoverRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid request body"})
		return
	}

	surface, err := s.apiSurfSvc.Discover(c.Request.Context(), req.RepoSlug)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, surface)
}

type apiSpecRequest struct {
	RepoSlug string `json:"repo_slug"`
}

// handleAPISpec handles POST /api/v1/api/spec.
func (s *Server) handleAPISpec(c *gin.Context) {
	if s.apiSurfSvc == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"message": "API surface service not available"})
		return
	}
	var req apiSpecRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid request body"})
		return
	}

	surface, err := s.apiSurfSvc.Discover(c.Request.Context(), req.RepoSlug)
	if err != nil {
		writeError(c, err)
		return
	}

	spec, err := s.apiSurfSvc.GenerateOpenAPI(c.Request.Context(), surface)
	if err != nil {
		writeError(c, err)
		return
	}

	c.Data(http.StatusOK, "application/json", spec)
}

// ---- Field Flow ─────────────────────────────────────────────────────────

type flowRequest struct {
	Symbol        string `json:"symbol"`
	FieldPath     string `json:"field_path"`
	RepoSlug      string `json:"repo_slug"`
	Direction     string `json:"direction"`
	Depth         int    `json:"depth"`
	ShowMutations bool   `json:"show_mutations"`
}

// handleFieldFlow handles POST /api/v1/flow.
func (s *Server) handleFieldFlow(c *gin.Context) {
	if s.flowSvc == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"message": "field flow service not available"})
		return
	}
	var req flowRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid request body"})
		return
	}
	if req.Symbol == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "symbol is required"})
		return
	}

	result, err := s.flowSvc.TraceFieldFlow(c.Request.Context(), app.FieldFlowRequest{
		Symbol:        req.Symbol,
		FieldPath:     req.FieldPath,
		RepoSlug:      req.RepoSlug,
		Direction:     req.Direction,
		Depth:         req.Depth,
		ShowMutations: req.ShowMutations,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// ---- Temporal History ───────────────────────────────────────────────────

type historyRequest struct {
	Symbol     string `json:"symbol"`
	RepoSlug   string `json:"repo_slug"`
	FromCommit string `json:"from_commit"`
	ToCommit   string `json:"to_commit"`
}

// handleHistory handles POST /api/v1/history.
func (s *Server) handleHistory(c *gin.Context) {
	if s.tempSvc == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"message": "temporal service not available"})
		return
	}
	var req historyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid request body"})
		return
	}

	result, err := s.tempSvc.QueryHistory(c.Request.Context(), app.TemporalQueryRequest{
		RepoSlug:      req.RepoSlug,
		NodeQualified: req.Symbol,
		FromCommit:    req.FromCommit,
		ToCommit:      req.ToCommit,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// ---- Find Root Cause (SSE streaming) ────────────────────────────────────

type findRootRequest struct {
	Description string `json:"description"`
	RepoSlug    string `json:"repo_slug"`
	RepoPath    string `json:"repo_path"`
	Since       string `json:"since"`
}

// handleFindRoot handles POST /api/v1/find-root with SSE streaming progress.
func (s *Server) handleFindRoot(c *gin.Context) {
	if s.rootCauseSvc == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"message": "root cause service not available"})
		return
	}
	var req findRootRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid request body"})
		return
	}
	if req.Description == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "description is required"})
		return
	}
	if req.RepoPath == "" {
		req.RepoPath = "."
	}

	// SSE for long-running analysis.
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)

	writeSSE(c, "status", map[string]string{"step": "starting", "message": "Starting root cause analysis..."})

	result, err := s.rootCauseSvc.FindRootCause(c.Request.Context(), app.RootCauseRequest{
		Description: req.Description,
		RepoSlug:    req.RepoSlug,
		RepoPath:    req.RepoPath,
		Since:       req.Since,
	})
	if err != nil {
		writeSSE(c, "error", map[string]string{"message": err.Error()})
		return
	}

	data, _ := json.Marshal(result)
	writeSSE(c, "result", json.RawMessage(data))
	writeSSE(c, "done", map[string]string{"status": "complete"})
}
