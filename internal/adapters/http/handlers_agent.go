package http

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/commit0-dev/commit0/internal/domain"
)

// handleAgentChat handles POST /api/v1/agent/chat with SSE streaming.
// The agent reasons, calls tools, and streams events back in real-time.
func (s *Server) handleAgentChat(c *gin.Context) {
	if s.agentRunner == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"message": "agent service not available"})
		return
	}

	var req struct {
		SessionID string `json:"session_id"`
		Message   string `json:"message"`
		RepoSlug  string `json:"repo_slug"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid request body"})
		return
	}
	if req.Message == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "message is required"})
		return
	}

	events, err := s.agentRunner.Chat(c.Request.Context(), domain.ChatRequest{
		SessionID: req.SessionID,
		RepoSlug:  req.RepoSlug,
		Message:   req.Message,
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

	for event := range events {
		data, _ := json.Marshal(event)
		fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", event.Type, data)
		c.Writer.Flush()
	}
}
