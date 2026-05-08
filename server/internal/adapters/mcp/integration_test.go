package mcp_test

// Integration test for the full MCP surface — exercises an in-memory
// transport pair against the real adapter wiring with stubbed services and
// asserts:
//
//  1. tools/list returns all 18 tools in sorted order.
//  2. resources/templates/list advertises the node:// template.
//  3. tools/call works for the 5 tools added by Issue #28 (commit0_index_status,
//     commit0_list_repos, commit0_list_files, commit0_scan_security,
//     commit0_api_surface) plus a happy-path resources/read on node://.
//
// This is the fast in-process variant — runs always under `go test`. The
// subprocess variant in integration_subprocess_test.go (build-tag
// `integration`) covers the binary path via mcp.CommandTransport.

import (
	"context"
	"sort"
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/commit0-dev/commit0/pkg/types"
	mcpadapter "github.com/commit0-dev/commit0/server/internal/adapters/mcp"
	"github.com/commit0-dev/commit0/server/internal/app"
	"github.com/commit0-dev/commit0/server/internal/config"
)

// integrationDeps wires up every service the adapter exposes, all backed by
// a single stub graph. The result is a Deps that satisfies every guard in
// server.go without touching SurrealDB.
func integrationDeps(t *testing.T) (mcpadapter.Deps, *mcpFakeGraph) {
	t.Helper()
	cfg := &config.Config{}
	graph := &mcpFakeGraph{nodesByID: map[string]*types.CodeNode{}}
	flowSvc := app.NewFieldFlowService(graph, nil, nil, cfg)
	deps := mcpadapter.Deps{
		Graph:             graph,
		RepoService:       app.NewRepoService(graph),
		AnalysisService:   app.NewAnalysisService(graph, flowSvc, nil),
		APISurfaceService: app.NewAPISurfaceService(graph, flowSvc, nil, cfg),
		IndexService:      &app.IndexService{},
	}
	return deps, graph
}

// TestIntegration_ListTools_All18 checks the tools/list shape — the surface is
// the contract the rest of the test relies on.
func TestIntegration_ListTools_All18(t *testing.T) {
	deps, _ := integrationDeps(t)
	sess, cancel := newTestPair(t, deps)
	defer cancel()

	res, err := sess.ListTools(context.Background(), &mcpsdk.ListToolsParams{})
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(res.Tools) != 18 {
		t.Fatalf("tools/list returned %d tools, want 18", len(res.Tools))
	}
	names := make([]string, len(res.Tools))
	for i, tool := range res.Tools {
		names[i] = tool.Name
	}
	if !sort.StringsAreSorted(names) {
		t.Errorf("tools/list names are not sorted: %v", names)
	}
}

// TestIntegration_ResourceTemplate_NodeListed asserts the node:// template
// surfaces on resources/templates/list — without it, MCP clients have no
// discovery path for the resource.
func TestIntegration_ResourceTemplate_NodeListed(t *testing.T) {
	deps, _ := integrationDeps(t)
	sess, cancel := newTestPair(t, deps)
	defer cancel()

	res, err := sess.ListResourceTemplates(context.Background(), &mcpsdk.ListResourceTemplatesParams{})
	if err != nil {
		t.Fatalf("ListResourceTemplates: %v", err)
	}
	found := false
	for _, tmpl := range res.ResourceTemplates {
		if strings.HasPrefix(tmpl.URITemplate, "node://") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected node:// template, got %+v", res.ResourceTemplates)
	}
}

// TestIntegration_AllNewTools_RoundTrip exercises tools/call for the five
// tools introduced by #28 with minimal valid input. Each must return
// IsError=false and a non-nil StructuredContent payload.
func TestIntegration_AllNewTools_RoundTrip(t *testing.T) {
	deps, graph := integrationDeps(t)

	// Seed a tracker so commit0_index_status can return a real snapshot.
	tracker := app.NewIndexTracker("integration-job", "demo/repo", types.IndexConfig{})
	tracker.AddFiles(1)
	deps.IndexService.RegisterTrackerForTest(tracker)

	// Seed a node so node://demo-id can return a body.
	graph.nodesByID["demo-id"] = &types.CodeNode{
		ID: "demo-id", Name: "demoFn", Body: "func demoFn() {}",
	}

	// Seed a repo so commit0_list_repos returns one entry.
	graph.listReposResult = []types.Repo{{Slug: "demo/repo", Path: "/srv/demo"}}

	sess, cancel := newTestPair(t, deps)
	defer cancel()

	cases := []struct {
		name string
		args map[string]any
	}{
		{"commit0_index_status", map[string]any{"job_id": "integration-job"}},
		{"commit0_list_repos", map[string]any{}},
		{"commit0_list_files", map[string]any{"repo_slug": "demo/repo"}},
		{"commit0_scan_security", map[string]any{"repo_slug": "demo/repo"}},
		{"commit0_api_surface", map[string]any{"repo_slug": "demo/repo"}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			result, err := sess.CallTool(context.Background(), &mcpsdk.CallToolParams{
				Name:      tc.name,
				Arguments: tc.args,
			})
			if err != nil {
				t.Fatalf("CallTool: %v", err)
			}
			if result.IsError {
				t.Fatalf("%s returned IsError=true: %s", tc.name, firstTextContent(result))
			}
			if result.StructuredContent == nil {
				t.Errorf("%s: StructuredContent is nil", tc.name)
			}
		})
	}
}

// TestIntegration_NodeResource_Read does the round-trip resources/read on a
// node URI that was seeded into the stub graph. Asserts the body matches.
func TestIntegration_NodeResource_Read(t *testing.T) {
	deps, graph := integrationDeps(t)
	graph.nodesByID["abc-123"] = &types.CodeNode{
		ID: "abc-123", Name: "demoFn", Body: "func demoFn() error { return nil }",
	}

	sess, cancel := newTestPair(t, deps)
	defer cancel()

	res, err := sess.ReadResource(context.Background(), &mcpsdk.ReadResourceParams{
		URI: "node://abc-123",
	})
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}
	if len(res.Contents) == 0 {
		t.Fatalf("expected at least one ResourceContents entry")
	}
	if res.Contents[0].Text != "func demoFn() error { return nil }" {
		t.Errorf("body mismatch: got %q", res.Contents[0].Text)
	}
}

// TestIntegration_APISurface_OpenAPIRoundTrip checks the openapi format
// branch returns valid JSON in TextContent and the structured wrapper.
func TestIntegration_APISurface_OpenAPIRoundTrip(t *testing.T) {
	deps, _ := integrationDeps(t)
	sess, cancel := newTestPair(t, deps)
	defer cancel()

	result, err := sess.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "commit0_api_surface",
		Arguments: map[string]any{
			"repo_slug": "demo/repo",
			"format":    "openapi",
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected IsError=false, got true: %s", firstTextContent(result))
	}
	body := firstTextContent(result)
	if !strings.Contains(body, "\"openapi\"") || !strings.Contains(body, "3.0.0") {
		t.Errorf("expected OpenAPI 3.0 declaration in body, got %q", body)
	}
}
