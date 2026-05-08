package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/commit0-dev/commit0/server/internal/domain"
)

// registerAPITools adds the API tool group to the MCP server.
func registerAPITools(server *mcpsdk.Server, deps Deps, log *slog.Logger) {
	addCommit0APISurface(server, deps, log)
}

func addCommit0APISurface(server *mcpsdk.Server, deps Deps, log *slog.Logger) {
	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name: "commit0_api_surface",
		Description: "Discover HTTP API endpoints for an indexed repository from the " +
			"code graph's route edges. Resolves each handler, auth middleware chain, " +
			"request/response bindings and exposed PII fields. Two output formats: " +
			"'summary' (default) returns the typed APISurface struct; 'openapi' " +
			"emits an OpenAPI 3.0 JSON specification as text content with a small " +
			"structured wrapper (format + endpoint count) for routing.",
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, input apiSurfaceIn) (*mcpsdk.CallToolResult, any, error) {
		repoSlug := strings.TrimSpace(input.RepoSlug)
		if repoSlug == "" {
			return toolError(domain.Validation("repo_slug is required")), nil, nil
		}
		format := strings.ToLower(strings.TrimSpace(input.Format))
		if !validAPISurfaceFormat(format) {
			return toolError(domain.Validation(fmt.Sprintf(
				"format must be 'summary' or 'openapi' (got %q)", input.Format,
			))), nil, nil
		}
		if format == "" {
			format = apiSurfaceFormatSummary
		}

		apiSvc, errResult := apiSurfaceServiceFromDeps(deps)
		if errResult != nil {
			return errResult, nil, nil
		}

		surface, err := apiSvc.Discover(ctx, repoSlug)
		if err != nil {
			log.Warn("commit0_api_surface: discover failed", "repo", repoSlug, "err", err)
			return toolError(err), nil, nil
		}

		switch format {
		case apiSurfaceFormatOpenAPI:
			specBytes, err := apiSvc.GenerateOpenAPI(ctx, surface)
			if err != nil {
				log.Warn("commit0_api_surface: GenerateOpenAPI failed", "repo", repoSlug, "err", err)
				return toolError(err), nil, nil
			}
			wrapper := apiSurfaceOpenAPIWrapper(repoSlug, surface, len(specBytes))
			return &mcpsdk.CallToolResult{
				Content: []mcpsdk.Content{
					&mcpsdk.TextContent{Text: string(specBytes)},
				},
				StructuredContent: wrapper,
			}, nil, nil

		default: // apiSurfaceFormatSummary
			return &mcpsdk.CallToolResult{
				Content: []mcpsdk.Content{
					&mcpsdk.TextContent{Text: apiSurfaceSummaryMarkdown(repoSlug, surface)},
				},
				StructuredContent: APISurfaceSummaryOut(*surface),
			}, nil, nil
		}
	})
}
