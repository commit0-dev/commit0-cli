package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/commit0-dev/commit0/pkg/types"
	"github.com/commit0-dev/commit0/server/internal/domain"
)

// registerTestsTools adds the test/subject mapping tools to the server.
//
// Both tools compose over the existing `OpenCodeGraph.TraverseGraph` using the
// `tests` edge kind, which is already produced by CallLinker today (any call
// edge whose caller is in *_test.go or named Test*/Benchmark* gets reclassified
// from `calls` to `tests`).
func registerTestsTools(server *mcpsdk.Server, deps Deps, log *slog.Logger) {
	addCommit0TestsFor(server, deps, log)
	addCommit0SubjectsFor(server, deps, log)
}

// ---------------------------------------------------------------------------
// Tool: commit0_tests_for
// ---------------------------------------------------------------------------

// testsForInput is the typed input for commit0_tests_for.
type testsForInput struct {
	Qualified  string `json:"qualified"             jsonschema:"Qualified name of the production symbol (e.g. 'app.QueryService.Query')."`
	RepoSlug   string `json:"repo_slug"             jsonschema:"Indexed repository slug."`
	Depth      int    `json:"depth,omitempty"       jsonschema:"Max upstream depth on the 'tests' edge (1-10). Default 5."`
	DirectOnly bool   `json:"direct_only,omitempty" jsonschema:"If true, only return tests at hop=1 (direct callers). Default false."`
}

func addCommit0TestsFor(server *mcpsdk.Server, deps Deps, log *slog.Logger) {
	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name: "commit0_tests_for",
		Description: "List the test functions whose call graph reaches a given production symbol. " +
			"Walks the graph in reverse along the 'tests' edge kind (test caller → subject) " +
			"up to depth hops. Use this before pushing a change to know which tests to run, " +
			"or to verify that a critical function has any test coverage at all.",
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, input testsForInput) (*mcpsdk.CallToolResult, any, error) {
		if input.Qualified == "" {
			return toolError(domain.Validation("qualified is required")), nil, nil
		}
		if input.RepoSlug == "" {
			return toolError(domain.Validation("repo_slug is required")), nil, nil
		}

		graph, errResult := graphFromDeps(deps)
		if errResult != nil {
			return errResult, nil, nil
		}

		depth := input.Depth
		if depth <= 0 {
			depth = 5
		}
		if input.DirectOnly {
			depth = 1
		}

		node, err := graph.FindNode(ctx, input.RepoSlug, input.Qualified)
		if err != nil {
			var de types.DomainError
			if asDomainError(err, &de) && de.Code == types.ErrNotFound {
				return notFoundResult(input.Qualified), nil, nil
			}
			log.Warn("commit0_tests_for find failed", "qualified", input.Qualified, "err", err)
			return toolError(err), nil, nil
		}

		hops, err := graph.TraverseGraph(ctx, node.ID, []string{"tests"}, "reverse", depth)
		if err != nil {
			log.Warn("commit0_tests_for traverse failed", "node_id", node.ID, "err", err)
			return toolError(err), nil, nil
		}

		result := buildTestsForResult(*node, hops)
		return &mcpsdk.CallToolResult{
			Content: []mcpsdk.Content{
				&mcpsdk.TextContent{Text: testsForMarkdown(result)},
			},
			StructuredContent: result,
		}, nil, nil
	})
}

// ---------------------------------------------------------------------------
// Tool: commit0_subjects_for
// ---------------------------------------------------------------------------

// subjectsForInput is the typed input for commit0_subjects_for.
type subjectsForInput struct {
	TestQualified string `json:"test_qualified"  jsonschema:"Qualified name of the test function (e.g. 'app.TestQueryService_Query_BasicSearch')."`
	RepoSlug      string `json:"repo_slug"       jsonschema:"Indexed repository slug."`
	Depth         int    `json:"depth,omitempty" jsonschema:"Max forward depth (1-10). Default 5."`
}

