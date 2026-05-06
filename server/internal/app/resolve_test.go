package app

import (
	"context"
	"testing"

	"github.com/commit0-dev/commit0/pkg/types"
	"github.com/commit0-dev/commit0/server/internal/domain"
)

func TestResolveSymbol_ExactMatch(t *testing.T) {
	ctx := context.Background()
	graph := newStubGraphStore()

	node := &types.CodeNode{
		ID:        "pkg.Func",
		Qualified: "pkg.Func",
	}
	graph.nodesByQ["repo::pkg.Func"] = node

	result, err := ResolveSymbol(ctx, graph, nil, "repo", "pkg.Func")

	if err != nil {
		t.Fatalf("ResolveSymbol failed: %v", err)
	}
	if result == nil || result.ID != "pkg.Func" {
		t.Errorf("Expected node with ID 'pkg.Func', got %v", result)
	}
}

func TestResolveSymbol_ExactMatchFails(t *testing.T) {
	ctx := context.Background()
	graph := newStubGraphStore()
	graph.err = domain.NotFound("not found")

	_, err := ResolveSymbol(ctx, graph, nil, "repo", "nonexistent.Func")

	if err == nil {
		t.Errorf("ResolveSymbol should fail when exact match fails")
	}
}

func TestResolveSymbol_SuffixMatchSingleResult(t *testing.T) {
	ctx := context.Background()
	graph := newStubGraphStore()

	// Setup IDsOnly list with a single match
	graph.nodeIDs = []string{"pkg:auth.Handler"}
	graph.nodes["pkg:auth.Handler"] = &types.CodeNode{
		ID:        "pkg:auth.Handler",
		Qualified: "auth.Handler",
	}

	result, err := ResolveSymbol(ctx, graph, nil, "repo", "Handler")

	if err != nil {
		t.Fatalf("ResolveSymbol failed: %v", err)
	}
	if result == nil || result.Qualified != "auth.Handler" {
		t.Errorf("Expected node with Qualified 'auth.Handler', got %v", result)
	}
}

func TestResolveSymbol_SuffixMatchWithSeparator(t *testing.T) {
	ctx := context.Background()
	graph := newStubGraphStore()

	// Setup node with middot separator (⋅) in ID
	graph.nodeIDs = []string{"pkg:auth⋅Handler"}
	graph.nodes["pkg:auth⋅Handler"] = &types.CodeNode{
		ID:        "pkg:auth⋅Handler",
		Qualified: "auth.Handler",
	}

	result, err := ResolveSymbol(ctx, graph, nil, "repo", "Handler")

	if err != nil {
		t.Fatalf("ResolveSymbol failed: %v", err)
	}
	if result == nil || result.Qualified != "auth.Handler" {
		t.Errorf("Expected node with Qualified 'auth.Handler', got %v", result)
	}
}

func TestResolveSymbol_SuffixMatchMultipleCandidates(t *testing.T) {
	ctx := context.Background()
	graph := newStubGraphStore()

	// Setup multiple matches for same suffix
	graph.nodeIDs = []string{"pkg1:auth.Handler", "pkg2:auth.Handler"}
	graph.nodes["pkg1:auth.Handler"] = &types.CodeNode{
		ID:        "pkg1:auth.Handler",
		Qualified: "auth.Handler",
		FilePath:  "pkg1/auth.go",
		StartLine: 10,
	}
	graph.nodes["pkg2:auth.Handler"] = &types.CodeNode{
		ID:        "pkg2:auth.Handler",
		Qualified: "auth.Handler",
		FilePath:  "pkg2/auth.go",
		StartLine: 20,
	}

	_, err := ResolveSymbol(ctx, graph, nil, "repo", "Handler")

	if err == nil {
		t.Errorf("ResolveSymbol should return AmbiguousSymbolError")
	}

	ambigErr, ok := err.(*domain.AmbiguousSymbolError)
	if !ok {
		t.Fatalf("Expected AmbiguousSymbolError, got %T", err)
	}
	if len(ambigErr.Candidates) != 2 {
		t.Errorf("Expected 2 candidates, got %d", len(ambigErr.Candidates))
	}
}

