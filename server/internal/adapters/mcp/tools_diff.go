package mcp

import (
	"context"
	"log/slog"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/commit0-dev/commit0/server/internal/app"
	"github.com/commit0-dev/commit0/server/internal/domain"
)

// registerDiffTools adds the diff-impact tool to the server.
func registerDiffTools(server *mcpsdk.Server, deps Deps, log *slog.Logger) {
	addCommit0DiffImpact(server, deps, log)
}

// ---------------------------------------------------------------------------
// Tool: commit0_diff_impact
// ---------------------------------------------------------------------------

// diffImpactInput is the typed input for commit0_diff_impact.
type diffImpactInput struct {
	RepoSlug        string   `json:"repo_slug"                  jsonschema:"Slug of the indexed repository, e.g. 'org/repo'. Required."`
	RepoPath        string   `json:"repo_path"                  jsonschema:"Local filesystem path to the git repository. Required."`
	FromRef         string   `json:"from_ref,omitempty"         jsonschema:"Base git ref for the diff range (default: 'main')."`
	ToRef           string   `json:"to_ref,omitempty"           jsonschema:"Target git ref, or 'WORKING' for staged+unstaged vs HEAD (default: 'HEAD')."`
	MaxDepth        int      `json:"max_depth,omitempty"        jsonschema:"Maximum blast traversal depth (1-5, default 5)."`
	EdgeLabels      []string `json:"edge_labels,omitempty"      jsonschema:"Edge types to follow during blast, e.g. ['calls', 'tests']. Default: ['calls']."`
	WithExplanation bool     `json:"with_explanation,omitempty" jsonschema:"When true, include an LLM-generated summary of the impact. Default false."`
}

func addCommit0DiffImpact(server *mcpsdk.Server, deps Deps, log *slog.Logger) {
	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name: "commit0_diff_impact",
		Description: "Analyze the blast radius of a git diff. " +
			"Given a ref range (from_ref..to_ref) or the working-tree diff, " +
			"maps each changed file's line ranges to indexed symbols, fans out " +
			"commit0_blast for every changed symbol, deduplicates the results, " +
			"and returns changed_symbols, affected (prod), and affected_tests. " +
			"Use before pushing to get a consolidated pre-push impact view.",
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, input diffImpactInput) (*mcpsdk.CallToolResult, any, error) {
		if input.RepoSlug == "" {
			return toolError(domain.Validation("repo_slug is required")), nil, nil
		}
		if input.RepoPath == "" {
			return toolError(domain.Validation("repo_path is required")), nil, nil
		}

		diffImpactSvc, errResult := diffImpactServiceFromDeps(deps)
		if errResult != nil {
			return errResult, nil, nil
		}

		analysisResult, err := diffImpactSvc.Analyze(ctx, app.DiffImpactRequest{
			RepoSlug:   input.RepoSlug,
			RepoPath:   input.RepoPath,
			FromRef:    input.FromRef,
			ToRef:      input.ToRef,
			MaxDepth:   input.MaxDepth,
			EdgeLabels: input.EdgeLabels,
			NoExplain:  !input.WithExplanation,
		})
		if err != nil {
			log.Warn("commit0_diff_impact failed", "repo", input.RepoSlug, "err", err)
			return toolError(err), nil, nil
		}

		result := diffImpactResultOut(analysisResult)
		return &mcpsdk.CallToolResult{
			Content: []mcpsdk.Content{
				&mcpsdk.TextContent{Text: diffImpactMarkdown(result)},
			},
			StructuredContent: result,
		}, nil, nil
	})
}
