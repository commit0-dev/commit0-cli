package http

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/commit0-dev/commit0/pkg/types"
	mcpadapter "github.com/commit0-dev/commit0/server/internal/adapters/mcp"
	"github.com/commit0-dev/commit0/server/internal/app"
)

// TestSetMCPHandler_RoutesUnderMCP starts the HTTP server on an httptest
// listener, mounts the MCP handler at /mcp, and confirms an MCP client
// driving the streamable-HTTP transport sees all 18 tools sorted. This is
// the integration-level proof that #56 closed: same process, same registry.
func TestSetMCPHandler_RoutesUnderMCP(t *testing.T) {
	server := defaultTestServer()
	server.SetMCPHandler(mcpadapter.Deps{
		// Empty deps: tools/list does not need a graph; tool calls would
		// surface dbUnavailable, but we are only asserting protocol surface.
		IndexService: &app.IndexService{},
	})

	httpSrv := httptest.NewServer(server.router)
	defer httpSrv.Close()

	transport := &mcpsdk.StreamableClientTransport{
		Endpoint: httpSrv.URL + "/mcp",
	}
	client := mcpsdk.NewClient(&mcpsdk.Implementation{
		Name:    "test-http-mcp-client",
		Version: "0.0.1",
	}, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	sess, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer func() { _ = sess.Close() }()

	res, err := sess.ListTools(ctx, &mcpsdk.ListToolsParams{})
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(res.Tools) != 18 {
		t.Errorf("/mcp tools/list returned %d tools, want 18", len(res.Tools))
	}
	names := make([]string, len(res.Tools))
	for i, tool := range res.Tools {
		names[i] = tool.Name
	}
	if !sort.StringsAreSorted(names) {
		t.Errorf("/mcp tools/list names not sorted: %v", names)
	}
}

// TestSetMCPHandler_TrailingSlashAccepted confirms both /mcp and /mcp/
// route to the same handler — useful because some MCP clients normalize URLs
// differently from others.
func TestSetMCPHandler_TrailingSlashAccepted(t *testing.T) {
	server := defaultTestServer()
	server.SetMCPHandler(mcpadapter.Deps{
		IndexService: &app.IndexService{},
	})

	httpSrv := httptest.NewServer(server.router)
	defer httpSrv.Close()

	for _, path := range []string{"/mcp", "/mcp/"} {
		path := path
		t.Run(strings.TrimPrefix(path, "/"), func(t *testing.T) {
			body := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"c","version":"1"}}}`)
			req, err := http.NewRequest("POST", httpSrv.URL+path, body)
			if err != nil {
				t.Fatalf("NewRequest: %v", err)
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "application/json, text/event-stream")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("Do: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
				raw, _ := io.ReadAll(resp.Body)
				t.Fatalf("status = %d, body = %s", resp.StatusCode, string(raw))
			}
		})
	}
}

// TestSetMCPHandler_IndexStatus_RegistryIsShared is the smoking-gun test for
// issue #56: a tracker registered through the same IndexService that backs
// the HTTP API must be visible to MCP clients connecting via /mcp. The bug
// before this PR was that MCP and HTTP ran in different processes, so the
// trackerRegistry was unreachable. Now they share one process.
func TestSetMCPHandler_IndexStatus_RegistryIsShared(t *testing.T) {
	server := defaultTestServer()

	// Seed a tracker on the IndexService that backs both surfaces.
	indexSvc := server.indexSvc
	tracker := app.NewIndexTracker("shared-job", "demo/repo", types.IndexConfig{})
	tracker.AddFiles(7)
	indexSvc.RegisterTrackerForTest(tracker)

	server.SetMCPHandler(mcpadapter.Deps{
		IndexService: indexSvc,
	})

	httpSrv := httptest.NewServer(server.router)
	defer httpSrv.Close()

	transport := &mcpsdk.StreamableClientTransport{
		Endpoint: httpSrv.URL + "/mcp",
	}
	client := mcpsdk.NewClient(&mcpsdk.Implementation{
		Name:    "registry-share-test",
		Version: "0.0.1",
	}, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	sess, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer func() { _ = sess.Close() }()

	result, err := sess.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "commit0_index_status",
		Arguments: map[string]any{"job_id": "shared-job"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if result.IsError {
		body := ""
		for _, c := range result.Content {
			if tc, ok := c.(*mcpsdk.TextContent); ok {
				body = tc.Text
				break
			}
		}
		t.Fatalf("commit0_index_status returned IsError=true: %s", body)
	}

	// StructuredContent should round-trip a snapshot keyed to shared-job.
	rawStructured, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(rawStructured, &parsed); err != nil {
		t.Fatalf("unmarshal structured content: %v", err)
	}
	if got, _ := parsed["job_id"].(string); got != "shared-job" {
		t.Errorf("job_id = %q, want %q", got, "shared-job")
	}
	if got, _ := parsed["files_indexed"].(float64); got != 7 {
		t.Errorf("files_indexed = %v, want 7", got)
	}
}
