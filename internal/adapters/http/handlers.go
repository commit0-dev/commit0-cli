package http

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/commit0-dev/commit0/internal/app"
	"github.com/commit0-dev/commit0/internal/domain"
	"github.com/commit0-dev/commit0/pkg/types"
)

// handleHealth returns a simple liveness check.
func (s *Server) handleHealth(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}

// ---- Query ----------------------------------------------------------------

type queryRequest struct {
	Question string  `json:"question"`
	RepoSlug string  `json:"repo_slug"`
	TopK     int     `json:"top_k"`
	MinScore float64 `json:"min_score"`
}

// handleQuery handles POST /api/v1/query.
func (s *Server) handleQuery(c echo.Context) error {
	var req queryRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}
	if req.Question == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "question is required")
	}

	result, err := s.querySvc.Query(c.Request().Context(), app.QueryRequest{
		Question: req.Question,
		RepoSlug: req.RepoSlug,
		TopK:     req.TopK,
		MinScore: req.MinScore,
	})
	if err != nil {
		return httpError(err)
	}
	return c.JSON(http.StatusOK, result)
}

// ---- Trace (SSE) ----------------------------------------------------------

type traceRequest struct {
	Symbol    string `json:"symbol"`
	RepoSlug  string `json:"repo_slug"`
	Direction string `json:"direction"`
	Depth     int    `json:"depth"`
}

// handleTrace handles POST /api/v1/trace with SSE streaming.
// It streams each top-level hop as an "hop" event, the explanation as an
// "explain" event, and finally a "done" event.
func (s *Server) handleTrace(c echo.Context) error {
	var req traceRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}
	if req.Symbol == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "symbol is required")
	}
	if req.Direction == "" {
		req.Direction = "forward"
	}
	if req.Depth <= 0 {
		req.Depth = 5
	}

	result, err := s.traceSvc.Trace(c.Request().Context(), app.TraceRequest{
		Symbol:    req.Symbol,
		RepoSlug:  req.RepoSlug,
		Direction: req.Direction,
		Depth:     req.Depth,
	})
	if err != nil {
		return httpError(err)
	}

	// Switch to SSE mode
	c.Response().Header().Set("Content-Type", "text/event-stream")
	c.Response().Header().Set("Cache-Control", "no-cache")
	c.Response().Header().Set("X-Accel-Buffering", "no")
	c.Response().WriteHeader(http.StatusOK)

	// Stream hops
	for _, hop := range result.Tree {
		writeSSE(c, "hop", hop)
	}

	// Stream explanation if present
	if result.Explanation != "" {
		writeSSE(c, "explain", map[string]string{"text": result.Explanation})
	}

	// Final event
	writeSSE(c, "done", map[string]any{
		"direction": result.Direction,
		"timing":    result.Timing,
	})

	return nil
}

// handleTraceJSON handles POST /api/v1/trace/json — returns the full trace
// result as a single JSON response (no SSE streaming). Used by the VSCode extension.
func (s *Server) handleTraceJSON(c echo.Context) error {
	var req traceRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}
	if req.Symbol == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "symbol is required")
	}
	if req.Direction == "" {
		req.Direction = "forward"
	}
	if req.Depth <= 0 {
		req.Depth = 5
	}

	result, err := s.traceSvc.Trace(c.Request().Context(), app.TraceRequest{
		Symbol:    req.Symbol,
		RepoSlug:  req.RepoSlug,
		Direction: req.Direction,
		Depth:     req.Depth,
	})
	if err != nil {
		return httpError(err)
	}
	return c.JSON(http.StatusOK, result)
}

// ---- Blast ----------------------------------------------------------------

type blastRequest struct {
	Symbol   string `json:"symbol"`
	RepoSlug string `json:"repo_slug"`
	MaxDepth int    `json:"max_depth"`
}

// handleBlast handles POST /api/v1/blast.
func (s *Server) handleBlast(c echo.Context) error {
	var req blastRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}
	if req.Symbol == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "symbol is required")
	}

	result, err := s.blastSvc.Blast(c.Request().Context(), app.BlastRequest{
		Symbol:   req.Symbol,
		RepoSlug: req.RepoSlug,
		MaxDepth: req.MaxDepth,
	})
	if err != nil {
		return httpError(err)
	}
	return c.JSON(http.StatusOK, result)
}

// ---- Repos ----------------------------------------------------------------

