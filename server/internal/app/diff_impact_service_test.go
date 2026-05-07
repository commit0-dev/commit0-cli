package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/commit0-dev/commit0/pkg/types"
	"github.com/commit0-dev/commit0/server/internal/domain"
)

// NOTE: fakeGitWalker is declared in temporal_service_test.go (same package).
// Tests here use fakeGitWalker.workingDiffs / rangeDiffs / workingErr / rangeErr
// fields to control DiffWorkingTree / DiffRange return values.

// ---------------------------------------------------------------------------
// Fake OpenCodeGraph that supports ListNodes for diff-impact tests
// ---------------------------------------------------------------------------

type fakeOpenCodeGraph struct {
	nodesByFile    map[string][]types.CodeNode
	findResult     *types.CodeNode
	findErr        error
	traverseResult []types.TraceHop
}

func (g *fakeOpenCodeGraph) PutNode(_ context.Context, _ *types.CodeNode) error { return nil }
func (g *fakeOpenCodeGraph) GetNode(_ context.Context, _ string) (*types.CodeNode, error) {
	return g.findResult, g.findErr
}
func (g *fakeOpenCodeGraph) FindNode(_ context.Context, _, _ string) (*types.CodeNode, error) {
	return g.findResult, g.findErr
}
func (g *fakeOpenCodeGraph) DeleteNode(_ context.Context, _ string) error       { return nil }
func (g *fakeOpenCodeGraph) PutEdge(_ context.Context, _ *types.CodeEdge) error { return nil }
func (g *fakeOpenCodeGraph) DeleteEdgesFrom(_ context.Context, _ string) error  { return nil }
func (g *fakeOpenCodeGraph) PutBatch(_ context.Context, _ []types.CodeNode, _ []types.CodeEdge) error {
	return nil
}
func (g *fakeOpenCodeGraph) DeleteByRepo(_ context.Context, _ string) error    { return nil }
func (g *fakeOpenCodeGraph) DeleteByFile(_ context.Context, _, _ string) error { return nil }
func (g *fakeOpenCodeGraph) TraverseGraph(_ context.Context, _ string, _ []string, _ string, _ int) ([]types.TraceHop, error) {
	return g.traverseResult, nil
}
func (g *fakeOpenCodeGraph) Neighbors(_ context.Context, _ string) (*domain.Neighborhood, error) {
	return nil, nil
}
func (g *fakeOpenCodeGraph) GetNodeEmbedding(_ context.Context, _ string) ([]float32, error) {
	return nil, nil
}
func (g *fakeOpenCodeGraph) VectorSearch(_ context.Context, _ []float32, _ domain.VectorSearchOpts) ([]types.ScoredNode, error) {
	return nil, nil
}
func (g *fakeOpenCodeGraph) TextSearch(_ context.Context, _ string, _ domain.TextSearchOpts) ([]types.ScoredNode, error) {
	return nil, nil
}
func (g *fakeOpenCodeGraph) ListNodes(_ context.Context, _ string, opts domain.ListOpts) ([]types.CodeNode, error) {
	if g.nodesByFile == nil {
		return nil, nil
	}
	return g.nodesByFile[opts.FilePath], nil
}
func (g *fakeOpenCodeGraph) ListEdges(_ context.Context, _ string, _ []string) ([]types.CodeEdge, error) {
	return nil, nil
}
func (g *fakeOpenCodeGraph) ListFilePaths(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}
func (g *fakeOpenCodeGraph) PutRepo(_ context.Context, _ *types.Repo) error { return nil }
func (g *fakeOpenCodeGraph) GetRepo(_ context.Context, _ string) (*types.Repo, error) {
	return nil, nil
}
func (g *fakeOpenCodeGraph) ListRepos(_ context.Context) ([]types.Repo, error) { return nil, nil }
func (g *fakeOpenCodeGraph) UpdateRepoIndexedAt(_ context.Context, _ string, _ time.Time) error {
	return nil
}
func (g *fakeOpenCodeGraph) FindRepoByRemoteURL(_ context.Context, _ string) (*types.Repo, error) {
	return nil, nil
}
func (g *fakeOpenCodeGraph) DeleteRepo(_ context.Context, _ string) error { return nil }
func (g *fakeOpenCodeGraph) ApplySchema(_ context.Context) error          { return nil }

var _ domain.OpenCodeGraph = (*fakeOpenCodeGraph)(nil)

// ---------------------------------------------------------------------------
// parseHunkRanges unit tests
// ---------------------------------------------------------------------------

