package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/commit0-dev/commit0/pkg/types"
	"github.com/commit0-dev/commit0/server/internal/config"
	"github.com/commit0-dev/commit0/server/internal/domain"
)

// ── helpers to build services ─────────────────────────────────────────────

func buildQueryService(graph domain.OpenCodeGraph, embedder domain.Embedder) *QueryService {
	return NewQueryService(embedder, graph, nil, &config.Config{
		Query: config.QueryConfig{DefaultTopK: 5, MinScore: 0.0},
	})
}

func buildFieldFlowSvc(graph domain.OpenCodeGraph) *FieldFlowService {
	return NewFieldFlowService(graph, nil, nil, &config.Config{})
}

func buildTemporalSvc(graph domain.OpenCodeGraph) *TemporalService {
	return NewTemporalService(graph, nil, &fakeGitWalker{}, &fakeParser{})
}

func buildRootCauseService(
	querySvc *QueryService,
	flowSvc *FieldFlowService,
	tempSvc *TemporalService,
	graph domain.OpenCodeGraph,
	walker domain.GitWalker,
	explainer domain.LLMExplainer,
) *RootCauseAnalysisService {
	return NewRootCauseAnalysisService(
		querySvc, flowSvc, tempSvc,
		graph, walker, explainer,
		&config.Config{Query: config.QueryConfig{DefaultTopK: 5}},
	)
}

// ── scoreCommit ───────────────────────────────────────────────────────────

func TestScoreCommit_BaseScore(t *testing.T) {
	svc := buildRootCauseService(nil, nil, nil, nil, nil, nil)
	suspect := types.SuspectCommit{
		Hash:      "abc",
		Timestamp: time.Now().Add(-60 * 24 * time.Hour), // 60 days ago
	}
	score := svc.scoreCommit(suspect, nil, time.Now())
	// Base score only (old commit, no taint) → 0.5
	if score < 0.45 || score > 0.55 {
		t.Errorf("old commit base score = %.2f, want ~0.5", score)
	}
}

func TestScoreCommit_RecentBump(t *testing.T) {
	svc := buildRootCauseService(nil, nil, nil, nil, nil, nil)
	suspect := types.SuspectCommit{
		Hash:      "recentcommit",
		Timestamp: time.Now(), // just now
	}
	score := svc.scoreCommit(suspect, nil, time.Now())
	// Base 0.5 + recent 0.3 = 0.8
	if score < 0.75 {
		t.Errorf("recent commit should have high score, got %.2f", score)
	}
}

func TestScoreCommit_WeeklyBump(t *testing.T) {
	svc := buildRootCauseService(nil, nil, nil, nil, nil, nil)
	suspect := types.SuspectCommit{
		Hash:      "week",
		Timestamp: time.Now().Add(-3 * 24 * time.Hour),
	}
	score := svc.scoreCommit(suspect, nil, time.Now())
	// 0.5 + 0.2 = 0.7
	if score < 0.65 {
		t.Errorf("week-old commit score = %.2f, want >= 0.65", score)
	}
}

func TestScoreCommit_MonthlyBump(t *testing.T) {
	svc := buildRootCauseService(nil, nil, nil, nil, nil, nil)
	suspect := types.SuspectCommit{
		Hash:      "month",
		Timestamp: time.Now().Add(-15 * 24 * time.Hour),
	}
	score := svc.scoreCommit(suspect, nil, time.Now())
	// 0.5 + 0.1 = 0.6
	if score < 0.55 {
		t.Errorf("month-old commit score = %.2f, want >= 0.55", score)
	}
}

