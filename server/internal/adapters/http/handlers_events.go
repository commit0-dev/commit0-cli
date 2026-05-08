package http

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/commit0-dev/commit0/pkg/types"
	"github.com/commit0-dev/commit0/server/internal/app"
)

// EventHandlers wraps EventService and provides HTTP endpoints.
type EventHandlers struct {
	eventService *app.EventService
}

// NewEventHandlers constructs EventHandlers.
func NewEventHandlers(eventService *app.EventService) *EventHandlers {
	return &EventHandlers{eventService: eventService}
}

// handleListEvents handles GET /api/v1/events
// Query parameters: repo, source, type, since, until
// Returns: {events: [...], count: N}
func (h *EventHandlers) handleListEvents(c *gin.Context) {
	filter := types.EventFilter{
		RepoSlug: c.Query("repo"),
		Source:   c.Query("source"),
	}

	if since := c.Query("since"); since != "" {
		t, err := time.Parse(time.RFC3339, since)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid since timestamp, use RFC3339 format"})
			return
		}
		filter.Since = &t
	}

	if until := c.Query("until"); until != "" {
		t, err := time.Parse(time.RFC3339, until)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid until timestamp, use RFC3339 format"})
			return
		}
		filter.Until = &t
	}

	if eventType := c.Query("type"); eventType != "" {
		filter.Types = []string{eventType}
	}

	events, err := h.eventService.Query(c.Request.Context(), filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"events": events,
		"count":  len(events),
	})
}

// handleEventStream handles GET /api/v1/events/stream (SSE)
// Query parameters: repo, source, type
// Returns: Server-Sent Events stream of matching events
func (h *EventHandlers) handleEventStream(c *gin.Context) {
	filter := types.EventFilter{
		RepoSlug: c.Query("repo"),
		Source:   c.Query("source"),
	}
	if eventType := c.Query("type"); eventType != "" {
		filter.Types = []string{eventType}
	}

	ch, err := h.eventService.Subscribe(c.Request.Context(), filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer func() { _ = h.eventService.Unsubscribe(c.Request.Context(), ch) }()

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)

	c.Stream(func(w io.Writer) bool {
		select {
		case event, ok := <-ch:
			if !ok {
				return false
			}
			data, _ := json.Marshal(event)
			_, _ = io.WriteString(w, "event: message\n")
			_, _ = io.WriteString(w, "data: "+string(data)+"\n\n")
			return true
		case <-c.Request.Context().Done():
			return false
		}
	})
}

// handleEventCount handles GET /api/v1/events/count
// Query parameters: repo
// Returns: {count: N}
func (h *EventHandlers) handleEventCount(c *gin.Context) {
	filter := types.EventFilter{
		RepoSlug: c.Query("repo"),
	}
	count, err := h.eventService.Count(c.Request.Context(), filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"count": count})
}
