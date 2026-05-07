package mcp_test

import (
	"context"
	"sort"
	"strings"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/commit0-dev/commit0/pkg/types"
	mcpadapter "github.com/commit0-dev/commit0/server/internal/adapters/mcp"
	"github.com/commit0-dev/commit0/server/internal/domain"
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

func TestToolsList_ReturnsAllTools(t *testing.T) {
	sess, cancel := newTestPair(t, mcpadapter.Deps{})
	defer cancel()

	result, err := sess.ListTools(context.Background(), &mcpsdk.ListToolsParams{})
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	const wantCount = 10 // 4 search + 4 trace/analysis + 2 tests/subjects
	if len(result.Tools) != wantCount {
		t.Errorf("expected %d tools, got %d", wantCount, len(result.Tools))
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
		"commit0_blast",
		"commit0_field_flow",
		"commit0_find_root_cause",
		"commit0_lookup",
		"commit0_neighborhood",
		"commit0_query",
		"commit0_show_node",
		"commit0_subjects_for",
		"commit0_tests_for",
		"commit0_trace",
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
// commit0_trace tests
// ---------------------------------------------------------------------------

func TestTrace_MissingSymbol_ReturnsToolError(t *testing.T) {
	sess, cancel := newTestPair(t, mcpadapter.Deps{})
	defer cancel()

	result, err := sess.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "commit0_trace",
		Arguments: map[string]any{
			"repo_slug": "commit0-dev/commit0",
		},
	})
	if err != nil {
		return // protocol-level validation is fine
	}
	if result == nil {
		t.Fatal("got nil result for missing symbol")
	}
	if !result.IsError {
		t.Errorf("expected isError=true for missing symbol")
	}
}

func TestTrace_DBUnavailable_ReturnsToolError(t *testing.T) {
	sess, cancel := newTestPair(t, mcpadapter.Deps{DBAddr: "localhost:9999"})
	defer cancel()

	result, err := sess.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "commit0_trace",
		Arguments: map[string]any{
			"symbol":    "app.IndexService.Index",
			"repo_slug": "commit0-dev/commit0",
		},
	})
	if err != nil {
		t.Fatalf("unexpected protocol error: %v", err)
	}
	if !result.IsError {
		t.Errorf("expected isError=true when TraceService is nil")
	}
	if !strings.Contains(toolResultText(result), "SurrealDB") {
		t.Errorf("expected error text to mention SurrealDB, got: %s", toolResultText(result))
	}
}

// ---------------------------------------------------------------------------
// commit0_blast tests
// ---------------------------------------------------------------------------

func TestBlast_MissingSymbol_ReturnsToolError(t *testing.T) {
	sess, cancel := newTestPair(t, mcpadapter.Deps{})
	defer cancel()

	result, err := sess.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "commit0_blast",
		Arguments: map[string]any{
			"repo_slug": "commit0-dev/commit0",
		},
	})
	if err != nil {
		return
	}
	if result == nil {
		t.Fatal("got nil result for missing symbol")
	}
	if !result.IsError {
		t.Errorf("expected isError=true for missing symbol")
	}
}

func TestBlast_DBUnavailable_ReturnsToolError(t *testing.T) {
	sess, cancel := newTestPair(t, mcpadapter.Deps{DBAddr: "localhost:9999"})
	defer cancel()

	result, err := sess.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "commit0_blast",
		Arguments: map[string]any{
			"symbol":    "app.QueryService.Query",
			"repo_slug": "commit0-dev/commit0",
		},
	})
	if err != nil {
		t.Fatalf("unexpected protocol error: %v", err)
	}
	if !result.IsError {
		t.Errorf("expected isError=true when BlastService is nil")
	}
}

// ---------------------------------------------------------------------------
// commit0_field_flow tests
// ---------------------------------------------------------------------------

func TestFieldFlow_MissingSymbol_ReturnsToolError(t *testing.T) {
	sess, cancel := newTestPair(t, mcpadapter.Deps{})
	defer cancel()

	result, err := sess.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "commit0_field_flow",
		Arguments: map[string]any{
			"repo_slug": "commit0-dev/commit0",
		},
	})
	if err != nil {
		return
	}
	if !result.IsError {
		t.Errorf("expected isError=true for missing symbol")
	}
}