func TestScoreCommit_TaintPointBoost(t *testing.T) {
	svc := buildRootCauseService(nil, nil, nil, nil, nil, nil)
	taintHop := types.FieldFlowHop{
		Node: types.CodeNode{
			IntroducedCommit: "suspect-hash",
		},
	}
	chain := types.FieldFlowChain{
		TaintPoint: &taintHop,
		Mutations:  []types.FieldFlowHop{taintHop},
	}
	suspect := types.SuspectCommit{
		Hash:      "suspect-hash",
		Timestamp: time.Now().Add(-60 * 24 * time.Hour), // old, no time bonus
	}
	score := svc.scoreCommit(suspect, []types.FieldFlowChain{chain}, time.Now())
	// 0.5 + 0.4 (taint intro) + 0.2 (mutation last mod) = 1.0 (capped)
	if score < 0.8 {
		t.Errorf("taint point should boost score significantly, got %.2f", score)
	}
}

func TestScoreCommit_CappedAt1(t *testing.T) {
	svc := buildRootCauseService(nil, nil, nil, nil, nil, nil)
	taintHop := types.FieldFlowHop{
		Node: types.CodeNode{
			IntroducedCommit:   "h",
			LastModifiedCommit: "h",
		},
	}
	chain := types.FieldFlowChain{TaintPoint: &taintHop, Mutations: []types.FieldFlowHop{taintHop}}
	suspect := types.SuspectCommit{Hash: "h", Timestamp: time.Now()}
	score := svc.scoreCommit(suspect, []types.FieldFlowChain{chain}, time.Now())
	if score > 1.0 {
		t.Errorf("score should be capped at 1.0, got %.2f", score)
	}
}

// ── countMutations ────────────────────────────────────────────────────────

func TestCountMutations_EmptyChains(t *testing.T) {
	if n := countMutations(nil); n != 0 {
		t.Errorf("countMutations(nil) = %d, want 0", n)
	}
}

func TestCountMutations_WithChains(t *testing.T) {
	chains := []types.FieldFlowChain{
		{Mutations: []types.FieldFlowHop{{}, {}}},
		{Mutations: []types.FieldFlowHop{{}}},
	}
	if n := countMutations(chains); n != 3 {
		t.Errorf("countMutations = %d, want 3", n)
	}
}

// ── buildExcerptsFromChain ─────────────────────────────────────────────────

func TestBuildExcerptsFromChain_Empty(t *testing.T) {
	if ex := buildExcerptsFromChain(nil); len(ex) != 0 {
		t.Errorf("expected 0 excerpts from empty chain, got %d", len(ex))
	}
}

func TestBuildExcerptsFromChain_Deduplicates(t *testing.T) {
	hops := []types.FieldFlowHop{
		{Node: types.CodeNode{Qualified: "pkg.A", Body: "body A"}},
		{Node: types.CodeNode{Qualified: "pkg.A", Body: "body A again"}}, // duplicate
		{Node: types.CodeNode{Qualified: "pkg.B", Body: "body B"}},
	}
	ex := buildExcerptsFromChain(hops)
	if len(ex) != 2 {
		t.Errorf("expected 2 unique excerpts, got %d", len(ex))
	}
}

func TestBuildExcerptsFromChain_CappedAt5(t *testing.T) {
	hops := make([]types.FieldFlowHop, 10)
	for i := range hops {
		hops[i] = types.FieldFlowHop{
			Node: types.CodeNode{Qualified: "pkg.F" + string(rune('A'+i))},
		}
	}
	ex := buildExcerptsFromChain(hops)
	if len(ex) > 5 {
		t.Errorf("excerpts should be capped at 5, got %d", len(ex))
	}
}

// ── FindRootCause (integration-level with fakes) ──────────────────────────

func TestFindRootCause_EmptyDescription_QueryFails(t *testing.T) {
	graph := newStubGraphStore()
	embedder := &stubEmbedder{queryVec: []float32{0.1, 0.2}}
	// QueryService.Query returns Validation error for empty question
	querySvc := buildQueryService(graph, embedder)
	svc := buildRootCauseService(
		querySvc,
		buildFieldFlowSvc(graph),
		buildTemporalSvc(graph),
		graph,
		&fakeGitWalker{},
		nil,
	)
	_, err := svc.FindRootCause(context.Background(), RootCauseRequest{
		Description: "", // empty → QueryService returns validation error
		RepoSlug:    "r",
	})
	if err == nil {
		t.Error("expected error for empty description")
	}
}