func TestResolveSymbol_SuffixMatchMultipleButOnlyOneSuccessfullyFetched(t *testing.T) {
	ctx := context.Background()
	graph := newStubGraphStore()

	// Setup two matches in IDsOnly, but one fetch fails
	graph.nodeIDs = []string{"pkg1:Handler", "pkg2:Handler"}
	graph.nodes["pkg1:Handler"] = &types.CodeNode{
		ID:        "pkg1:Handler",
		Qualified: "Handler",
	}
	// pkg2:Handler is not in the nodes map, so GetNode will fail for it

	_, err := ResolveSymbol(ctx, graph, nil, "repo", "Handler")

	if err != nil {
		t.Fatalf("ResolveSymbol failed: %v", err)
	}
}

func TestResolveSymbol_ExactQualifiedMatch(t *testing.T) {
	ctx := context.Background()
	graph := newStubGraphStore()

	graph.nodeIDs = []string{"pkg:auth.Handler"}
	graph.nodes["pkg:auth.Handler"] = &types.CodeNode{
		ID:        "pkg:auth.Handler",
		Qualified: "auth.Handler",
	}

	result, err := ResolveSymbol(ctx, graph, nil, "repo", "auth.Handler")

	if err != nil {
		t.Fatalf("ResolveSymbol failed: %v", err)
	}
	if result == nil || result.Qualified != "auth.Handler" {
		t.Errorf("Expected exact match for 'auth.Handler'")
	}
}

func TestResolveSymbol_ListNodesFailsFallsBackToVectorSearch(t *testing.T) {
	ctx := context.Background()
	graph := newStubGraphStore()
	graph.err = domain.NotFound("list failed")

	embedder := &stubEmbedder{
		queryVec: []float32{0.1, 0.2},
	}
	graph.vectorResults = []types.ScoredNode{
		{
			Node: types.CodeNode{
				ID:        "found:node",
				Qualified: "found.Node",
			},
			FusedScore: 0.9,
		},
	}

	result, err := ResolveSymbol(ctx, graph, embedder, "repo", "Node")

	if err != nil {
		t.Fatalf("ResolveSymbol failed: %v", err)
	}
	if result == nil || result.Qualified != "found.Node" {
		t.Errorf("Expected fallback to vector search result")
	}
}

func TestResolveSymbol_VectorSearchFallbackWhenNoSuffixMatches(t *testing.T) {
	ctx := context.Background()
	graph := newStubGraphStore()

	// Empty nodeIDs list (no suffix matches)
	graph.nodeIDs = []string{}

	embedder := &stubEmbedder{
		queryVec: []float32{0.1},
	}
	graph.vectorResults = []types.ScoredNode{
		{
			Node: types.CodeNode{
				ID:        "vec:result",
				Qualified: "vector.Result",
			},
			FusedScore: 0.85,
		},
	}

	result, err := ResolveSymbol(ctx, graph, embedder, "repo", "VectorResult")

	if err != nil {
		t.Fatalf("ResolveSymbol failed: %v", err)
	}
	if result == nil || result.Qualified != "vector.Result" {
		t.Errorf("Expected vector search result")
	}
}

func TestResolveSymbol_VectorSearchEmbeddingFails(t *testing.T) {
	ctx := context.Background()
	graph := newStubGraphStore()
	graph.nodeIDs = []string{}

	embedder := &stubEmbedder{
		err: domain.RateLimit("embed failed"),
	}

	_, err := ResolveSymbol(ctx, graph, embedder, "repo", "unknown")

	if err == nil {
		t.Errorf("ResolveSymbol should return error when embedding and suffix both fail")
	}
}

func TestResolveSymbol_VectorSearchFails(t *testing.T) {
	ctx := context.Background()
	graph := newStubGraphStore()
	graph.nodeIDs = []string{}

	embedder := &stubEmbedder{
		queryVec: []float32{0.1},
	}
	graph.vectorErr = domain.Timeout("search timeout", nil)

	_, err := ResolveSymbol(ctx, graph, embedder, "repo", "unknown")

	if err == nil {
		t.Errorf("ResolveSymbol should return error when vector search fails")
	}
}

