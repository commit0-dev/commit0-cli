package http

import (
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/commit0-dev/commit0/pkg/types"
	"github.com/commit0-dev/commit0/server/internal/app"
	"github.com/commit0-dev/commit0/server/internal/domain"
)

// SetSyncService attaches the sync service and registers sync routes.
// Called after NewServer when sync is enabled.
func (s *Server) SetSyncService(
	syncSvc *app.SyncService,
	peerStore domain.PeerStore,
	scopeStore domain.ScopeStore,
	passphrase string,
) {
	s.syncSvc = syncSvc
	s.peerStore = peerStore
	s.scopeStore = scopeStore

	sync := s.router.Group("/api/v1/sync")
	sync.Use(SyncAuthMiddleware(passphrase))

	// Bundle operations
	sync.POST("/export", s.handleSyncExport)
	sync.POST("/import", s.handleSyncImport)
	sync.GET("/manifest/:slug", s.handleSyncManifest)

	// Remote peer management
	sync.POST("/remotes", s.handleAddRemote)
	sync.GET("/remotes", s.handleListRemotes)
	sync.GET("/remotes/:name", s.handleGetRemote)
	sync.DELETE("/remotes/:name", s.handleDeleteRemote)
	sync.POST("/remotes/:name/handshake", s.handleHandshake)

	// Scope management
	sync.POST("/scope", s.handleAddScope)
	sync.GET("/scope", s.handleListScope)
	sync.DELETE("/scope/:slug", s.handleRemoveScope)
}

// --- Bundle handlers ---

func (s *Server) handleSyncExport(c *gin.Context) {
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
	slug := c.Param("slug")
	manifest, err := s.syncSvc.Manifest(c.Request.Context(), slug)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, manifest)
}

// --- Remote peer handlers ---

func (s *Server) handleAddRemote(c *gin.Context) {
	var req struct {
		Name     string `json:"name" binding:"required"`
		Endpoint string `json:"endpoint" binding:"required"`
		APIURL   string `json:"api_url"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}

	peer := &types.PeerInfo{
		Name:     req.Name,
		Endpoint: req.Endpoint,
		APIURL:   req.APIURL,
		AddedAt:  time.Now(),
	}
	if err := s.peerStore.UpsertPeer(c.Request.Context(), peer); err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusCreated, peer)
}

func (s *Server) handleListRemotes(c *gin.Context) {
	peers, err := s.peerStore.ListPeers(c.Request.Context())
	if err != nil {
		writeError(c, err)
		return
	}
	if peers == nil {
		peers = []types.PeerInfo{}
	}
	c.JSON(http.StatusOK, peers)
}

func (s *Server) handleGetRemote(c *gin.Context) {
	name := c.Param("name")
	peer, err := s.peerStore.GetPeer(c.Request.Context(), name)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, peer)
}

func (s *Server) handleDeleteRemote(c *gin.Context) {
	name := c.Param("name")
	if err := s.peerStore.DeletePeer(c.Request.Context(), name); err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": fmt.Sprintf("remote %q removed", name)})
}

func (s *Server) handleHandshake(c *gin.Context) {
	name := c.Param("name")
	peer, err := s.peerStore.GetPeer(c.Request.Context(), name)
	if err != nil {
		writeError(c, err)
		return
	}
	// Return our manifest list so the peer knows what we have.
	c.JSON(http.StatusOK, gin.H{
		"peer":    peer.Name,
		"status":  "ok",
		"message": "handshake successful",
	})
}

// --- Scope handlers ---

func (s *Server) handleAddScope(c *gin.Context) {
	var req struct {
		RepoSlug string `json:"repo_slug" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}

	if err := s.scopeStore.AddToScope(c.Request.Context(), req.RepoSlug); err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"repo_slug": req.RepoSlug, "status": "added"})
}

func (s *Server) handleListScope(c *gin.Context) {
	scopes, err := s.scopeStore.ListScope(c.Request.Context())
	if err != nil {
		writeError(c, err)
		return
	}
	if scopes == nil {
		scopes = []types.SyncScope{}
	}
	c.JSON(http.StatusOK, scopes)
}

func (s *Server) handleRemoveScope(c *gin.Context) {
	slug := c.Param("slug")
	if err := s.scopeStore.RemoveFromScope(c.Request.Context(), slug); err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"repo_slug": slug, "status": "removed"})
}
