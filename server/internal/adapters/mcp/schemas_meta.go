package mcp

import (
	"fmt"
	"strings"
	"time"

	"github.com/commit0-dev/commit0/pkg/types"
)

// indexStatusOut is the structured payload for commit0_index_status.
//
// We re-export types.IndexProgress under a stable adapter-layer name so the
// MCP wire schema is decoupled from any future field tweaks in pkg/types.
// The two are aliased today (no field rename) to keep the change diff small;
// switch to a converter when that ceases to hold.
type indexStatusOut = types.IndexProgress

// ---------------------------------------------------------------------------
// commit0_list_repos
// ---------------------------------------------------------------------------

// RepoOut is the MCP-level representation of a repository.
type RepoOut struct {
	Slug          string     `json:"slug"`
	Path          string     `json:"path,omitempty"`
	RemoteURL     string     `json:"remote_url,omitempty"`
	DefaultBranch string     `json:"default_branch,omitempty"`
	LastCommit    string     `json:"last_commit,omitempty"`
	Languages     []string   `json:"languages,omitempty"`
	CreatedAt     *time.Time `json:"created_at,omitempty"`
	LastIndexedAt *time.Time `json:"last_indexed_at,omitempty"`
}

// ListReposToolResult is the structured output of commit0_list_repos.
type ListReposToolResult struct {
	Repos []RepoOut `json:"repos"`
}

// repoOut converts a domain Repo into its MCP wire shape.
func repoOut(r types.Repo) RepoOut {
	out := RepoOut{
		Slug:          r.Slug,
		Path:          r.Path,
		RemoteURL:     r.RemoteURL,
		DefaultBranch: r.DefaultBranch,
		LastCommit:    r.LastCommit,
		Languages:     r.Languages,
		LastIndexedAt: r.LastIndexedAt,
	}
	if !r.CreatedAt.IsZero() {
		created := r.CreatedAt
		out.CreatedAt = &created
	}
	return out
}

// listReposMarkdown formats a ListReposToolResult as Markdown for the
// text content. Sorted-by-input order (the underlying graph ListRepos
// already returns repos in a stable order).
func listReposMarkdown(r ListReposToolResult) string {
	if len(r.Repos) == 0 {
		return "## Repositories\nNo repositories indexed.\n"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "## Repositories (%d)\n\n", len(r.Repos))
	for _, repo := range r.Repos {
		fmt.Fprintf(&b, "- `%s`", repo.Slug)
		if repo.DefaultBranch != "" {
			fmt.Fprintf(&b, " (branch: %s)", repo.DefaultBranch)
		}
		if len(repo.Languages) > 0 {
			fmt.Fprintf(&b, " — %s", strings.Join(repo.Languages, ", "))
		}
		if repo.LastIndexedAt != nil {
			fmt.Fprintf(&b, " · last indexed %s", repo.LastIndexedAt.Format(time.RFC3339))
		}
		b.WriteString("\n")
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// commit0_list_files
// ---------------------------------------------------------------------------

// FileNodeOut is the MCP-level representation of a file node.
// File-kind nodes do not carry a body; the heavy fields on CodeNode (Body,
// Embedding, Methods) are intentionally omitted from the wire shape.
type FileNodeOut struct {
	ID             string     `json:"id"`
	FilePath       string     `json:"file_path"`
	Language       string     `json:"language,omitempty"`
	RepoSlug       string     `json:"repo_slug,omitempty"`
	StartLine      int        `json:"start_line,omitempty"`
	EndLine        int        `json:"end_line,omitempty"`
	LastModifiedAt *time.Time `json:"last_modified_at,omitempty"`
}

// ListFilesToolResult is the structured output of commit0_list_files.
type ListFilesToolResult struct {
	RepoSlug   string        `json:"repo_slug"`
	PathPrefix string        `json:"path_prefix,omitempty"`
	Files      []FileNodeOut `json:"files"`
	Truncated  bool          `json:"truncated"`
	Limit      int           `json:"limit"`
}

// fileNodeOut converts a CodeNode into the file-listing wire shape.
func fileNodeOut(n types.CodeNode) FileNodeOut {
	return FileNodeOut{
		ID:             n.ID,
		FilePath:       n.FilePath,
		Language:       n.Language,
		RepoSlug:       n.RepoSlug,
		StartLine:      n.StartLine,
		EndLine:        n.EndLine,
		LastModifiedAt: n.LastModifiedAt,
	}
}

// listFilesMarkdown renders a ListFilesToolResult as a short Markdown
// summary suitable for the LLM consumer.
func listFilesMarkdown(r ListFilesToolResult) string {
	if len(r.Files) == 0 {
		var b strings.Builder
		fmt.Fprintf(&b, "## Files in `%s`\nNo file nodes match", r.RepoSlug)
		if r.PathPrefix != "" {
			fmt.Fprintf(&b, " prefix `%s`", r.PathPrefix)
		}
		b.WriteString(".\n")
		return b.String()
	}
	var b strings.Builder
	fmt.Fprintf(&b, "## Files in `%s` (%d", r.RepoSlug, len(r.Files))
	if r.Truncated {
		fmt.Fprintf(&b, ", truncated at limit %d", r.Limit)
	}
	b.WriteString(")\n\n")
	if r.PathPrefix != "" {
		fmt.Fprintf(&b, "_path_prefix:_ `%s`\n\n", r.PathPrefix)
	}
	for _, f := range r.Files {
		fmt.Fprintf(&b, "- `%s`", f.FilePath)
		if f.Language != "" {
			fmt.Fprintf(&b, " (%s)", f.Language)
		}
		b.WriteString("\n")
	}
	return b.String()
}
