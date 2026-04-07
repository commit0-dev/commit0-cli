package http

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/commit0-dev/commit0/internal/domain"
)

// handleAgentChat handles POST /api/v1/agent/chat with SSE streaming.
// The agent reasons, calls tools, and streams events back in real-time.
func (s *Server) handleAgentChat(c echo.Context) error {
	if s.agentRunner == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "agent service not available")
	}

	var req struct {
		SessionID string `json:"session_id"`
		Message   string `json:"message"`
		RepoSlug  string `json:"repo_slug"`
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}
	if req.Message == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "message is required")
	}

	events, err := s.agentRunner.Chat(c.Request().Context(), domain.ChatRequest{
		SessionID: req.SessionID,
		RepoSlug:  req.RepoSlug,
		Message:   req.Message,
	})
	if err != nil {
		return httpError(err)
	}

	// Switch to SSE mode
	c.Response().Header().Set("Content-Type", "text/event-stream")
	c.Response().Header().Set("Cache-Control", "no-cache")
	c.Response().Header().Set("X-Accel-Buffering", "no")
	c.Response().WriteHeader(http.StatusOK)

	for event := range events {
		data, _ := json.Marshal(event)
		fmt.Fprintf(c.Response(), "event: %s\ndata: %s\n\n", event.Type, data)
		c.Response().Flush()
	}

	return nil
}