func TestFieldFlow_DBUnavailable_ReturnsToolError(t *testing.T) {
	sess, cancel := newTestPair(t, mcpadapter.Deps{DBAddr: "localhost:9999"})
	defer cancel()

	result, err := sess.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "commit0_field_flow",
		Arguments: map[string]any{
			"symbol":    "http.NewLoginHandler",
			"repo_slug": "commit0-dev/commit0",
		},
	})
	if err != nil {
		t.Fatalf("unexpected protocol error: %v", err)
	}
	if !result.IsError {
		t.Errorf("expected isError=true when FieldFlowService is nil")
	}
}

// ---------------------------------------------------------------------------
// commit0_find_root_cause tests
// ---------------------------------------------------------------------------

func TestFindRootCause_MissingDescription_ReturnsToolError(t *testing.T) {
	sess, cancel := newTestPair(t, mcpadapter.Deps{})
	defer cancel()

	result, err := sess.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "commit0_find_root_cause",
		Arguments: map[string]any{
			"repo_slug": "commit0-dev/commit0",
			"repo_path": "/tmp/repo",
		},
	})
	if err != nil {
		return
	}
	if !result.IsError {
		t.Errorf("expected isError=true for missing description")
	}
}

func TestFindRootCause_MissingRepoPath_ReturnsToolError(t *testing.T) {
	sess, cancel := newTestPair(t, mcpadapter.Deps{})
	defer cancel()

	result, err := sess.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "commit0_find_root_cause",
		Arguments: map[string]any{
			"description": "queries return wrong embedding dimension",
			"repo_slug":   "commit0-dev/commit0",
			// repo_path intentionally missing
		},
	})
	if err != nil {
		return
	}
	if !result.IsError {
		t.Errorf("expected isError=true for missing repo_path")
	}
	if !strings.Contains(toolResultText(result), "repo_path") {
		t.Errorf("expected error text to mention repo_path, got: %s", toolResultText(result))
	}
}

func TestFindRootCause_DBUnavailable_ReturnsToolError(t *testing.T) {
	sess, cancel := newTestPair(t, mcpadapter.Deps{DBAddr: "localhost:9999"})
	defer cancel()

	result, err := sess.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "commit0_find_root_cause",
		Arguments: map[string]any{
			"description": "queries return wrong embedding dimension",
			"repo_slug":   "commit0-dev/commit0",
			"repo_path":   "/tmp/repo",
		},
	})
	if err != nil {
		t.Fatalf("unexpected protocol error: %v", err)
	}
	if !result.IsError {
		t.Errorf("expected isError=true when RootCauseService is nil")
	}
}

// ---------------------------------------------------------------------------
// commit0_tests_for tests
// ---------------------------------------------------------------------------

func TestTestsFor_MissingQualified_ReturnsToolError(t *testing.T) {
	sess, cancel := newTestPair(t, mcpadapter.Deps{})
	defer cancel()

	result, err := sess.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "commit0_tests_for",
		Arguments: map[string]any{
			"repo_slug": "commit0-dev/commit0",
		},
	})
	if err != nil {
		return // SDK-level validation is acceptable
	}
	if !result.IsError {
		t.Errorf("expected isError=true for missing qualified")
	}
}

func TestTestsFor_DBUnavailable_ReturnsToolError(t *testing.T) {
	sess, cancel := newTestPair(t, mcpadapter.Deps{DBAddr: "localhost:9999"})
	defer cancel()

	result, err := sess.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "commit0_tests_for",
		Arguments: map[string]any{
			"qualified": "app.QueryService.Query",
			"repo_slug": "commit0-dev/commit0",
		},
	})
	if err != nil {
		t.Fatalf("unexpected protocol error: %v", err)
	}
	if !result.IsError {
		t.Errorf("expected isError=true when Graph is nil")
	}
	if !strings.Contains(toolResultText(result), "SurrealDB") {
		t.Errorf("expected error text to mention SurrealDB, got: %s", toolResultText(result))
	}
}

