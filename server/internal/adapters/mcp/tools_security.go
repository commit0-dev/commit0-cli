package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/commit0-dev/commit0/server/internal/domain"
)

// registerSecurityTools adds the security tool group to the MCP server.
// Currently a single tool — commit0_scan_security — but kept as its own
// registrar for symmetry with registerMetaTools / registerAPITools and to
// leave room for follow-ups (e.g. commit0_audit_secrets).
func registerSecurityTools(server *mcpsdk.Server, deps Deps, log *slog.Logger) {
	addCommit0ScanSecurity(server, deps, log)
}

func addCommit0ScanSecurity(server *mcpsdk.Server, deps Deps, log *slog.Logger) {
	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name: "commit0_scan_security",
		Description: "Run the commit0 security scanner over an indexed repository. " +
			"Combines taint analysis (SQL injection, command injection, XSS, path " +
			"traversal) traced through the data-flow graph with auth-gap detection " +
			"on HTTP handlers. Optionally filters issues by minimum severity. " +
			"Returns a structured list of AnalysisIssueOut findings ranked by " +
			"severity, plus a Markdown summary grouped by severity.",
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, input scanSecurityIn) (*mcpsdk.CallToolResult, any, error) {
		repoSlug := strings.TrimSpace(input.RepoSlug)
		if repoSlug == "" {
			return toolError(domain.Validation("repo_slug is required")), nil, nil
		}
		severityMin := strings.ToLower(strings.TrimSpace(input.SeverityMin))
		if !validSeverityFilter(severityMin) {
			return toolError(domain.Validation(fmt.Sprintf(
				"severity_min must be one of 'critical', 'high', 'medium', 'low' (got %q)",
				input.SeverityMin,
			))), nil, nil
		}

		analysisSvc, errResult := analysisServiceFromDeps(deps)
		if errResult != nil {
			return errResult, nil, nil
		}

		scan, err := analysisSvc.Scan(ctx, repoSlug)
		if err != nil {
			log.Warn("commit0_scan_security failed", "repo", repoSlug, "err", err)
			return toolError(err), nil, nil
		}

		out := scanSecurityResultOut(repoSlug, scan, severityMin)
		return &mcpsdk.CallToolResult{
			Content: []mcpsdk.Content{
				&mcpsdk.TextContent{Text: scanSecurityMarkdown(out)},
			},
			StructuredContent: out,
		}, nil, nil
	})
}
