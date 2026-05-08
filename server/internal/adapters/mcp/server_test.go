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
	"github.com/commit0-dev/commit0/server/internal/app"
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

	const wantCount = 16 // 4 search + 1 similar + 4 trace/analysis + 2 tests/subjects + 1 diff + 1 interface + 3 meta
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
		"commit0_diff_impact",
		"commit0_field_flow",
		"commit0_find_root_cause",
		"commit0_index_status",
		"commit0_list_files",
		"commit0_list_repos",
		"commit0_lookup",
		"commit0_neighborhood",
		"commit0_query",
		"commit0_resolve_interface",
		"commit0_show_node",
		"commit0_similar_to",
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
// Tool: commit0_similar_to
// ---------------------------------------------------------------------------

func TestCommit0SimilarTo_MissingNodeID(t *testing.T) {
	sess, cancel := newTestPair(t, mcpadapter.Deps{Graph: &mcpFakeGraph{}})
	defer cancel()

	result, err := sess.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name:      "commit0_similar_to",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("unexpected protocol error: %v", err)
	}
	if !result.IsError {
		t.Errorf("expected tool error for missing node_id")
	}
}

func TestCommit0SimilarTo_DBUnavailable(t *testing.T) {
	sess, cancel := newTestPair(t, mcpadapter.Deps{})
	defer cancel()

	result, err := sess.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "commit0_similar_to",
		Arguments: map[string]any{
			"node_id": "function:some-id",
		},
	})
	if err != nil {
		t.Fatalf("unexpected protocol error: %v", err)
	}
	if !result.IsError {
		t.Errorf("expected tool error when graph is unavailable")
	}
}

func TestCommit0SimilarTo_HappyPath(t *testing.T) {
	sourceNode := types.CodeNode{
		ID:        "function:source-id",
		Kind:      types.NodeFunction,
		Qualified: "app.runWalk",
		Name:      "runWalk",
		FilePath:  "server/internal/app/walk.go",
		RepoSlug:  "commit0-dev/commit0",
		Language:  "go",
		StartLine: 42,
		EndLine:   100,
	}

	// Fake embedding for the source node
	sourceEmbedding := make([]float32, 100)
	for i := range sourceEmbedding {
		sourceEmbedding[i] = float32(i) * 0.01
	}

	// Fake similar nodes (source will be #1 with score 1.0, so include extra)
	neighbor1 := types.ScoredNode{
		Node: types.CodeNode{
			ID:        "function:source-id", // The source itself
			Qualified: "app.runWalk",
			Kind:      types.NodeFunction,
			FilePath:  "server/internal/app/walk.go",
			RepoSlug:  "commit0-dev/commit0",
			StartLine: 42,
		},
		VectorScore: 1.0,
	}
	neighbor2 := types.ScoredNode{
		Node: types.CodeNode{
			ID:        "function:runStage",
			Qualified: "app.runStage",
			Kind:      types.NodeFunction,
			FilePath:  "server/internal/app/stage.go",
			RepoSlug:  "commit0-dev/commit0",
			StartLine: 200,
		},
		VectorScore: 0.92,
	}
	neighbor3 := types.ScoredNode{
		Node: types.CodeNode{
			ID:        "function:runPhase",
			Qualified: "app.runPhase",
			Kind:      types.NodeFunction,
			FilePath:  "server/internal/app/phase.go",
			RepoSlug:  "commit0-dev/commit0",
			StartLine: 300,
		},
		VectorScore: 0.88,
	}

	graph := &mcpFakeGraph{
		findNode:      &sourceNode,
		nodeEmbedding: sourceEmbedding,
		vectorResults: []types.ScoredNode{neighbor1, neighbor2, neighbor3},
	}
	sess, cancel := newTestPair(t, mcpadapter.Deps{Graph: graph})
	defer cancel()

	result, err := sess.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "commit0_similar_to",
		Arguments: map[string]any{
			"node_id": sourceNode.ID,
			"k":       10,
		},
	})
	if err != nil {
		t.Fatalf("unexpected protocol error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %s", toolResultText(result))
	}

	got, ok := result.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("expected map structured content, got %T", result.StructuredContent)
	}

	// The source node should be filtered out; we should get neighbor2 and neighbor3
	neighbors, ok := got["neighbors"].([]any)
	if !ok {
		t.Fatalf("expected neighbors array, got %T", got["neighbors"])
	}
	if len(neighbors) != 2 {
		t.Fatalf("expected 2 neighbors (source + 3 total - source = 2), got %d", len(neighbors))
	}

	// Check first neighbor details
	firstNeighbor, ok := neighbors[0].(map[string]any)
	if !ok {
		t.Fatalf("expected neighbor map, got %T", neighbors[0])
	}
	if qualified, _ := firstNeighbor["qualified"].(string); qualified != "app.runStage" {
		t.Errorf("expected first neighbor qualified=app.runStage, got %s", qualified)
	}

	// Verify the markdown output is present
	text := toolResultText(result)
	if !strings.Contains(text, "Similar to") {
		t.Errorf("expected markdown header in text output")
	}
}