func TestTestsFor_HappyPath_ReturnsTestsSorted(t *testing.T) {
	subject := types.CodeNode{
		ID:        "function:app⋅QueryService⋅Query",
		Kind:      types.NodeFunction,
		Qualified: "app.QueryService.Query",
		Name:      "Query",
		FilePath:  "server/internal/app/query_service.go",
		StartLine: 50,
		EndLine:   120,
		RepoSlug:  "commit0-dev/commit0",
	}
	hops := []types.TraceHop{
		// hop=2 — should come second; mixed order.
		{Depth: 2, Node: types.CodeNode{
			Kind:      types.NodeFunction,
			Qualified: "app.TestQueryService_Integration",
			Name:      "TestQueryService_Integration",
			FilePath:  "server/internal/app/query_service_test.go",
			StartLine: 200,
		}},
		// hop=1, alphabetically later qualified — should come first by hop count.
		{Depth: 1, Node: types.CodeNode{
			Kind:      types.NodeFunction,
			Qualified: "app.TestQueryService_Query_BasicSearch",
			Name:      "TestQueryService_Query_BasicSearch",
			FilePath:  "server/internal/app/query_service_test.go",
			StartLine: 30,
		}},
		// non-test caller — must be filtered out.
		{Depth: 1, Node: types.CodeNode{
			Kind:      types.NodeFunction,
			Qualified: "app.QueryService.QueryWithExplain",
			Name:      "QueryWithExplain",
			FilePath:  "server/internal/app/query_service.go",
			StartLine: 130,
		}},
		// duplicate of hop=1 test at higher hop — must dedupe to lower hop.
		{Depth: 3, Node: types.CodeNode{
			Kind:      types.NodeFunction,
			Qualified: "app.TestQueryService_Query_BasicSearch",
			Name:      "TestQueryService_Query_BasicSearch",
			FilePath:  "server/internal/app/query_service_test.go",
		}},
	}

	graph := &mcpFakeGraph{findNode: &subject, traverseHops: hops}
	sess, cancel := newTestPair(t, mcpadapter.Deps{Graph: graph})
	defer cancel()

	result, err := sess.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "commit0_tests_for",
		Arguments: map[string]any{
			"qualified": "app.QueryService.Query",
			"repo_slug": "commit0-dev/commit0",
			"depth":     5,
		},
	})
	if err != nil {
		t.Fatalf("unexpected protocol error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %s", toolResultText(result))
	}

	// Verify traversal called with correct args.
	if graph.lastLabels == nil || graph.lastLabels[0] != "tests" {
		t.Errorf("expected traversal on 'tests' label, got %v", graph.lastLabels)
	}
	if graph.lastDirection != "reverse" {
		t.Errorf("expected reverse direction, got %s", graph.lastDirection)
	}

	got, ok := result.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("expected map structured content, got %T", result.StructuredContent)
	}
	if total, _ := got["total"].(float64); int(total) != 2 {
		t.Errorf("expected 2 unique tests after dedup, got total=%v", got["total"])
	}
	tests, _ := got["tests"].([]any)
	if len(tests) != 2 {
		t.Fatalf("expected 2 tests in payload, got %d", len(tests))
	}
	first, _ := tests[0].(map[string]any)
	if name, _ := first["qualified"].(string); name != "app.TestQueryService_Query_BasicSearch" {
		t.Errorf("expected hop=1 test first, got %v", first["qualified"])
	}
	if hop, _ := first["hop_count"].(float64); int(hop) != 1 {
		t.Errorf("expected first hop=1, got %v", first["hop_count"])
	}
}

func TestTestsFor_DirectOnly_OverridesDepth(t *testing.T) {
	subject := types.CodeNode{
		ID:        "function:app⋅Foo",
		Qualified: "app.Foo",
		FilePath:  "foo.go",
	}
	graph := &mcpFakeGraph{findNode: &subject, traverseHops: nil}
	sess, cancel := newTestPair(t, mcpadapter.Deps{Graph: graph})
	defer cancel()

	_, err := sess.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "commit0_tests_for",
		Arguments: map[string]any{
			"qualified":   "app.Foo",
			"repo_slug":   "x/y",
			"depth":       9,
			"direct_only": true,
		},
	})
	if err != nil {
		t.Fatalf("unexpected protocol error: %v", err)
	}
	if graph.lastDepth != 1 {
		t.Errorf("direct_only should force depth=1, got %d", graph.lastDepth)
	}
}

