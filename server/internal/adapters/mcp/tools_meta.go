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

// registerMetaTools adds the meta tool group (index status, list repos,
// list files) to the MCP server. Subsequent slices in the meta+security
// roadmap fold list_repos and list_files into this same registrar.
func registerMetaTools(server *mcpsdk.Server, deps Deps, log *slog.Logger) {
	addCommit0IndexStatus(server, deps, log)
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
