package mcp_test

import (
	"context"
	"sort"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	mcpadapter "github.com/commit0-dev/commit0/server/internal/adapters/mcp"
)

// newTestPair creates a server-client pair connected via in-memory transport.
// The server is started in a goroutine; cancel() shuts it down.
func newTestPair(t *testing.T, deps mcpadapter.Deps) (session *mcpsdk.ClientSession, cancel func()) {
	t.Helper()

	server := mcpadapter.New(deps)
	serverTransport, clientTransport := mcpsdk.NewInMemoryTransports()

	ctx, cancelCtx := context.WithCancel(context.Background())

	go func() {
		if err := server.Run(ctx, serverTransport); err != nil && ctx.Err() == nil {
			t.Logf("server exited: %v", err)
		}
	}()

	client := mcpsdk.NewClient(&mcpsdk.Implementation{
		Name:    "test-client",
		Version: "0.0.1",
	}, nil)

	sess, err := client.Connect(context.Background(), clientTransport, nil)
	if err != nil {
		cancelCtx()
		t.Fatalf("connect: %v", err)
	}

	return sess, func() {
		sess.Close()
		cancelCtx()
	}
}

// ---------------------------------------------------------------------------
// Lifecycle + capability tests
// ---------------------------------------------------------------------------

func TestToolsList_ReturnsFourTools(t *testing.T) {
	sess, cancel := newTestPair(t, mcpadapter.Deps{})
	defer cancel()

	result, err := sess.ListTools(context.Background(), &mcpsdk.ListToolsParams{})
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	if len(result.Tools) != 4 {
		t.Errorf("expected 4 tools, got %d", len(result.Tools))
		for _, tool := range result.Tools {
			t.Logf("  tool: %s", tool.Name)
		}
	}
}

func TestToolsList_NamesAreSorted(t *testing.T) {
	sess, cancel := newTestPair(t, mcpadapter.Deps{})
	defer cancel()

	result, err := sess.ListTools(context.Background(), &mcpsdk.ListToolsParams{})
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	names := make([]string, len(result.Tools))
	for i, tool := range result.Tools {
		names[i] = tool.Name
	}

	if !sort.StringsAreSorted(names) {
		t.Errorf("tool names are not sorted: %v", names)
	}
}

func TestToolsList_ExpectedNames(t *testing.T) {
	sess, cancel := newTestPair(t, mcpadapter.Deps{})
	defer cancel()

	result, err := sess.ListTools(context.Background(), &mcpsdk.ListToolsParams{})
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	want := []string{
		"commit0_lookup",
		"commit0_neighborhood",
		"commit0_query",
		"commit0_show_node",
	}

	got := make([]string, len(result.Tools))
	for i, tool := range result.Tools {
		got[i] = tool.Name
	}
	sort.Strings(got)

	if len(got) != len(want) {
		t.Fatalf("expected %d tools, got %d: %v", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("tool[%d]: got %q, want %q", i, got[i], want[i])
		}
	}
}

func TestToolsList_AllHaveDescriptions(t *testing.T) {
	sess, cancel := newTestPair(t, mcpadapter.Deps{})
	defer cancel()

	result, err := sess.ListTools(context.Background(), &mcpsdk.ListToolsParams{})
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	for _, tool := range result.Tools {
		if tool.Description == "" {
			t.Errorf("tool %q has empty description", tool.Name)
		}
		if tool.InputSchema == nil {
			t.Errorf("tool %q has nil input schema", tool.Name)
		}
	}
}

// ---------------------------------------------------------------------------
// commit0_query tests
// ---------------------------------------------------------------------------

func TestQuery_MissingRequiredField_ReturnsToolError(t *testing.T) {
	sess, cancel := newTestPair(t, mcpadapter.Deps{})
	defer cancel()

	// Missing repo_slug — should return a tool error (isError=true), not a protocol error.
	result, err := sess.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "commit0_query",
		Arguments: map[string]any{
			"question": "where is the query service?",
			// repo_slug intentionally omitted
		},
	})
	// The SDK validates required fields and returns a protocol error (non-nil err)
	// OR a tool error (IsError=true). Either is acceptable for missing required fields.
	if err != nil {
		// Protocol-level validation: acceptable.
		return
	}
	if result == nil {
		t.Fatal("got nil result for missing required field")
	}
	// If we get a result, it should be an error.
	if !result.IsError {
		t.Errorf("expected isError=true for missing required field, got content: %v", result.Content)
	}
}

