package sdk

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/commit0-dev/commit0-cli/pkg/types"
)

// FindRootRequest is the request body for root cause analysis.
type FindRootRequest struct {
	Description string `json:"description"`
	RepoSlug    string `json:"repo_slug"`
	RepoPath    string `json:"repo_path,omitempty"`
	Since       string `json:"since,omitempty"`
}

// FindRootEvent is a single SSE event from the find-root stream.
type FindRootEvent struct {
	Type   string                 // "status", "result", "error", "done"
	Result *types.RootCauseReport // non-nil when Type == "result"
	Status string                 // message when Type == "status"
	Error  string                 // message when Type == "error"
}

// FindRoot performs root cause analysis via SSE streaming.
// Returns a channel of events. The caller should read until the channel closes.
func (c *Client) FindRoot(ctx context.Context, req FindRootRequest) (<-chan FindRootEvent, error) {
	body, _ := json.Marshal(req)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.rc.BaseURL()+"/api/v1/find-root", strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("find-root: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := http.DefaultClient.Do(httpReq) //nolint:bodyclose // closed in the goroutine via defer resp.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("find-root: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("find-root: HTTP %d", resp.StatusCode)
	}

	ch := make(chan FindRootEvent, 16)
	go func() {
		defer close(ch)
		defer resp.Body.Close()

		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 4*1024*1024), 4*1024*1024)

		var eventType string
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "event: ") {
				eventType = strings.TrimPrefix(line, "event: ")
			} else if strings.HasPrefix(line, "data: ") {
				data := strings.TrimPrefix(line, "data: ")
				evt := FindRootEvent{Type: eventType}

				switch eventType {
				case "result":
					var report types.RootCauseReport
					if json.Unmarshal([]byte(data), &report) == nil {
						evt.Result = &report
					}
				case "status":
					var m map[string]string
					if json.Unmarshal([]byte(data), &m) == nil {
						evt.Status = m["message"]
					}
				case "error":
					var m map[string]string
					if json.Unmarshal([]byte(data), &m) == nil {
						evt.Error = m["message"]
					}
				case "done":
					ch <- evt
					return
				}
				ch <- evt
			}
		}
	}()

	return ch, nil
}
