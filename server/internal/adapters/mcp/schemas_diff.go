package mcp

import (
	"fmt"
	"strings"

	"github.com/commit0-dev/commit0/server/internal/app"
)

// ---------------------------------------------------------------------------
// Output structs for commit0_diff_impact.
// ---------------------------------------------------------------------------

// ChangedSymbolOut is a code symbol whose line range overlaps the diff.
type ChangedSymbolOut struct {
	ID        string `json:"id"`
	Qualified string `json:"qualified"`
	Kind      string `json:"kind"`
	FilePath  string `json:"file_path"`
	StartLine int    `json:"start_line,omitempty"`
	EndLine   int    `json:"end_line,omitempty"`
}

// DiffAffectedOut is one transitively-impacted node from a diff blast.
type DiffAffectedOut struct {
	Qualified string `json:"qualified"`
	Kind      string `json:"kind"`
	FilePath  string `json:"file_path"`
	HopCount  int    `json:"hop_count"`
}

// DiffImpactToolResult is the structured output of commit0_diff_impact.
type DiffImpactToolResult struct {
	ChangedSymbols []ChangedSymbolOut `json:"changed_symbols"`
	Affected       []DiffAffectedOut  `json:"affected"`
	AffectedTests  []DiffAffectedOut  `json:"affected_tests"`
	Summary        string             `json:"summary,omitempty"`
	Timing         TimingOut          `json:"timing"`
}

// diffImpactResultOut converts a DiffImpactResult to the wire output struct.
func diffImpactResultOut(r *app.DiffImpactResult) DiffImpactToolResult {
	if r == nil {
		return DiffImpactToolResult{}
	}

	changed := make([]ChangedSymbolOut, len(r.ChangedSymbols))
	for i, n := range r.ChangedSymbols {
		changed[i] = ChangedSymbolOut{
			ID:        n.ID,
			Qualified: n.Qualified,
			Kind:      string(n.Kind),
			FilePath:  n.FilePath,
			StartLine: n.StartLine,
			EndLine:   n.EndLine,
		}
	}

	affected := make([]DiffAffectedOut, len(r.Affected))
	for i, an := range r.Affected {
		affected[i] = DiffAffectedOut{
			Qualified: an.Node.Qualified,
			Kind:      string(an.Node.Kind),
			FilePath:  an.Node.FilePath,
			HopCount:  an.HopCount,
		}
	}

	affectedTests := make([]DiffAffectedOut, len(r.AffectedTests))
	for i, an := range r.AffectedTests {
		affectedTests[i] = DiffAffectedOut{
			Qualified: an.Node.Qualified,
			Kind:      string(an.Node.Kind),
			FilePath:  an.Node.FilePath,
			HopCount:  an.HopCount,
		}
	}

	return DiffImpactToolResult{
		ChangedSymbols: changed,
		Affected:       affected,
		AffectedTests:  affectedTests,
		Summary:        r.Summary,
		Timing:         timingOut(r.Timing),
	}
}

// ---------------------------------------------------------------------------
// Markdown formatter
// ---------------------------------------------------------------------------

// diffImpactMarkdown formats a DiffImpactToolResult as Markdown.
func diffImpactMarkdown(r DiffImpactToolResult) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "## Diff Impact — %d changed symbol(s), %d affected (prod), %d affected (tests)\n\n",
		len(r.ChangedSymbols), len(r.Affected), len(r.AffectedTests))

	if r.Summary != "" {
		fmt.Fprintf(&sb, "%s\n\n", r.Summary)
	}

	if len(r.ChangedSymbols) > 0 {
		sb.WriteString("### Changed Symbols\n")
		for i, n := range r.ChangedSymbols {
			fmt.Fprintf(&sb, "%d. `%s` (%s) — %s", i+1, n.Qualified, n.Kind, n.FilePath)
			if n.StartLine > 0 {
				fmt.Fprintf(&sb, ":%d", n.StartLine)
			}
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	if len(r.Affected) > 0 {
		sb.WriteString("### Affected (production)\n")
		for i, a := range r.Affected {
			fmt.Fprintf(&sb, "%d. `%s` (%s, hop %d) — %s\n", i+1, a.Qualified, a.Kind, a.HopCount, a.FilePath)
		}
		sb.WriteString("\n")
	}

	if len(r.AffectedTests) > 0 {
		sb.WriteString("### Affected (tests)\n")
		for i, a := range r.AffectedTests {
			fmt.Fprintf(&sb, "%d. `%s` (%s, hop %d) — %s\n", i+1, a.Qualified, a.Kind, a.HopCount, a.FilePath)
		}
		sb.WriteString("\n")
	}

	fmt.Fprintf(&sb, "_Timing: graph=%dms explain=%dms total=%dms_\n",
		r.Timing.GraphMS, r.Timing.ExplainMS, r.Timing.TotalMS)
	return sb.String()
}
