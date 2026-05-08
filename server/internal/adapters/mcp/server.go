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
	QueryService      *app.QueryService
	TraceService      *app.TraceService
	BlastService      *app.BlastService
	FieldFlowService  *app.FieldFlowService
	RootCauseService  *app.RootCauseAnalysisService
	DiffImpactService *app.DiffImpactService
	IndexService      *app.IndexService
	RepoService       *app.RepoService
	Graph             domain.OpenCodeGraph
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
				"Search: commit0_query (semantic), commit0_lookup (qualified name), " +
				"commit0_neighborhood (one hop), commit0_show_node (full body), " +
				"commit0_similar_to (find similar code by embedding). " +
				"Trace: commit0_trace (call chain forward/reverse), commit0_blast " +
				"(transitive impact of a change), commit0_field_flow (field-level " +
				"data flow + mutations), commit0_find_root_cause (commit-zero " +
				"detection from a bug description). " +
				"Tests: commit0_tests_for (which tests cover a symbol), " +
				"commit0_subjects_for (which prod symbols a test exercises). " +
				"Diff: commit0_diff_impact (git-aware blast fan-out across a diff range). " +
				"Interfaces: commit0_resolve_interface (find all concrete types that " +
				"satisfy a Go interface and optionally locate their DI wiring sites). " +
				"Meta: commit0_index_status (poll an indexing job by ID, even after it " +
				"finishes — trackers are retained for ~30 minutes), " +
				"commit0_list_repos (enumerate every indexed repository), " +
				"commit0_list_files (enumerate file nodes in a repository, optionally " +
				"by path prefix). " +
				"Resources: node://<id> (read the full body of a CodeNode by graph ID).",
		},
	)

	registerSearchTools(server, deps, log)
	registerTraceTools(server, deps, log)
	registerTestsTools(server, deps, log)
	registerSimilarTools(server, deps, log)
	registerDiffTools(server, deps, log)
	registerInterfaceTools(server, deps, log)
	registerMetaTools(server, deps, log)
	registerNodeResource(server, deps, log)

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

// traceServiceFromDeps returns the TraceService or a nil guard with an error.
func traceServiceFromDeps(deps Deps) (*app.TraceService, *mcpsdk.CallToolResult) {
	if deps.TraceService == nil {
		addr := deps.DBAddr
		if addr == "" {
			addr = "localhost:8000"
		}
		return nil, dbUnavailableError(addr)
	}
	return deps.TraceService, nil
}

// blastServiceFromDeps returns the BlastService or a nil guard with an error.
func blastServiceFromDeps(deps Deps) (*app.BlastService, *mcpsdk.CallToolResult) {
	if deps.BlastService == nil {
		addr := deps.DBAddr
		if addr == "" {
			addr = "localhost:8000"
		}
		return nil, dbUnavailableError(addr)
	}
	return deps.BlastService, nil
}

// fieldFlowServiceFromDeps returns the FieldFlowService or a nil guard with an error.
func fieldFlowServiceFromDeps(deps Deps) (*app.FieldFlowService, *mcpsdk.CallToolResult) {
	if deps.FieldFlowService == nil {
		addr := deps.DBAddr
		if addr == "" {
			addr = "localhost:8000"
		}
		return nil, dbUnavailableError(addr)
	}
	return deps.FieldFlowService, nil
}

// rootCauseServiceFromDeps returns the RootCauseAnalysisService or a nil guard with an error.
func rootCauseServiceFromDeps(deps Deps) (*app.RootCauseAnalysisService, *mcpsdk.CallToolResult) {
	if deps.RootCauseService == nil {
		addr := deps.DBAddr
		if addr == "" {
			addr = "localhost:8000"
		}
		return nil, dbUnavailableError(addr)
	}
	return deps.RootCauseService, nil
}

// diffImpactServiceFromDeps returns the DiffImpactService or a nil guard with an error.
func diffImpactServiceFromDeps(deps Deps) (*app.DiffImpactService, *mcpsdk.CallToolResult) {
	if deps.DiffImpactService == nil {
		addr := deps.DBAddr
		if addr == "" {
			addr = "localhost:8000"
		}
		return nil, dbUnavailableError(addr)
	}
	return deps.DiffImpactService, nil
}

// indexServiceFromDeps returns the IndexService or a nil guard with an error.
// Used by the meta tool group (commit0_index_status) to look up trackers by
// jobID. Like the other guards, an unwired service surfaces dbUnavailableError
// because the index registry can only be populated by a running server.
func indexServiceFromDeps(deps Deps) (*app.IndexService, *mcpsdk.CallToolResult) {
	if deps.IndexService == nil {
		addr := deps.DBAddr
		if addr == "" {
			addr = "localhost:8000"
		}
		return nil, dbUnavailableError(addr)
	}
	return deps.IndexService, nil
}

// repoServiceFromDeps returns the RepoService or a nil guard with an error.
// Used by the meta tool group (commit0_list_repos) to enumerate repositories;
// like the other guards, an unwired service surfaces dbUnavailableError because
// the repo registry can only be populated by a running server.
func repoServiceFromDeps(deps Deps) (*app.RepoService, *mcpsdk.CallToolResult) {
	if deps.RepoService == nil {
		addr := deps.DBAddr
		if addr == "" {
			addr = "localhost:8000"
		}
		return nil, dbUnavailableError(addr)
	}
	return deps.RepoService, nil
}

// RunStdio starts the MCP server on stdio transport, blocking until the client
// disconnects or ctx is canceled.
func RunStdio(ctx context.Context, deps Deps) error {
	server := New(deps)
	log := slog.Default().With("component", "mcp.stdio")
	log.Info("starting commit0 MCP server on stdio")
	return server.Run(ctx, &mcpsdk.StdioTransport{})
}