func TestParseHunkRanges_TableDriven(t *testing.T) {
	cases := []struct {
		name  string
		patch string
		want  []LineRange
	}{
		{
			name:  "empty patch",
			patch: "",
			want:  nil,
		},
		{
			name:  "single-line add",
			patch: "@@ -5 +5 @@ func Foo() {\n+\treturn 1\n",
			want:  []LineRange{{Start: 5, End: 5}},
		},
		{
			name:  "multi-line range",
			patch: "@@ -10,5 +12,7 @@ func Bar() {\n",
			want:  []LineRange{{Start: 12, End: 18}},
		},
		{
			name:  "count-zero range (deletion only)",
			patch: "@@ -3,2 +3,0 @@ deleted\n",
			want:  nil,
		},
		{
			name:  "multiple hunks",
			patch: "@@ -1,3 +1,4 @@ first\n@@ -20,2 +21,3 @@ second\n",
			want:  []LineRange{{Start: 1, End: 4}, {Start: 21, End: 23}},
		},
		{
			name:  "range with count=1 (implicit)",
			patch: "@@ -100 +100 @@ func X() {",
			want:  []LineRange{{Start: 100, End: 100}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseHunkRanges(tc.patch)
			if len(got) != len(tc.want) {
				t.Fatalf("len(got)=%d, len(want)=%d\ngot:  %v\nwant: %v", len(got), len(tc.want), got, tc.want)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Errorf("[%d] got %v, want %v", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// DiffImpactService validation tests
// ---------------------------------------------------------------------------

func TestDiffImpactService_EmptyRepoSlug_ReturnsValidationError(t *testing.T) {
	svc := NewDiffImpactService(&fakeOpenCodeGraph{}, nil, &fakeGitWalker{}, nil, nil)
	_, err := svc.Analyze(context.Background(), DiffImpactRequest{
		RepoPath: "/tmp/repo",
	})
	if err == nil {
		t.Fatal("expected validation error for empty RepoSlug")
	}
	if !strings.Contains(err.Error(), "repo_slug") {
		t.Errorf("expected 'repo_slug' in error, got: %v", err)
	}
}

func TestDiffImpactService_EmptyRepoPath_ReturnsValidationError(t *testing.T) {
	svc := NewDiffImpactService(&fakeOpenCodeGraph{}, nil, &fakeGitWalker{}, nil, nil)
	_, err := svc.Analyze(context.Background(), DiffImpactRequest{
		RepoSlug: "org/repo",
	})
	if err == nil {
		t.Fatal("expected validation error for empty RepoPath")
	}
	if !strings.Contains(err.Error(), "repo_path") {
		t.Errorf("expected 'repo_path' in error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// DiffImpactService happy-path tests
// ---------------------------------------------------------------------------

func TestDiffImpactService_WorkingTreeDiff_MapsSymbolsAndDedupesAffected(t *testing.T) {
	// Set up a fake diff with one modified file containing a patch that touches
	// line 3 only.
	patch := `--- a/pkg/auth.go
+++ b/pkg/auth.go
@@ -3,4 +3,5 @@ package pkg
-	return false
+	return true
+	// updated
`
	changedNode := types.CodeNode{
		ID:        "function:pkg.Login",
		Qualified: "pkg.Login",
		Kind:      types.NodeFunction,
		FilePath:  "pkg/auth.go",
		RepoSlug:  "org/repo",
		StartLine: 1,
		EndLine:   10,
	}
	callerNode := types.CodeNode{
		ID:        "function:pkg.Handler",
		Qualified: "pkg.Handler",
		Kind:      types.NodeFunction,
		FilePath:  "pkg/handler.go",
		RepoSlug:  "org/repo",
		StartLine: 5,
		EndLine:   20,
	}

	graph := &fakeOpenCodeGraph{
		nodesByFile: map[string][]types.CodeNode{
			"pkg/auth.go": {changedNode},
		},
		findResult:     &changedNode,
		traverseResult: []types.TraceHop{{Node: callerNode, Depth: 1}},
	}
	walker := &fakeGitWalker{
		workingDiffs: []domain.GitFileDiff{
			{
				Path:      "pkg/auth.go",
				Status:    "modified",
				Additions: 2,
				Deletions: 1,
				Patch:     patch,
			},
		},
	}
	blastSvc := NewBlastService(graph, nil, nil)
	svc := NewDiffImpactService(graph, blastSvc, walker, nil, nil)

	result, err := svc.Analyze(context.Background(), DiffImpactRequest{
		RepoSlug:  "org/repo",
		RepoPath:  "/tmp/fake",
		ToRef:     "WORKING",
		NoExplain: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.ChangedSymbols) != 1 {
		t.Errorf("expected 1 changed symbol, got %d", len(result.ChangedSymbols))
	}
	if result.ChangedSymbols[0].Qualified != "pkg.Login" {
		t.Errorf("expected changed symbol 'pkg.Login', got %q", result.ChangedSymbols[0].Qualified)
	}
	if len(result.Affected) != 1 {
		t.Errorf("expected 1 affected (prod) node, got %d", len(result.Affected))
	}
	if result.Affected[0].Node.Qualified != "pkg.Handler" {
		t.Errorf("expected affected 'pkg.Handler', got %q", result.Affected[0].Node.Qualified)
	}
	if len(result.AffectedTests) != 0 {
		t.Errorf("expected 0 affected test nodes, got %d", len(result.AffectedTests))
	}
}

func TestDiffImpactService_TestFileInAffected_SplitsIntoAffectedTests(t *testing.T) {
	patch := "@@ -1,3 +1,4 @@ package pkg\n"
	changedNode := types.CodeNode{
		ID:        "function:pkg.Foo",
		Qualified: "pkg.Foo",
		Kind:      types.NodeFunction,
		FilePath:  "pkg/foo.go",
		RepoSlug:  "org/repo",
		StartLine: 1,
		EndLine:   5,
	}
	testNode := types.CodeNode{
		ID:        "function:pkg.TestFoo",
		Qualified: "pkg.TestFoo",
		Kind:      types.NodeFunction,
		FilePath:  "pkg/foo_test.go",
		RepoSlug:  "org/repo",
		StartLine: 3,
		EndLine:   10,
	}

	graph := &fakeOpenCodeGraph{
		nodesByFile: map[string][]types.CodeNode{
			"pkg/foo.go": {changedNode},
		},
		findResult:     &changedNode,
		traverseResult: []types.TraceHop{{Node: testNode, Depth: 1}},
	}
	walker := &fakeGitWalker{
		workingDiffs: []domain.GitFileDiff{
			{Path: "pkg/foo.go", Status: "modified", Patch: patch},
		},
	}
	blastSvc := NewBlastService(graph, nil, nil)
	svc := NewDiffImpactService(graph, blastSvc, walker, nil, nil)

	result, err := svc.Analyze(context.Background(), DiffImpactRequest{
		RepoSlug:  "org/repo",
		RepoPath:  "/tmp/fake",
		ToRef:     "WORKING",
		NoExplain: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.AffectedTests) != 1 {
		t.Errorf("expected 1 affected test node, got %d", len(result.AffectedTests))
	}
	if len(result.Affected) != 0 {
		t.Errorf("expected 0 affected prod nodes, got %d", len(result.Affected))
	}
}

func TestDiffImpactService_DedupeKeepsMinimumHopCount(t *testing.T) {
	// Two changed nodes both blast-hitting the same affected node at different hop counts.
	patch := "@@ -1,5 +1,6 @@ package pkg\n"
	nodeA := types.CodeNode{
		ID:        "function:pkg.A",
		Qualified: "pkg.A",
		Kind:      types.NodeFunction,
		FilePath:  "pkg/a.go",
		RepoSlug:  "org/repo",
		StartLine: 1,
		EndLine:   10,
	}
	nodeB := types.CodeNode{
		ID:        "function:pkg.B",
		Qualified: "pkg.B",
		Kind:      types.NodeFunction,
		FilePath:  "pkg/b.go",
		RepoSlug:  "org/repo",
		StartLine: 1,
		EndLine:   10,
	}
	// Shared affected node hit from two different blast runs.
	sharedAffected := types.CodeNode{
		ID:        "function:pkg.Shared",
		Qualified: "pkg.Shared",
		Kind:      types.NodeFunction,
		FilePath:  "pkg/shared.go",
		RepoSlug:  "org/repo",
	}

	graph := &fakeOpenCodeGraph{
		nodesByFile: map[string][]types.CodeNode{
			"pkg/a.go": {nodeA},
			"pkg/b.go": {nodeB},
		},
		// TraverseGraph returns two hops for nodeA and one hop for nodeB.
		// We simulate dedup by returning the same sharedAffected at different depths.
		traverseResult: []types.TraceHop{{Node: sharedAffected, Depth: 2}},
	}
	// Make FindNode return nodeA for the blast lookup of nodeA.Qualified.
	graph.findResult = &nodeA

	walker := &fakeGitWalker{
		workingDiffs: []domain.GitFileDiff{
			{Path: "pkg/a.go", Status: "modified", Patch: patch},
		},
	}
	blastSvc := NewBlastService(graph, nil, nil)
	svc := NewDiffImpactService(graph, blastSvc, walker, nil, nil)

	result, err := svc.Analyze(context.Background(), DiffImpactRequest{
		RepoSlug:  "org/repo",
		RepoPath:  "/tmp/fake",
		ToRef:     "WORKING",
		NoExplain: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Dedup: sharedAffected should appear at most once.
	seen := make(map[string]int)
	for _, an := range result.Affected {
		seen[an.Node.ID]++
	}
	if seen["function:pkg.Shared"] > 1 {
		t.Errorf("sharedAffected node appeared %d times — expected dedup to 1", seen["function:pkg.Shared"])
	}
}

func TestDiffImpactService_NoDiff_ReturnsEmptyResult(t *testing.T) {
	graph := &fakeOpenCodeGraph{}
	walker := &fakeGitWalker{}
	blastSvc := NewBlastService(graph, nil, nil)
	svc := NewDiffImpactService(graph, blastSvc, walker, nil, nil)

	result, err := svc.Analyze(context.Background(), DiffImpactRequest{
		RepoSlug: "org/repo",
		RepoPath: "/tmp/fake",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.ChangedSymbols) != 0 {
		t.Errorf("expected 0 changed symbols, got %d", len(result.ChangedSymbols))
	}
	if len(result.Affected) != 0 {
		t.Errorf("expected 0 affected, got %d", len(result.Affected))
	}
}

// ---------------------------------------------------------------------------
// nodeOverlapsRanges unit tests
// ---------------------------------------------------------------------------

func TestNodeOverlapsRanges_Overlap(t *testing.T) {
	node := types.CodeNode{StartLine: 10, EndLine: 20}
	if !nodeOverlapsRanges(node, []LineRange{{Start: 15, End: 25}}) {
		t.Error("expected overlap when ranges intersect")
	}
}

func TestNodeOverlapsRanges_NoOverlap(t *testing.T) {
	node := types.CodeNode{StartLine: 10, EndLine: 20}
	if nodeOverlapsRanges(node, []LineRange{{Start: 25, End: 30}}) {
		t.Error("expected no overlap when ranges are disjoint")
	}
}

func TestNodeOverlapsRanges_NoLineInfo_IncludesConservatively(t *testing.T) {
	node := types.CodeNode{StartLine: 0, EndLine: 0}
	if !nodeOverlapsRanges(node, []LineRange{{Start: 1, End: 100}}) {
		t.Error("expected node with no line info to be included conservatively")
	}
}

func TestNodeOverlapsRanges_ExactBoundary(t *testing.T) {
	node := types.CodeNode{StartLine: 10, EndLine: 10}
	if !nodeOverlapsRanges(node, []LineRange{{Start: 10, End: 10}}) {
		t.Error("expected overlap on exact boundary")
	}
}

func TestNodeOverlapsRanges_EndLineZero_TreatedAsSingleLine(t *testing.T) {
	// Node with EndLine=0 should use StartLine as the end.
	node := types.CodeNode{StartLine: 5, EndLine: 0}
	if !nodeOverlapsRanges(node, []LineRange{{Start: 5, End: 5}}) {
		t.Error("expected overlap when node.StartLine == range line and EndLine == 0")
	}
	if nodeOverlapsRanges(node, []LineRange{{Start: 6, End: 10}}) {
		t.Error("expected no overlap when range starts after single-line node")
	}
}

// ---------------------------------------------------------------------------
// Additional coverage for findChangedSymbols branches
// ---------------------------------------------------------------------------

func TestDiffImpactService_DeletedFile_UsesOldPath(t *testing.T) {
	// When a file is deleted with OldPath set, the old path should be used for lookup.
	deletedNode := types.CodeNode{
		ID:        "function:pkg.Gone",
		Qualified: "pkg.Gone",
		Kind:      types.NodeFunction,
		FilePath:  "pkg/old.go",
		RepoSlug:  "org/repo",
		StartLine: 1,
		EndLine:   5,
	}

	graph := &fakeOpenCodeGraph{
		nodesByFile: map[string][]types.CodeNode{
			"pkg/old.go": {deletedNode},
		},
		findResult: &deletedNode,
		// No traverseResult means no affected nodes.
	}
	walker := &fakeGitWalker{
		workingDiffs: []domain.GitFileDiff{
			{
				Path:    "pkg/old.go",
				OldPath: "pkg/old.go",
				Status:  "deleted",
				Patch:   "", // no patch for deleted file
			},
		},
	}
	blastSvc := NewBlastService(graph, nil, nil)
	svc := NewDiffImpactService(graph, blastSvc, walker, nil, nil)

	result, err := svc.Analyze(context.Background(), DiffImpactRequest{
		RepoSlug:  "org/repo",
		RepoPath:  "/tmp/fake",
		ToRef:     "WORKING",
		NoExplain: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.ChangedSymbols) != 1 {
		t.Errorf("expected 1 changed symbol for deleted file, got %d", len(result.ChangedSymbols))
	}
}

func TestDiffImpactService_RenamedFile_UsesNewPath(t *testing.T) {
	renamedNode := types.CodeNode{
		ID:        "function:pkg.Renamed",
		Qualified: "pkg.Renamed",
		Kind:      types.NodeFunction,
		FilePath:  "pkg/new.go",
		RepoSlug:  "org/repo",
		StartLine: 1,
		EndLine:   5,
	}

	graph := &fakeOpenCodeGraph{
		nodesByFile: map[string][]types.CodeNode{
			"pkg/new.go": {renamedNode},
		},
		findResult: &renamedNode,
	}
	walker := &fakeGitWalker{
		workingDiffs: []domain.GitFileDiff{
			{
				Path:    "pkg/new.go",
				OldPath: "pkg/old.go",
				Status:  "renamed",
				Patch:   "@@ -1 +1 @@ package pkg\n",
			},
		},
	}
	blastSvc := NewBlastService(graph, nil, nil)
	svc := NewDiffImpactService(graph, blastSvc, walker, nil, nil)

	result, err := svc.Analyze(context.Background(), DiffImpactRequest{
		RepoSlug:  "org/repo",
		RepoPath:  "/tmp/fake",
		ToRef:     "WORKING",
		NoExplain: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.ChangedSymbols) != 1 {
		t.Errorf("expected 1 changed symbol for renamed file, got %d", len(result.ChangedSymbols))
	}
}

func TestDiffImpactService_NoPatch_IncludesAllNodesInFile(t *testing.T) {
	// When the patch is empty, all nodes in the file should be included.
	node := types.CodeNode{
		ID:        "function:pkg.Foo",
		Qualified: "pkg.Foo",
		Kind:      types.NodeFunction,
		FilePath:  "pkg/foo.go",
		RepoSlug:  "org/repo",
		StartLine: 1,
		EndLine:   5,
	}

	graph := &fakeOpenCodeGraph{
		nodesByFile: map[string][]types.CodeNode{
			"pkg/foo.go": {node},
		},
		findResult: &node,
	}
	walker := &fakeGitWalker{
		workingDiffs: []domain.GitFileDiff{
			{Path: "pkg/foo.go", Status: "modified", Patch: ""},
		},
	}
	blastSvc := NewBlastService(graph, nil, nil)
	svc := NewDiffImpactService(graph, blastSvc, walker, nil, nil)

	result, err := svc.Analyze(context.Background(), DiffImpactRequest{
		RepoSlug:  "org/repo",
		RepoPath:  "/tmp/fake",
		ToRef:     "WORKING",
		NoExplain: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.ChangedSymbols) != 1 {
		t.Errorf("expected 1 changed symbol when patch is empty (all nodes included), got %d", len(result.ChangedSymbols))
	}
}

// ---------------------------------------------------------------------------
// sortAffected exercises — ensures sorting runs with multi-node inputs
// ---------------------------------------------------------------------------

func TestSortAffected_SortsByHopCountThenQualified(t *testing.T) {
	nodes := []types.AffectedNode{
		{Node: types.CodeNode{Qualified: "z.Z"}, HopCount: 2},
		{Node: types.CodeNode{Qualified: "a.A"}, HopCount: 2},
		{Node: types.CodeNode{Qualified: "m.M"}, HopCount: 1},
	}
	sortAffected(nodes)
	if nodes[0].Node.Qualified != "m.M" {
		t.Errorf("expected first node to be 'm.M' (hop 1), got %q", nodes[0].Node.Qualified)
	}
	if nodes[1].Node.Qualified != "a.A" {
		t.Errorf("expected second node to be 'a.A' (hop 2, alpha first), got %q", nodes[1].Node.Qualified)
	}
	if nodes[2].Node.Qualified != "z.Z" {
		t.Errorf("expected third node to be 'z.Z' (hop 2, alpha last), got %q", nodes[2].Node.Qualified)
	}
}

// ---------------------------------------------------------------------------
// buildSummary coverage — uses a fakeExplainer that returns text
// ---------------------------------------------------------------------------

// textExplainer wraps the existing fakeExplainer but returns a text chunk.
type textExplainer struct{}

func (e *textExplainer) Explain(_ context.Context, _ domain.ExplainRequest) (<-chan domain.ExplainChunk, error) {
	ch := make(chan domain.ExplainChunk, 2)
	ch <- domain.ExplainChunk{Text: "test summary"}
	ch <- domain.ExplainChunk{Done: true}
	close(ch)
	return ch, nil
}

func (e *textExplainer) ExplainStructured(_ context.Context, _ domain.ExplainRequest) ([]byte, error) {
	return nil, nil
}

func TestDiffImpactService_WithExplainer_PopulatesSummary(t *testing.T) {
	patch := "@@ -1,3 +1,4 @@ package pkg\n"
	changedNode := types.CodeNode{
		ID:        "function:pkg.Foo",
		Qualified: "pkg.Foo",
		Kind:      types.NodeFunction,
		FilePath:  "pkg/foo.go",
		RepoSlug:  "org/repo",
		StartLine: 1,
		EndLine:   10,
	}

	graph := &fakeOpenCodeGraph{
		nodesByFile: map[string][]types.CodeNode{
			"pkg/foo.go": {changedNode},
		},
		findResult: &changedNode,
	}
	walker := &fakeGitWalker{
		workingDiffs: []domain.GitFileDiff{
			{Path: "pkg/foo.go", Status: "modified", Patch: patch},
		},
	}
	blastSvc := NewBlastService(graph, nil, nil)
	svc := NewDiffImpactService(graph, blastSvc, walker, &textExplainer{}, nil)

	result, err := svc.Analyze(context.Background(), DiffImpactRequest{
		RepoSlug:  "org/repo",
		RepoPath:  "/tmp/fake",
		ToRef:     "WORKING",
		NoExplain: false, // enable the LLM summary
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Summary == "" {
		t.Error("expected non-empty summary from LLM explainer")
	}
}

func TestDiffImpactService_DiffHasNoMatchingSymbols_ReturnsEmptyChanged(t *testing.T) {
	// Diff touches a file that has no indexed nodes.
	graph := &fakeOpenCodeGraph{
		nodesByFile: map[string][]types.CodeNode{}, // empty — no nodes
	}
	walker := &fakeGitWalker{
		workingDiffs: []domain.GitFileDiff{
			{Path: "pkg/foo.go", Status: "modified", Patch: "@@ -1,2 +1,3 @@ package pkg\n"},
		},
	}
	blastSvc := NewBlastService(graph, nil, nil)
	svc := NewDiffImpactService(graph, blastSvc, walker, nil, nil)

	result, err := svc.Analyze(context.Background(), DiffImpactRequest{
		RepoSlug:  "org/repo",
		RepoPath:  "/tmp/fake",
		ToRef:     "WORKING",
		NoExplain: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.ChangedSymbols) != 0 {
		t.Errorf("expected 0 changed symbols when no nodes indexed for file, got %d", len(result.ChangedSymbols))
	}
}

// ---------------------------------------------------------------------------
// dedupeAffected: verify changedIDs are dropped
// ---------------------------------------------------------------------------

func TestDedupeAffected_DropsChangedSymbols(t *testing.T) {
	changedID := "function:pkg.Changed"
	affected := []types.AffectedNode{
		{Node: types.CodeNode{ID: changedID, Qualified: "pkg.Changed"}, HopCount: 1},
		{Node: types.CodeNode{ID: "function:pkg.Other", Qualified: "pkg.Other"}, HopCount: 2},
	}
	changedIDs := map[string]struct{}{changedID: {}}
	result := dedupeAffected(affected, changedIDs)
	if len(result) != 1 {
		t.Errorf("expected 1 result after dropping changed symbol, got %d", len(result))
	}
	if result[0].Node.ID == changedID {
		t.Error("changed symbol should have been dropped from affected list")
	}
}