func addCommit0SubjectsFor(server *mcpsdk.Server, deps Deps, log *slog.Logger) {
	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name: "commit0_subjects_for",
		Description: "List the production functions a given test function exercises. " +
			"Walks forward from the test along both 'tests' and 'calls' edges, then " +
			"filters out test-file nodes from the result. Use this to understand what a " +
			"flaky test actually covers, or to find the prod path responsible for a " +
			"regression surfaced by one specific test.",
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, input subjectsForInput) (*mcpsdk.CallToolResult, any, error) {
		if input.TestQualified == "" {
			return toolError(domain.Validation("test_qualified is required")), nil, nil
		}
		if input.RepoSlug == "" {
			return toolError(domain.Validation("repo_slug is required")), nil, nil
		}

		graph, errResult := graphFromDeps(deps)
		if errResult != nil {
			return errResult, nil, nil
		}

		depth := input.Depth
		if depth <= 0 {
			depth = 5
		}

		test, err := graph.FindNode(ctx, input.RepoSlug, input.TestQualified)
		if err != nil {
			var de types.DomainError
			if asDomainError(err, &de) && de.Code == types.ErrNotFound {
				return notFoundResult(input.TestQualified), nil, nil
			}
			log.Warn("commit0_subjects_for find failed", "qualified", input.TestQualified, "err", err)
			return toolError(err), nil, nil
		}

		hops, err := graph.TraverseGraph(ctx, test.ID, []string{"tests", "calls"}, "forward", depth)
		if err != nil {
			log.Warn("commit0_subjects_for traverse failed", "node_id", test.ID, "err", err)
			return toolError(err), nil, nil
		}

		result := buildSubjectsForResult(*test, hops)
		return &mcpsdk.CallToolResult{
			Content: []mcpsdk.Content{
				&mcpsdk.TextContent{Text: subjectsForMarkdown(result)},
			},
			StructuredContent: result,
		}, nil, nil
	})
}

// ---------------------------------------------------------------------------
// Result construction helpers
// ---------------------------------------------------------------------------

// TestRefOut is one test function reference returned by commit0_tests_for.
type TestRefOut struct {
	Qualified string `json:"qualified"`
	FilePath  string `json:"file_path"`
	StartLine int    `json:"start_line,omitempty"`
	HopCount  int    `json:"hop_count"`
}

// TestsForToolResult is the structured output of commit0_tests_for.
type TestsForToolResult struct {
	Subject CodeNodeOut  `json:"subject"`
	Tests   []TestRefOut `json:"tests"`
	Total   int          `json:"total"`
}

// SubjectRefOut is one production function reference returned by commit0_subjects_for.
type SubjectRefOut struct {
	Qualified string `json:"qualified"`
	Kind      string `json:"kind"`
	FilePath  string `json:"file_path"`
	StartLine int    `json:"start_line,omitempty"`
	HopCount  int    `json:"hop_count"`
}

// SubjectsForToolResult is the structured output of commit0_subjects_for.
type SubjectsForToolResult struct {
	Test     CodeNodeOut     `json:"test"`
	Subjects []SubjectRefOut `json:"subjects"`
	Total    int             `json:"total"`
}

// buildTestsForResult collects the unique reverse-hops as test references.
// Dedupes by qualified name and keeps the smallest hop count seen.
func buildTestsForResult(subject types.CodeNode, hops []types.TraceHop) TestsForToolResult {
	byQualified := make(map[string]TestRefOut)
	for _, h := range hops {
		if h.Node.Qualified == "" || h.Node.Qualified == subject.Qualified {
			continue
		}
		// Defensive: only keep callers that look like tests. With the `tests`
		// edge filter on the traversal, this should always be true, but
		// callers in mixed call/tests chains may slip through.
		if !looksLikeTest(h.Node) {
			continue
		}
		ref := TestRefOut{
			Qualified: h.Node.Qualified,
			FilePath:  h.Node.FilePath,
			StartLine: h.Node.StartLine,
			HopCount:  h.Depth,
		}
		if existing, ok := byQualified[ref.Qualified]; !ok || ref.HopCount < existing.HopCount {
			byQualified[ref.Qualified] = ref
		}
	}
	tests := make([]TestRefOut, 0, len(byQualified))
	for _, t := range byQualified {
		tests = append(tests, t)
	}
	sort.SliceStable(tests, func(i, j int) bool {
		if tests[i].HopCount != tests[j].HopCount {
			return tests[i].HopCount < tests[j].HopCount
		}
		return tests[i].Qualified < tests[j].Qualified
	})
	return TestsForToolResult{
		Subject: codeNodeOut(subject, false),
		Tests:   tests,
		Total:   len(tests),
	}
}

