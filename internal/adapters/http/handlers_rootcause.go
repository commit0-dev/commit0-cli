package http

import (
	"encoding/json"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/commit0-dev/commit0/internal/app"
)

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
func (s *Server) handleFieldFlow(c echo.Context) error {
	if s.flowSvc == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "field flow service not available")
	}
	var req flowRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}
	if req.Symbol == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "symbol is required")
	}

	result, err := s.flowSvc.TraceFieldFlow(c.Request().Context(), app.FieldFlowRequest{
		Symbol:        req.Symbol,
		FieldPath:     req.FieldPath,
		RepoSlug:      req.RepoSlug,
		Direction:     req.Direction,
		Depth:         req.Depth,
		ShowMutations: req.ShowMutations,
	})
	if err != nil {
		return httpError(err)
	}
	return c.JSON(http.StatusOK, result)
}

// ---- Temporal History ───────────────────────────────────────────────────

type historyRequest struct {
	Symbol     string `json:"symbol"`
	RepoSlug   string `json:"repo_slug"`
	FromCommit string `json:"from_commit"`
	ToCommit   string `json:"to_commit"`
}

// handleHistory handles POST /api/v1/history.
func (s *Server) handleHistory(c echo.Context) error {
	if s.tempSvc == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "temporal service not available")
	}
	var req historyRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}

	result, err := s.tempSvc.QueryHistory(c.Request().Context(), app.TemporalQueryRequest{
		RepoSlug:      req.RepoSlug,
		NodeQualified: req.Symbol,
		FromCommit:    req.FromCommit,
		ToCommit:      req.ToCommit,
	})
	if err != nil {
		return httpError(err)
	}
	return c.JSON(http.StatusOK, result)
}

// ---- Find Root Cause (SSE streaming) ────────────────────────────────────

type findRootRequest struct {
	Description string `json:"description"`
	RepoSlug    string `json:"repo_slug"`
	RepoPath    string `json:"repo_path"`
	Since       string `json:"since"`
}

// handleFindRoot handles POST /api/v1/find-root with SSE streaming progress.
func (s *Server) handleFindRoot(c echo.Context) error {
	if s.rootCauseSvc == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "root cause service not available")
	}
	var req findRootRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}
	if req.Description == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "description is required")
	}
	if req.RepoPath == "" {
		req.RepoPath = "."
	}

	// SSE for long-running analysis
	c.Response().Header().Set("Content-Type", "text/event-stream")
	c.Response().Header().Set("Cache-Control", "no-cache")
	c.Response().Header().Set("X-Accel-Buffering", "no")
	c.Response().WriteHeader(http.StatusOK)

	writeSSE(c, "status", map[string]string{"step": "starting", "message": "Starting root cause analysis..."})

	result, err := s.rootCauseSvc.FindRootCause(c.Request().Context(), app.RootCauseRequest{
		Description: req.Description,
		RepoSlug:    req.RepoSlug,
		RepoPath:    req.RepoPath,
		Since:       req.Since,
	})
	if err != nil {
		writeSSE(c, "error", map[string]string{"message": err.Error()})
		return nil
	}

	data, _ := json.Marshal(result)
	writeSSE(c, "result", json.RawMessage(data))
	writeSSE(c, "done", map[string]string{"status": "complete"})
	return nil
}
