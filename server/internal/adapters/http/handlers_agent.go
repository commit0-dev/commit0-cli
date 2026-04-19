package http

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/commit0-dev/commit0/server/internal/domain"
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

	// Switch to SSE mode — disable write deadline so long-running agent
	// sessions (analyze, deep investigations) don't get killed by the
	// server's default WriteTimeout.
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)

	// Disable write deadline for SSE streaming.
	if ctrl := http.NewResponseController(c.Writer); ctrl != nil {
		_ = ctrl.SetWriteDeadline(time.Time{}) // zero = no deadline
	}

	for event := range events {
		data, _ := json.Marshal(event)
		fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", event.Type, data)
		c.Writer.Flush()
	}
}
