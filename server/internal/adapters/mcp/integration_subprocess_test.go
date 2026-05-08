//go:build integration

// Subprocess integration test: spawns the compiled `commit0 mcp` binary and
// drives it over the go-sdk's CommandTransport. This catches binary-level
// regressions (cobra wiring, init order, env handling) that the in-memory
// integration_test.go cannot.
//
// Runs only with the `integration` build tag:
//
//   cd server && make build-server && go test -tags integration -timeout=120s ./internal/adapters/mcp/...
//
// CI runs both this and the in-memory variant. Locally, skip if the binary
// hasn't been built — t.Skip avoids breaking developers who only run
// `go test ./...`.

package mcp_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// commit0BinaryPath returns the absolute path to ./bin/commit0 relative to
// the server submodule, which is where `make build-server` writes the
// binary. Returns "" if the binary is not present.
func commit0BinaryPath(t *testing.T) string {
	t.Helper()
	// server/internal/adapters/mcp → ../../../../bin/commit0
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	candidate := filepath.Join(wd, "..", "..", "..", "..", "bin", "commit0")
	abs, err := filepath.Abs(candidate)
	if err != nil {
		t.Fatalf("filepath.Abs: %v", err)
	}
	if _, err := os.Stat(abs); err != nil {
		return ""
	}
	return abs
}

// TestSubprocess_ToolsList_All18 — drives the real binary and confirms the
// 18-tool surface is exposed over CommandTransport just like over stdio.
func TestSubprocess_ToolsList_All18(t *testing.T) {
	binary := commit0BinaryPath(t)
	if binary == "" {
		t.Skip("commit0 binary not found — run `make build-server` first")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	transport := &mcpsdk.CommandTransport{
		Command: exec.CommandContext(ctx, binary, "mcp"),
	}
	client := mcpsdk.NewClient(&mcpsdk.Implementation{
		Name:    "subprocess-test",
		Version: "0.0.1",
	}, nil)
	sess, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("CommandTransport connect: %v", err)
	}
	defer func() { _ = sess.Close() }()

	res, err := sess.ListTools(ctx, &mcpsdk.ListToolsParams{})
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(res.Tools) != 18 {
		t.Fatalf("subprocess tools/list returned %d, want 18", len(res.Tools))
	}
	names := make([]string, len(res.Tools))
	for i, tool := range res.Tools {
		names[i] = tool.Name
	}
	if !sort.StringsAreSorted(names) {
		t.Errorf("subprocess tool names not sorted: %v", names)
	}

	// Sanity-call one of the new tools — DB unavailability is fine here, we
	// just want to prove the binary's wiring reaches the handler.
	callRes, err := sess.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "commit0_list_repos",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("subprocess CallTool: %v", err)
	}
	if callRes == nil {
		t.Fatalf("subprocess CallTool returned nil result")
	}
}
