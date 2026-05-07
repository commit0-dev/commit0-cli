package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sort"

	"github.com/spf13/cobra"

	mcpadapter "github.com/commit0-dev/commit0/server/internal/adapters/mcp"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/commit0-dev/commit0/server/internal/config"
)

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Start the commit0 MCP server on stdio",
	Long: `Start the commit0 MCP (Model Context Protocol) server using the stdio transport.

The server exposes commit0's code intelligence as MCP tools, accessible to
Claude Code, Cursor, Cline, and any other MCP-aware client.

Add to Claude Code:
  claude mcp add --scope user --transport stdio commit0 -- commit0 mcp

Or add .mcp.json to your project:
  {
    "mcpServers": {
      "commit0": { "type": "stdio", "command": "commit0", "args": ["mcp"] }
    }
  }

Self-test (verify the server works):
  commit0 mcp --self-test`,
	RunE: func(cmd *cobra.Command, args []string) error {
		selfTest, _ := cmd.Flags().GetBool("self-test")

		cfg, err := config.Load(configPath(cmd))
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}

		if selfTest {
			return runSelfTest(cmd.Context(), cfg)
		}

		return runMCPServer(cmd.Context(), cfg)
	},
}

func init() {
	rootCmd.AddCommand(mcpCmd)
	mcpCmd.Flags().Bool("self-test", false, "Run an in-process self-test (initialize → tools/list → sample call) and exit")
}

// runMCPServer wires the MCP deps and starts the stdio server.
// SurrealDB failures are non-fatal at startup: the server boots with nil
// graph/service fields, and individual tool calls return a helpful error.
func runMCPServer(ctx context.Context, cfg *config.Config) error {
	log := slog.Default().With("component", "mcp")

	deps := mcpadapter.Deps{
		DBAddr: cfg.Surreal.URL,
	}

	// Best-effort: try to wire services. If it fails, boot anyway.
	// First tool call that needs the graph will return a clear DB error.
	svcs, err := wireServeServices(ctx, cfg)
	if err != nil {
		log.Warn("db unavailable at startup — tools will fail until SurrealDB is reachable",
			"err", err,
			"hint", "run: docker compose up surreal",
		)
	} else {
		deps.QueryService = svcs.query
		deps.TraceService = svcs.trace
		deps.BlastService = svcs.blast
		deps.FieldFlowService = svcs.flow
		deps.RootCauseService = svcs.rootCause
		deps.DiffImpactService = svcs.diffImpact
		deps.Graph = svcs.graph
		defer svcs.cleanup()
	}

	log.Info("starting commit0 MCP server", "transport", "stdio")
	return mcpadapter.RunStdio(ctx, deps)
}

// runSelfTest executes an in-process round-trip:
//  1. Initialize the server with an in-memory transport.
//  2. Assert capabilities include "tools".
//  3. List tools — expect 10 tools with sorted names.
//  4. Call commit0_query with a synthetic query (will return a db-unavailable
//     tool error, but the protocol round-trip is valid).
//  5. Print "OK" and exit 0; print diagnostic and exit 1 on failure.
func runSelfTest(ctx context.Context, cfg *config.Config) error {
	slog.Info("running MCP self-test")

	// Build the server with empty deps (no DB needed for structural tests).
	deps := mcpadapter.Deps{
		DBAddr: cfg.Surreal.URL,
	}
	server := mcpadapter.New(deps)

	// Wire in-memory transports.
	serverTransport, clientTransport := mcpsdk.NewInMemoryTransports()

	// Run server in background.
	serverDone := make(chan error, 1)
	serverCtx, serverCancel := context.WithCancel(ctx)
	defer serverCancel()
	go func() {
		serverDone <- server.Run(serverCtx, serverTransport)
	}()

	// Connect client.
	client := mcpsdk.NewClient(&mcpsdk.Implementation{
		Name:    "commit0-self-test",
		Version: "0.0.1",
	}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		return fmt.Errorf("self-test: connect failed: %w", err)
	}
	defer func() { _ = session.Close() }()

	// --- Step 1: list tools ---
	toolsResult, err := session.ListTools(ctx, &mcpsdk.ListToolsParams{})
	if err != nil {
		return fmt.Errorf("self-test: tools/list failed: %w", err)
	}

	tools := toolsResult.Tools
	if len(tools) != 12 {
		return fmt.Errorf("self-test: expected 12 tools, got %d", len(tools))
	}

	// Check names are sorted.
	names := make([]string, len(tools))
	for i, t := range tools {
		names[i] = t.Name
	}
	if !sort.StringsAreSorted(names) {
		return fmt.Errorf("self-test: tool names are not sorted: %v", names)
	}

	expectedTools := []string{
		"commit0_blast",
		"commit0_diff_impact",
		"commit0_field_flow",
		"commit0_find_root_cause",
		"commit0_lookup",
		"commit0_neighborhood",
		"commit0_query",
		"commit0_show_node",
		"commit0_similar_to",
		"commit0_subjects_for",
		"commit0_tests_for",
		"commit0_trace",
	}
	for i, want := range expectedTools {
		if names[i] != want {
			return fmt.Errorf("self-test: tool[%d] = %q, want %q", i, names[i], want)
		}
	}

	// --- Step 2: call commit0_query (will fail with db-unavailable, that's OK) ---
	callResult, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: "commit0_query",
		Arguments: map[string]any{
			"question":  "test query for self-test",
			"repo_slug": "self-test",
		},
	})
	if err != nil {
		return fmt.Errorf("self-test: tools/call protocol error: %w", err)
	}
	// The call may return IsError=true (DB unavailable), that's fine.
	// What we care about is that the protocol worked: result is non-nil.
	if callResult == nil {
		return fmt.Errorf("self-test: got nil result from commit0_query")
	}

	// Shutdown.
	serverCancel()

	fmt.Fprintln(os.Stderr, "commit0 MCP self-test: OK")
	fmt.Fprintf(os.Stderr, "  tools (%d): %s\n", len(names), joinStrings(names))
	fmt.Fprintf(os.Stderr, "  commit0_query: isError=%v (db-unavailable expected without docker compose)\n", callResult.IsError)
	return nil
}

func joinStrings(ss []string) string {
	result := ""
	for i, s := range ss {
		if i > 0 {
			result += ", "
		}
		result += s
	}
	return result
}