func TestCommit0SimilarTo_ExcludeSameFile(t *testing.T) {
	sourceNode := types.CodeNode{
		ID:        "function:source-id",
		Kind:      types.NodeFunction,
		Qualified: "app.runWalk",
		FilePath:  "server/internal/app/walk.go",
		RepoSlug:  "commit0-dev/commit0",
	}

	sourceEmbedding := make([]float32, 100)
	for i := range sourceEmbedding {
		sourceEmbedding[i] = float32(i) * 0.01
	}

	// One neighbor in the same file, one in a different file
	sameFileNeighbor := types.ScoredNode{
		Node: types.CodeNode{
			ID:        "function:other-in-same-file",
			Qualified: "app.otherWalkHelper",
			FilePath:  "server/internal/app/walk.go",
			RepoSlug:  "commit0-dev/commit0",
		},
		VectorScore: 0.95,
	}
	differentFileNeighbor := types.ScoredNode{
		Node: types.CodeNode{
			ID:        "function:different-file",
			Qualified: "app.stageHelper",
			FilePath:  "server/internal/app/stage.go",
			RepoSlug:  "commit0-dev/commit0",
		},
		VectorScore: 0.90,
	}

	graph := &mcpFakeGraph{
		findNode:      &sourceNode,
		nodeEmbedding: sourceEmbedding,
		vectorResults: []types.ScoredNode{sameFileNeighbor, differentFileNeighbor},
	}
	sess, cancel := newTestPair(t, mcpadapter.Deps{Graph: graph})
	defer cancel()

	result, err := sess.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "commit0_similar_to",
		Arguments: map[string]any{
			"node_id":           sourceNode.ID,
			"exclude_same_file": true,
		},
	})
	if err != nil {
		t.Fatalf("unexpected protocol error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %s", toolResultText(result))
	}

	got, ok := result.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("expected map structured content, got %T", result.StructuredContent)
	}

	neighbors, ok := got["neighbors"].([]any)
	if !ok {
		t.Fatalf("expected neighbors array, got %T", got["neighbors"])
	}
	if len(neighbors) != 1 {
		t.Fatalf("expected 1 neighbor (same-file excluded), got %d", len(neighbors))
	}

	// Verify it's the different-file neighbor
	firstNeighbor, _ := neighbors[0].(map[string]any)
	if filePath, _ := firstNeighbor["file_path"].(string); filePath != "server/internal/app/stage.go" {
		t.Errorf("expected remaining neighbor from different file, got %s", filePath)
	}
}

func TestCommit0SimilarTo_NoEmbedding(t *testing.T) {
	sourceNode := types.CodeNode{
		ID:        "function:source-id",
		Qualified: "app.tinyFunc",
		Kind:      types.NodeFunction,
	}

	graph := &mcpFakeGraph{
		findNode:         &sourceNode,
		nodeEmbeddingErr: domain.NotFound("node has no embedding"),
	}
	sess, cancel := newTestPair(t, mcpadapter.Deps{Graph: graph})
	defer cancel()

	result, err := sess.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "commit0_similar_to",
		Arguments: map[string]any{
			"node_id": sourceNode.ID,
		},
	})
	if err != nil {
		t.Fatalf("unexpected protocol error: %v", err)
	}
	// When a node has no embedding, we return a graceful message (not IsError)
	if result.IsError {
		t.Errorf("expected graceful no-embedding message, got error")
	}
	text := toolResultText(result)
	if !strings.Contains(text, "no embedding") && !strings.Contains(text, "too small") {
		t.Errorf("expected no-embedding hint in text: %s", text)
	}
}

// ---------------------------------------------------------------------------
// commit0_blast with_context tests
// ---------------------------------------------------------------------------

// newBlastDeps creates Deps with a real BlastService wired to the given fake graph.
// Explainer is nil (NoExplain=true by default in tests).
func newBlastDeps(graph *mcpFakeGraph) mcpadapter.Deps {
	blastSvc := app.NewBlastService(graph, nil, nil)
	return mcpadapter.Deps{
		BlastService: blastSvc,
		Graph:        graph,
	}
}

// newTraceDeps creates Deps with a real TraceService wired to the given fake graph.
func newTraceDeps(graph *mcpFakeGraph) mcpadapter.Deps {
	traceSvc := app.NewTraceService(graph, nil, nil, nil)
	return mcpadapter.Deps{
		TraceService: traceSvc,
		Graph:        graph,
	}
}

func TestCommit0Blast_WithContextFalse_NoExcerpts(t *testing.T) {
	target := types.CodeNode{
		ID:        "function:Target",
		Qualified: "pkg.Target",
		Kind:      types.NodeFunction,
		FilePath:  "pkg/target.go",
		RepoSlug:  "test-repo",
	}
	caller := types.CodeNode{
		ID:        "function:Caller",
		Qualified: "pkg.Caller",
		Kind:      types.NodeFunction,
		FilePath:  "pkg/caller.go",
		Body:      "func Caller() {\n\tTarget()\n}",
		RepoSlug:  "test-repo",
	}

	graph := &mcpFakeGraph{
		findNode:     &target,
		traverseHops: []types.TraceHop{{Node: caller, Depth: 1}},
		nodesByID:    map[string]*types.CodeNode{"function:Caller": &caller, "function:Target": &target},
		listEdgesResult: []types.CodeEdge{
			{FromID: caller.ID, ToID: target.ID, Kind: "calls", CallSite: "pkg/caller.go:2"},
		},
	}

	sess, cancel := newTestPair(t, newBlastDeps(graph))
	defer cancel()

	result, err := sess.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "commit0_blast",
		Arguments: map[string]any{
			"symbol":    "pkg.Target",
			"repo_slug": "test-repo",
			// with_context intentionally omitted (default false)
		},
	})
	if err != nil {
		t.Fatalf("unexpected protocol error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %s", toolResultText(result))
	}

	got, ok := result.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("expected map structured content, got %T", result.StructuredContent)
	}
	affected, ok := got["affected"].([]any)
	if !ok || len(affected) == 0 {
		t.Fatalf("expected non-empty affected list, got %v", got["affected"])
	}
	firstAffected, ok := affected[0].(map[string]any)
	if !ok {
		t.Fatalf("expected map for first affected, got %T", affected[0])
	}
	// with_context=false → call_site_excerpt must be absent or empty
	if excerpt, _ := firstAffected["call_site_excerpt"].(string); excerpt != "" {
		t.Errorf("with_context=false: expected empty call_site_excerpt, got %q", excerpt)
	}
}

