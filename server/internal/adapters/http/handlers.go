package http

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/commit0-dev/commit0/server/internal/app"
	"github.com/commit0-dev/commit0/server/internal/domain"
	"github.com/commit0-dev/commit0/pkg/types"
)

// handleHealth returns liveness check with server state (idle/indexing).
func (s *Server) handleHealth(c *gin.Context) {
	activeJobs := 0
	if s.jobs != nil {
		s.jobs.mu.RLock()
		for _, j := range s.jobs.jobs {
			if j.Status == "indexing" {
				activeJobs++
			}
		}
		s.jobs.mu.RUnlock()
	}
	state := "idle"
	if activeJobs > 0 {
		state = "indexing"
	}
	c.JSON(http.StatusOK, gin.H{
		"status":      "ok",
		"state":       state,
		"active_jobs": activeJobs,
	})
}

// ---- Query ----------------------------------------------------------------

type queryRequest struct {
	Question  string   `json:"question"`
	RepoSlug  string   `json:"repo_slug"`
	TopK      int      `json:"top_k"`
	MinScore  float64  `json:"min_score"`
	NoExplain bool     `json:"no_explain"`
	NodeKinds []string `json:"node_kinds"`
	FilePath  string   `json:"file_path"`
}

// handleQuery handles POST /api/v1/query.
func (s *Server) handleQuery(c *gin.Context) {
	var req queryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid request body"})
		return
	}
	if req.Question == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "question is required"})
		return
	}

	// Convert string node kinds to typed.
	var nodeKinds []types.NodeKind
	for _, k := range req.NodeKinds {
		nodeKinds = append(nodeKinds, types.NodeKind(k))
	}

	result, err := s.querySvc.Query(c.Request.Context(), app.QueryRequest{
		Question:  req.Question,
		RepoSlug:  req.RepoSlug,
		TopK:      req.TopK,
		MinScore:  req.MinScore,
		NoExplain: req.NoExplain,
		NodeKinds: nodeKinds,
		FilePath:  req.FilePath,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// ---- Trace (SSE) ----------------------------------------------------------

type traceRequest struct {
	Symbol    string `json:"symbol"`
	RepoSlug  string `json:"repo_slug"`
	Direction string `json:"direction"`
	Depth     int    `json:"depth"`
}

// handleTrace handles POST /api/v1/trace with SSE streaming.
func (s *Server) handleTrace(c *gin.Context) {
	var req traceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid request body"})
		return
	}
	if req.Symbol == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "symbol is required"})
		return
	}
	if req.Direction == "" {
		req.Direction = "forward"
	}
	if req.Depth <= 0 {
		req.Depth = 5
	}

	result, err := s.traceSvc.Trace(c.Request.Context(), app.TraceRequest{
		Symbol:    req.Symbol,
		RepoSlug:  req.RepoSlug,
		Direction: req.Direction,
		Depth:     req.Depth,
	})
	if err != nil {
		writeError(c, err)
		return
	}

	// Switch to SSE mode.
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)

	for _, hop := range result.Tree {
		writeSSE(c, "hop", hop)
	}

	if result.Explanation != "" {
		writeSSE(c, "explain", map[string]string{"text": result.Explanation})
	}

	writeSSE(c, "done", map[string]any{
		"direction": result.Direction,
		"timing":    result.Timing,
	})
}

