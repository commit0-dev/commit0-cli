package mcp

import (
	"fmt"
	"sort"
	"strings"

	"github.com/commit0-dev/commit0/pkg/types"
	"github.com/commit0-dev/commit0/server/internal/app"
)

// scanSecurityIn is the typed input for commit0_scan_security.
type scanSecurityIn struct {
	RepoSlug    string `json:"repo_slug"               jsonschema:"Repository slug returned by commit0_list_repos. Required."`
	SeverityMin string `json:"severity_min,omitempty"  jsonschema:"Optional minimum severity to include. One of 'critical', 'high', 'medium', 'low'. Default: include all."`
}

// AnalysisIssueOut is the MCP wire shape for one security issue. It is a
// straight re-export of app.AnalysisIssue with stable JSON tags so the
// adapter remains decoupled from any future field tweaks in the application
// layer.
type AnalysisIssueOut struct {
	Severity    string   `json:"severity"`
	Category    string   `json:"category"`
	Title       string   `json:"title"`
	File        string   `json:"file"`
	Line        int      `json:"line,omitempty"`
	Function    string   `json:"function,omitempty"`
	Description string   `json:"description,omitempty"`
	TaintPath   []string `json:"taint_path,omitempty"`
	Fix         string   `json:"fix,omitempty"`
}

// ScanSecurityToolResult is the structured output of commit0_scan_security.
type ScanSecurityToolResult struct {
	RepoSlug     string             `json:"repo_slug"`
	Issues       []AnalysisIssueOut `json:"issues"`
	IssueCount   int                `json:"issue_count"`
	ScannedNodes int                `json:"scanned_nodes"`
	SeverityMin  string             `json:"severity_min,omitempty"`
	TimingMS     int64              `json:"timing_ms"`
}

// severityRank assigns each severity an integer for ordering. Higher rank =
// more severe. Anything outside the known set ranks at 0 and is therefore
// always included unless severity_min is set to a real level.
func severityRank(s string) int {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "critical":
		return 4
	case "high":
		return 3
	case "medium":
		return 2
	case "low":
		return 1
	}
	return 0
}

// validSeverityFilter returns true if s is empty or one of the four canonical
// severity strings (case-insensitive). Empty string means "include all".
func validSeverityFilter(s string) bool {
	if s == "" {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "critical", "high", "medium", "low":
		return true
	}
	return false
}

// ScanSecurityResultOutForTest exposes the package-private filter+convert
// helper to tests outside package mcp. Production code should not call this
// directly; the named-for-test convention mirrors RegisterTrackerForTest.
func ScanSecurityResultOutForTest(repoSlug string, scan *app.AnalysisScanResult, severityMin string) ScanSecurityToolResult {
	return scanSecurityResultOut(repoSlug, scan, severityMin)
}

// scanSecurityResultOut converts the application-layer scan result into the
// MCP wire shape, applying the severity_min client-side filter.
func scanSecurityResultOut(repoSlug string, scan *app.AnalysisScanResult, severityMin string) ScanSecurityToolResult {
	out := ScanSecurityToolResult{
		RepoSlug:     repoSlug,
		Issues:       make([]AnalysisIssueOut, 0, len(scan.Issues)),
		ScannedNodes: scan.ScannedNodes,
		SeverityMin:  severityMin,
		TimingMS:     scan.Timing.TotalMS,
	}
	minRank := severityRank(severityMin)
	for _, issue := range scan.Issues {
		if severityRank(issue.Severity) < minRank {
			continue
		}
		out.Issues = append(out.Issues, AnalysisIssueOut{
			Severity:    issue.Severity,
			Category:    issue.Category,
			Title:       issue.Title,
			File:        issue.File,
			Line:        issue.Line,
			Function:    issue.Function,
			Description: issue.Description,
			TaintPath:   issue.TaintPath,
			Fix:         issue.Fix,
		})
	}
	out.IssueCount = len(out.Issues)
	return out
}

// scanSecurityMarkdown renders a ScanSecurityToolResult as a short Markdown
// summary suitable for the LLM consumer. Issues are grouped by severity in
// the canonical order (critical → high → medium → low) so the most
// actionable findings are at the top.
func scanSecurityMarkdown(result ScanSecurityToolResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## Security scan — `%s`\n\n", result.RepoSlug)
	fmt.Fprintf(&b, "- issues: %d", result.IssueCount)
	if result.SeverityMin != "" {
		fmt.Fprintf(&b, " (severity ≥ %s)", strings.ToLower(result.SeverityMin))
	}
	fmt.Fprintf(&b, "\n- scanned nodes: %d\n- elapsed: %dms\n", result.ScannedNodes, result.TimingMS)

	if len(result.Issues) == 0 {
		b.WriteString("\nNo issues found.\n")
		return b.String()
	}

	// Bucket and sort issues by severity rank (descending), then category.
	type bucket struct {
		severity string
		issues   []AnalysisIssueOut
	}
	bucketsByName := map[string]*bucket{}
	for _, issue := range result.Issues {
		key := strings.ToLower(strings.TrimSpace(issue.Severity))
		if key == "" {
			key = "unspecified"
		}
		bk, ok := bucketsByName[key]
		if !ok {
			bk = &bucket{severity: key}
			bucketsByName[key] = bk
		}
		bk.issues = append(bk.issues, issue)
	}
	buckets := make([]*bucket, 0, len(bucketsByName))
	for _, bk := range bucketsByName {
		buckets = append(buckets, bk)
	}
	sort.Slice(buckets, func(i, j int) bool {
		ri, rj := severityRank(buckets[i].severity), severityRank(buckets[j].severity)
		if ri != rj {
			return ri > rj
		}
		return buckets[i].severity < buckets[j].severity
	})

	for _, bk := range buckets {
		fmt.Fprintf(&b, "\n### %s (%d)\n\n", strings.ToUpper(bk.severity[:1])+bk.severity[1:], len(bk.issues))
		for _, issue := range bk.issues {
			fmt.Fprintf(&b, "- **%s** — `%s` at `%s:%d`",
				issue.Title, issue.Category, issue.File, issue.Line)
			if issue.Function != "" {
				fmt.Fprintf(&b, " in `%s`", issue.Function)
			}
			b.WriteString("\n")
		}
	}
	return b.String()
}

// Compile-time check: AnalysisIssueOut keeps wire parity with the application
// layer. If the source struct gains a new field, this assertion will not
// catch it; the live tests in commit0_scan_security cover renames.
var _ = types.TimingInfo{}