func TestResolveSymbol_VectorSearchReturnsNoResults(t *testing.T) {
	ctx := context.Background()
	graph := newStubGraphStore()
	graph.nodeIDs = []string{}
	graph.vectorResults = []types.ScoredNode{} // Empty results

	embedder := &stubEmbedder{
		queryVec: []float32{0.1},
	}

	_, err := ResolveSymbol(ctx, graph, embedder, "repo", "unknown")

	if err == nil {
		t.Errorf("ResolveSymbol should return NotFound error")
	}
}

func TestResolveSymbol_NoEmbedderFallback(t *testing.T) {
	ctx := context.Background()
	graph := newStubGraphStore()
	graph.nodeIDs = []string{} // No suffix matches

	// No embedder provided, so vector search is skipped
	_, err := ResolveSymbol(ctx, graph, nil, "repo", "unknown")

	if err == nil {
		t.Errorf("ResolveSymbol should return NotFound when no matches and no embedder")
	}
}

func TestResolveSymbol_GetNodeFails(t *testing.T) {
	ctx := context.Background()
	graph := newStubGraphStore()

	// Setup suffix match but GetNode fails
	graph.nodeIDs = []string{"pkg:Handler"}
	graph.err = domain.Conflict("fetch failed")

	_, err := ResolveSymbol(ctx, graph, nil, "repo", "Handler")

	if err == nil {
		t.Errorf("ResolveSymbol should fail when GetNode fails")
	}
}

func TestResolveSymbol_MultipleMatchesPartialFetch(t *testing.T) {
	ctx := context.Background()
	graph := newStubGraphStore()

	// Setup multiple matches
	graph.nodeIDs = []string{"auth1:Handler", "auth2:Handler", "auth3:Handler"}
	graph.nodes["auth1:Handler"] = &types.CodeNode{
		ID:        "auth1:Handler",
		Qualified: "auth1.Handler",
		FilePath:  "auth1.go",
		StartLine: 1,
	}
	graph.nodes["auth2:Handler"] = &types.CodeNode{
		ID:        "auth2:Handler",
		Qualified: "auth2.Handler",
		FilePath:  "auth2.go",
		StartLine: 2,
	}
	// auth3:Handler not in nodes map

	_, err := ResolveSymbol(ctx, graph, nil, "repo", "Handler")

	if err == nil {
		t.Errorf("ResolveSymbol should return AmbiguousSymbolError with >1 candidates")
	}

	ambigErr, ok := err.(*domain.AmbiguousSymbolError)
	if !ok {
		t.Fatalf("Expected AmbiguousSymbolError, got %T", err)
	}
	if len(ambigErr.Candidates) != 2 {
		t.Errorf("Expected 2 candidates (2 successfully fetched), got %d", len(ambigErr.Candidates))
	}
}

func TestResolveSymbol_IDWithoutColon(t *testing.T) {
	ctx := context.Background()
	graph := newStubGraphStore()

	// Node ID without colon separator (edge case)
	graph.nodeIDs = []string{"Handler"}
	graph.nodes["Handler"] = &types.CodeNode{
		ID:        "Handler",
		Qualified: "Handler",
	}

	_, err := ResolveSymbol(ctx, graph, nil, "repo", "Handler")

	if err != nil {
		t.Fatalf("ResolveSymbol failed: %v", err)
	}
}

func TestResolveSymbol_VectorSearchMinScore(t *testing.T) {
	ctx := context.Background()
	graph := newStubGraphStore()
	graph.nodeIDs = []string{}

	embedder := &stubEmbedder{
		queryVec: []float32{0.1},
	}

	// Even though we return a result with low score from the stub,
	// the actual graph layer would filter it based on minScore (0.8).
	// For this test, we ensure behavior when no results pass the filter.
	graph.vectorResults = []types.ScoredNode{}

	_, err := ResolveSymbol(ctx, graph, embedder, "repo", "unknown")

	// Should fail when no results pass filtering
	if err == nil {
		t.Errorf("ResolveSymbol with no passing results should fail")
	}
}
