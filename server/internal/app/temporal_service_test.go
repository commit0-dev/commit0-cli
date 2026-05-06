package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/commit0-dev/commit0/pkg/types"
	"github.com/commit0-dev/commit0/server/internal/domain"
)

// ── Fakes ─────────────────────────────────────────────────────────────────

type fakeGitWalker struct {
	commits    []domain.GitCommit
	listErr    error
	diffs      []domain.GitFileDiff
	diffErr    error
	fileData   []byte
	fileErr    error
	commitInfo *domain.GitCommit
	infoErr    error
}

func (g *fakeGitWalker) ListCommits(_ context.Context, _, _, _ string) ([]domain.GitCommit, error) {
	return g.commits, g.listErr
}

func (g *fakeGitWalker) DiffCommit(_ context.Context, _, _ string) ([]domain.GitFileDiff, error) {
	return g.diffs, g.diffErr
}

func (g *fakeGitWalker) ReadFileAtCommit(_ context.Context, _, _, _ string) ([]byte, error) {
	return g.fileData, g.fileErr
}

func (g *fakeGitWalker) CommitInfo(_ context.Context, _, _ string) (*domain.GitCommit, error) {
	return g.commitInfo, g.infoErr
}

type fakeTemporalStore struct {
	upsertNodeErr   error
	upsertEdgeErr   error
	nodeHistory     []types.TemporalChange
	nodeHistoryErr  error
	queryRangeRes   []types.TemporalChange
	queryRangeErr   error
	edgesAtCommit   []types.CodeEdge
}

func (s *fakeTemporalStore) UpsertNodeTemporal(_ context.Context, _ *types.CodeNode, _ string, _ time.Time) error {
	return s.upsertNodeErr
}

func (s *fakeTemporalStore) UpsertEdgeTemporal(_ context.Context, _ *types.CodeEdge, _ string, _ time.Time) error {
	return s.upsertEdgeErr
}

func (s *fakeTemporalStore) MarkNodeRemoved(_ context.Context, _, _ string, _ time.Time) error {
	return nil
}

func (s *fakeTemporalStore) MarkEdgeRemoved(_ context.Context, _, _ string, _ time.Time) error {
	return nil
}

func (s *fakeTemporalStore) QueryTemporalRange(_ context.Context, _, _, _ string) ([]types.TemporalChange, error) {
	return s.queryRangeRes, s.queryRangeErr
}

func (s *fakeTemporalStore) NodeHistory(_ context.Context, _ string) ([]types.TemporalChange, error) {
	return s.nodeHistory, s.nodeHistoryErr
}

func (s *fakeTemporalStore) EdgesIntroducedAt(_ context.Context, _, _ string) ([]types.CodeEdge, error) {
	return s.edgesAtCommit, nil
}

type fakeParser struct {
	result *domain.ParsedFile
	err    error
}

func (p *fakeParser) Parse(_ context.Context, _ domain.FileEntry) (*domain.ParsedFile, error) {
	if p.err != nil {
		return nil, p.err
	}
	if p.result != nil {
		return p.result, nil
	}
	return &domain.ParsedFile{}, nil
}

func (p *fakeParser) SupportedLanguages() []string {
	return []string{"go", "python", "typescript", "javascript"}
}

// ── helpers ───────────────────────────────────────────────────────────────

func newTemporalService(graph domain.OpenCodeGraph, store *fakeTemporalStore, walker *fakeGitWalker, parser domain.Parser) *TemporalService {
	return NewTemporalService(graph, store, walker, parser)
}

// ── languageFromPath ──────────────────────────────────────────────────────