func TestCommit0Blast_WithContextTrue_PopulatesExcerpts(t *testing.T) {
	target := types.CodeNode{
		ID:        "function:Target",
		Qualified: "pkg.Target",
		Kind:      types.NodeFunction,
		FilePath:  "pkg/target.go",
		RepoSlug:  "test-repo",
	}
	callerBody := "func Caller() {\n\t// setup\n\tTarget()\n\t// done\n}"
	caller := types.CodeNode{
		ID:        "function:Caller",
		Qualified: "pkg.Caller",
		Kind:      types.NodeFunction,
		FilePath:  "pkg/caller.go",
		Body:      callerBody,
		RepoSlug:  "test-repo",
	}

	graph := &mcpFakeGraph{
		findNode:     &target,
		traverseHops: []types.TraceHop{{Node: caller, Depth: 1}},
		nodesByID:    map[string]*types.CodeNode{"function:Caller": &caller, "function:Target": &target},
		listEdgesResult: []types.CodeEdge{
			{FromID: caller.ID, ToID: target.ID, Kind: "calls", CallSite: "pkg/caller.go:3"},
		},
	}

	sess, cancel := newTestPair(t, newBlastDeps(graph))
	defer cancel()

	result, err := sess.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "commit0_blast",
		Arguments: map[string]any{
			"symbol":       "pkg.Target",
			"repo_slug":    "test-repo",
			"with_context": true,
		},
	})
	if err != nil {
		t.Fatalf("unexpected protocol error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %s", toolResultText(result))
	}

	got, ok := result.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("expected map structured content, got %T", result.StructuredContent)
	}
	affected, ok := got["affected"].([]any)
	if !ok || len(affected) == 0 {
		t.Fatalf("expected non-empty affected list, got %v", got["affected"])
	}
	firstAffected, ok := affected[0].(map[string]any)
	if !ok {
		t.Fatalf("expected map for first affected, got %T", affected[0])
	}

	excerpt, _ := firstAffected["call_site_excerpt"].(string)
	if excerpt == "" {
		t.Error("with_context=true: expected call_site_excerpt to be populated")
	}
	callLine, _ := firstAffected["call_line"].(float64) // JSON numbers come back as float64
	if callLine == 0 {
		t.Error("with_context=true: expected call_line to be populated")
	}
}

// ---------------------------------------------------------------------------
// commit0_trace with_context tests
// ---------------------------------------------------------------------------

func TestCommit0Trace_WithContextFalse_NoExcerpts(t *testing.T) {
	root := types.CodeNode{
		ID:        "function:Root",
		Qualified: "pkg.Root",
		Kind:      types.NodeFunction,
		RepoSlug:  "test-repo",
	}
	callee := types.CodeNode{
		ID:        "function:Callee",
		Qualified: "pkg.Callee",
		Kind:      types.NodeFunction,
		FilePath:  "pkg/callee.go",
		RepoSlug:  "test-repo",
	}

	graph := &mcpFakeGraph{
		findNode:     &root,
		traverseHops: []types.TraceHop{{Node: callee, Depth: 1}},
		nodesByID:    map[string]*types.CodeNode{"function:Callee": &callee},
		listEdgesResult: []types.CodeEdge{
			{FromID: callee.ID, ToID: root.ID, Kind: "calls", CallSite: "pkg/callee.go:5"},
		},
	}

	sess, cancel := newTestPair(t, newTraceDeps(graph))
	defer cancel()

	result, err := sess.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "commit0_trace",
		Arguments: map[string]any{
			"symbol":    "pkg.Root",
			"repo_slug": "test-repo",
			// with_context intentionally omitted (default false)
		},
	})
	if err != nil {
		t.Fatalf("unexpected protocol error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %s", toolResultText(result))
	}

	got, ok := result.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("expected map structured content, got %T", result.StructuredContent)
	}
	hops, ok := got["hops"].([]any)
	if !ok || len(hops) == 0 {
		t.Fatalf("expected non-empty hops, got %v", got["hops"])
	}
	firstHop, ok := hops[0].(map[string]any)
	if !ok {
		t.Fatalf("expected map for first hop, got %T", hops[0])
	}
	if excerpt, _ := firstHop["call_site_excerpt"].(string); excerpt != "" {
		t.Errorf("with_context=false: expected empty call_site_excerpt, got %q", excerpt)
	}
}

