package http

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/commit0-dev/commit0/pkg/types"
	"github.com/commit0-dev/commit0/server/internal/app"
	"github.com/commit0-dev/commit0/server/internal/domain"
)

// handleHealth returns liveness check with server state (idle/indexing).
func (s *Server) handleHealth(c *gin.Context) {
	activeJobs := 0
	if s.trackers != nil {
		s.trackers.mu.RLock()
		for _, t := range s.trackers.trackers {
			snap := t.Snapshot()
			if snap.Status == "indexing" {
				activeJobs++
			}
		}
		s.trackers.mu.RUnlock()
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

// healthzCheck represents the status of a single dependency.
type healthzCheck struct {
	Status string `json:"status"` // "ok" or "fail"
	Detail string `json:"detail,omitempty"`
}

// handleHealthz is a readiness probe that checks all critical dependencies.
// Returns 200 when every check passes, 503 if any check fails.
func (s *Server) handleHealthz(c *gin.Context) {
	checks := make(map[string]healthzCheck)
	allOK := true

	// 1. Database (SurrealDB) — lightweight read.
	if s.repoSvc != nil {
		_, err := s.repoSvc.ListRepos(c.Request.Context())
		if err != nil {
			checks["database"] = healthzCheck{Status: "fail", Detail: err.Error()}
			allOK = false
		} else {
			checks["database"] = healthzCheck{Status: "ok"}
		}
	} else {
		checks["database"] = healthzCheck{Status: "fail", Detail: "repo service not initialized"}
		allOK = false
	}

	// 2. Graph store.
	if s.graph != nil {
		checks["graph"] = healthzCheck{Status: "ok"}
	} else {
		checks["graph"] = healthzCheck{Status: "fail", Detail: "graph store not initialized"}
		allOK = false
	}

	// 3. Agent (LLM provider configured).
	if s.agentRunner != nil {
		checks["agent"] = healthzCheck{Status: "ok"}
	} else {
		checks["agent"] = healthzCheck{Status: "ok", Detail: "agent not configured (optional)"}
	}

	// 4. Provider info from config.
	if s.fullCfg != nil {
		checks["llm_provider"] = healthzCheck{Status: "ok", Detail: s.fullCfg.LLMProvider}
		checks["embed_provider"] = healthzCheck{Status: "ok", Detail: s.fullCfg.EmbedProvider}

		// 5. List all configured LLM providers (shows available backends).
		var providers []string
		if s.fullCfg.Gemini.APIKey != "" {
			providers = append(providers, "gemini")
		}
		if s.fullCfg.OpenRouter.APIKey != "" {
			providers = append(providers, "openrouter")
		}
		if s.fullCfg.Ollama.Model != "" {
			providers = append(providers, "ollama")
		}
		if s.fullCfg.Unsloth.Model != "" {
			providers = append(providers, "unsloth")
		}
		if s.fullCfg.Unsloth.EmbedModel != "" {
			providers = append(providers, "unsloth-embed")
		}
		checks["configured_providers"] = healthzCheck{
			Status: "ok",
			Detail: strings.Join(providers, ","),
		}
	}

	status := http.StatusOK
	result := "ready"
	if !allOK {
		status = http.StatusServiceUnavailable
		result = "not_ready"
	}
	c.JSON(status, gin.H{"status": result, "checks": checks})
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
	Symbol     string   `json:"symbol"`
	RepoSlug   string   `json:"repo_slug"`
	Direction  string   `json:"direction"`
	Depth      int      `json:"depth"`
	NoExplain  bool     `json:"no_explain"`
	EdgeLabels []string `json:"edge_labels,omitempty"`
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
		Symbol:     req.Symbol,
		RepoSlug:   req.RepoSlug,
		Direction:  req.Direction,
		Depth:      req.Depth,
		NoExplain:  req.NoExplain,
		EdgeLabels: req.EdgeLabels,
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
		Symbol:     req.Symbol,
		RepoSlug:   req.RepoSlug,
		Direction:  req.Direction,
		Depth:      req.Depth,
		NoExplain:  req.NoExplain,
		EdgeLabels: req.EdgeLabels,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// ---- Blast ----------------------------------------------------------------

type blastRequest struct {
	Symbol     string   `json:"symbol"`
	RepoSlug   string   `json:"repo_slug"`
	MaxDepth   int      `json:"max_depth"`
	NoExplain  bool     `json:"no_explain"`
	EdgeLabels []string `json:"edge_labels,omitempty"`
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
		Symbol:     req.Symbol,
		RepoSlug:   req.RepoSlug,
		MaxDepth:   req.MaxDepth,
		NoExplain:  req.NoExplain,
		EdgeLabels: req.EdgeLabels,
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

// handleGetRepo handles GET /api/v1/repos/*slug.
func (s *Server) handleGetRepo(c *gin.Context) {
	slug := strings.TrimPrefix(c.Param("slug"), "/")
	repo, err := s.repoSvc.GetRepo(c.Request.Context(), slug)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, repo)
}

// handleDeleteRepo handles DELETE /api/v1/repos/*slug.
func (s *Server) handleDeleteRepo(c *gin.Context) {
	slug := strings.TrimPrefix(c.Param("slug"), "/")
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

	node, err := s.graph.FindNode(c.Request.Context(), repo, qualified)
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

	nodes, err := s.graph.ListNodes(c.Request.Context(), repo, domain.ListOpts{FilePath: path})
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

	neighborhood, err := s.graph.Neighbors(c.Request.Context(), id)
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
	// Ambiguous symbol — 400 with candidates list.
	var ambig *types.AmbiguousSymbolError
	if errors.As(err, &ambig) {
		c.JSON(http.StatusBadRequest, gin.H{"message": ambig.Error(), "candidates": ambig.Candidates})
		return
	}

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
