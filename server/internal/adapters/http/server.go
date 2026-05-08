package http

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-contrib/requestid"
	"github.com/gin-gonic/gin"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	mcpadapter "github.com/commit0-dev/commit0/server/internal/adapters/mcp"
	"github.com/commit0-dev/commit0/server/internal/app"
	"github.com/commit0-dev/commit0/server/internal/config"
	"github.com/commit0-dev/commit0/server/internal/domain"
)

// Server wraps a Gin engine with all application services.
type Server struct {
	router       *gin.Engine
	srv          *http.Server
	indexSvc     *app.IndexService
	querySvc     *app.QueryService
	traceSvc     *app.TraceService
	blastSvc     *app.BlastService
	repoSvc      *app.RepoService
	graph        domain.OpenCodeGraph
	agentRunner  domain.AgentRunner
	flowSvc      *app.FieldFlowService
	tempSvc      *app.TemporalService
	rootCauseSvc *app.RootCauseAnalysisService
	apiSurfSvc   *app.APISurfaceService
	syncSvc      *app.SyncService
	peerStore    domain.PeerStore
	scopeStore   domain.ScopeStore
	cfg          *config.ServerConfig
	fullCfg      *config.Config
	log          *slog.Logger
	trackers     *indexTrackerStore
}

// NewServer constructs the HTTP server, registers middleware and routes.
func NewServer(
	indexSvc *app.IndexService,
	querySvc *app.QueryService,
	traceSvc *app.TraceService,
	blastSvc *app.BlastService,
	repoSvc *app.RepoService,
	graph domain.OpenCodeGraph,
	agentRunner domain.AgentRunner,
	flowSvc *app.FieldFlowService,
	tempSvc *app.TemporalService,
	rootCauseSvc *app.RootCauseAnalysisService,
	apiSurfSvc *app.APISurfaceService,
	cfg *config.ServerConfig,
	fullCfg ...*config.Config,
) *Server {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()

	s := &Server{
		router:       r,
		indexSvc:     indexSvc,
		querySvc:     querySvc,
		traceSvc:     traceSvc,
		blastSvc:     blastSvc,
		repoSvc:      repoSvc,
		graph:        graph,
		agentRunner:  agentRunner,
		flowSvc:      flowSvc,
		tempSvc:      tempSvc,
		rootCauseSvc: rootCauseSvc,
		apiSurfSvc:   apiSurfSvc,
		cfg:          cfg,
		log:          slog.Default(),
		trackers:     newIndexTrackerStore(),
	}
	if len(fullCfg) > 0 && fullCfg[0] != nil {
		s.fullCfg = fullCfg[0]
	}
	s.registerMiddleware()
	s.registerRoutes()
	return s
}

func (s *Server) registerMiddleware() {
	s.router.Use(gin.Recovery())
	s.router.Use(requestid.New())
	s.router.Use(SlogMiddleware(s.log))
	s.router.Use(cors.New(cors.Config{
		AllowOrigins: s.cfg.CORSOrigins,
		AllowMethods: []string{http.MethodGet, http.MethodPost, http.MethodDelete},
	}))
}

func (s *Server) registerRoutes() {
	s.router.GET("/health", s.handleHealth)
	s.router.GET("/healthz", s.handleHealthz)

	v1 := s.router.Group("/api/v1")

	// Repos
	v1.GET("/repos", s.handleListRepos)
	v1.POST("/repos", s.handleCreateRepo)
	v1.GET("/repos/*slug", s.handleGetRepo)
	v1.DELETE("/repos/*slug", s.handleDeleteRepo)

	// Indexing
	v1.POST("/index", s.handleStartIndex)
	v1.GET("/index/:job_id", s.handleIndexStatus)
	v1.GET("/index/:job_id/stream", s.handleIndexStream)

	// Static graph (HUD canvas seed)
	v1.GET("/graph", s.handleGraph)

	// Analysis
	v1.POST("/query", s.handleQuery)
	v1.POST("/query/stream", s.handleQueryStream)
	v1.POST("/trace", s.handleTrace)
	v1.POST("/trace/json", s.handleTraceJSON)
	v1.POST("/blast", s.handleBlast)

	// Agent (agentic conversation with tool use)
	v1.POST("/agent/chat", s.handleAgentChat)

	// Root cause analysis
	v1.POST("/flow", s.handleFieldFlow)
	v1.POST("/history", s.handleHistory)
	v1.POST("/find-root", s.handleFindRoot)

	// API surface
	v1.POST("/api/discover", s.handleAPIDiscover)
	v1.POST("/api/spec", s.handleAPISpec)

	// Re-embedding (after provider switch)
	v1.POST("/reembed", s.handleReEmbed)

	// Nodes (for VSCode extension: CodeLens, graph, hover)
	v1.GET("/nodes/lookup", s.handleNodeLookup)
	v1.GET("/nodes/by-file", s.handleNodesByFile)
	v1.GET("/nodes/:id/neighborhood", s.handleGetNeighborhood)
}

// SetMCPHandler wires the MCP server (same surface as `commit0 mcp`) into the
// HTTP router at `/mcp` using the streamable-HTTP transport. Callers may pass
// their already-constructed services via deps so the MCP server shares the
// same IndexService instance — and therefore the same per-process tracker
// registry — as the HTTP API. This closes the integration loop reported in
// issue #56: index jobs started via POST /api/v1/index become observable
// via the MCP commit0_index_status tool.
//
// Idempotent in the sense that calling it twice replaces the prior route.
// Safe to call before Start; not safe to call after Shutdown.
func (s *Server) SetMCPHandler(deps mcpadapter.Deps) {
	mcpServer := mcpadapter.New(deps)
	handler := mcpsdk.NewStreamableHTTPHandler(func(_ *http.Request) *mcpsdk.Server {
		return mcpServer
	}, &mcpsdk.StreamableHTTPOptions{})

	// MCP streamable transport uses POST for client→server messages, GET for
	// server-initiated SSE streams, and DELETE to terminate sessions. Mount
	// the handler on Any so the SDK owns method dispatch.
	s.router.Any("/mcp", gin.WrapH(handler))
	s.router.Any("/mcp/", gin.WrapH(handler))
	s.log.Info("MCP HTTP transport mounted", "path", "/mcp")
}

// Start binds the server to the configured port and blocks until stopped.
func (s *Server) Start() error {
	addr := fmt.Sprintf(":%d", s.cfg.Port)
	s.log.Info("HTTP server starting", "addr", addr)

	s.srv = &http.Server{
		Addr:         addr,
		Handler:      s.router,
		ReadTimeout:  time.Duration(s.cfg.ReadTimeoutSec) * time.Second,
		WriteTimeout: time.Duration(s.cfg.WriteTimeoutSec) * time.Second,
	}
	return s.srv.ListenAndServe()
}

// Shutdown gracefully stops the server.
func (s *Server) Shutdown(ctx context.Context) error {
	if s.srv == nil {
		return nil
	}
	return s.srv.Shutdown(ctx)
}
