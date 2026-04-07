package http

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"

	"github.com/commit0-dev/commit0/internal/app"
	"github.com/commit0-dev/commit0/internal/config"
	"github.com/commit0-dev/commit0/internal/domain"
)

// Server wraps an Echo instance with all application services.
type Server struct {
	echo     *echo.Echo
	indexSvc *app.IndexService
	querySvc *app.QueryService
	traceSvc *app.TraceService
	blastSvc *app.BlastService
	repoSvc  *app.RepoService
	db       domain.GraphStore
	cfg      *config.ServerConfig
	log      *slog.Logger
	jobs     *indexJobStore
}

// NewServer constructs the HTTP server, registers middleware and routes.
func NewServer(
	indexSvc *app.IndexService,
	querySvc *app.QueryService,
	traceSvc *app.TraceService,
	blastSvc *app.BlastService,
	repoSvc *app.RepoService,
	db domain.GraphStore,
	cfg *config.ServerConfig,
) *Server {
	e := echo.New()
	e.HideBanner = true
	e.HidePort = true

	s := &Server{
		echo:     e,
		indexSvc: indexSvc,
		querySvc: querySvc,
		traceSvc: traceSvc,
		blastSvc: blastSvc,
		repoSvc:  repoSvc,
		db:       db,
		cfg:      cfg,
		log:      slog.Default(),
		jobs:     newIndexJobStore(),
	}
	s.registerMiddleware()
	s.registerRoutes()
	return s
}

func (s *Server) registerMiddleware() {
	s.echo.Use(middleware.RequestID())
	s.echo.Use(middleware.Recover())
	s.echo.Use(SlogMiddleware(s.log))
	s.echo.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins: s.cfg.CORSOrigins,
		AllowMethods: []string{http.MethodGet, http.MethodPost, http.MethodDelete},
	}))
}

func (s *Server) registerRoutes() {
	s.echo.GET("/health", s.handleHealth)

	v1 := s.echo.Group("/api/v1")

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

	// Nodes (for VSCode extension: CodeLens, graph, hover)
	v1.GET("/nodes/lookup", s.handleNodeLookup)
	v1.GET("/nodes/by-file", s.handleNodesByFile)
	v1.GET("/nodes/:id/neighborhood", s.handleGetNeighborhood)
}

// Start binds the server to the configured port and blocks until stopped.
func (s *Server) Start() error {
	addr := fmt.Sprintf(":%d", s.cfg.Port)
	s.log.Info("HTTP server starting", "addr", addr)

	srv := &http.Server{
		Addr:         addr,
		ReadTimeout:  time.Duration(s.cfg.ReadTimeoutSec) * time.Second,
		WriteTimeout: time.Duration(s.cfg.WriteTimeoutSec) * time.Second,
	}
	return s.echo.StartServer(srv)
}

// Shutdown gracefully stops the server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.echo.Shutdown(ctx)
}
