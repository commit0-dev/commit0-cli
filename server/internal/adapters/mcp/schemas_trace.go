package mcp

import (
	"fmt"
	"strings"
	"time"

	"github.com/commit0-dev/commit0/pkg/types"
)

// ---------------------------------------------------------------------------
// Output structs for trace/blast/flow/rootcause tools.
// ---------------------------------------------------------------------------

// TraceHopOut is one hop in a flattened trace tree.
//
// We flatten the recursive TraceHop tree into a hop list with parent indices
// so the wire format stays JSON-friendly without nested arrays. Clients that
// want the tree shape can rebuild it from ParentIndex (-1 = root child).
type TraceHopOut struct {
	Qualified       string `json:"qualified"`
	Kind            string `json:"kind"`
	FilePath        string `json:"file_path"`
	StartLine       int    `json:"start_line,omitempty"`
	EndLine         int    `json:"end_line,omitempty"`
	Depth           int    `json:"depth"`
	EdgeKind        string `json:"edge_kind,omitempty"`
	ParentIndex     int    `json:"parent_index"` // -1 for top-level hops
	CallSiteExcerpt string `json:"call_site_excerpt,omitempty"`
	CallExpression  string `json:"call_expression,omitempty"`
}

// TraceToolResult is the structured output of commit0_trace.
type TraceToolResult struct {
	Root        CodeNodeOut   `json:"root"`
	Direction   string        `json:"direction"`
	Hops        []TraceHopOut `json:"hops"`
	Explanation string        `json:"explanation,omitempty"`
	Timing      TimingOut     `json:"timing"`
}

// traceResultOut converts a domain TraceResult to its output representation.
func traceResultOut(tr *types.TraceResult) TraceToolResult {
	if tr == nil {
		return TraceToolResult{}
	}
	out := TraceToolResult{
		Root:        codeNodeOut(tr.Root, false),
		Direction:   tr.Direction,
		Explanation: tr.Explanation,
		Timing:      timingOut(tr.Timing),
	}
	out.Hops = flattenTraceHops(tr.Tree, -1)
	return out
}

// flattenTraceHops walks the trace tree depth-first and returns a flat list
// where each entry remembers its parent's index in that list.
func flattenTraceHops(tree []types.TraceHop, parentIdx int) []TraceHopOut {
	var out []TraceHopOut
	for _, h := range tree {
		out = append(out, TraceHopOut{
			Qualified:       h.Node.Qualified,
			Kind:            string(h.Node.Kind),
			FilePath:        h.Node.FilePath,
			StartLine:       h.Node.StartLine,
			EndLine:         h.Node.EndLine,
			Depth:           h.Depth,
			EdgeKind:        string(h.Edge.Kind),
			ParentIndex:     parentIdx,
			CallSiteExcerpt: h.CallSiteExcerpt,
			CallExpression:  h.CallExpression,
		})
		thisIdx := len(out) - 1
		out = append(out, flattenTraceHops(h.Children, thisIdx)...)
	}
	return out
}

// AffectedNodeOut is a flat representation of a blast-affected node.
type AffectedNodeOut struct {
	Qualified       string `json:"qualified"`
	Kind            string `json:"kind"`
	FilePath        string `json:"file_path"`
	HopCount        int    `json:"hop_count"`
	Module          string `json:"module,omitempty"`
	CallSiteExcerpt string `json:"call_site_excerpt,omitempty"`
	CallExpression  string `json:"call_expression,omitempty"`
	CallLine        int    `json:"call_line,omitempty"`
}

// BlastToolResult is the structured output of commit0_blast.
type BlastToolResult struct {
	Target   CodeNodeOut       `json:"target"`
	Affected []AffectedNodeOut `json:"affected"`
	Summary  string            `json:"summary,omitempty"`
	Timing   TimingOut         `json:"timing"`
}

