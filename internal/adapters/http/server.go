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

	"github.com/commit0-dev/commit0/internal/app"
	"github.com/commit0-dev/commit0/internal/config"
	"github.com/commit0-dev/commit0/internal/domain"
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
	db           domain.GraphStore
	agentRunner  domain.AgentRunner
	flowSvc      *app.FieldFlowService
	tempSvc      *app.TemporalService
	rootCauseSvc *app.RootCauseAnalysisService
	apiSurfSvc   *app.APISurfaceService
	cfg          *config.ServerConfig
	log          *slog.Logger
	jobs         *indexJobStore
}

// NewServer constructs the HTTP server, registers middleware and routes.
func NewServer(
	indexSvc *app.IndexService,
	querySvc *app.QueryService,
	traceSvc *app.TraceService,
	blastSvc *app.BlastService,
	repoSvc *app.RepoService,
	db domain.GraphStore,
	agentRunner domain.AgentRunner,
	flowSvc *app.FieldFlowService,
	tempSvc *app.TemporalService,
	rootCauseSvc *app.RootCauseAnalysisService,
	apiSurfSvc *app.APISurfaceService,
	cfg *config.ServerConfig,
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
		db:           db,
		agentRunner:  agentRunner,
		flowSvc:      flowSvc,
		tempSvc:      tempSvc,
		rootCauseSvc: rootCauseSvc,
		apiSurfSvc:   apiSurfSvc,
		cfg:          cfg,
		log:          slog.Default(),
		jobs:         newIndexJobStore(),
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

	v1 := s.router.Group("/api/v1")

	// Repos
	v1.GET("/repos", s.handleListRepos)
	v1.POST("/repos", s.handleCreateRepo)
	v1.GET("/repos/:slug", s.handleGetRepo)
	v1.DELETE("/repos/:slug", s.handleDeleteRepo)

	// Indexing
	v1.POST("/index", s.handleStartIndex)
	v1.GET("/index/:job_id", s.handleIndexStatus)

	// Analysis
	v1.POST("/query", s.handleQuery)
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

	// Nodes (for VSCode extension: CodeLens, graph, hover)
	v1.GET("/nodes/lookup", s.handleNodeLookup)
	v1.GET("/nodes/by-file", s.handleNodesByFile)
	v1.GET("/nodes/:id/neighborhood", s.handleGetNeighborhood)
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