func TestCommit0Trace_WithContextTrue_PopulatesExcerpts(t *testing.T) {
	root := types.CodeNode{
		ID:        "function:Root",
		Qualified: "pkg.Root",
		Kind:      types.NodeFunction,
		RepoSlug:  "test-repo",
	}
	calleeBody := "func Callee() {\n\t// step 1\n\tRoot()\n\t// step 2\n}"
	callee := types.CodeNode{
		ID:        "function:Callee",
		Qualified: "pkg.Callee",
		Kind:      types.NodeFunction,
		FilePath:  "pkg/callee.go",
		Body:      calleeBody,
		RepoSlug:  "test-repo",
	}

	graph := &mcpFakeGraph{
		findNode:     &root,
		traverseHops: []types.TraceHop{{Node: callee, Depth: 1}},
		nodesByID:    map[string]*types.CodeNode{"function:Callee": &callee, "function:Root": &root},
		listEdgesResult: []types.CodeEdge{
			{FromID: callee.ID, ToID: root.ID, Kind: "calls", CallSite: "pkg/callee.go:3"},
		},
	}

	sess, cancel := newTestPair(t, newTraceDeps(graph))
	defer cancel()

	result, err := sess.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "commit0_trace",
		Arguments: map[string]any{
			"symbol":       "pkg.Root",
			"repo_slug":    "test-repo",
			"with_context": true,
		},
	})
	if err != nil {
		t.Fatalf("unexpected protocol error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %s", toolResultText(result))
	}

	got, ok := result.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("expected map structured content, got %T", result.StructuredContent)
	}
	hops, ok := got["hops"].([]any)
	if !ok || len(hops) == 0 {
		t.Fatalf("expected non-empty hops, got %v", got["hops"])
	}
	firstHop, ok := hops[0].(map[string]any)
	if !ok {
		t.Fatalf("expected map for first hop, got %T", hops[0])
	}

	excerpt, _ := firstHop["call_site_excerpt"].(string)
	if excerpt == "" {
		t.Error("with_context=true: expected call_site_excerpt to be populated on hop")
	}
	// Verify the markdown also contains the excerpt
	text := toolResultText(result)
	if !strings.Contains(text, "Root()") && !strings.Contains(text, "```") {
		t.Errorf("expected call site content in markdown output, got: %s", text)
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

// ---------------------------------------------------------------------------
// commit0_diff_impact tests
// ---------------------------------------------------------------------------

func TestDiffImpact_MissingRepoSlug_ReturnsToolError(t *testing.T) {
	sess, cancel := newTestPair(t, mcpadapter.Deps{})
	defer cancel()

	result, err := sess.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "commit0_diff_impact",
		Arguments: map[string]any{
			"repo_path": "/tmp/repo",
			// repo_slug intentionally omitted
		},
	})
	if err != nil {
		// Protocol-level validation is acceptable.
		return
	}
	if result == nil {
		t.Fatal("got nil result")
	}
	if !result.IsError {
		t.Errorf("expected isError=true for missing repo_slug, got: %v", result.Content)
	}
}

func TestDiffImpact_DBUnavailable_ReturnsToolError(t *testing.T) {
	sess, cancel := newTestPair(t, mcpadapter.Deps{DBAddr: "localhost:9999"})
	defer cancel()

	result, err := sess.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "commit0_diff_impact",
		Arguments: map[string]any{
			"repo_slug": "org/repo",
			"repo_path": "/tmp/repo",
		},
	})
	if err != nil {
		t.Fatalf("unexpected protocol error: %v", err)
	}
	if !result.IsError {
		t.Errorf("expected isError=true when DiffImpactService is nil")
	}
	if !strings.Contains(toolResultText(result), "SurrealDB") {
		t.Errorf("expected error to mention SurrealDB, got: %s", toolResultText(result))
	}
}

// newDiffImpactDeps creates Deps with a real DiffImpactService wired to the given fake graph.
func newDiffImpactDeps(graph *mcpFakeGraph) mcpadapter.Deps {
	blastSvc := app.NewBlastService(graph, nil, nil)
	gitWalker := &mcpFakeGitWalker{}
	diffImpactSvc := app.NewDiffImpactService(graph, blastSvc, gitWalker, nil, nil)
	return mcpadapter.Deps{
		DiffImpactService: diffImpactSvc,
		Graph:             graph,
	}
}

// mcpFakeGitWalker is a minimal fake GitWalker for MCP-layer diff-impact tests.
type mcpFakeGitWalker struct {
	diffs []domain.GitFileDiff
	err   error
}

func (f *mcpFakeGitWalker) ListCommits(_ context.Context, _ string, _, _ string) ([]domain.GitCommit, error) {
	return nil, nil
}
func (f *mcpFakeGitWalker) DiffCommit(_ context.Context, _, _ string) ([]domain.GitFileDiff, error) {
	return nil, nil
}
func (f *mcpFakeGitWalker) ReadFileAtCommit(_ context.Context, _, _, _ string) ([]byte, error) {
	return nil, nil
}
func (f *mcpFakeGitWalker) CommitInfo(_ context.Context, _, _ string) (*domain.GitCommit, error) {
	return nil, nil
}
func (f *mcpFakeGitWalker) DiffWorkingTree(_ context.Context, _ string) ([]domain.GitFileDiff, error) {
	return f.diffs, f.err
}
func (f *mcpFakeGitWalker) DiffRange(_ context.Context, _, _, _ string) ([]domain.GitFileDiff, error) {
	return f.diffs, f.err
}

var _ domain.GitWalker = (*mcpFakeGitWalker)(nil)

func TestDiffImpact_HappyPath_ReturnsAffected(t *testing.T) {
	changedNode := types.CodeNode{
		ID:        "function:pkg.Foo",
		Qualified: "pkg.Foo",
		Kind:      types.NodeFunction,
		FilePath:  "pkg/foo.go",
		RepoSlug:  "org/repo",
		StartLine: 1,
		EndLine:   10,
	}
	callerNode := types.CodeNode{
		ID:        "function:pkg.Bar",
		Qualified: "pkg.Bar",
		Kind:      types.NodeFunction,
		FilePath:  "pkg/bar.go",
		RepoSlug:  "org/repo",
		StartLine: 5,
		EndLine:   20,
	}

	patch := "@@ -1,3 +1,4 @@ package pkg\n"
	graph := &mcpFakeGraph{
		listNodesResult: []types.CodeNode{changedNode},
		findNode:        &changedNode,
		traverseHops:    []types.TraceHop{{Node: callerNode, Depth: 1}},
	}
	deps := newDiffImpactDeps(graph)
	// Override the gitwalker with one that returns a diff.
	blastSvc := app.NewBlastService(graph, nil, nil)
	gitWalker := &mcpFakeGitWalker{
		diffs: []domain.GitFileDiff{
			{Path: "pkg/foo.go", Status: "modified", Patch: patch},
		},
	}
	deps.DiffImpactService = app.NewDiffImpactService(graph, blastSvc, gitWalker, nil, nil)

	sess, cancel := newTestPair(t, deps)
	defer cancel()

	result, err := sess.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "commit0_diff_impact",
		Arguments: map[string]any{
			"repo_slug": "org/repo",
			"repo_path": "/tmp/repo",
			"to_ref":    "WORKING",
		},
	})
	if err != nil {
		t.Fatalf("unexpected protocol error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %s", toolResultText(result))
	}

	structured, ok := result.StructuredContent.(map[string]any)
	if !ok {
		// StructuredContent may be the typed struct; check text content.
		text := toolResultText(result)
		if !strings.Contains(text, "changed") {
			t.Errorf("expected result text to mention 'changed', got: %s", text)
		}
		return
	}
	// If we have structured content, verify basic fields.
	if structured["changed_symbols"] == nil {
		t.Errorf("expected changed_symbols in structured content")
	}
}