// blastResultOut converts a domain BlastResult to its output representation.
func blastResultOut(br *types.BlastResult) BlastToolResult {
	if br == nil {
		return BlastToolResult{}
	}
	affected := make([]AffectedNodeOut, len(br.Affected))
	for i, a := range br.Affected {
		affected[i] = AffectedNodeOut{
			Qualified:       a.Node.Qualified,
			Kind:            string(a.Node.Kind),
			FilePath:        a.Node.FilePath,
			HopCount:        a.HopCount,
			Module:          a.Module,
			CallSiteExcerpt: a.CallSiteExcerpt,
			CallExpression:  a.CallExpression,
			CallLine:        a.CallLine,
		}
	}
	return BlastToolResult{
		Target:   codeNodeOut(br.Target, false),
		Affected: affected,
		Summary:  br.Summary,
		Timing:   timingOut(br.Timing),
	}
}

// FieldFlowHopOut is one step in a field-level data-flow chain.
type FieldFlowHopOut struct {
	Qualified    string `json:"qualified"`
	FilePath     string `json:"file_path"`
	StartLine    int    `json:"start_line,omitempty"`
	FieldPath    string `json:"field_path,omitempty"`
	ParamName    string `json:"param_name,omitempty"`
	ArgExpr      string `json:"arg_expr,omitempty"`
	MutationType string `json:"mutation_type,omitempty"`
	MutationExpr string `json:"mutation_expr,omitempty"`
	MutationLine int    `json:"mutation_line,omitempty"`
	Depth        int    `json:"depth"`
}

// FieldFlowChainOut is one end-to-end chain of a tracked field.
type FieldFlowChainOut struct {
	FieldPath    string            `json:"field_path"`
	Hops         []FieldFlowHopOut `json:"hops"`
	Mutations    []FieldFlowHopOut `json:"mutations,omitempty"`
	HasTaintHop  bool              `json:"has_taint_hop"`
	TaintHopName string            `json:"taint_hop_name,omitempty"`
}

// FieldFlowToolResult is the structured output of commit0_field_flow.
type FieldFlowToolResult struct {
	Root        CodeNodeOut         `json:"root"`
	Direction   string              `json:"direction"`
	Chains      []FieldFlowChainOut `json:"chains"`
	Explanation string              `json:"explanation,omitempty"`
	Timing      TimingOut           `json:"timing"`
}

// fieldFlowResultOut converts a domain FieldFlowResult to its output representation.
func fieldFlowResultOut(fr *types.FieldFlowResult) FieldFlowToolResult {
	if fr == nil {
		return FieldFlowToolResult{}
	}
	chains := make([]FieldFlowChainOut, len(fr.Chains))
	for i, c := range fr.Chains {
		chains[i] = fieldFlowChainOut(c)
	}
	return FieldFlowToolResult{
		Root:        codeNodeOut(fr.Root, false),
		Direction:   fr.Direction,
		Chains:      chains,
		Explanation: fr.Explanation,
		Timing:      timingOut(fr.Timing),
	}
}

func fieldFlowChainOut(c types.FieldFlowChain) FieldFlowChainOut {
	hops := make([]FieldFlowHopOut, len(c.Hops))
	for i, h := range c.Hops {
		hops[i] = fieldFlowHopOut(h)
	}
	mutations := make([]FieldFlowHopOut, len(c.Mutations))
	for i, h := range c.Mutations {
		mutations[i] = fieldFlowHopOut(h)
	}
	out := FieldFlowChainOut{
		FieldPath:   c.FieldPath,
		Hops:        hops,
		Mutations:   mutations,
		HasTaintHop: c.TaintPoint != nil,
	}
	if c.TaintPoint != nil {
		out.TaintHopName = c.TaintPoint.Node.Qualified
	}
	return out
}

func fieldFlowHopOut(h types.FieldFlowHop) FieldFlowHopOut {
	return FieldFlowHopOut{
		Qualified:    h.Node.Qualified,
		FilePath:     h.Node.FilePath,
		StartLine:    h.Node.StartLine,
		FieldPath:    h.FieldPath,
		ParamName:    h.ParamName,
		ArgExpr:      h.ArgExpr,
		MutationType: string(h.MutationType),
		MutationExpr: h.MutationExpr,
		MutationLine: h.MutationLine,
		Depth:        h.Depth,
	}
}