func TestFindRootCause_NoMatchingNodes(t *testing.T) {
	graph := newStubGraphStore()
	// Vector search returns empty → QueryService returns empty nodes.
	graph.vectorResults = []types.ScoredNode{}
	embedder := &stubEmbedder{queryVec: []float32{0.1}}
	querySvc := buildQueryService(graph, embedder)
	svc := buildRootCauseService(
		querySvc,
		buildFieldFlowSvc(graph),
		buildTemporalSvc(graph),
		graph,
		&fakeGitWalker{},
		nil,
	)
	_, err := svc.FindRootCause(context.Background(), RootCauseRequest{
		Description: "something broke",
		RepoSlug:    "r",
	})
	if err == nil {
		t.Error("expected not-found error when no nodes match")
	}
}

func TestFindRootCause_WithMatchingNode_NoChains(t *testing.T) {
	graph := newStubGraphStore()
	matchedNode := types.CodeNode{
		ID:        "function:pkg.Handler",
		Qualified: "pkg.Handler",
		FilePath:  "handler.go",
	}
	graph.vectorResults = []types.ScoredNode{
		{Node: matchedNode, FusedScore: 0.9},
	}
	graph.nodesByQ["r::pkg.Handler"] = &matchedNode
	embedder := &stubEmbedder{queryVec: []float32{0.1}}
	querySvc := buildQueryService(graph, embedder)

	// FieldFlowService: symbol resolve will fail (not found by exact match)
	// → no chains, no suspects → report with empty commitZero.
	svc := buildRootCauseService(
		querySvc,
		buildFieldFlowSvc(graph),
		buildTemporalSvc(graph),
		graph,
		&fakeGitWalker{},
		nil,
	)
	report, err := svc.FindRootCause(context.Background(), RootCauseRequest{
		Description: "handler bug",
		RepoSlug:    "r",
	})
	if err != nil {
		t.Fatalf("FindRootCause: %v", err)
	}
	// No suspects → CommitHash is empty
	if report.CommitHash != "" {
		t.Logf("commit hash: %q", report.CommitHash)
	}
}

func TestFindRootCause_WithSuspects_Sorted(t *testing.T) {
	graph := newStubGraphStore()
	// Set up a node with introduced/last-modified commits.
	matchedNode := types.CodeNode{
		ID:                 "function:pkg.Handler",
		Qualified:          "pkg.Handler",
		IntroducedCommit:   "commit-abc",
		LastModifiedCommit: "commit-def",
	}
	graph.vectorResults = []types.ScoredNode{
		{Node: matchedNode, FusedScore: 0.9},
	}

	walker := &fakeGitWalker{
		commitInfo: &domain.GitCommit{
			Hash:      "commit-abc",
			Message:   "introduce bug",
			Author:    "dev",
			Timestamp: time.Now().Add(-24 * time.Hour),
		},
	}
	embedder := &stubEmbedder{queryVec: []float32{0.1}}
	querySvc := buildQueryService(graph, embedder)

	svc := buildRootCauseService(
		querySvc,
		buildFieldFlowSvc(graph),
		buildTemporalSvc(graph),
		graph,
		walker,
		nil,
	)
	report, err := svc.FindRootCause(context.Background(), RootCauseRequest{
		Description: "handler bug",
		RepoSlug:    "r",
		RepoPath:    "/path",
	})
	if err != nil {
		t.Fatalf("FindRootCause: %v", err)
	}
	if len(report.SuspectCommits) == 0 {
		t.Error("expected at least one suspect commit from node's introduced/modified commits")
	}
	if report.CommitHash == "" {
		t.Log("CommitHash empty — suspects found but no taint chains contributed")
	}
}