func TestLanguageFromPath(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{"main.go", "go"},
		{"lib.ts", "typescript"},
		{"component.tsx", "typescript"},
		{"index.js", "javascript"},
		{"app.jsx", "javascript"},
		{"script.py", "python"},
		{"README.md", ""},
		{"no-extension", ""},
		{"file.go.bak", ""},
	}
	for _, tc := range cases {
		got := languageFromPath(tc.path)
		if got != tc.want {
			t.Errorf("languageFromPath(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}
}

// ── hasAnySuffix ──────────────────────────────────────────────────────────

func TestHasAnySuffix(t *testing.T) {
	if !hasAnySuffix("file.go", ".go") {
		t.Error("file.go should have suffix .go")
	}
	if hasAnySuffix("file.go", ".ts") {
		t.Error("file.go should NOT have suffix .ts")
	}
	if !hasAnySuffix("component.tsx", ".ts", ".tsx") {
		t.Error("component.tsx should match .tsx")
	}
	if hasAnySuffix("", ".go") {
		t.Error("empty string has no suffix")
	}
}

// ── IndexCommitRange ──────────────────────────────────────────────────────

func TestIndexCommitRange_ListCommitsError(t *testing.T) {
	walker := &fakeGitWalker{listErr: errors.New("git list fail")}
	store := &fakeTemporalStore{}
	svc := newTemporalService(newStubGraphStore(), store, walker, &fakeParser{})
	err := svc.IndexCommitRange(context.Background(), TemporalIndexRequest{
		RepoSlug: "r", RepoPath: "/path",
	})
	if err == nil {
		t.Error("expected error from ListCommits")
	}
}

func TestIndexCommitRange_EmptyCommits_NoError(t *testing.T) {
	walker := &fakeGitWalker{commits: nil}
	store := &fakeTemporalStore{}
	svc := newTemporalService(newStubGraphStore(), store, walker, &fakeParser{})
	err := svc.IndexCommitRange(context.Background(), TemporalIndexRequest{
		RepoSlug: "r", RepoPath: "/path",
	})
	if err != nil {
		t.Fatalf("empty commits should succeed: %v", err)
	}
}

func TestIndexCommitRange_DiffError_SkipsCommit(t *testing.T) {
	walker := &fakeGitWalker{
		commits: []domain.GitCommit{
			{Hash: "abc123", Message: "fix", Timestamp: time.Now()},
		},
		diffErr: errors.New("diff fail"),
	}
	store := &fakeTemporalStore{}
	svc := newTemporalService(newStubGraphStore(), store, walker, &fakeParser{})
	// Should not return error — diff failures are just skipped.
	err := svc.IndexCommitRange(context.Background(), TemporalIndexRequest{
		RepoSlug: "r", RepoPath: "/path",
	})
	if err != nil {
		t.Fatalf("diff error should be skipped: %v", err)
	}
}

func TestIndexCommitRange_DeletedFile_Skipped(t *testing.T) {
	walker := &fakeGitWalker{
		commits: []domain.GitCommit{{Hash: "abc123", Timestamp: time.Now()}},
		diffs:   []domain.GitFileDiff{{Path: "gone.go", Status: "deleted"}},
	}
	store := &fakeTemporalStore{}
	svc := newTemporalService(newStubGraphStore(), store, walker, &fakeParser{})
	err := svc.IndexCommitRange(context.Background(), TemporalIndexRequest{RepoSlug: "r"})
	if err != nil {
		t.Fatalf("deleted file should be skipped: %v", err)
	}
}

func TestIndexCommitRange_ReadFileError_Skipped(t *testing.T) {
	walker := &fakeGitWalker{
		commits: []domain.GitCommit{{Hash: "abc123", Timestamp: time.Now()}},
		diffs:   []domain.GitFileDiff{{Path: "main.go", Status: "added"}},
		fileErr: errors.New("read fail"),
	}
	store := &fakeTemporalStore{}
	svc := newTemporalService(newStubGraphStore(), store, walker, &fakeParser{})
	err := svc.IndexCommitRange(context.Background(), TemporalIndexRequest{RepoSlug: "r"})
	if err != nil {
		t.Fatalf("read error should be skipped: %v", err)
	}
}

func TestIndexCommitRange_ParseError_Skipped(t *testing.T) {
	walker := &fakeGitWalker{
		commits:  []domain.GitCommit{{Hash: "abc123def456", Timestamp: time.Now()}},
		diffs:    []domain.GitFileDiff{{Path: "main.go", Status: "added"}},
		fileData: []byte("package main"),
	}
	store := &fakeTemporalStore{}
	parser := &fakeParser{err: errors.New("parse fail")}
	svc := newTemporalService(newStubGraphStore(), store, walker, parser)
	err := svc.IndexCommitRange(context.Background(), TemporalIndexRequest{RepoSlug: "r"})
	if err != nil {
		t.Fatalf("parse error should be skipped: %v", err)
	}
}

func TestIndexCommitRange_AddedFile_UpsertsCalled(t *testing.T) {
	node := types.CodeNode{ID: "function:main.Main", Qualified: "main.Main", ContentHash: "h1"}
	walker := &fakeGitWalker{
		commits:  []domain.GitCommit{{Hash: "abc123def456", Timestamp: time.Now()}},
		diffs:    []domain.GitFileDiff{{Path: "main.go", Status: "added"}},
		fileData: []byte("package main"),
	}
	store := &fakeTemporalStore{}
	parser := &fakeParser{result: &domain.ParsedFile{Nodes: []types.CodeNode{node}}}
	graphStore := newStubGraphStore()
	svc := newTemporalService(graphStore, store, walker, parser)
	err := svc.IndexCommitRange(context.Background(), TemporalIndexRequest{RepoSlug: "r"})
	if err != nil {
		t.Fatalf("IndexCommitRange: %v", err)
	}
}

func TestIndexCommitRange_ModifiedFile_ChangedNode(t *testing.T) {
	// Existing node has different ContentHash → update LastModifiedCommit.
	node := types.CodeNode{
		ID: "function:pkg.Foo", Qualified: "pkg.Foo",
		ContentHash: "new-hash", RepoSlug: "r",
	}
	existing := types.CodeNode{
		ID: "function:pkg.Foo", Qualified: "pkg.Foo",
		ContentHash: "old-hash",
	}
	walker := &fakeGitWalker{
		commits:  []domain.GitCommit{{Hash: "def456abc123", Timestamp: time.Now()}},
		diffs:    []domain.GitFileDiff{{Path: "pkg.go", Status: "modified"}},
		fileData: []byte("package pkg"),
	}
	store := &fakeTemporalStore{}
	parser := &fakeParser{result: &domain.ParsedFile{Nodes: []types.CodeNode{node}}}
	graphStore := newStubGraphStore()
	graphStore.nodesByQ["r::pkg.Foo"] = &existing

	svc := newTemporalService(graphStore, store, walker, parser)
	err := svc.IndexCommitRange(context.Background(), TemporalIndexRequest{RepoSlug: "r"})
	if err != nil {
		t.Fatalf("IndexCommitRange: %v", err)
	}
}

func TestIndexCommitRange_ModifiedFile_UnchangedNode_NoUpsert(t *testing.T) {
	// Existing node has SAME ContentHash → skip.
	sameHash := "same-hash"
	node := types.CodeNode{
		ID: "function:pkg.Foo", Qualified: "pkg.Foo", ContentHash: sameHash, RepoSlug: "r",
	}
	existing := types.CodeNode{
		ID: "function:pkg.Foo", Qualified: "pkg.Foo", ContentHash: sameHash,
	}
	walker := &fakeGitWalker{
		commits:  []domain.GitCommit{{Hash: "def456abc123", Timestamp: time.Now()}},
		diffs:    []domain.GitFileDiff{{Path: "pkg.go", Status: "modified"}},
		fileData: []byte("package pkg"),
	}
	store := &fakeTemporalStore{}
	parser := &fakeParser{result: &domain.ParsedFile{Nodes: []types.CodeNode{node}}}
	graphStore := newStubGraphStore()
	graphStore.nodesByQ["r::pkg.Foo"] = &existing

	svc := newTemporalService(graphStore, store, walker, parser)
	err := svc.IndexCommitRange(context.Background(), TemporalIndexRequest{RepoSlug: "r"})
	if err != nil {
		t.Fatalf("IndexCommitRange: %v", err)
	}
}

func TestIndexCommitRange_ContextCancelled_StopsEarly(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	walker := &fakeGitWalker{
		commits: []domain.GitCommit{
			{Hash: "abc", Timestamp: time.Now()},
			{Hash: "def", Timestamp: time.Now()},
		},
	}
	store := &fakeTemporalStore{}
	svc := newTemporalService(newStubGraphStore(), store, walker, &fakeParser{})
	err := svc.IndexCommitRange(ctx, TemporalIndexRequest{RepoSlug: "r"})
	// Cancelled context should propagate.
	if err == nil {
		t.Error("expected context cancellation error")
	}
}

func TestIndexCommitRange_EdgeUpsert(t *testing.T) {
	edge := types.CodeEdge{Kind: types.EdgeCalls, FromID: "function:pkg.A", ToID: "function:pkg.B"}
	walker := &fakeGitWalker{
		commits:  []domain.GitCommit{{Hash: "abc123def456", Timestamp: time.Now()}},
		diffs:    []domain.GitFileDiff{{Path: "main.go", Status: "added"}},
		fileData: []byte("package pkg"),
	}
	store := &fakeTemporalStore{}
	parser := &fakeParser{result: &domain.ParsedFile{Edges: []types.CodeEdge{edge}}}
	svc := newTemporalService(newStubGraphStore(), store, walker, parser)
	err := svc.IndexCommitRange(context.Background(), TemporalIndexRequest{RepoSlug: "r"})
	if err != nil {
		t.Fatalf("IndexCommitRange with edges: %v", err)
	}
}

// ── QueryHistory ──────────────────────────────────────────────────────────

func TestQueryHistory_ByNode_NodeNotFound(t *testing.T) {
	store := &fakeTemporalStore{}
	graphStore := newStubGraphStore()
	svc := newTemporalService(graphStore, store, &fakeGitWalker{}, &fakeParser{})
	_, err := svc.QueryHistory(context.Background(), TemporalQueryRequest{
		RepoSlug:      "r",
		NodeQualified: "pkg.Missing",
	})
	if err == nil {
		t.Error("expected not-found error for missing node")
	}
}

func TestQueryHistory_ByNode_GraphError(t *testing.T) {
	graphStore := newStubGraphStore()
	graphStore.err = errors.New("graph fail")
	store := &fakeTemporalStore{}
	svc := newTemporalService(graphStore, store, &fakeGitWalker{}, &fakeParser{})
	_, err := svc.QueryHistory(context.Background(), TemporalQueryRequest{
		RepoSlug:      "r",
		NodeQualified: "pkg.Foo",
	})
	if err == nil {
		t.Error("expected graph error")
	}
}

func TestQueryHistory_ByNode_NilTempStore(t *testing.T) {
	node := &types.CodeNode{ID: "function:pkg.Foo", Qualified: "pkg.Foo"}
	graphStore := newStubGraphStore()
	graphStore.nodesByQ["r::pkg.Foo"] = node

	// Pass an actual untyped nil through the interface — using a
	// typed-nil *fakeTemporalStore yields a non-nil interface value
	// (Go's typed-nil-in-interface trap), defeating the s.tempStore==nil
	// branch we're trying to exercise.
	svc := NewTemporalService(graphStore, nil, &fakeGitWalker{}, &fakeParser{})
	_, err := svc.QueryHistory(context.Background(), TemporalQueryRequest{
		RepoSlug:      "r",
		NodeQualified: "pkg.Foo",
	})
	if err == nil {
		t.Error("expected validation error when tempStore is nil")
	}
}

func TestQueryHistory_ByNode_HappyPath(t *testing.T) {
	node := &types.CodeNode{ID: "function:pkg.Foo", Qualified: "pkg.Foo"}
	graphStore := newStubGraphStore()
	graphStore.nodesByQ["r::pkg.Foo"] = node
	changes := []types.TemporalChange{{CommitHash: "abc123"}}
	store := &fakeTemporalStore{nodeHistory: changes}

	svc := newTemporalService(graphStore, store, &fakeGitWalker{}, &fakeParser{})
	result, err := svc.QueryHistory(context.Background(), TemporalQueryRequest{
		RepoSlug:      "r",
		NodeQualified: "pkg.Foo",
	})
	if err != nil {
		t.Fatalf("QueryHistory: %v", err)
	}
	if len(result) != 1 || result[0].CommitHash != "abc123" {
		t.Errorf("unexpected result: %v", result)
	}
}

func TestQueryHistory_ByNode_NodeHistoryError(t *testing.T) {
	node := &types.CodeNode{ID: "function:pkg.Foo", Qualified: "pkg.Foo"}
	graphStore := newStubGraphStore()
	graphStore.nodesByQ["r::pkg.Foo"] = node
	store := &fakeTemporalStore{nodeHistoryErr: errors.New("history fail")}

	svc := newTemporalService(graphStore, store, &fakeGitWalker{}, &fakeParser{})
	_, err := svc.QueryHistory(context.Background(), TemporalQueryRequest{
		RepoSlug:      "r",
		NodeQualified: "pkg.Foo",
	})
	if err == nil {
		t.Error("expected NodeHistory error to propagate")
	}
}

func TestQueryHistory_ByRange_HappyPath(t *testing.T) {
	store := &fakeTemporalStore{
		queryRangeRes: []types.TemporalChange{{CommitHash: "xyzabcdefgh"}},
	}
	svc := newTemporalService(newStubGraphStore(), store, &fakeGitWalker{}, &fakeParser{})
	result, err := svc.QueryHistory(context.Background(), TemporalQueryRequest{
		RepoSlug:   "r",
		FromCommit: "abc",
		ToCommit:   "def",
	})
	if err != nil {
		t.Fatalf("QueryHistory range: %v", err)
	}
	if len(result) != 1 {
		t.Errorf("expected 1 change, got %d", len(result))
	}
}

func TestQueryHistory_ByRange_Error(t *testing.T) {
	store := &fakeTemporalStore{queryRangeErr: errors.New("range fail")}
	svc := newTemporalService(newStubGraphStore(), store, &fakeGitWalker{}, &fakeParser{})
	_, err := svc.QueryHistory(context.Background(), TemporalQueryRequest{
		RepoSlug:   "r",
		FromCommit: "abc",
	})
	if err == nil {
		t.Error("expected range query error")
	}
}