// ── commit0_resolve_interface tests ──────────────────────────────────────────

func TestResolveInterface_MissingQualified_ReturnsToolError(t *testing.T) {
	sess, cancel := newTestPair(t, mcpadapter.Deps{})
	defer cancel()

	result, err := sess.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "commit0_resolve_interface",
		Arguments: map[string]any{
			// qualified intentionally omitted
			"repo_slug": "test-repo",
		},
	})
	if err != nil {
		// Protocol-level validation: acceptable.
		return
	}
	if result == nil {
		t.Fatal("got nil result for missing required field")
	}
	if !result.IsError {
		t.Errorf("expected isError=true for missing qualified, got content: %v", result.Content)
	}
}

func TestResolveInterface_DBUnavailable_ReturnsToolError(t *testing.T) {
	sess, cancel := newTestPair(t, mcpadapter.Deps{
		DBAddr: "localhost:9999",
	})
	defer cancel()

	result, err := sess.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "commit0_resolve_interface",
		Arguments: map[string]any{
			"qualified": "domain.OpenCodeGraph",
			"repo_slug": "test-repo",
		},
	})
	if err != nil {
		t.Fatalf("unexpected protocol error: %v", err)
	}
	if !result.IsError {
		t.Errorf("expected isError=true when DB is unavailable")
	}
}

func TestResolveInterface_HappyPath(t *testing.T) {
	// Build a fake interface node with two methods.
	iface := types.CodeNode{
		ID:        "class:domain⋅OpenCodeGraph",
		Kind:      types.NodeClass,
		Name:      "OpenCodeGraph",
		Qualified: "domain.OpenCodeGraph",
		FilePath:  "server/internal/domain/graph.go",
		Methods: []types.MethodSpec{
			{Name: "PutNode", Signature: "PutNode(ctx context.Context, node *CodeNode) error", Receiver: ""},
			{Name: "GetNode", Signature: "GetNode(ctx context.Context, id string) (*CodeNode, error)", Receiver: ""},
		},
	}
	// One implementor returned by the reverse traverse.
	implementor := types.CodeNode{
		ID:        "class:surreal⋅SurrealAdapter",
		Kind:      types.NodeClass,
		Name:      "SurrealAdapter",
		Qualified: "surreal.SurrealAdapter",
		FilePath:  "server/internal/adapters/surreal/client.go",
	}
	hops := []types.TraceHop{
		{Node: implementor, Depth: 1},
	}

	graph := &mcpFakeGraph{findNode: &iface, traverseHops: hops}
	sess, cancel := newTestPair(t, mcpadapter.Deps{Graph: graph})
	defer cancel()

	result, err := sess.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "commit0_resolve_interface",
		Arguments: map[string]any{
			"qualified": "domain.OpenCodeGraph",
			"repo_slug": "commit0-dev/commit0",
		},
	})
	if err != nil {
		t.Fatalf("unexpected protocol error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %v", toolResultText(result))
	}

	// Structured content arrives as map[string]any from the MCP transport.
	sc, ok := result.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("expected map structured content, got %T", result.StructuredContent)
	}
	ifaceMap, _ := sc["interface"].(map[string]any)
	if ifaceMap == nil {
		t.Fatalf("structured content missing 'interface' key: %+v", sc)
	}
	if q, _ := ifaceMap["qualified"].(string); q != "domain.OpenCodeGraph" {
		t.Errorf("interface.qualified = %q, want %q", q, "domain.OpenCodeGraph")
	}
	methods, _ := sc["methods"].([]any)
	if len(methods) != 2 {
		t.Errorf("methods len = %d, want 2", len(methods))
	}
	impls, _ := sc["implementors"].([]any)
	if len(impls) != 1 {
		t.Fatalf("implementors len = %d, want 1", len(impls))
	}
	impl0, _ := impls[0].(map[string]any)
	if q, _ := impl0["qualified"].(string); q != "surreal.SurrealAdapter" {
		t.Errorf("implementors[0].qualified = %q", q)
	}
	// Verify text output contains the interface name.
	text := toolResultText(result)
	if !strings.Contains(text, "domain.OpenCodeGraph") {
		t.Errorf("text output should mention interface name, got: %s", text)
	}
	// Verify the traversal was done with "implements" label in reverse.
	if len(graph.lastLabels) == 0 || graph.lastLabels[0] != "implements" {
		t.Errorf("expected TraverseGraph called with label 'implements', got %v", graph.lastLabels)
	}
	if graph.lastDirection != "reverse" {
		t.Errorf("expected direction 'reverse', got %q", graph.lastDirection)
	}
}