func TestQuery_DBUnavailable_ReturnsToolError(t *testing.T) {
	// Deps with nil QueryService → db unavailable error.
	sess, cancel := newTestPair(t, mcpadapter.Deps{
		DBAddr: "localhost:9999",
	})
	defer cancel()

	result, err := sess.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "commit0_query",
		Arguments: map[string]any{
			"question":  "where is authentication handled?",
			"repo_slug": "test-repo",
		},
	})
	if err != nil {
		t.Fatalf("unexpected protocol error: %v", err)
	}
	if !result.IsError {
		t.Errorf("expected isError=true when DB is unavailable")
	}
	// The error message should mention SurrealDB or how to fix it.
	text := toolResultText(result)
	if text == "" {
		t.Errorf("expected non-empty error text")
	}
}

// ---------------------------------------------------------------------------
// commit0_lookup tests
// ---------------------------------------------------------------------------

func TestLookup_DBUnavailable_ReturnsToolError(t *testing.T) {
	sess, cancel := newTestPair(t, mcpadapter.Deps{DBAddr: "localhost:9999"})
	defer cancel()

	result, err := sess.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "commit0_lookup",
		Arguments: map[string]any{
			"qualified": "server/internal/app.QueryService.Query",
			"repo_slug": "commit0-dev/commit0",
		},
	})
	if err != nil {
		t.Fatalf("unexpected protocol error: %v", err)
	}
	if !result.IsError {
		t.Errorf("expected isError=true when DB is unavailable")
	}
}

// ---------------------------------------------------------------------------
// commit0_neighborhood tests
// ---------------------------------------------------------------------------

func TestNeighborhood_NoInputs_ReturnsToolError(t *testing.T) {
	sess, cancel := newTestPair(t, mcpadapter.Deps{})
	defer cancel()

	// Neither node_id nor qualified provided.
	result, err := sess.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name:      "commit0_neighborhood",
		Arguments: map[string]any{},
	})
	if err != nil {
		// Protocol-level validation is fine too.
		return
	}
	if result == nil {
		t.Fatal("got nil result")
	}
	if !result.IsError {
		t.Errorf("expected isError=true when neither node_id nor qualified is provided")
	}
}

func TestNeighborhood_DBUnavailable_ReturnsToolError(t *testing.T) {
	sess, cancel := newTestPair(t, mcpadapter.Deps{DBAddr: "localhost:9999"})
	defer cancel()

	result, err := sess.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "commit0_neighborhood",
		Arguments: map[string]any{
			"node_id": "some-node-id",
		},
	})
	if err != nil {
		t.Fatalf("unexpected protocol error: %v", err)
	}
	if !result.IsError {
		t.Errorf("expected isError=true when DB is unavailable")
	}
}

// ---------------------------------------------------------------------------
// commit0_show_node tests
// ---------------------------------------------------------------------------

func TestShowNode_MissingNodeID_ReturnsError(t *testing.T) {
	sess, cancel := newTestPair(t, mcpadapter.Deps{})
	defer cancel()

	result, err := sess.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name:      "commit0_show_node",
		Arguments: map[string]any{},
	})
	if err != nil {
		// Protocol validation: fine.
		return
	}
	if result == nil {
		t.Fatal("got nil result")
	}
	if !result.IsError {
		t.Errorf("expected isError=true for missing node_id, got content: %v", result.Content)
	}
}

func TestShowNode_DBUnavailable_ReturnsToolError(t *testing.T) {
	sess, cancel := newTestPair(t, mcpadapter.Deps{DBAddr: "localhost:9999"})
	defer cancel()

	result, err := sess.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "commit0_show_node",
		Arguments: map[string]any{
			"node_id": "some-node-id",
		},
	})
	if err != nil {
		t.Fatalf("unexpected protocol error: %v", err)
	}
	if !result.IsError {
		t.Errorf("expected isError=true when DB is unavailable")
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// toolResultText returns the concatenated text content of a tool result.
func toolResultText(r *mcpsdk.CallToolResult) string {
	if r == nil {
		return ""
	}
	out := ""
	for _, c := range r.Content {
		if tc, ok := c.(*mcpsdk.TextContent); ok {
			out += tc.Text
		}
	}
	return out
}
