package sdk

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/commit0-dev/commit0/pkg/types"
)

// AgentChatRequest is the request body for agent chat SSE streaming.
type AgentChatRequest struct {
	SessionID string `json:"session_id,omitempty"`
	Message   string `json:"message"`
	RepoSlug  string `json:"repo_slug,omitempty"`
}

// AgentChat starts a streaming agent chat session via SSE.
// Uses raw net/http for SSE instead of resty EventSource to handle
// POST requests with large streaming responses reliably.
func (c *Client) AgentChat(ctx context.Context, req AgentChatRequest) (<-chan types.ChatEvent, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.rc.BaseURL()+"/api/v1/agent/chat", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("agent chat: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("agent chat: status %d", resp.StatusCode)
	}

	ch := make(chan types.ChatEvent, 32)

	var chMu sync.Mutex
	chOpen := true

	closeCh := func() {
		chMu.Lock()
		defer chMu.Unlock()
		if chOpen {
			chOpen = false
			close(ch)
		}
	}

	trySend := func(event types.ChatEvent) {
		chMu.Lock()
		defer chMu.Unlock()
		if chOpen {
			select {
			case ch <- event:
			default:
			}
		}
	}

	go func() {
		defer resp.Body.Close()
		defer closeCh()

		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 4*1024*1024), 4*1024*1024) // 4 MB buffer

		for scanner.Scan() {
			line := scanner.Text()

			// SSE format: "data: {json}"
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")

			var chatEvent types.ChatEvent
			if err := json.Unmarshal([]byte(data), &chatEvent); err != nil {
				c.log.Debug("skip unparseable SSE event", "data", data[:min(len(data), 100)], "err", err)
				continue
			}

			trySend(chatEvent)

			if chatEvent.Done {
				return
			}
		}

		if err := scanner.Err(); err != nil {
			trySend(types.ChatEvent{Type: "error", Content: fmt.Sprintf("stream error: %v", err), Done: true})
		}
	}()

	go func() {
		<-ctx.Done()
		resp.Body.Close()
	}()

	return ch, nil
}