// handleListRepos handles GET /api/v1/repos.
func (s *Server) handleListRepos(c echo.Context) error {
	repos, err := s.repoSvc.ListRepos(c.Request().Context())
	if err != nil {
		return httpError(err)
	}
	if repos == nil {
		repos = []types.Repo{}
	}
	return c.JSON(http.StatusOK, repos)
}

type createRepoRequest struct {
	Slug      string   `json:"slug"`
	Path      string   `json:"path"`
	RemoteURL string   `json:"remote_url"`
	Languages []string `json:"languages"`
}

// handleCreateRepo handles POST /api/v1/repos.
func (s *Server) handleCreateRepo(c echo.Context) error {
	var req createRepoRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}
	if req.Slug == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "slug is required")
	}
	if req.Path == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "path is required")
	}

	repo, err := s.repoSvc.CreateRepo(c.Request().Context(), app.CreateRepoRequest{
		Slug:      req.Slug,
		Path:      req.Path,
		RemoteURL: req.RemoteURL,
		Languages: req.Languages,
	})
	if err != nil {
		return httpError(err)
	}
	return c.JSON(http.StatusCreated, repo)
}

// handleGetRepo handles GET /api/v1/repos/:slug.
func (s *Server) handleGetRepo(c echo.Context) error {
	slug := c.Param("slug")
	repo, err := s.repoSvc.GetRepo(c.Request().Context(), slug)
	if err != nil {
		return httpError(err)
	}
	return c.JSON(http.StatusOK, repo)
}

// handleDeleteRepo handles DELETE /api/v1/repos/:slug.
func (s *Server) handleDeleteRepo(c echo.Context) error {
	slug := c.Param("slug")
	repo, err := s.repoSvc.DeleteRepo(c.Request().Context(), slug)
	if err != nil {
		return httpError(err)
	}
	return c.JSON(http.StatusOK, repo)
}

// ---- Nodes (for VSCode extension) -----------------------------------------

// handleNodeLookup handles GET /api/v1/nodes/lookup?repo=slug&qualified=name.
func (s *Server) handleNodeLookup(c echo.Context) error {
	repo := c.QueryParam("repo")
	qualified := c.QueryParam("qualified")
	if repo == "" || qualified == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "repo and qualified are required")
	}

	node, err := s.db.GetNodeByQualified(c.Request().Context(), repo, qualified)
	if err != nil {
		return httpError(err)
	}
	return c.JSON(http.StatusOK, node)
}

// handleNodesByFile handles GET /api/v1/nodes/by-file?repo=slug&path=relative/path.
func (s *Server) handleNodesByFile(c echo.Context) error {
	repo := c.QueryParam("repo")
	path := c.QueryParam("path")
	if repo == "" || path == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "repo and path are required")
	}

	nodes, err := s.db.ListNodesByFile(c.Request().Context(), repo, path)
	if err != nil {
		return httpError(err)
	}
	if nodes == nil {
		nodes = []types.CodeNode{}
	}
	return c.JSON(http.StatusOK, nodes)
}

// handleGetNeighborhood handles GET /api/v1/nodes/:id/neighborhood.
func (s *Server) handleGetNeighborhood(c echo.Context) error {
	id := c.Param("id")
	if id == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "node id is required")
	}

	neighborhood, err := s.db.GetNeighborhood(c.Request().Context(), id)
	if err != nil {
		return httpError(err)
	}
	return c.JSON(http.StatusOK, neighborhood)
}

// ---- SSE helpers ----------------------------------------------------------

// writeSSE writes a single Server-Sent Event frame to the response.
func writeSSE(c echo.Context, event string, data interface{}) {
	jsonData, _ := json.Marshal(data)
	fmt.Fprintf(c.Response(), "event: %s\ndata: %s\n\n", event, jsonData)
	c.Response().Flush()
}

// ---- Error mapping --------------------------------------------------------

// httpError maps domain errors to appropriate HTTP error responses.
func httpError(err error) *echo.HTTPError {
	var de *domain.DomainError
	if errors.As(err, &de) {
		switch de.Code {
		case domain.ErrNotFound:
			return echo.NewHTTPError(http.StatusNotFound, de.Message)
		case domain.ErrValidation:
			return echo.NewHTTPError(http.StatusBadRequest, de.Message)
		case domain.ErrConflict:
			return echo.NewHTTPError(http.StatusConflict, de.Message)
		}
	}
	return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
}