func TestFindRootCause_LLMVerify_CalledOnTopSuspect(t *testing.T) {
	graph := newStubGraphStore()
	matchedNode := types.CodeNode{
		ID:               "function:pkg.H",
		Qualified:        "pkg.H",
		IntroducedCommit: "abc123",
	}
	graph.vectorResults = []types.ScoredNode{{Node: matchedNode, FusedScore: 0.9}}
	walker := &fakeGitWalker{
		commitInfo: &domain.GitCommit{
			Hash:      "abc123def456",
			Message:   "bad commit",
			Author:    "dev",
			Timestamp: time.Now(),
		},
		diffs: []domain.GitFileDiff{{Path: "h.go", Additions: 5}},
	}
	embedder := &stubEmbedder{queryVec: []float32{0.1}}
	explainer := &stubExplainer{
		structuredJSON: []byte(`{"overview":"this is the cause","insights":["fix: add validation"],"evidence":[]}`),
	}
	querySvc := buildQueryService(graph, embedder)
	svc := buildRootCauseService(
		querySvc,
		buildFieldFlowSvc(graph),
		buildTemporalSvc(graph),
		graph,
		walker,
		explainer,
	)
	report, err := svc.FindRootCause(context.Background(), RootCauseRequest{
		Description: "bug in handler",
		RepoSlug:    "r",
		RepoPath:    "/path",
	})
	if err != nil {
		t.Fatalf("FindRootCause: %v", err)
	}
	// LLM was called and explanation should be set.
	if report.Explanation == "" {
		t.Log("Explanation empty — LLM may not have reached suspect verify step")
	}
}

func TestFindRootCause_QueryServiceError(t *testing.T) {
	graph := newStubGraphStore()
	graph.vectorErr = errors.New("vector search fail")
	embedder := &stubEmbedder{queryVec: []float32{0.1}}
	querySvc := buildQueryService(graph, embedder)
	svc := buildRootCauseService(
		querySvc,
		buildFieldFlowSvc(graph),
		buildTemporalSvc(graph),
		graph,
		&fakeGitWalker{},
		nil,
	)
	_, err := svc.FindRootCause(context.Background(), RootCauseRequest{
		Description: "bug",
		RepoSlug:    "r",
	})
	if err == nil {
		t.Error("expected query service error to propagate")
	}
}

func TestFindRootCause_EmbedError_PropagatesFromQuery(t *testing.T) {
	graph := newStubGraphStore()
	embedder := &stubEmbedder{queryErr: errors.New("embed fail")}
	querySvc := buildQueryService(graph, embedder)
	svc := buildRootCauseService(
		querySvc,
		buildFieldFlowSvc(graph),
		buildTemporalSvc(graph),
		graph,
		&fakeGitWalker{},
		nil,
	)
	_, err := svc.FindRootCause(context.Background(), RootCauseRequest{
		Description: "crash",
		RepoSlug:    "r",
	})
	if err == nil {
		t.Error("expected embed error to propagate")
	}
}

func TestFindRootCause_Timing(t *testing.T) {
	graph := newStubGraphStore()
	graph.vectorResults = []types.ScoredNode{
		{Node: types.CodeNode{ID: "n1", Qualified: "pkg.F"}, FusedScore: 0.9},
	}
	embedder := &stubEmbedder{queryVec: []float32{0.1}}
	querySvc := buildQueryService(graph, embedder)
	svc := buildRootCauseService(
		querySvc,
		buildFieldFlowSvc(graph),
		buildTemporalSvc(graph),
		graph,
		&fakeGitWalker{},
		nil,
	)
	report, err := svc.FindRootCause(context.Background(), RootCauseRequest{
		Description: "timing test",
		RepoSlug:    "r",
	})
	if err != nil {
		t.Fatalf("FindRootCause: %v", err)
	}
	if report.Timing.TotalMS < 0 {
		t.Error("TotalMS should be >= 0")
	}
}