// buildSubjectsForResult collects unique forward-hops as subject references,
// excluding the test itself and any node living in a *_test.go file.
func buildSubjectsForResult(test types.CodeNode, hops []types.TraceHop) SubjectsForToolResult {
	byQualified := make(map[string]SubjectRefOut)
	for _, h := range hops {
		if h.Node.Qualified == "" || h.Node.Qualified == test.Qualified {
			continue
		}
		if looksLikeTest(h.Node) {
			continue
		}
		ref := SubjectRefOut{
			Qualified: h.Node.Qualified,
			Kind:      string(h.Node.Kind),
			FilePath:  h.Node.FilePath,
			StartLine: h.Node.StartLine,
			HopCount:  h.Depth,
		}
		if existing, ok := byQualified[ref.Qualified]; !ok || ref.HopCount < existing.HopCount {
			byQualified[ref.Qualified] = ref
		}
	}
	subjects := make([]SubjectRefOut, 0, len(byQualified))
	for _, s := range byQualified {
		subjects = append(subjects, s)
	}
	sort.SliceStable(subjects, func(i, j int) bool {
		if subjects[i].HopCount != subjects[j].HopCount {
			return subjects[i].HopCount < subjects[j].HopCount
		}
		return subjects[i].Qualified < subjects[j].Qualified
	})
	return SubjectsForToolResult{
		Test:     codeNodeOut(test, false),
		Subjects: subjects,
		Total:    len(subjects),
	}
}

// looksLikeTest returns true if the node lives in a test file or is named
// like a Go test/benchmark function. Mirrors CallLinker.classifyCallKind.
func looksLikeTest(n types.CodeNode) bool {
	if strings.Contains(n.FilePath, "_test.go") || strings.Contains(n.FilePath, "_test.") {
		return true
	}
	if strings.HasPrefix(n.Name, "Test") || strings.HasPrefix(n.Name, "Benchmark") {
		return true
	}
	return false
}

// notFoundResult returns a tool result for the "node not found" case.
// The MCP spec encourages returning IsError=false with a human-readable
// "no results" message rather than a hard error for this kind of lookup miss.
func notFoundResult(qualified string) *mcpsdk.CallToolResult {
	msg := fmt.Sprintf("No node found for %q. Verify the qualified name and that the repo is indexed.", qualified)
	return &mcpsdk.CallToolResult{
		Content: []mcpsdk.Content{
			&mcpsdk.TextContent{Text: msg},
		},
	}
}

// ---------------------------------------------------------------------------
// Markdown formatters
// ---------------------------------------------------------------------------

func testsForMarkdown(r TestsForToolResult) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "## Tests for `%s` (%d test(s))\n\n", r.Subject.Qualified, r.Total)
	if r.Total == 0 {
		sb.WriteString("_No tests found in the indexed graph reach this symbol._\n")
		return sb.String()
	}
	for i, t := range r.Tests {
		fmt.Fprintf(&sb, "%d. `%s` (hop %d) — %s", i+1, t.Qualified, t.HopCount, t.FilePath)
		if t.StartLine > 0 {
			fmt.Fprintf(&sb, ":%d", t.StartLine)
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

func subjectsForMarkdown(r SubjectsForToolResult) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "## Subjects exercised by `%s` (%d subject(s))\n\n", r.Test.Qualified, r.Total)
	if r.Total == 0 {
		sb.WriteString("_This test does not call any indexed production symbols._\n")
		return sb.String()
	}
	for i, s := range r.Subjects {
		fmt.Fprintf(&sb, "%d. `%s` (%s, hop %d) — %s", i+1, s.Qualified, s.Kind, s.HopCount, s.FilePath)
		if s.StartLine > 0 {
			fmt.Fprintf(&sb, ":%d", s.StartLine)
		}
		sb.WriteString("\n")
	}
	return sb.String()
}