// SuspectCommitOut is one ranked candidate in the commit-zero output.
type SuspectCommitOut struct {
	Hash        string  `json:"hash"`
	Message     string  `json:"message"`
	Author      string  `json:"author"`
	Timestamp   string  `json:"timestamp,omitempty"`
	Score       float64 `json:"score"`
	Reasoning   string  `json:"reasoning,omitempty"`
	DiffSummary string  `json:"diff_summary,omitempty"`
}

// RootCauseToolResult is the structured output of commit0_find_root_cause.
type RootCauseToolResult struct {
	CommitHash     string             `json:"commit_hash,omitempty"`
	CommitMessage  string             `json:"commit_message,omitempty"`
	Author         string             `json:"author,omitempty"`
	Timestamp      string             `json:"timestamp,omitempty"`
	Confidence     float64            `json:"confidence"`
	Explanation    string             `json:"explanation,omitempty"`
	SuggestedFix   string             `json:"suggested_fix,omitempty"`
	CausalChain    []FieldFlowHopOut  `json:"causal_chain,omitempty"`
	SuspectCommits []SuspectCommitOut `json:"suspect_commits"`
	Timing         TimingOut          `json:"timing"`
}

// rootCauseReportOut converts a domain RootCauseReport to its output representation.
func rootCauseReportOut(r *types.RootCauseReport) RootCauseToolResult {
	if r == nil {
		return RootCauseToolResult{}
	}
	suspects := make([]SuspectCommitOut, len(r.SuspectCommits))
	for i, s := range r.SuspectCommits {
		suspects[i] = SuspectCommitOut{
			Hash:        s.Hash,
			Message:     s.Message,
			Author:      s.Author,
			Timestamp:   formatTime(s.Timestamp),
			Score:       s.Score,
			Reasoning:   s.Reasoning,
			DiffSummary: s.DiffSummary,
		}
	}
	chain := make([]FieldFlowHopOut, len(r.CausalChain))
	for i, h := range r.CausalChain {
		chain[i] = fieldFlowHopOut(h)
	}
	return RootCauseToolResult{
		CommitHash:     r.CommitHash,
		CommitMessage:  r.CommitMessage,
		Author:         r.Author,
		Timestamp:      formatTime(r.Timestamp),
		Confidence:     r.Confidence,
		Explanation:    r.Explanation,
		SuggestedFix:   r.SuggestedFix,
		CausalChain:    chain,
		SuspectCommits: suspects,
		Timing:         timingOut(r.Timing),
	}
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

// ---------------------------------------------------------------------------
// Markdown formatters
// ---------------------------------------------------------------------------

// traceMarkdown formats a TraceToolResult as Markdown.
func traceMarkdown(r TraceToolResult) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "## Trace: `%s` (%s, %d hops)\n\n", r.Root.Qualified, r.Direction, len(r.Hops))
	if r.Explanation != "" {
		fmt.Fprintf(&sb, "%s\n\n", r.Explanation)
	}
	for i, h := range r.Hops {
		indent := strings.Repeat("  ", h.Depth)
		edge := ""
		if h.EdgeKind != "" {
			edge = fmt.Sprintf(" [%s]", h.EdgeKind)
		}
		fmt.Fprintf(&sb, "%s%d. `%s`%s — %s", indent, i+1, h.Qualified, edge, h.FilePath)
		if h.StartLine > 0 {
			fmt.Fprintf(&sb, ":%d", h.StartLine)
		}
		if h.CallExpression != "" {
			fmt.Fprintf(&sb, "  ← `%s`", h.CallExpression)
		}
		sb.WriteString("\n")
		if h.CallSiteExcerpt != "" {
			fmt.Fprintf(&sb, "%s   ```\n", indent)
			for _, line := range strings.Split(h.CallSiteExcerpt, "\n") {
				fmt.Fprintf(&sb, "%s   %s\n", indent, line)
			}
			fmt.Fprintf(&sb, "%s   ```\n", indent)
		}
	}
	fmt.Fprintf(&sb, "\n_Timing: search=%dms graph=%dms total=%dms_\n",
		r.Timing.SearchMS, r.Timing.GraphMS, r.Timing.TotalMS)
	return sb.String()
}

