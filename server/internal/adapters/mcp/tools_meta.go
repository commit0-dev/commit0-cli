package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/commit0-dev/commit0/pkg/types"
	"github.com/commit0-dev/commit0/server/internal/domain"
)

// listFilesDefaultLimit is the per-call cap when the caller does not specify
// a Limit. listFilesMaxLimit clamps requests with very large limits to protect
// the database.
const (
	listFilesDefaultLimit = 100
	listFilesMaxLimit     = 1000
)

// registerMetaTools adds the meta tool group (index status, list repos,
// list files) to the MCP server.
func registerMetaTools(server *mcpsdk.Server, deps Deps, log *slog.Logger) {
	addCommit0IndexStatus(server, deps, log)
	addCommit0ListRepos(server, deps, log)
	addCommit0ListFiles(server, deps, log)
}

// ---------------------------------------------------------------------------
// Tool: commit0_index_status
// ---------------------------------------------------------------------------

// indexStatusInput is the typed input for commit0_index_status.
type indexStatusInput struct {
	JobID string `json:"job_id" jsonschema:"Job ID returned by POST /api/v1/index. Required. The tracker remains queryable for ~30 minutes after the job finishes."`
}

func addCommit0IndexStatus(server *mcpsdk.Server, deps Deps, log *slog.Logger) {
	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name: "commit0_index_status",
		Description: "Fetch the live progress snapshot for an index job by ID. " +
			"Returns the current stage, files/nodes/edges counters, per-stage timings " +
			"and error counts, and the AST ↔ embedding coverage gap. Equivalent to " +
			"polling GET /api/v1/index/:job_id over HTTP. The tracker stays queryable " +
			"for ~30 minutes after the job finishes so a final snapshot can be fetched " +
			"after the SSE stream closes.",
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, input indexStatusInput) (*mcpsdk.CallToolResult, any, error) {
		if strings.TrimSpace(input.JobID) == "" {
			return toolError(domain.Validation("job_id is required")), nil, nil
		}

		indexSvc, errResult := indexServiceFromDeps(deps)
		if errResult != nil {
			return errResult, nil, nil
		}

		tracker, ok := indexSvc.GetTracker(input.JobID)
		if !ok {
			log.Debug("commit0_index_status: tracker not found", "job_id", input.JobID)
			return toolError(domain.NotFound(fmt.Sprintf(
				"index job %q is unknown or has been evicted (trackers are kept for ~30 minutes after Finish)",
				input.JobID,
			))), nil, nil
		}

		snapshot := tracker.Snapshot()
		out := indexStatusOut(snapshot)
		return &mcpsdk.CallToolResult{
			Content: []mcpsdk.Content{
				&mcpsdk.TextContent{Text: indexStatusMarkdown(snapshot)},
			},
			StructuredContent: out,
		}, nil, nil
	})
}