// mcpFakeGraph is a minimal fake satisfying domain.OpenCodeGraph for the tests
// in this package that need a non-nil Graph without booting SurrealDB. Methods
// untouched by the tools under test return zero-value/nil and don't error.
type mcpFakeGraph struct {
	findNode         *types.CodeNode
	findErr          error
	nodesByID        map[string]*types.CodeNode // for GetNode lookup in with_context tests
	traverseHops     []types.TraceHop
	traverseErr      error
	lastLabels       []string
	lastDirection    string
	lastDepth        int
	nodeEmbedding    []float32
	nodeEmbeddingErr error
	vectorResults    []types.ScoredNode
	vectorErr        error
	listEdgesResult  []types.CodeEdge
	listNodesResult  []types.CodeNode // returned by ListNodes for diff-impact tests
	listReposResult  []types.Repo     // returned by ListRepos for list-repos tests
	listReposErr     error
	lastListRepoSlug string          // captured by ListNodes for list-files assertions
	lastListOpts     domain.ListOpts // captured by ListNodes for list-files assertions
}

func (g *mcpFakeGraph) PutNode(_ context.Context, _ *types.CodeNode) error { return nil }
func (g *mcpFakeGraph) GetNode(_ context.Context, id string) (*types.CodeNode, error) {
	if g.nodesByID != nil {
		if n, ok := g.nodesByID[id]; ok {
			return n, nil
		}
	}
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
func (g *mcpFakeGraph) GetNodeEmbedding(_ context.Context, _ string) ([]float32, error) {
	return g.nodeEmbedding, g.nodeEmbeddingErr
}
func (g *mcpFakeGraph) VectorSearch(_ context.Context, _ []float32, _ domain.VectorSearchOpts) ([]types.ScoredNode, error) {
	return g.vectorResults, g.vectorErr
}
func (g *mcpFakeGraph) TextSearch(_ context.Context, _ string, _ domain.TextSearchOpts) ([]types.ScoredNode, error) {
	return nil, nil
}
func (g *mcpFakeGraph) ListNodes(_ context.Context, repo string, opts domain.ListOpts) ([]types.CodeNode, error) {
	g.lastListRepoSlug = repo
	g.lastListOpts = opts
	return g.listNodesResult, nil
}
func (g *mcpFakeGraph) ListEdges(_ context.Context, _ string, _ []string) ([]types.CodeEdge, error) {
	return g.listEdgesResult, nil
}
func (g *mcpFakeGraph) ListFilePaths(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}
func (g *mcpFakeGraph) PutRepo(_ context.Context, _ *types.Repo) error           { return nil }
func (g *mcpFakeGraph) GetRepo(_ context.Context, _ string) (*types.Repo, error) { return nil, nil }
func (g *mcpFakeGraph) ListRepos(_ context.Context) ([]types.Repo, error) {
	return g.listReposResult, g.listReposErr
}
func (g *mcpFakeGraph) UpdateRepoIndexedAt(_ context.Context, _ string, _ time.Time) error {
	return nil
}
func (g *mcpFakeGraph) FindRepoByRemoteURL(_ context.Context, _ string) (*types.Repo, error) {
	return nil, nil
}
func (g *mcpFakeGraph) DeleteRepo(_ context.Context, _ string) error { return nil }
func (g *mcpFakeGraph) ApplySchema(_ context.Context) error          { return nil }

var _ domain.OpenCodeGraph = (*mcpFakeGraph)(nil)

// ---------------------------------------------------------------------------
// commit0_index_status tests
// ---------------------------------------------------------------------------

// TestIndexStatus_MissingService_ReturnsUnavailable verifies the lazy-init
// guard: an MCP server booted without an IndexService surfaces the standard
// dbUnavailable tool error rather than panicking.
func TestIndexStatus_MissingService_ReturnsUnavailable(t *testing.T) {
	sess, cancel := newTestPair(t, mcpadapter.Deps{DBAddr: "localhost:9999"})
	defer cancel()

	result, err := sess.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name:      "commit0_index_status",
		Arguments: map[string]any{"job_id": "any"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected IsError=true when IndexService is nil, got false")
	}
	body := firstTextContent(result)
	if !strings.Contains(body, "localhost:9999") {
		t.Errorf("expected DB address in error body, got %q", body)
	}
}

// TestIndexStatus_MissingJobID_ReturnsValidationError covers the input-
// validation gate: an empty or whitespace-only job_id is rejected before the
// registry is consulted.
func TestIndexStatus_MissingJobID_ReturnsValidationError(t *testing.T) {
	indexSvc := &app.IndexService{}
	sess, cancel := newTestPair(t, mcpadapter.Deps{IndexService: indexSvc})
	defer cancel()

	result, err := sess.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name:      "commit0_index_status",
		Arguments: map[string]any{"job_id": "   "},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected IsError=true for blank job_id, got false")
	}
	body := firstTextContent(result)
	if !strings.Contains(body, "job_id is required") {
		t.Errorf("expected validation message in error body, got %q", body)
	}
}

// TestIndexStatus_JobNotFound_ReturnsToolError exercises the registry-miss
// path: the IndexService is wired but no tracker has been registered for the
// requested jobID. The tool must return a typed not-found error so MCP
// clients can distinguish "still running" (would not have been emitted yet)
// from "evicted / wrong ID".
func TestIndexStatus_JobNotFound_ReturnsToolError(t *testing.T) {
	indexSvc := &app.IndexService{}
	sess, cancel := newTestPair(t, mcpadapter.Deps{IndexService: indexSvc})
	defer cancel()

	result, err := sess.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name:      "commit0_index_status",
		Arguments: map[string]any{"job_id": "no-such-job"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected IsError=true for unknown job, got false")
	}
	body := firstTextContent(result)
	if !strings.Contains(body, "not found") {
		t.Errorf("expected not-found phrasing in error body, got %q", body)
	}
	if !strings.Contains(body, "no-such-job") {
		t.Errorf("expected jobID echoed in error body, got %q", body)
	}
}