// handleTraceJSON handles POST /api/v1/trace/json — returns the full trace
// result as a single JSON response (no SSE streaming). Used by the VSCode extension.
func (s *Server) handleTraceJSON(c *gin.Context) {
	var req traceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid request body"})
		return
	}
	if req.Symbol == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "symbol is required"})
		return
	}
	if req.Direction == "" {
		req.Direction = "forward"
	}
	if req.Depth <= 0 {
		req.Depth = 5
	}

	result, err := s.traceSvc.Trace(c.Request.Context(), app.TraceRequest{
		Symbol:    req.Symbol,
		RepoSlug:  req.RepoSlug,
		Direction: req.Direction,
		Depth:     req.Depth,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// ---- Blast ----------------------------------------------------------------

type blastRequest struct {
	Symbol   string `json:"symbol"`
	RepoSlug string `json:"repo_slug"`
	MaxDepth int    `json:"max_depth"`
}

// handleBlast handles POST /api/v1/blast.
func (s *Server) handleBlast(c *gin.Context) {
	var req blastRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid request body"})
		return
	}
	if req.Symbol == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "symbol is required"})
		return
	}

	result, err := s.blastSvc.Blast(c.Request.Context(), app.BlastRequest{
		Symbol:   req.Symbol,
		RepoSlug: req.RepoSlug,
		MaxDepth: req.MaxDepth,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// ---- Repos ----------------------------------------------------------------

// handleListRepos handles GET /api/v1/repos.
func (s *Server) handleListRepos(c *gin.Context) {
	repos, err := s.repoSvc.ListRepos(c.Request.Context())
	if err != nil {
		writeError(c, err)
		return
	}
	if repos == nil {
		repos = []types.Repo{}
	}
	c.JSON(http.StatusOK, repos)
}

type createRepoRequest struct {
	Slug      string   `json:"slug"`
	Path      string   `json:"path"`
	RemoteURL string   `json:"remote_url"`
	Languages []string `json:"languages"`
}

// handleCreateRepo handles POST /api/v1/repos.
func (s *Server) handleCreateRepo(c *gin.Context) {
	var req createRepoRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid request body"})
		return
	}
	if req.Slug == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "slug is required"})
		return
	}
	if req.Path == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "path is required"})
		return
	}

	repo, err := s.repoSvc.CreateRepo(c.Request.Context(), app.CreateRepoRequest{
		Slug:      req.Slug,
		Path:      req.Path,
		RemoteURL: req.RemoteURL,
		Languages: req.Languages,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusCreated, repo)
}

// handleGetRepo handles GET /api/v1/repos/:slug.
func (s *Server) handleGetRepo(c *gin.Context) {
	slug := c.Param("slug")
	repo, err := s.repoSvc.GetRepo(c.Request.Context(), slug)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, repo)
}

// handleDeleteRepo handles DELETE /api/v1/repos/:slug.
func (s *Server) handleDeleteRepo(c *gin.Context) {
	slug := c.Param("slug")
	repo, err := s.repoSvc.DeleteRepo(c.Request.Context(), slug)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, repo)
}

// ---- Nodes (for VSCode extension) -----------------------------------------

// handleNodeLookup handles GET /api/v1/nodes/lookup?repo=slug&qualified=name.
func (s *Server) handleNodeLookup(c *gin.Context) {
	repo := c.Query("repo")
	qualified := c.Query("qualified")
	if repo == "" || qualified == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "repo and qualified are required"})
		return
	}

	node, err := s.db.GetNodeByQualified(c.Request.Context(), repo, qualified)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, node)
}

// handleNodesByFile handles GET /api/v1/nodes/by-file?repo=slug&path=relative/path.
func (s *Server) handleNodesByFile(c *gin.Context) {
	repo := c.Query("repo")
	path := c.Query("path")
	if repo == "" || path == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "repo and path are required"})
		return
	}

	nodes, err := s.db.ListNodesByFile(c.Request.Context(), repo, path)
	if err != nil {
		writeError(c, err)
		return
	}
	if nodes == nil {
		nodes = []types.CodeNode{}
	}
	c.JSON(http.StatusOK, nodes)
}

// handleGetNeighborhood handles GET /api/v1/nodes/:id/neighborhood.
func (s *Server) handleGetNeighborhood(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "node id is required"})
		return
	}

	neighborhood, err := s.db.GetNeighborhood(c.Request.Context(), id)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, neighborhood)
}

// ---- SSE helpers ----------------------------------------------------------

// writeSSE writes a single Server-Sent Event frame to the response.
func writeSSE(c *gin.Context, event string, data interface{}) {
	jsonData, _ := json.Marshal(data)
	fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", event, jsonData)
	c.Writer.Flush()
}

// ---- Error mapping --------------------------------------------------------

// writeError maps domain errors to appropriate HTTP error responses.
func writeError(c *gin.Context, err error) {
	var de *domain.DomainError
	if errors.As(err, &de) {
		switch de.Code {
		case domain.ErrNotFound:
			c.JSON(http.StatusNotFound, gin.H{"message": de.Message})
		case domain.ErrValidation:
			c.JSON(http.StatusBadRequest, gin.H{"message": de.Message})
		case domain.ErrConflict:
			c.JSON(http.StatusConflict, gin.H{"message": de.Message})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		}
		return
	}
	c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
}
