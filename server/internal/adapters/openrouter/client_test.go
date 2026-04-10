package openrouter

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// newTestClient creates a Client pointing at a test server.
func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return NewClient("test-key", srv.URL)
}

func TestChat_Success(t *testing.T) {
	want := ChatResponse{
		ID:    "gen-123",
		Model: "test-model",
		Choices: []Choice{{
			Index:        0,
			Message:      Message{Role: "assistant", Content: "Hello!"},
			FinishReason: "stop",
		}},
		Usage: Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
	}

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		// Verify headers.
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("auth header = %q, want Bearer test-key", got)
		}
		if got := r.Header.Get("X-Title"); got != "commit0" {
			t.Errorf("X-Title = %q, want commit0", got)
		}
		if r.Method != "POST" {
			t.Errorf("method = %s, want POST", r.Method)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(want)
	})

	resp, err := client.Chat(context.Background(), ChatRequest{Model: "test-model"})
	if err != nil {
		t.Fatalf("Chat() error: %v", err)
	}

	if resp.ID != want.ID {
		t.Errorf("ID = %q, want %q", resp.ID, want.ID)
	}
	if len(resp.Choices) != 1 {
		t.Fatalf("choices = %d, want 1", len(resp.Choices))
	}
	if resp.Choices[0].Message.Content != "Hello!" {
		t.Errorf("content = %v, want Hello!", resp.Choices[0].Message.Content)
	}

	// Verify cost tracking.
	in, out := client.GetCost()
	if in != 10 || out != 5 {
		t.Errorf("cost = (%d, %d), want (10, 5)", in, out)
	}
}

func TestChat_ErrorStatus(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error":{"message":"bad model"}}`)
	})

	_, err := client.Chat(context.Background(), ChatRequest{Model: "bad"})
	if err == nil {
		t.Fatal("Chat() expected error for 400")
	}
	if got := err.Error(); got == "" {
		t.Error("error message should not be empty")
	}
}

func TestChat_RetryOn429(t *testing.T) {
	var attempts atomic.Int32

	want := ChatResponse{
		ID:    "gen-retry",
		Model: "test-model",
		Choices: []Choice{{
			Message:      Message{Role: "assistant", Content: "OK"},
			FinishReason: "stop",
		}},
		Usage: Usage{PromptTokens: 5, CompletionTokens: 2, TotalTokens: 7},
	}

	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		n := attempts.Add(1)
		if n <= 2 {
			w.WriteHeader(http.StatusTooManyRequests)
			fmt.Fprint(w, "rate limited")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(want)
	})

	// Override retry wait times for faster test.
	client.rc.SetRetryWaitTime(10 * time.Millisecond).SetRetryMaxWaitTime(50 * time.Millisecond)

	resp, err := client.Chat(context.Background(), ChatRequest{Model: "test-model"})
	if err != nil {
		t.Fatalf("Chat() should succeed after retries: %v", err)
	}
	if resp.ID != "gen-retry" {
		t.Errorf("ID = %q, want gen-retry", resp.ID)
	}
	if got := attempts.Load(); got != 3 {
		t.Errorf("attempts = %d, want 3", got)
	}
}

func TestChat_RetryExhausted(t *testing.T) {
	var attempts atomic.Int32

	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "server error")
	})

	client.rc.SetRetryWaitTime(10 * time.Millisecond).SetRetryMaxWaitTime(50 * time.Millisecond)

	_, err := client.Chat(context.Background(), ChatRequest{Model: "test-model"})
	if err == nil {
		t.Fatal("Chat() should fail after exhausting retries")
	}
	// 1 initial + 3 retries = 4 attempts.
	if got := attempts.Load(); got != 4 {
		t.Errorf("attempts = %d, want 4", got)
	}
}

func TestChatStream_Success(t *testing.T) {
	chunks := []ChatStreamChunk{
		{ID: "gen-s1", Choices: []StreamChoice{{Delta: StreamDelta{Content: "Hello"}}}},
		{ID: "gen-s1", Choices: []StreamChoice{{Delta: StreamDelta{Content: " world"}}}},
		{ID: "gen-s1", Choices: []StreamChoice{{FinishReason: ptr("stop")}}, Usage: &Usage{PromptTokens: 10, CompletionTokens: 4, TotalTokens: 14}},
	}

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("server does not support flushing")
		}

		for _, chunk := range chunks {
			data, _ := json.Marshal(chunk)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	})

	var collected []ChatStreamChunk
	for chunk, err := range client.ChatStream(context.Background(), ChatRequest{Model: "test-model"}) {
		if err != nil {
			t.Fatalf("stream error: %v", err)
		}
		collected = append(collected, *chunk)
	}

	if len(collected) != 3 {
		t.Fatalf("chunks = %d, want 3", len(collected))
	}
	if collected[0].Choices[0].Delta.Content != "Hello" {
		t.Errorf("chunk[0] content = %q, want Hello", collected[0].Choices[0].Delta.Content)
	}

	// Verify usage was recorded from final chunk.
	in, out := client.GetCost()
	if in != 10 || out != 4 {
		t.Errorf("cost = (%d, %d), want (10, 4)", in, out)
	}
}

func TestChatStream_ErrorStatus(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, "bad request")
	})

	for _, err := range client.ChatStream(context.Background(), ChatRequest{Model: "bad"}) {
		if err != nil {
			return // expected
		}
	}
	t.Fatal("expected an error from stream")
}

func TestChatStream_ContextCancel(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)

		// Send one chunk, then hang.
		chunk := ChatStreamChunk{ID: "gen-c", Choices: []StreamChoice{{Delta: StreamDelta{Content: "hi"}}}}
		data, _ := json.Marshal(chunk)
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()

		// Block briefly — EventSource close doesn't interrupt reads,
		// so the test waits for the server to close the connection.
		<-time.After(2 * time.Second)
	})

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	count := 0
	for _, err := range client.ChatStream(ctx, ChatRequest{Model: "test-model"}) {
		if err != nil {
			break
		}
		count++
	}

	if count < 1 {
		t.Error("expected at least 1 chunk before cancel")
	}
}

func TestGetCost_Concurrent(t *testing.T) {
	client := NewClient("key", "http://unused")

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			client.recordUsage("gen", "model", Usage{PromptTokens: 1, CompletionTokens: 2, TotalTokens: 3})
		}()
	}
	wg.Wait()

	in, out := client.GetCost()
	if in != 100 || out != 200 {
		t.Errorf("cost = (%d, %d), want (100, 200)", in, out)
	}

	gens := client.GetGenerations()
	if len(gens) != 100 {
		t.Errorf("generations = %d, want 100", len(gens))
	}
}

func ptr(s string) *string { return &s }