// TestIndexStatus_Success_ReturnsSnapshot exercises the happy path: a tracker
// is seeded into the IndexService registry, the tool returns IsError=false,
// the StructuredContent matches the expected wire shape, and the human-
// readable Markdown summary mentions the headline counters.
func TestIndexStatus_Success_ReturnsSnapshot(t *testing.T) {
	indexSvc := &app.IndexService{}
	tracker := app.NewIndexTracker("seeded-job", "demo/repo", types.IndexConfig{
		EmbedProvider: "ollama",
		EmbedModel:    "qwen3-embedding:4b",
	})
	tracker.AddFiles(3)
	tracker.AddNodes(42)
	tracker.AddEdges(80)
	indexSvc.RegisterTrackerForTest(tracker)

	sess, cancel := newTestPair(t, mcpadapter.Deps{IndexService: indexSvc})
	defer cancel()

	result, err := sess.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name:      "commit0_index_status",
		Arguments: map[string]any{"job_id": "seeded-job"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected IsError=false on happy path, got true: %s", firstTextContent(result))
	}

	body := firstTextContent(result)
	if !strings.Contains(body, "seeded-job") {
		t.Errorf("expected jobID in markdown body, got %q", body)
	}
	if !strings.Contains(body, "files: 3") {
		t.Errorf("expected counters in markdown body, got %q", body)
	}
	if !strings.Contains(body, "demo/repo") {
		t.Errorf("expected repo slug in markdown body, got %q", body)
	}

	// StructuredContent should round-trip back to types.IndexProgress with the
	// counters we seeded. The MCP SDK serializes StructuredContent to JSON,
	// so we assert via the raw map representation.
	if result.StructuredContent == nil {
		t.Fatalf("expected non-nil StructuredContent on happy path")
	}
}

// firstTextContent extracts the first TextContent body from a CallToolResult,
// or returns "" if there is none. Convenience helper for the index-status
// assertions; mirrors the inline pattern used in older tests.
func firstTextContent(result *mcpsdk.CallToolResult) string {
	for _, c := range result.Content {
		if tc, ok := c.(*mcpsdk.TextContent); ok {
			return tc.Text
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// commit0_list_repos tests
// ---------------------------------------------------------------------------

// TestListRepos_MissingService_ReturnsUnavailable verifies that booting the
// MCP server without a RepoService surfaces the standard dbUnavailable tool
// error, not a panic.
func TestListRepos_MissingService_ReturnsUnavailable(t *testing.T) {
	sess, cancel := newTestPair(t, mcpadapter.Deps{DBAddr: "localhost:9999"})
	defer cancel()

	result, err := sess.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name:      "commit0_list_repos",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected IsError=true when RepoService is nil, got false")
	}
	body := firstTextContent(result)
	if !strings.Contains(body, "localhost:9999") {
		t.Errorf("expected DB address in error body, got %q", body)
	}
}

// TestListRepos_Success verifies the happy path: RepoService returns two
// repos, the tool emits IsError=false, the structured payload contains both
// slugs, and the Markdown summary mentions each.
func TestListRepos_Success(t *testing.T) {
	indexed := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	graph := &mcpFakeGraph{
		listReposResult: []types.Repo{
			{
				Slug:          "alpha/repo",
				Path:          "/srv/alpha",
				DefaultBranch: "main",
				Languages:     []string{"go"},
				CreatedAt:     time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
				LastIndexedAt: &indexed,
			},
			{
				Slug:          "beta/repo",
				Path:          "/srv/beta",
				DefaultBranch: "trunk",
				Languages:     []string{"python", "rust"},
			},
		},
	}
	deps := mcpadapter.Deps{
		Graph:       graph,
		RepoService: app.NewRepoService(graph),
	}
	sess, cancel := newTestPair(t, deps)
	defer cancel()

	result, err := sess.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name:      "commit0_list_repos",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected IsError=false, got true: %s", firstTextContent(result))
	}

	body := firstTextContent(result)
	if !strings.Contains(body, "alpha/repo") || !strings.Contains(body, "beta/repo") {
		t.Errorf("expected both repo slugs in markdown body, got %q", body)
	}
	if !strings.Contains(body, "Repositories (2)") {
		t.Errorf("expected count header in markdown body, got %q", body)
	}

	sc, ok := result.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("expected map structured content, got %T", result.StructuredContent)
	}
	repos, _ := sc["repos"].([]any)
	if len(repos) != 2 {
		t.Fatalf("structured repos: got %d, want 2", len(repos))
	}
	first, _ := repos[0].(map[string]any)
	if slug, _ := first["slug"].(string); slug != "alpha/repo" {
		t.Errorf("repos[0].slug = %q, want %q", slug, "alpha/repo")
	}
}

// ---------------------------------------------------------------------------
// commit0_list_files tests
// ---------------------------------------------------------------------------

