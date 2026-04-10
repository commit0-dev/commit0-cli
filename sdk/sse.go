package sdk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	"resty.dev/v3"

	"github.com/commit0-dev/commit0/pkg/types"
)

// AgentChatRequest is the request body for agent chat SSE streaming.
type AgentChatRequest struct {
	SessionID string `json:"session_id,omitempty"`
	Message   string `json:"message"`
	RepoSlug  string `json:"repo_slug,omitempty"`
}

// AgentChat starts a streaming agent chat session via SSE.
// Returns a channel of ChatEvents that the caller consumes.
func (c *Client) AgentChat(ctx context.Context, req AgentChatRequest) (<-chan types.ChatEvent, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
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

	es := resty.NewEventSource().
		SetURL(c.rc.BaseURL()+"/api/v1/agent/chat").
		SetMethod("POST").
		SetBody(bytes.NewReader(body)).
		SetHeader("Content-Type", "application/json").
		SetRetryCount(0).
		OnMessage(func(e any) {
			event, ok := e.(*resty.Event)
			if !ok {
				return
			}

			var chatEvent types.ChatEvent
			if err := json.Unmarshal([]byte(event.Data), &chatEvent); err != nil {
				c.log.Debug("skip unparseable SSE event", "data", event.Data, "err", err)
				return
			}

			trySend(chatEvent)

			if chatEvent.Done {
				closeCh()
			}
		}, nil).
		OnError(func(err error) {
			trySend(types.ChatEvent{Type: "error", Content: err.Error(), Done: true})
			closeCh()
		}).
		OnRequestFailure(func(err error, _ *http.Response) {
			trySend(types.ChatEvent{Type: "error", Content: fmt.Sprintf("request failed: %v", err), Done: true})
			closeCh()
		})

	go func() {
		defer closeCh()
		if err := es.Get(); err != nil {
			trySend(types.ChatEvent{Type: "error", Content: err.Error(), Done: true})
		}
	}()

	go func() {
		<-ctx.Done()
		es.Close()
	}()

	return ch, nil
}
