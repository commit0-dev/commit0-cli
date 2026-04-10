package http

import (
	"fmt"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/commit0-dev/commit0/server/internal/app"
)

// SetSyncService attaches the sync service and registers sync routes.
// Called after NewServer when sync is enabled.
func (s *Server) SetSyncService(syncSvc *app.SyncService) {
	s.syncSvc = syncSvc

	sync := s.router.Group("/api/v1/sync")
	sync.POST("/export", s.handleSyncExport)
	sync.POST("/import", s.handleSyncImport)
	sync.GET("/manifest/:slug", s.handleSyncManifest)
}

func (s *Server) handleSyncExport(c *gin.Context) {
	if s.syncSvc == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"message": "sync not enabled"})
		return
	}

	var req struct {
		RepoSlug string `json:"repo_slug" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}

	bundle, err := s.syncSvc.BuildBundle(c.Request.Context(), req.RepoSlug)
	if err != nil {
		writeError(c, err)
		return
	}

	c.JSON(http.StatusOK, bundle)
}

func (s *Server) handleSyncImport(c *gin.Context) {
	if s.syncSvc == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"message": "sync not enabled"})
		return
	}

	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": fmt.Sprintf("read body: %v", err)})
		return
	}

	result, err := s.syncSvc.ImportFromBytes(c.Request.Context(), body)
	if err != nil {
		writeError(c, err)
		return
	}

	c.JSON(http.StatusOK, result)
}

func (s *Server) handleSyncManifest(c *gin.Context) {
	if s.syncSvc == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"message": "sync not enabled"})
		return
	}

	slug := c.Param("slug")
	manifest, err := s.syncSvc.Manifest(c.Request.Context(), slug)
	if err != nil {
		writeError(c, err)
		return
	}

	c.JSON(http.StatusOK, manifest)
}
