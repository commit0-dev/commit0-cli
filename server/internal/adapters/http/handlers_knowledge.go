package http

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/commit0-dev/commit0/pkg/types"
	"github.com/commit0-dev/commit0/server/internal/app"
	"github.com/commit0-dev/commit0/server/internal/domain"
)

// KnowledgeHandlers groups the knowledge-graph CRUD endpoints.
type KnowledgeHandlers struct {
	svc *app.KnowledgeService
}

// NewKnowledgeHandlers constructs the handler bundle.
func NewKnowledgeHandlers(svc *app.KnowledgeService) *KnowledgeHandlers {
	return &KnowledgeHandlers{svc: svc}
}

type createKnowledgeRequest struct {
	Kind        string   `json:"kind"        binding:"required"`
	RepoSlug    string   `json:"repo_slug"   binding:"required"`
	Title       string   `json:"title"       binding:"required"`
	Body        string   `json:"body"`
	Author      string   `json:"author"`
	Status      string   `json:"status"`
	Tags        []string `json:"tags"`
	URL         string   `json:"url"`
	AccessScope string   `json:"access_scope"`
	ID          string   `json:"id"`
}

func (h *KnowledgeHandlers) handleCreate(c *gin.Context) {
	var req createKnowledgeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, domain.Validation(err.Error()))
		return
	}
	identity := domain.IdentityFrom(c.Request.Context())
	author := req.Author
	if author == "" {
		author = identity.AuthorID()
	}
	node := &types.KnowledgeNode{
		ID:          req.ID,
		Kind:        req.Kind,
		RepoSlug:    req.RepoSlug,
		Title:       req.Title,
		Body:        req.Body,
		Author:      author,
		Status:      req.Status,
		Tags:        req.Tags,
		URL:         req.URL,
		AccessScope: req.AccessScope,
	}
	if err := h.svc.CreateNode(c.Request.Context(), node); err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusCreated, node)
}

func (h *KnowledgeHandlers) handleGet(c *gin.Context) {
	id := c.Param("id")
	node, err := h.svc.GetNode(c.Request.Context(), id)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, node)
}

func (h *KnowledgeHandlers) handleList(c *gin.Context) {
	repoSlug := c.Query("repo_slug")
	kind := c.Query("kind")
	if repoSlug == "" {
		writeError(c, domain.Validation("repo_slug query parameter is required"))
		return
	}
	nodes, err := h.svc.ListNodes(c.Request.Context(), repoSlug, kind)
	if err != nil {
		writeError(c, err)
		return
	}
	if nodes == nil {
		nodes = []types.KnowledgeNode{}
	}
	c.JSON(http.StatusOK, gin.H{"nodes": nodes, "count": len(nodes)})
}

func (h *KnowledgeHandlers) handleDelete(c *gin.Context) {
	id := c.Param("id")
	if err := h.svc.DeleteNode(c.Request.Context(), id); err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": id})
}

type linkRequest struct {
	FromID   string            `json:"from_id"  binding:"required"`
	ToID     string            `json:"to_id"    binding:"required"`
	Kind     string            `json:"kind"     binding:"required"`
	Metadata map[string]string `json:"metadata"`
}

func (h *KnowledgeHandlers) handleLink(c *gin.Context) {
	var req linkRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, domain.Validation(err.Error()))
		return
	}
	if err := h.svc.LinkNodes(c.Request.Context(), req.FromID, req.ToID, req.Kind, req.Metadata); err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{
		"from_id": req.FromID,
		"to_id":   req.ToID,
		"kind":    req.Kind,
	})
}

type ingestMarkdownRequest struct {
	RepoSlug string `json:"repo_slug" binding:"required"`
	Kind     string `json:"kind"      binding:"required"`
	Source   string `json:"source"`
	Body     string `json:"body"      binding:"required"`
}

func (h *KnowledgeHandlers) handleIngestMarkdown(c *gin.Context) {
	var req ingestMarkdownRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, domain.Validation(err.Error()))
		return
	}
	node, err := h.svc.IngestMarkdown(c.Request.Context(), req.RepoSlug, req.Kind, req.Source, req.Body)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusCreated, node)
}