// blastMarkdown formats a BlastToolResult as Markdown.
func blastMarkdown(r BlastToolResult) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "## Blast: `%s` — %d affected\n\n", r.Target.Qualified, len(r.Affected))
	if r.Summary != "" {
		fmt.Fprintf(&sb, "%s\n\n", r.Summary)
	}
	for i, a := range r.Affected {
		fmt.Fprintf(&sb, "%d. `%s` (hop %d) — %s", i+1, a.Qualified, a.HopCount, a.FilePath)
		if a.CallExpression != "" {
			fmt.Fprintf(&sb, "  ← `%s`", a.CallExpression)
		}
		sb.WriteString("\n")
		if a.CallSiteExcerpt != "" {
			sb.WriteString("   ```\n")
			for _, line := range strings.Split(a.CallSiteExcerpt, "\n") {
				fmt.Fprintf(&sb, "   %s\n", line)
			}
			sb.WriteString("   ```\n")
		}
	}
	fmt.Fprintf(&sb, "\n_Timing: search=%dms graph=%dms total=%dms_\n",
		r.Timing.SearchMS, r.Timing.GraphMS, r.Timing.TotalMS)
	return sb.String()
}

// fieldFlowMarkdown formats a FieldFlowToolResult as Markdown.
func fieldFlowMarkdown(r FieldFlowToolResult) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "## Field Flow: `%s` (%s, %d chains)\n\n",
		r.Root.Qualified, r.Direction, len(r.Chains))
	if r.Explanation != "" {
		fmt.Fprintf(&sb, "%s\n\n", r.Explanation)
	}
	for i, c := range r.Chains {
		fmt.Fprintf(&sb, "### Chain %d: `%s` (%d hops, %d mutations",
			i+1, c.FieldPath, len(c.Hops), len(c.Mutations))
		if c.HasTaintHop {
			fmt.Fprintf(&sb, ", taint at `%s`", c.TaintHopName)
		}
		sb.WriteString(")\n")
		for j, h := range c.Hops {
			indent := strings.Repeat("  ", h.Depth)
			fmt.Fprintf(&sb, "%s%d. `%s` — %s", indent, j+1, h.Qualified, h.FilePath)
			if h.StartLine > 0 {
				fmt.Fprintf(&sb, ":%d", h.StartLine)
			}
			if h.MutationType != "" && h.MutationType != "none" {
				fmt.Fprintf(&sb, "  ← MUTATION (%s): `%s`", h.MutationType, h.MutationExpr)
			}
			sb.WriteString("\n")
		}
	}
	fmt.Fprintf(&sb, "\n_Timing: total=%dms_\n", r.Timing.TotalMS)
	return sb.String()
}

// rootCauseMarkdown formats a RootCauseToolResult as Markdown.
func rootCauseMarkdown(r RootCauseToolResult) string {
	var sb strings.Builder
	if r.CommitHash != "" {
		fmt.Fprintf(&sb, "## Root Cause: `%s` (confidence %.2f)\n\n", r.CommitHash, r.Confidence)
		fmt.Fprintf(&sb, "**Commit:** %s\n", r.CommitMessage)
		if r.Author != "" {
			fmt.Fprintf(&sb, "**Author:** %s", r.Author)
			if r.Timestamp != "" {
				fmt.Fprintf(&sb, " · %s", r.Timestamp)
			}
			sb.WriteString("\n")
		}
		if r.Explanation != "" {
			fmt.Fprintf(&sb, "\n%s\n", r.Explanation)
		}
		if r.SuggestedFix != "" {
			fmt.Fprintf(&sb, "\n### Suggested Fix\n%s\n", r.SuggestedFix)
		}
	} else {
		sb.WriteString("## Root Cause: no single commit identified\n\n")
	}
	if len(r.SuspectCommits) > 0 {
		sb.WriteString("\n### Suspect Commits\n")
		for i, s := range r.SuspectCommits {
			fmt.Fprintf(&sb, "%d. `%s` (score %.3f) — %s\n", i+1, s.Hash, s.Score, s.Message)
		}
	}
	fmt.Fprintf(&sb, "\n_Timing: total=%dms_\n", r.Timing.TotalMS)
	return sb.String()
}
