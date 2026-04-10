package openrouter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"resty.dev/v3"
)

// Client is an HTTP client for OpenRouter's OpenAI-compatible API.
type Client struct {
	rc      *resty.Client
	apiKey  string // kept for EventSource (separate header management)
	baseURL string
	log     *slog.Logger

	// Thread-safe cost accumulators.
	mu                sync.Mutex
	TotalInputTokens  int64
	TotalOutputTokens int64
	Generations       []GenerationRecord
}

// GenerationRecord tracks a single API call for cost reporting.
type GenerationRecord struct {
	ID           string
	Model        string
	InputTokens  int
	OutputTokens int
	Timestamp    time.Time
}

// NewClient creates an OpenRouter HTTP client with retry and fluent request support.
func NewClient(apiKey, baseURL string) *Client {
	if baseURL == "" {
		baseURL = "https://openrouter.ai/api/v1"
	}
	baseURL = strings.TrimRight(baseURL, "/")

	rc := resty.New().
		SetBaseURL(baseURL).
		SetTimeout(120 * time.Second).
		SetAuthToken(apiKey).
		SetHeaders(map[string]string{
			"HTTP-Referer": "https://github.com/commit0-dev/commit0",
			"X-Title":      "commit0",
		}).
		SetRetryCount(3).
		SetRetryWaitTime(1 * time.Second).
		SetRetryMaxWaitTime(30 * time.Second).
		SetAllowNonIdempotentRetry(true) // POST retries for chat completions

	return &Client{
		rc:      rc,
		apiKey:  apiKey,
		baseURL: baseURL,
		log:     slog.Default().With("adapter", "openrouter"),
	}
}

// Chat sends a non-streaming chat completion request.
// Resty retry (3 attempts with exponential backoff) applies automatically for 429/500+.
func (c *Client) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	req.Stream = false

	var chatResp ChatResponse
	resp, err := c.rc.R().
		SetContext(ctx).
		SetBody(req).
		SetResult(&chatResp).
		Post("/chat/completions")
	if err != nil {
		return nil, fmt.Errorf("openrouter request: %w", err)
	}

	if resp.IsError() {
		return nil, fmt.Errorf("openrouter error %d: %s", resp.StatusCode(), resp.String())
	}

	c.recordUsage(chatResp.ID, chatResp.Model, chatResp.Usage)
	return &chatResp, nil
}

// ChatStream sends a streaming chat completion request and returns an iterator
// of SSE chunks. Uses Resty EventSource with POST for proper SSE protocol handling.
func (c *Client) ChatStream(ctx context.Context, req ChatRequest) iter.Seq2[*ChatStreamChunk, error] {
	req.Stream = true

	return func(yield func(*ChatStreamChunk, error) bool) {
		body, err := json.Marshal(req)
		if err != nil {
			yield(nil, fmt.Errorf("marshal request: %w", err))
			return
		}

		type streamMsg struct {
			chunk *ChatStreamChunk
			err   error
		}
		ch := make(chan streamMsg, 32)

		// Mutex-guarded channel ops prevent send-on-closed-channel panics.
		// Multiple goroutines (OnMessage, OnError, Get() return) can race to
		// send/close, so all access is serialized.
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

		trySend := func(msg streamMsg) {
			chMu.Lock()
			defer chMu.Unlock()
			if chOpen {
				select {
				case ch <- msg:
				default:
				}
			}
		}

		es := resty.NewEventSource().
			SetURL(c.baseURL+"/chat/completions").
			SetMethod("POST").
			SetBody(bytes.NewReader(body)).
			SetHeader("Content-Type", "application/json").
			SetHeader("Authorization", "Bearer "+c.apiKey).
			SetHeader("HTTP-Referer", "https://github.com/commit0-dev/commit0").
			SetHeader("X-Title", "commit0").
			SetRetryCount(0). // no reconnection — LLM streaming is one-shot
			OnMessage(func(e any) {
				event, ok := e.(*resty.Event)
				if !ok {
					return
				}

				// End of stream sentinel.
				if event.Data == "[DONE]" {
					closeCh()
					return
				}

				var chunk ChatStreamChunk
				if err := json.Unmarshal([]byte(event.Data), &chunk); err != nil {
					c.log.Debug("skip unparseable chunk", "data", event.Data, "err", err)
					return
				}

				// Record usage from final chunk.
				if chunk.Usage != nil {
					c.recordUsage(chunk.ID, chunk.Model, *chunk.Usage)
				}

				trySend(streamMsg{chunk: &chunk})
			}, nil).
			OnError(func(err error) {
				trySend(streamMsg{err: err})
				closeCh()
			}).
			OnRequestFailure(func(err error, _ *http.Response) {
				trySend(streamMsg{err: fmt.Errorf("stream request failed: %w", err)})
				closeCh()
			})

		// es.Get() blocks while reading SSE — run in goroutine.
		go func() {
			defer closeCh()
			if err := es.Get(); err != nil {
				trySend(streamMsg{err: err})
			}
		}()

		// Context cancellation closes EventSource.
		go func() {
			<-ctx.Done()
			es.Close()
		}()

		defer es.Close()

		// Pull from channel → yield to iter.Seq2 consumer.
		for msg := range ch {
			if msg.err != nil {
				yield(nil, msg.err)
				return
			}
			if !yield(msg.chunk, nil) {
				return
			}
		}
	}
}

// GetCost returns accumulated session token counts.
func (c *Client) GetCost() (inputTokens, outputTokens int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.TotalInputTokens, c.TotalOutputTokens
}

// GetGenerations returns all recorded generations for detailed cost reporting.
func (c *Client) GetGenerations() []GenerationRecord {
	c.mu.Lock()
	defer c.mu.Unlock()
	result := make([]GenerationRecord, len(c.Generations))
	copy(result, c.Generations)
	return result
}

func (c *Client) recordUsage(genID, modelName string, usage Usage) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.TotalInputTokens += int64(usage.PromptTokens)
	c.TotalOutputTokens += int64(usage.CompletionTokens)
	c.Generations = append(c.Generations, GenerationRecord{
		ID:           genID,
		Model:        modelName,
		InputTokens:  usage.PromptTokens,
		OutputTokens: usage.CompletionTokens,
		Timestamp:    time.Now(),
	})
}
