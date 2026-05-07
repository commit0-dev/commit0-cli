package mcp

import (
	"context"
	"log/slog"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/commit0-dev/commit0/server/internal/app"
	"github.com/commit0-dev/commit0/server/internal/domain"
)

const (
	serverName    = "commit0"
	serverVersion = "0.1.0"
)

// Deps holds the application-layer dependencies required by the MCP server.
// All fields are optional: a nil field means the corresponding tool group
// will return a descriptive tool error rather than panicking.
//
// The MCP server must boot even when SurrealDB is unreachable (lazy init).
// Callers set these fields after successfully wiring adapters; the server
// falls back to dbUnavailableError() on first use if they remain nil.
type Deps struct {
	QueryService *app.QueryService
	Graph        domain.OpenCodeGraph
	// DBAddr is shown in the unavailability error message when Graph is nil.
	DBAddr string
}

// New constructs an mcp.Server with all 4 search tools registered.
// It does NOT start any transport — call server.Run(ctx, transport) to serve.
func New(deps Deps) *mcpsdk.Server {
	log := slog.Default().With("component", "mcp.server")

	server := mcpsdk.NewServer(
		&mcpsdk.Implementation{
			Name:    serverName,
			Version: serverVersion,
		},
		&mcpsdk.ServerOptions{
			Instructions: "commit0 provides graph-based code intelligence. " +
				"Use commit0_query to search semantically, commit0_lookup to resolve a symbol, " +
				"commit0_neighborhood to explore immediate graph context, and " +
				"commit0_show_node to retrieve full source of a node.",
		},
	)

	registerSearchTools(server, deps, log)

	return server
}

// registerSearchTools adds the 4 search tools to the server.
func registerSearchTools(server *mcpsdk.Server, deps Deps, log *slog.Logger) {
	addCommit0Query(server, deps, log)
	addCommit0Lookup(server, deps, log)
	addCommit0Neighborhood(server, deps, log)
	addCommit0ShowNode(server, deps, log)
}

// graphFromDeps returns the OpenCodeGraph or a nil guard with an error result.
// Used in tool handlers to fail gracefully when the DB adapter is not wired.
func graphFromDeps(deps Deps) (domain.OpenCodeGraph, *mcpsdk.CallToolResult) {
	if deps.Graph == nil {
		addr := deps.DBAddr
		if addr == "" {
			addr = "localhost:8000"
		}
		return nil, dbUnavailableError(addr)
	}
	return deps.Graph, nil
}

// queryServiceFromDeps returns the QueryService or a nil guard with an error.
func queryServiceFromDeps(deps Deps) (*app.QueryService, *mcpsdk.CallToolResult) {
	if deps.QueryService == nil {
		addr := deps.DBAddr
		if addr == "" {
			addr = "localhost:8000"
		}
		return nil, dbUnavailableError(addr)
	}
	return deps.QueryService, nil
}

// RunStdio starts the MCP server on stdio transport, blocking until the client
// disconnects or ctx is canceled.
func RunStdio(ctx context.Context, deps Deps) error {
	server := New(deps)
	log := slog.Default().With("component", "mcp.stdio")
	log.Info("starting commit0 MCP server on stdio")
	return server.Run(ctx, &mcpsdk.StdioTransport{})
}
