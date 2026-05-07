// Package mcp provides the MCP (Model Context Protocol) server adapter for commit0.
// It exposes code intelligence services as MCP tools accessible to Claude Code,
// Cursor, Cline, and any other MCP-aware client.
//
// Stdout is reserved for JSON-RPC framing — no fmt.Print* or os.Stdout writes.
// All logging goes to slog (stderr).
package mcp

import (
	"fmt"
	"strings"

	"github.com/commit0-dev/commit0/pkg/types"
)

// ---------------------------------------------------------------------------
// Output structs — wire format for MCP tool responses.
// json tags produce the JSON shape; jsonschema tags generate the outputSchema.
// ---------------------------------------------------------------------------

// TimingOut serializes TimingInfo for MCP output.
type TimingOut struct {
	EmbedMS   int64 `json:"embed_ms"`
	SearchMS  int64 `json:"search_ms"`
	GraphMS   int64 `json:"graph_ms"`
	ExplainMS int64 `json:"explain_ms"`
	TotalMS   int64 `json:"total_ms"`
}

// timingOut converts a domain TimingInfo to its output representation.
func timingOut(t types.TimingInfo) TimingOut {
	return TimingOut{
		EmbedMS:   t.EmbedMS,
		SearchMS:  t.SearchMS,
		GraphMS:   t.GraphMS,
		ExplainMS: t.ExplainMS,
		TotalMS:   t.TotalMS,
	}
}

// CodeNodeOut is the MCP-level representation of a code node.
// Body is omitted on list results to reduce token cost; commit0_show_node returns it.
type CodeNodeOut struct {
	ID        string   `json:"id"`
	Kind      string   `json:"kind"`
	Qualified string   `json:"qualified"`
	Name      string   `json:"name"`
	FilePath  string   `json:"file_path"`
	RepoSlug  string   `json:"repo_slug"`
	Language  string   `json:"language,omitempty"`
	Signature string   `json:"signature,omitempty"`
	Docstring string   `json:"docstring,omitempty"`
	Body      string   `json:"body,omitempty"`
	StartLine int      `json:"start_line,omitempty"`
	EndLine   int      `json:"end_line,omitempty"`
	Concepts  []string `json:"concepts,omitempty"`
}

// codeNodeOut converts a domain CodeNode to its output representation.
// withBody controls whether the Body field is populated.
func codeNodeOut(n types.CodeNode, withBody bool) CodeNodeOut {
	out := CodeNodeOut{
		ID:        n.ID,
		Kind:      string(n.Kind),
		Qualified: n.Qualified,
		Name:      n.Name,
		FilePath:  n.FilePath,
		RepoSlug:  n.RepoSlug,
		Language:  n.Language,
		Signature: n.Signature,
		Docstring: n.Docstring,
		StartLine: n.StartLine,
		EndLine:   n.EndLine,
		Concepts:  n.Concepts,
	}
	if withBody {
		out.Body = n.Body
	}
	return out
}

// ScoredNodeOut extends CodeNodeOut with relevance scores for query results.
type ScoredNodeOut struct {
	CodeNodeOut
	Score float64 `json:"score"`
}

// scoredNodeOut converts a domain ScoredNode to its output representation.
func scoredNodeOut(sn types.ScoredNode) ScoredNodeOut {
	return ScoredNodeOut{
		CodeNodeOut: codeNodeOut(sn.Node, false),
		Score:       sn.FusedScore,
	}
}

// NeighborOut is a lightweight reference to a neighboring node.
type NeighborOut struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Qualified string `json:"qualified"`
	FilePath  string `json:"file_path"`
}

// QueryToolResult is the structured output of commit0_query.
type QueryToolResult struct {
	Nodes       []ScoredNodeOut `json:"nodes"`
	Explanation string          `json:"explanation,omitempty"`
	RepoSlug    string          `json:"repo_slug"`
	Timing      TimingOut       `json:"timing"`
}

// LookupToolResult is the structured output of commit0_lookup.
type LookupToolResult struct {
	Node *CodeNodeOut `json:"node"`
}

// NeighborhoodToolResult is the structured output of commit0_neighborhood.
type NeighborhoodToolResult struct {
	Callers     []NeighborOut `json:"callers"`
	Callees     []NeighborOut `json:"callees"`
	DataSources []NeighborOut `json:"data_sources"`
	DataSinks   []NeighborOut `json:"data_sinks"`
	Reads       []string      `json:"reads"`
	Writes      []string      `json:"writes"`
}

// ShowNodeToolResult is the structured output of commit0_show_node.
type ShowNodeToolResult struct {
	Node *CodeNodeOut `json:"node"`
}