// ---------------------------------------------------------------------------
// commit0_subjects_for tests
// ---------------------------------------------------------------------------

func TestSubjectsFor_MissingTestQualified_ReturnsToolError(t *testing.T) {
	sess, cancel := newTestPair(t, mcpadapter.Deps{})
	defer cancel()

	result, err := sess.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "commit0_subjects_for",
		Arguments: map[string]any{
			"repo_slug": "commit0-dev/commit0",
		},
	})
	if err != nil {
		return
	}
	if !result.IsError {
		t.Errorf("expected isError=true for missing test_qualified")
	}
}

func TestSubjectsFor_DBUnavailable_ReturnsToolError(t *testing.T) {
	sess, cancel := newTestPair(t, mcpadapter.Deps{DBAddr: "localhost:9999"})
	defer cancel()

	result, err := sess.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "commit0_subjects_for",
		Arguments: map[string]any{
			"test_qualified": "app.TestFoo",
			"repo_slug":      "commit0-dev/commit0",
		},
	})
	if err != nil {
		t.Fatalf("unexpected protocol error: %v", err)
	}
	if !result.IsError {
		t.Errorf("expected isError=true when Graph is nil")
	}
}

func TestSubjectsFor_HappyPath_ExcludesTestNodesAndSelf(t *testing.T) {
	test := types.CodeNode{
		ID:        "function:app⋅TestQueryService_Query_BasicSearch",
		Kind:      types.NodeFunction,
		Qualified: "app.TestQueryService_Query_BasicSearch",
		Name:      "TestQueryService_Query_BasicSearch",
		FilePath:  "server/internal/app/query_service_test.go",
	}
	hops := []types.TraceHop{
		// production fn — keep.
		{Depth: 1, Node: types.CodeNode{
			Kind:      types.NodeFunction,
			Qualified: "app.QueryService.Query",
			Name:      "Query",
			FilePath:  "server/internal/app/query_service.go",
			StartLine: 50,
		}},
		// helper test in same _test.go — drop.
		{Depth: 1, Node: types.CodeNode{
			Kind:      types.NodeFunction,
			Qualified: "app.testHelperBuildQueryService",
			Name:      "testHelperBuildQueryService",
			FilePath:  "server/internal/app/query_service_test.go",
		}},
		// transitively-called prod fn — keep.
		{Depth: 2, Node: types.CodeNode{
			Kind:      types.NodeFunction,
			Qualified: "surreal.SurrealAdapter.VectorSearch",
			Name:      "VectorSearch",
			FilePath:  "server/internal/adapters/surreal/vector_index.go",
			StartLine: 34,
		}},
		// echoes the test itself — drop.
		{Depth: 0, Node: types.CodeNode{
			Qualified: "app.TestQueryService_Query_BasicSearch",
			FilePath:  "server/internal/app/query_service_test.go",
		}},
	}

	graph := &mcpFakeGraph{findNode: &test, traverseHops: hops}
	sess, cancel := newTestPair(t, mcpadapter.Deps{Graph: graph})
	defer cancel()

	result, err := sess.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "commit0_subjects_for",
		Arguments: map[string]any{
			"test_qualified": test.Qualified,
			"repo_slug":      "commit0-dev/commit0",
		},
	})
	if err != nil {
		t.Fatalf("unexpected protocol error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %s", toolResultText(result))
	}

	// Forward traversal on calls + tests.
	gotLabels := map[string]bool{}
	for _, l := range graph.lastLabels {
		gotLabels[l] = true
	}
	if !gotLabels["tests"] || !gotLabels["calls"] {
		t.Errorf("expected traversal on tests+calls, got %v", graph.lastLabels)
	}
	if graph.lastDirection != "forward" {
		t.Errorf("expected forward direction, got %s", graph.lastDirection)
	}

	got, ok := result.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("expected map structured content, got %T", result.StructuredContent)
	}
	subjects, _ := got["subjects"].([]any)
	if len(subjects) != 2 {
		t.Fatalf("expected 2 prod subjects after filtering, got %d", len(subjects))
	}
	for _, s := range subjects {
		m, _ := s.(map[string]any)
		path, _ := m["file_path"].(string)
		if strings.Contains(path, "_test.go") {
			t.Errorf("test file leaked into subjects: %s", path)
		}
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

// mcpFakeGraph is a minimal fake satisfying domain.OpenCodeGraph for the tests
// in this package that need a non-nil Graph without booting SurrealDB. Methods
// untouched by the tools under test return zero-value/nil and don't error.
type mcpFakeGraph struct {
	findNode      *types.CodeNode
	findErr       error
	traverseHops  []types.TraceHop
	traverseErr   error
	lastLabels    []string
	lastDirection string
	lastDepth     int
}

func (g *mcpFakeGraph) PutNode(_ context.Context, _ *types.CodeNode) error { return nil }
func (g *mcpFakeGraph) GetNode(_ context.Context, _ string) (*types.CodeNode, error) {
	return g.findNode, g.findErr
}
func (g *mcpFakeGraph) FindNode(_ context.Context, _, _ string) (*types.CodeNode, error) {
	return g.findNode, g.findErr
}
func (g *mcpFakeGraph) DeleteNode(_ context.Context, _ string) error       { return nil }
func (g *mcpFakeGraph) PutEdge(_ context.Context, _ *types.CodeEdge) error { return nil }
func (g *mcpFakeGraph) DeleteEdgesFrom(_ context.Context, _ string) error  { return nil }
func (g *mcpFakeGraph) PutBatch(_ context.Context, _ []types.CodeNode, _ []types.CodeEdge) error {
	return nil
}
func (g *mcpFakeGraph) DeleteByRepo(_ context.Context, _ string) error    { return nil }
func (g *mcpFakeGraph) DeleteByFile(_ context.Context, _, _ string) error { return nil }
func (g *mcpFakeGraph) TraverseGraph(_ context.Context, _ string, labels []string, direction string, depth int) ([]types.TraceHop, error) {
	g.lastLabels = labels
	g.lastDirection = direction
	g.lastDepth = depth
	return g.traverseHops, g.traverseErr
}
func (g *mcpFakeGraph) Neighbors(_ context.Context, _ string) (*domain.Neighborhood, error) {
	return nil, nil
}
func (g *mcpFakeGraph) VectorSearch(_ context.Context, _ []float32, _ domain.VectorSearchOpts) ([]types.ScoredNode, error) {
	return nil, nil
}
func (g *mcpFakeGraph) TextSearch(_ context.Context, _ string, _ domain.TextSearchOpts) ([]types.ScoredNode, error) {
	return nil, nil
}
func (g *mcpFakeGraph) ListNodes(_ context.Context, _ string, _ domain.ListOpts) ([]types.CodeNode, error) {
	return nil, nil
}
func (g *mcpFakeGraph) ListEdges(_ context.Context, _ string, _ []string) ([]types.CodeEdge, error) {
	return nil, nil
}
func (g *mcpFakeGraph) ListFilePaths(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}
func (g *mcpFakeGraph) PutRepo(_ context.Context, _ *types.Repo) error           { return nil }
func (g *mcpFakeGraph) GetRepo(_ context.Context, _ string) (*types.Repo, error) { return nil, nil }
func (g *mcpFakeGraph) ListRepos(_ context.Context) ([]types.Repo, error)        { return nil, nil }
func (g *mcpFakeGraph) UpdateRepoIndexedAt(_ context.Context, _ string, _ time.Time) error {
	return nil
}
func (g *mcpFakeGraph) FindRepoByRemoteURL(_ context.Context, _ string) (*types.Repo, error) {
	return nil, nil
}
func (g *mcpFakeGraph) DeleteRepo(_ context.Context, _ string) error { return nil }
func (g *mcpFakeGraph) ApplySchema(_ context.Context) error          { return nil }

var _ domain.OpenCodeGraph = (*mcpFakeGraph)(nil)
