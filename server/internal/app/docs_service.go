package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/commit0-dev/commit0/server/internal/domain"
	"github.com/commit0-dev/commit0/pkg/types"
)

// DocsRequest configures documentation generation.
type DocsRequest struct {
	RepoSlug string
	Sections []string // optional: which docs to generate ("readme", "architecture", "api", "modules")
}

// GeneratedDoc is a single generated documentation file.
type GeneratedDoc struct {
	Title    string `json:"title"`
	Filename string `json:"filename"`
	Content  string `json:"content"` // Markdown
}

// DocsResult contains all generated documentation.
type DocsResult struct {
	Documents []GeneratedDoc `json:"documents"`
	Timing    types.TimingInfo
}

// DocsService generates project documentation from the code graph + LLM.
type DocsService struct {
	graph     domain.OpenCodeGraph
	querySvc  *QueryService
	explainer domain.LLMExplainer
	log       *slog.Logger
}

// NewDocsService creates a documentation generator.
func NewDocsService(
	graph domain.OpenCodeGraph,
	querySvc *QueryService,
	explainer domain.LLMExplainer,
) *DocsService {
	return &DocsService{
		graph:     graph,
		querySvc:  querySvc,
		explainer: explainer,
		log:       slog.Default().With("service", "docs"),
	}
}

// Generate creates documentation for the project.
func (s *DocsService) Generate(ctx context.Context, req DocsRequest) (*DocsResult, error) {
	startTime := time.Now()
	s.log.Info("generating documentation", "repo", req.RepoSlug)

	sections := req.Sections
	if len(sections) == 0 {
		sections = []string{"readme", "architecture", "api"}
	}

	var docs []GeneratedDoc

	for _, section := range sections {
		switch section {
		case "readme":
			doc, err := s.generateREADME(ctx, req.RepoSlug)
			if err != nil {
				s.log.Warn("readme generation failed", "err", err)
				continue
			}
			docs = append(docs, *doc)

		case "architecture":
			doc, err := s.generateArchitecture(ctx, req.RepoSlug)
			if err != nil {
				s.log.Warn("architecture doc failed", "err", err)
				continue
			}
			docs = append(docs, *doc)

		case "api":
			doc, err := s.generateAPI(ctx, req.RepoSlug)
			if err != nil {
				s.log.Warn("api doc failed", "err", err)
				continue
			}
			docs = append(docs, *doc)
		}
	}

	return &DocsResult{
		Documents: docs,
		Timing: types.TimingInfo{
			TotalMS: time.Since(startTime).Milliseconds(),
		},
	}, nil
}

// generateREADME creates a project overview.
func (s *DocsService) generateREADME(ctx context.Context, repoSlug string) (*GeneratedDoc, error) {
	// Get repo info
	repo, err := s.graph.GetRepo(ctx, repoSlug)
	if err != nil {
		return nil, err
	}

	// Count nodes by type
	nodeIDs, _ := s.graph.ListNodes(ctx, repoSlug, domain.ListOpts{IDsOnly: true})

	// Get top functions by centrality (via query)
	topFunctions, _ := s.querySvc.Query(ctx, QueryRequest{
		Question: "main entry points and key functions",
		RepoSlug: repoSlug,
		TopK:     10,
	})

	// Build context for LLM
	var context_str strings.Builder
	fmt.Fprintf(&context_str, "Project: %s\n", repo.Slug)
	fmt.Fprintf(&context_str, "Path: %s\n", repo.Path)
	fmt.Fprintf(&context_str, "Languages: %s\n", strings.Join(repo.Languages, ", "))
	fmt.Fprintf(&context_str, "Total nodes: %d\n\n", len(nodeIDs))

	if topFunctions != nil {
		context_str.WriteString("Key functions:\n")
		for _, n := range topFunctions.Nodes {
			fmt.Fprintf(&context_str, "- %s (%s:%d) — %s\n",
				n.Node.Qualified, n.Node.FilePath, n.Node.StartLine, n.Node.Summary)
		}
	}

	// Generate with LLM
	if s.explainer == nil {
		return &GeneratedDoc{
			Title:    "README",
			Filename: "README.md",
			Content:  fmt.Sprintf("# %s\n\n%s\n", repo.Slug, context_str.String()),
		}, nil
	}

	raw, err := s.explainer.ExplainStructured(ctx, domain.ExplainRequest{
		QueryType: "search",
		UserQuery: fmt.Sprintf("Generate a README.md for this project. Include: overview, key features, architecture summary, getting started.\n\n%s", context_str.String()),
	})
	if err != nil {
		return nil, err
	}

	var result struct {
		Overview string   `json:"overview"`
		Insights []string `json:"insights"`
	}
	if json.Unmarshal(raw, &result) == nil && result.Overview != "" {
		content := fmt.Sprintf("# %s\n\n%s\n", repo.Slug, result.Overview)
		if len(result.Insights) > 0 {
			content += "\n## Key Points\n\n"
			for _, insight := range result.Insights {
				content += fmt.Sprintf("- %s\n", insight)
			}
		}
		return &GeneratedDoc{Title: "README", Filename: "README.md", Content: content}, nil
	}

	return &GeneratedDoc{Title: "README", Filename: "README.md", Content: string(raw)}, nil
}