// ---------------------------------------------------------------------------
// Markdown summary helpers
// ---------------------------------------------------------------------------

// queryMarkdown formats a QueryToolResult as human-readable Markdown for the
// text content item. This satisfies the MCP spec recommendation of dual output.
func queryMarkdown(r QueryToolResult) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "## Query Results (%d nodes, repo: %s)\n\n", len(r.Nodes), r.RepoSlug)
	if r.Explanation != "" {
		fmt.Fprintf(&sb, "%s\n\n", r.Explanation)
	}
	for i, n := range r.Nodes {
		fmt.Fprintf(&sb, "### %d. `%s` (score: %.3f)\n", i+1, n.Qualified, n.Score)
		fmt.Fprintf(&sb, "- **Kind:** %s\n- **File:** %s", n.Kind, n.FilePath)
		if n.StartLine > 0 {
			fmt.Fprintf(&sb, ":%d", n.StartLine)
		}
		sb.WriteString("\n")
		if n.Signature != "" {
			fmt.Fprintf(&sb, "- **Signature:** `%s`\n", n.Signature)
		}
	}
	fmt.Fprintf(&sb, "\n_Timing: embed=%dms search=%dms total=%dms_\n",
		r.Timing.EmbedMS, r.Timing.SearchMS, r.Timing.TotalMS)
	return sb.String()
}

// lookupMarkdown formats a LookupToolResult as Markdown.
func lookupMarkdown(r LookupToolResult) string {
	if r.Node == nil {
		return "## Lookup\nNode not found.\n"
	}
	n := r.Node
	var sb strings.Builder
	fmt.Fprintf(&sb, "## Lookup: `%s`\n\n", n.Qualified)
	fmt.Fprintf(&sb, "- **Kind:** %s\n- **File:** %s", n.Kind, n.FilePath)
	if n.StartLine > 0 {
		fmt.Fprintf(&sb, ":%d-%d", n.StartLine, n.EndLine)
	}
	sb.WriteString("\n")
	if n.Language != "" {
		fmt.Fprintf(&sb, "- **Language:** %s\n", n.Language)
	}
	if n.Signature != "" {
		fmt.Fprintf(&sb, "\n**Signature:**\n```\n%s\n```\n", n.Signature)
	}
	if n.Docstring != "" {
		fmt.Fprintf(&sb, "\n**Docstring:** %s\n", n.Docstring)
	}
	return sb.String()
}

// neighborhoodMarkdown formats a NeighborhoodToolResult as Markdown.
func neighborhoodMarkdown(r NeighborhoodToolResult) string {
	var sb strings.Builder
	sb.WriteString("## Neighborhood\n\n")
	printNeighbors := func(label string, ns []NeighborOut) {
		if len(ns) == 0 {
			return
		}
		fmt.Fprintf(&sb, "### %s\n", label)
		for _, n := range ns {
			fmt.Fprintf(&sb, "- `%s` (%s) — %s\n", n.Qualified, n.Kind, n.FilePath)
		}
	}
	printNeighbors("Callers", r.Callers)
	printNeighbors("Callees", r.Callees)
	printNeighbors("Data Sources", r.DataSources)
	printNeighbors("Data Sinks", r.DataSinks)
	if len(r.Reads) > 0 {
		fmt.Fprintf(&sb, "### Reads\n%s\n", strings.Join(r.Reads, ", "))
	}
	if len(r.Writes) > 0 {
		fmt.Fprintf(&sb, "### Writes\n%s\n", strings.Join(r.Writes, ", "))
	}
	return sb.String()
}

// showNodeMarkdown formats a ShowNodeToolResult as Markdown.
func showNodeMarkdown(r ShowNodeToolResult) string {
	if r.Node == nil {
		return "## Show Node\nNode not found.\n"
	}
	n := r.Node
	var sb strings.Builder
	fmt.Fprintf(&sb, "## `%s`\n\n", n.Qualified)
	fmt.Fprintf(&sb, "- **Kind:** %s\n- **File:** %s", n.Kind, n.FilePath)
	if n.StartLine > 0 {
		fmt.Fprintf(&sb, ":%d-%d", n.StartLine, n.EndLine)
	}
	sb.WriteString("\n")
	if n.Signature != "" {
		fmt.Fprintf(&sb, "\n**Signature:**\n```\n%s\n```\n", n.Signature)
	}
	if n.Docstring != "" {
		fmt.Fprintf(&sb, "\n**Docstring:** %s\n", n.Docstring)
	}
	if n.Body != "" {
		lang := n.Language
		if lang == "" {
			lang = "go"
		}
		fmt.Fprintf(&sb, "\n**Body:**\n```%s\n%s\n```\n", lang, n.Body)
	}
	return sb.String()
}