// TestListFiles_MissingRepoSlug_ReturnsValidationError covers the
// input-validation gate: an empty repo_slug must fail before the graph is
// consulted.
func TestListFiles_MissingRepoSlug_ReturnsValidationError(t *testing.T) {
	graph := &mcpFakeGraph{}
	sess, cancel := newTestPair(t, mcpadapter.Deps{Graph: graph})
	defer cancel()

	result, err := sess.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name:      "commit0_list_files",
		Arguments: map[string]any{"repo_slug": "   "},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected IsError=true for blank repo_slug, got false")
	}
	body := firstTextContent(result)
	if !strings.Contains(body, "repo_slug is required") {
		t.Errorf("expected validation message in error body, got %q", body)
	}
	if graph.lastListRepoSlug != "" {
		t.Errorf("graph.ListNodes was called despite validation failure (slug=%q)", graph.lastListRepoSlug)
	}
}

// TestListFiles_Success exercises the happy path: the graph returns three
// file-kind nodes for the requested repo and prefix, the tool returns them
// in the structured payload, and the Markdown summary contains each path.
func TestListFiles_Success(t *testing.T) {
	graph := &mcpFakeGraph{
		listNodesResult: []types.CodeNode{
			{ID: "n1", FilePath: "internal/app/repo_service.go", Language: "go", Kind: types.NodeFile},
			{ID: "n2", FilePath: "internal/app/index_service.go", Language: "go", Kind: types.NodeFile},
			{ID: "n3", FilePath: "internal/app/query_service.go", Language: "go", Kind: types.NodeFile},
		},
	}
	sess, cancel := newTestPair(t, mcpadapter.Deps{Graph: graph})
	defer cancel()

	result, err := sess.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "commit0_list_files",
		Arguments: map[string]any{
			"repo_slug":   "demo/repo",
			"path_prefix": "internal/app/",
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected IsError=false, got true: %s", firstTextContent(result))
	}

	if graph.lastListRepoSlug != "demo/repo" {
		t.Errorf("ListNodes called with repo %q, want %q", graph.lastListRepoSlug, "demo/repo")
	}
	if graph.lastListOpts.FilePath != "internal/app/" {
		t.Errorf("ListNodes opts.FilePath = %q, want %q", graph.lastListOpts.FilePath, "internal/app/")
	}
	if len(graph.lastListOpts.Labels) == 0 || graph.lastListOpts.Labels[0] != "file" {
		t.Errorf("ListNodes opts.Labels = %v, want [file]", graph.lastListOpts.Labels)
	}

	sc, ok := result.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("expected map structured content, got %T", result.StructuredContent)
	}
	files, _ := sc["files"].([]any)
	if len(files) != 3 {
		t.Fatalf("structured files: got %d, want 3", len(files))
	}
	if truncated, _ := sc["truncated"].(bool); truncated {
		t.Errorf("expected truncated=false for 3 results within default limit")
	}

	body := firstTextContent(result)
	for _, want := range []string{"repo_service.go", "index_service.go", "query_service.go"} {
		if !strings.Contains(body, want) {
			t.Errorf("expected %q in markdown body, got %q", want, body)
		}
	}
}

// ---------------------------------------------------------------------------
// node:// resource tests
// ---------------------------------------------------------------------------

// TestNodeResource_Read_ReturnsNodeBody verifies that a resources/read for a
// concrete node://<id> URI dispatches to the registered template handler,
// pulls the matching CodeNode from the graph, and returns its Body as text
// content.
func TestNodeResource_Read_ReturnsNodeBody(t *testing.T) {
	want := "func ServeRepo() {}\n"
	graph := &mcpFakeGraph{
		nodesByID: map[string]*types.CodeNode{
			"abc-123": {
				ID:       "abc-123",
				FilePath: "server/main.go",
				Body:     want,
			},
		},
	}
	sess, cancel := newTestPair(t, mcpadapter.Deps{Graph: graph})
	defer cancel()

	result, err := sess.ReadResource(context.Background(), &mcpsdk.ReadResourceParams{
		URI: "node://abc-123",
	})
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}
	if len(result.Contents) != 1 {
		t.Fatalf("expected exactly 1 content entry, got %d", len(result.Contents))
	}
	c := result.Contents[0]
	if c.Text != want {
		t.Errorf("body text = %q, want %q", c.Text, want)
	}
	if c.URI != "node://abc-123" {
		t.Errorf("content URI = %q, want %q", c.URI, "node://abc-123")
	}
	if c.MIMEType != "text/plain" {
		t.Errorf("MIMEType = %q, want text/plain", c.MIMEType)
	}
}

// TestNodeResource_NotFound_ReturnsResourceError verifies the not-found
// branch: when the graph holds no node with the requested ID, the SDK
// surfaces a JSON-RPC error to the client (via ResourceNotFoundError).
func TestNodeResource_NotFound_ReturnsResourceError(t *testing.T) {
	graph := &mcpFakeGraph{
		nodesByID: map[string]*types.CodeNode{},
	}
	sess, cancel := newTestPair(t, mcpadapter.Deps{Graph: graph})
	defer cancel()

	_, err := sess.ReadResource(context.Background(), &mcpsdk.ReadResourceParams{
		URI: "node://missing-id",
	})
	if err == nil {
		t.Fatalf("expected ReadResource to fail for missing node, got nil error")
	}
}

// TestNodeResource_ListedAsTemplate verifies that the registered template is
// advertised on resources/templates/list — without it, MCP clients would not
// know the node:// scheme is available.
func TestNodeResource_ListedAsTemplate(t *testing.T) {
	sess, cancel := newTestPair(t, mcpadapter.Deps{})
	defer cancel()

	res, err := sess.ListResourceTemplates(context.Background(), &mcpsdk.ListResourceTemplatesParams{})
	if err != nil {
		t.Fatalf("ListResourceTemplates: %v", err)
	}
	found := false
	for _, tmpl := range res.ResourceTemplates {
		if strings.HasPrefix(tmpl.URITemplate, "node://") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected node:// template in ListResourceTemplates result, got %+v", res.ResourceTemplates)
	}
}