// generateArchitecture creates an architecture overview with module dependency diagram.
func (s *DocsService) generateArchitecture(ctx context.Context, repoSlug string) (*GeneratedDoc, error) {
	// Get all file nodes to understand module structure
	nodeIDs, err := s.graph.ListNodes(ctx, repoSlug, domain.ListOpts{IDsOnly: true})
	if err != nil {
		return nil, err
	}

	// Group files by directory (module)
	modules := make(map[string]int) // dir → function count
	for _, id := range nodeIDs {
		node, err := s.graph.GetNode(ctx, id.ID)
		if err != nil || node == nil {
			continue
		}
		dir := dirFromPath(node.FilePath)
		modules[dir]++
	}

	// Build Mermaid diagram
	var mermaid strings.Builder
	mermaid.WriteString("```mermaid\ngraph TD\n")

	// Sort modules by function count (most important first)
	type moduleInfo struct {
		name  string
		count int
	}
	var sorted []moduleInfo
	for name, count := range modules {
		sorted = append(sorted, moduleInfo{name, count})
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].count > sorted[j].count })

	for i, m := range sorted {
		if i >= 15 { // limit to top 15 modules
			break
		}
		safeID := strings.ReplaceAll(m.name, "/", "_")
		safeID = strings.ReplaceAll(safeID, ".", "_")
		fmt.Fprintf(&mermaid, "    %s[\"%s (%d)\"]\n", safeID, m.name, m.count)
	}
	mermaid.WriteString("```\n")

	content := fmt.Sprintf("# Architecture\n\n## Module Structure\n\n%s\n\n", mermaid.String())
	content += fmt.Sprintf("Total modules: %d\nTotal nodes: %d\n", len(modules), len(nodeIDs))

	return &GeneratedDoc{
		Title:    "Architecture",
		Filename: "ARCHITECTURE.md",
		Content:  content,
	}, nil
}

// generateAPI creates an API reference from exported functions.
func (s *DocsService) generateAPI(ctx context.Context, repoSlug string) (*GeneratedDoc, error) {
	nodeIDs, err := s.graph.ListNodes(ctx, repoSlug, domain.ListOpts{IDsOnly: true})
	if err != nil {
		return nil, err
	}

	var content strings.Builder
	content.WriteString("# API Reference\n\n")

	// Group by file
	fileGroups := make(map[string][]types.CodeNode)
	for _, id := range nodeIDs {
		node, err := s.graph.GetNode(ctx, id.ID)
		if err != nil || node == nil {
			continue
		}
		if node.Kind != types.NodeFunction || node.Visibility != "public" {
			continue
		}
		fileGroups[node.FilePath] = append(fileGroups[node.FilePath], *node)
	}

	for file, nodes := range fileGroups {
		fmt.Fprintf(&content, "## %s\n\n", file)
		for _, node := range nodes {
			fmt.Fprintf(&content, "### `%s`\n\n", node.Qualified)
			if node.Signature != "" {
				fmt.Fprintf(&content, "```\n%s\n```\n\n", node.Signature)
			}
			if node.Summary != "" {
				fmt.Fprintf(&content, "%s\n\n", node.Summary)
			} else if node.Docstring != "" {
				fmt.Fprintf(&content, "%s\n\n", node.Docstring)
			}
		}
	}

	return &GeneratedDoc{
		Title:    "API Reference",
		Filename: "API.md",
		Content:  content.String(),
	}, nil
}

func dirFromPath(path string) string {
	lastSlash := strings.LastIndex(path, "/")
	if lastSlash < 0 {
		return "."
	}
	return path[:lastSlash]
}