// indexStatusMarkdown renders a short human-readable summary of an index
// job. Optimized for the LLM consumer: stage rail at the top, headline
// counters in the middle, error/coverage gaps if any below.
func indexStatusMarkdown(p types.IndexProgress) string {
	var b strings.Builder
	fmt.Fprintf(&b, "**Index job `%s`** — %s\n", p.JobID, p.Status)
	if p.RepoSlug != "" {
		fmt.Fprintf(&b, "- repo: `%s`\n", p.RepoSlug)
	}
	if p.CurrentStage != "" {
		fmt.Fprintf(&b, "- current stage: `%s`\n", p.CurrentStage)
	}
	fmt.Fprintf(&b, "- files: %d · nodes: %d · edges: %d\n",
		p.FilesIndexed, p.NodesCreated, p.EdgesCreated)
	fmt.Fprintf(&b, "- elapsed: %dms", p.ElapsedMS)
	if p.FinishedAt != nil {
		fmt.Fprintf(&b, " (finished %s)", p.FinishedAt.Format("15:04:05"))
	}
	b.WriteString("\n")

	if p.TotalErrors > 0 {
		fmt.Fprintf(&b, "- ⚠ %d total errors across stages\n", p.TotalErrors)
	}
	if p.Error != "" {
		fmt.Fprintf(&b, "- ✗ final error: %s\n", p.Error)
	}

	// Coverage line — the AST↔downstream gap metric is the most actionable
	// single number for a maintainer reading a status snapshot.
	cov := p.Coverage
	if cov.NodesExtracted > 0 {
		fmt.Fprintf(&b,
			"- coverage: summary %.1f%% · embed %.1f%% · store %.1f%% · edge-resolution %.1f%%\n",
			cov.SummaryCoverage, cov.EmbedCoverage, cov.StoreCoverage, cov.EdgeResolution)
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// Tool: commit0_list_repos
// ---------------------------------------------------------------------------

// listReposInput is the typed input for commit0_list_repos. The tool takes
// no arguments; the empty struct exists to satisfy the SDK's typed handler
// signature.
type listReposInput struct{}

func addCommit0ListRepos(server *mcpsdk.Server, deps Deps, log *slog.Logger) {
	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name: "commit0_list_repos",
		Description: "Enumerate every repository indexed by commit0. Returns slug, " +
			"path, default branch, languages, and last_indexed_at for each repo. " +
			"Use this as the discovery step before calling any tool that requires " +
			"a repo_slug argument.",
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, _ listReposInput) (*mcpsdk.CallToolResult, any, error) {
		repoSvc, errResult := repoServiceFromDeps(deps)
		if errResult != nil {
			return errResult, nil, nil
		}

		repos, err := repoSvc.ListRepos(ctx)
		if err != nil {
			log.Warn("commit0_list_repos failed", "err", err)
			return toolError(err), nil, nil
		}

		out := ListReposToolResult{Repos: make([]RepoOut, 0, len(repos))}
		for _, repo := range repos {
			out.Repos = append(out.Repos, repoOut(repo))
		}

		return &mcpsdk.CallToolResult{
			Content: []mcpsdk.Content{
				&mcpsdk.TextContent{Text: listReposMarkdown(out)},
			},
			StructuredContent: out,
		}, nil, nil
	})
}

// ---------------------------------------------------------------------------
// Tool: commit0_list_files
// ---------------------------------------------------------------------------

// listFilesInput is the typed input for commit0_list_files.
type listFilesInput struct {
	RepoSlug   string `json:"repo_slug"             jsonschema:"Repository slug returned by commit0_list_repos. Required."`
	PathPrefix string `json:"path_prefix,omitempty" jsonschema:"Optional file-path prefix filter (substring match against the start of the file_path)."`
	Limit      int    `json:"limit,omitempty"       jsonschema:"Maximum number of files to return. Defaults to 100; capped at 1000."`
}

func addCommit0ListFiles(server *mcpsdk.Server, deps Deps, log *slog.Logger) {
	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name: "commit0_list_files",
		Description: "Enumerate file nodes for one repository, optionally filtered by " +
			"path prefix. Each result is a file-kind CodeNode reference (id, " +
			"file_path, language, last_modified_at) — bodies are not returned. " +
			"Use this to discover the on-disk layout of an indexed repo before " +
			"calling commit0_show_node or commit0_query.",
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, input listFilesInput) (*mcpsdk.CallToolResult, any, error) {
		repoSlug := strings.TrimSpace(input.RepoSlug)
		if repoSlug == "" {
			return toolError(domain.Validation("repo_slug is required")), nil, nil
		}

		graph, errResult := graphFromDeps(deps)
		if errResult != nil {
			return errResult, nil, nil
		}

		limit := input.Limit
		if limit <= 0 {
			limit = listFilesDefaultLimit
		}
		if limit > listFilesMaxLimit {
			limit = listFilesMaxLimit
		}

		// Request limit+1 so we can detect truncation without a second call;
		// trim back to limit before serializing.
		queryLimit := limit + 1

		nodes, err := graph.ListNodes(ctx, repoSlug, domain.ListOpts{
			Labels:   []string{"file"},
			FilePath: input.PathPrefix,
			Limit:    queryLimit,
		})
		if err != nil {
			log.Warn("commit0_list_files failed", "repo", repoSlug, "err", err)
			return toolError(err), nil, nil
		}

		truncated := len(nodes) > limit
		if truncated {
			nodes = nodes[:limit]
		}

		out := ListFilesToolResult{
			RepoSlug:   repoSlug,
			PathPrefix: input.PathPrefix,
			Files:      make([]FileNodeOut, 0, len(nodes)),
			Truncated:  truncated,
			Limit:      limit,
		}
		for _, node := range nodes {
			out.Files = append(out.Files, fileNodeOut(node))
		}

		return &mcpsdk.CallToolResult{
			Content: []mcpsdk.Content{
				&mcpsdk.TextContent{Text: listFilesMarkdown(out)},
			},
			StructuredContent: out,
		}, nil, nil
	})
}
