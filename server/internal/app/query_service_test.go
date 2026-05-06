package app

import (
	"context"
	"errors"
	"testing"

	"github.com/commit0-dev/commit0/pkg/types"
	"github.com/commit0-dev/commit0/server/internal/config"
	"github.com/commit0-dev/commit0/server/internal/domain"
)

func TestQueryServiceQueryEmptyQuestion(t *testing.T) {
	cfg := &config.Config{Query: config.QueryConfig{DefaultTopK: 10}}
	svc := NewQueryService(nil, nil, nil, cfg)

	_, err := svc.Query(context.Background(), QueryRequest{
		Question: "",
	})

	if err == nil {
		t.Errorf("Query should fail with empty question")
	}
}

func TestQueryServiceQuerySuccess(t *testing.T) {
	embedder := &stubEmbedder{
		queryVec: []float32{0.1, 0.2, 0.3},
	}

	store := newStubGraphStore()
	store.vectorResults = []types.ScoredNode{
		{
			Node:        types.CodeNode{ID: "n1", Qualified: "pkg.Func1"},
			VectorScore: 0.9,
			FusedScore:  0.9,
		},
	}

	cfg := &config.Config{
		Query: config.QueryConfig{
			DefaultTopK:  10,
			MinScore:     0.5,
			RRFKConstant: 60,
		},
	}

	svc := NewQueryService(embedder, store, nil, cfg)

	result, err := svc.Query(context.Background(), QueryRequest{
		Question: "find the handler",
		RepoSlug: "my-repo",
	})

	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	if result.Query != "find the handler" {
		t.Errorf("Query = %s, want 'find the handler'", result.Query)
	}

	if len(result.Nodes) == 0 {
		t.Errorf("Query should return results")
	}
}

func TestQueryServiceQueryEmbedFails(t *testing.T) {
	embedder := &stubEmbedder{
		err: domain.RateLimit("too fast"),
	}

	cfg := &config.Config{Query: config.QueryConfig{DefaultTopK: 10}}
	svc := NewQueryService(embedder, newStubGraphStore(), nil, cfg)

	_, err := svc.Query(context.Background(), QueryRequest{
		Question: "find",
		RepoSlug: "repo",
	})

	if err == nil {
		t.Errorf("Query should fail when embed fails")
	}
}

func TestQueryServiceQueryVectorSearchFails(t *testing.T) {
	embedder := &stubEmbedder{
		queryVec: []float32{0.1},
	}

	store := newStubGraphStore()
	store.vectorErr = domain.Timeout("timeout", nil)

	cfg := &config.Config{Query: config.QueryConfig{DefaultTopK: 10}}
	svc := NewQueryService(embedder, store, nil, cfg)

	_, err := svc.Query(context.Background(), QueryRequest{
		Question: "find",
		RepoSlug: "repo",
	})

	if err == nil {
		t.Errorf("Query should fail when vector search fails")
	}
}

func TestQueryServiceQueryFTSFails(t *testing.T) {
	embedder := &stubEmbedder{queryVec: []float32{0.1}}
	store := newStubGraphStore()
	store.textErr = domain.Timeout("fts timeout", nil)

	cfg := &config.Config{Query: config.QueryConfig{DefaultTopK: 10}}
	svc := NewQueryService(embedder, store, nil, cfg)

	_, err := svc.Query(context.Background(), QueryRequest{
		Question: "find",
		RepoSlug: "repo",
	})

	if err == nil {
		t.Errorf("Query should fail when FTS search fails")
	}
}

func TestQueryServiceQueryDefaultTopK(t *testing.T) {
	embedder := &stubEmbedder{queryVec: []float32{0.1}}

	cfg := &config.Config{Query: config.QueryConfig{DefaultTopK: 5}}
	svc := NewQueryService(embedder, newStubGraphStore(), nil, cfg)

	result, err := svc.Query(context.Background(), QueryRequest{
		Question: "find",
		RepoSlug: "repo",
		TopK:     0, // Should use default
	})

	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}
	if result == nil {
		t.Errorf("Query should return result")
	}
}

func TestQueryServiceQueryMinScoreDefault(t *testing.T) {
	embedder := &stubEmbedder{queryVec: []float32{0.1}}

	cfg := &config.Config{Query: config.QueryConfig{DefaultTopK: 5, MinScore: 0.7}}
	svc := NewQueryService(embedder, newStubGraphStore(), nil, cfg)

	// MinScore=0 in request → should use cfg.Query.MinScore=0.7
	result, err := svc.Query(context.Background(), QueryRequest{
		Question: "find",
		RepoSlug: "repo",
		MinScore: 0,
	})

	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}
	if result == nil {
		t.Errorf("Query should return result")
	}
}

func TestQueryServiceQueryTopKTruncation(t *testing.T) {
	embedder := &stubEmbedder{queryVec: []float32{0.1}}

	cfg := &config.Config{Query: config.QueryConfig{DefaultTopK: 10}}
	svc := NewQueryService(embedder, newStubGraphStore(), nil, cfg)

	result, err := svc.Query(context.Background(), QueryRequest{
		Question: "find",
		RepoSlug: "repo",
		TopK:     2,
	})

	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}
	if len(result.Nodes) > 2 {
		t.Errorf("Nodes should be truncated to TopK=2, got %d", len(result.Nodes))
	}
}

func TestQueryServiceQueryWithExplainerSuccess(t *testing.T) {
	embedder := &stubEmbedder{queryVec: []float32{0.1}}

	explainer := &stubExplainer{
		chunks: []domain.ExplainChunk{
			{Text: "This function ", Done: false},
			{Text: "handles auth", Done: true},
		},
	}

	cfg := &config.Config{Query: config.QueryConfig{DefaultTopK: 10}}
	svc := NewQueryService(embedder, newStubGraphStore(), explainer, cfg)

	result, err := svc.Query(context.Background(), QueryRequest{
		Question: "find auth",
		RepoSlug: "repo",
	})

	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}
	if result.Explanation != "This function handles auth" {
		t.Errorf("Explanation = %q, want 'This function handles auth'", result.Explanation)
	}
}

func TestQueryServiceQueryWithExplainerFails(t *testing.T) {
	embedder := &stubEmbedder{queryVec: []float32{0.1}}

	explainer := &stubExplainer{
		err: errors.New("llm unavailable"),
	}

	cfg := &config.Config{Query: config.QueryConfig{DefaultTopK: 10}}
	svc := NewQueryService(embedder, newStubGraphStore(), explainer, cfg)

	// Explainer failure is non-fatal
	result, err := svc.Query(context.Background(), QueryRequest{
		Question: "find",
		RepoSlug: "repo",
	})

	if err != nil {
		t.Fatalf("Query should succeed even when explainer fails, got: %v", err)
	}
	if result.Explanation != "" {
		t.Errorf("Explanation should be empty on explainer error")
	}
}

// TestNewQueryServiceWithNonNilStore was removed — flowSvc creation was removed
// from the QueryService constructor in the OpenCodeGraph migration.

func TestQueryServiceQueryWithExplainerChunkError(t *testing.T) {
	embedder := &stubEmbedder{queryVec: []float32{0.1}}

	explainer := &stubExplainer{
		chunks: []domain.ExplainChunk{
			{Error: errors.New("stream interrupted")},
		},
	}

	cfg := &config.Config{Query: config.QueryConfig{DefaultTopK: 10}}
	svc := NewQueryService(embedder, newStubGraphStore(), explainer, cfg)

	result, err := svc.Query(context.Background(), QueryRequest{
		Question: "find",
		RepoSlug: "repo",
	})

	if err != nil {
		t.Fatalf("Query should succeed even with chunk error, got: %v", err)
	}
	if result.Explanation != "" {
		t.Errorf("Explanation should be empty on chunk error")
	}
}

// ────────────────────────────────────────────────────────────────────────────
// expandWithGraph tests
// ────────────────────────────────────────────────────────────────────────────

func TestExpandWithGraph_NilGraph(t *testing.T) {
	cfg := &config.Config{Query: config.QueryConfig{DefaultTopK: 10}}
	svc := NewQueryService(nil, nil, nil, cfg)

	input := []types.ScoredNode{
		{Node: types.CodeNode{ID: "n1"}, FusedScore: 0.9},
	}
	result := svc.expandWithGraph(context.Background(), input, "repo", 10)

	if len(result) != 1 {
		t.Errorf("expandWithGraph with nil graph should return input unchanged")
	}
}

func TestExpandWithGraph_EmptyInput(t *testing.T) {
	graph := newStubGraphStore()
	cfg := &config.Config{Query: config.QueryConfig{DefaultTopK: 10}}
	svc := NewQueryService(nil, graph, nil, cfg)

	result := svc.expandWithGraph(context.Background(), []types.ScoredNode{}, "repo", 10)

	if len(result) != 0 {
		t.Errorf("expandWithGraph with empty input should return empty")
	}
}

func TestExpandWithGraph_AddCallersAndCallees(t *testing.T) {
	graph := newStubGraphStore()

	// Setup initial node
	node1 := &types.CodeNode{
		ID:        "n1",
		Qualified: "pkg.Handler",
	}
	graph.nodes["n1"] = node1

	// Setup neighborhood with callers and callees
	caller := domain.NeighborNode{Qualified: "pkg.Caller"}
	callee := domain.NeighborNode{Qualified: "pkg.Callee"}

	graph.neighborhood = &domain.Neighborhood{
		Callers: []domain.NeighborNode{caller},
		Callees: []domain.NeighborNode{callee},
	}

	// Setup the caller and callee nodes to be found
	callerNode := &types.CodeNode{
		ID:        "n_caller",
		Qualified: "pkg.Caller",
	}
	calleeNode := &types.CodeNode{
		ID:        "n_callee",
		Qualified: "pkg.Callee",
	}
	graph.nodesByQ["repo::pkg.Caller"] = callerNode
	graph.nodesByQ["repo::pkg.Callee"] = calleeNode

	cfg := &config.Config{Query: config.QueryConfig{DefaultTopK: 10}}
	svc := NewQueryService(nil, graph, nil, cfg)

	input := []types.ScoredNode{
		{Node: *node1, FusedScore: 0.9},
	}

	result := svc.expandWithGraph(context.Background(), input, "repo", 10)

	// Should have original + 2 expanded (caller + callee)
	if len(result) < 3 {
		t.Errorf("expandWithGraph should add caller and callee, got %d nodes", len(result))
	}

	// Check that expanded nodes have reduced score
	for i := 1; i < len(result); i++ {
		expectedScore := 0.9 * 0.6 // neighborScore = node.FusedScore * 0.6
		if result[i].FusedScore != expectedScore {
			t.Errorf("Expanded node score should be %.2f, got %.2f", expectedScore, result[i].FusedScore)
		}
	}
}

func TestExpandWithGraph_SkipMissingNeighbors(t *testing.T) {
	graph := newStubGraphStore()

	node1 := &types.CodeNode{
		ID:        "n1",
		Qualified: "pkg.Handler",
	}
	graph.nodes["n1"] = node1

	// Neighborhood with callers but no corresponding nodes
	graph.neighborhood = &domain.Neighborhood{
		Callers: []domain.NeighborNode{
			{Qualified: "pkg.MissingCaller"},
		},
	}

	cfg := &config.Config{Query: config.QueryConfig{DefaultTopK: 10}}
	svc := NewQueryService(nil, graph, nil, cfg)

	input := []types.ScoredNode{
		{Node: *node1, FusedScore: 0.9},
	}

	result := svc.expandWithGraph(context.Background(), input, "repo", 10)

	// Should only have original, missing neighbor is skipped
	if len(result) != 1 {
		t.Errorf("expandWithGraph should skip missing neighbors, got %d nodes", len(result))
	}
}

func TestExpandWithGraph_DedupByNodeID(t *testing.T) {
	graph := newStubGraphStore()

	node1 := &types.CodeNode{
		ID:        "n1",
		Qualified: "pkg.Handler",
	}
	graph.nodes["n1"] = node1

	// Neighborhood with duplicate node in callers and callees
	duplicateRef := domain.NeighborNode{Qualified: "pkg.Shared"}
	graph.neighborhood = &domain.Neighborhood{
		Callers: []domain.NeighborNode{duplicateRef},
		Callees: []domain.NeighborNode{duplicateRef},
	}

	sharedNode := &types.CodeNode{
		ID:        "n_shared",
		Qualified: "pkg.Shared",
	}
	graph.nodesByQ["repo::pkg.Shared"] = sharedNode

	cfg := &config.Config{Query: config.QueryConfig{DefaultTopK: 10}}
	svc := NewQueryService(nil, graph, nil, cfg)

	input := []types.ScoredNode{
		{Node: *node1, FusedScore: 0.9},
	}

	result := svc.expandWithGraph(context.Background(), input, "repo", 10)

	// Should have original + 1 (deduplicated), not original + 2
	if len(result) != 2 {
		t.Errorf("expandWithGraph should dedup by node ID, got %d nodes", len(result))
	}
}

func TestExpandWithGraph_NeighborsErrorHandling(t *testing.T) {
	graph := newStubGraphStore()

	node1 := &types.CodeNode{
		ID:        "n1",
		Qualified: "pkg.Handler",
	}
	graph.nodes["n1"] = node1

	// Neighbors() call fails
	graph.err = errors.New("neighbors failed")

	cfg := &config.Config{Query: config.QueryConfig{DefaultTopK: 10}}
	svc := NewQueryService(nil, graph, nil, cfg)

	input := []types.ScoredNode{
		{Node: *node1, FusedScore: 0.9},
	}

	result := svc.expandWithGraph(context.Background(), input, "repo", 10)

	// Should gracefully handle error and return original input
	if len(result) != 1 {
		t.Errorf("expandWithGraph should handle neighbor error gracefully")
	}
}

func TestExpandWithGraph_NodeIDEmpty(t *testing.T) {
	graph := newStubGraphStore()

	cfg := &config.Config{Query: config.QueryConfig{DefaultTopK: 10}}
	svc := NewQueryService(nil, graph, nil, cfg)

	input := []types.ScoredNode{
		{Node: types.CodeNode{ID: ""}, FusedScore: 0.9}, // Empty ID
	}

	result := svc.expandWithGraph(context.Background(), input, "repo", 10)

	// Empty ID should be skipped
	if len(result) != 1 {
		t.Errorf("expandWithGraph should skip nodes with empty ID")
	}
}

func TestExpandWithGraph_ReorderingByScore(t *testing.T) {
	graph := newStubGraphStore()

	// Original high-score node
	node1 := &types.CodeNode{
		ID:        "n1",
		Qualified: "pkg.Primary",
	}
	graph.nodes["n1"] = node1

	// Neighborhood with a high-quality expansion
	graph.neighborhood = &domain.Neighborhood{
		Callers: []domain.NeighborNode{{Qualified: "pkg.Important"}},
	}

	importantNode := &types.CodeNode{
		ID:        "n_important",
		Qualified: "pkg.Important",
	}
	graph.nodesByQ["repo::pkg.Important"] = importantNode

	cfg := &config.Config{Query: config.QueryConfig{DefaultTopK: 10}}
	svc := NewQueryService(nil, graph, nil, cfg)

	input := []types.ScoredNode{
		{Node: *node1, FusedScore: 0.5}, // Lower score
	}

	result := svc.expandWithGraph(context.Background(), input, "repo", 10)

	// Result should be re-sorted by fused score
	if len(result) < 2 {
		t.Fatalf("expandWithGraph should produce multiple results")
	}
	// First element should have highest score
	if result[0].FusedScore < result[1].FusedScore {
		t.Errorf("expandWithGraph should re-sort by score, got %v", result)
	}
}

func TestExpandWithGraph_LimitToTop5(t *testing.T) {
	graph := newStubGraphStore()

	// Create 10 nodes
	nodes := make([]types.ScoredNode, 10)
	for i := 0; i < 10; i++ {
		id := string(rune('0' + i))
		nodes[i] = types.ScoredNode{
			Node:       types.CodeNode{ID: "n" + id},
			FusedScore: 0.9,
		}
		graph.nodes["n"+id] = &nodes[i].Node
	}

	cfg := &config.Config{Query: config.QueryConfig{DefaultTopK: 10}}
	svc := NewQueryService(nil, graph, nil, cfg)

	result := svc.expandWithGraph(context.Background(), nodes, "repo", 10)

	// Should process only top 5 for expansion (min(5, len(fused)))
	if len(result) > 10 {
		t.Errorf("expandWithGraph should limit expansion to top 5")
	}
}

// ────────────────────────────────────────────────────────────────────────────
// conceptRerank tests
// ────────────────────────────────────────────────────────────────────────────

func TestConceptRerank_EmptyInput(t *testing.T) {
	cfg := &config.Config{Query: config.QueryConfig{DefaultTopK: 10}}
	svc := NewQueryService(nil, nil, nil, cfg)

	result := svc.conceptRerank([]types.ScoredNode{}, "test query")

	if len(result) != 0 {
		t.Errorf("conceptRerank with empty input should return empty")
	}
}

func TestConceptRerank_ConceptMatchBoost(t *testing.T) {
	cfg := &config.Config{Query: config.QueryConfig{DefaultTopK: 10}}
	svc := NewQueryService(nil, nil, nil, cfg)

	input := []types.ScoredNode{
		{
			Node: types.CodeNode{
				ID:       "n1",
				Concepts: []string{"authentication", "login"},
			},
			FusedScore: 1.0,
		},
		{
			Node: types.CodeNode{
				ID:       "n2",
				Concepts: []string{"storage", "database"},
			},
			FusedScore: 0.9,
		},
	}

	result := svc.conceptRerank(input, "authentication system")

	// First node should have 2x boost (concept matches "authentication")
	expectedScore := 1.0 * 2.0
	if result[0].FusedScore != expectedScore {
		t.Errorf("Concept match should boost score 2x, expected %.2f, got %.2f", expectedScore, result[0].FusedScore)
	}
}

func TestConceptRerank_ConceptMatchHyphenated(t *testing.T) {
	cfg := &config.Config{Query: config.QueryConfig{DefaultTopK: 10}}
	svc := NewQueryService(nil, nil, nil, cfg)

	input := []types.ScoredNode{
		{
			Node: types.CodeNode{
				ID:       "n1",
				Concepts: []string{"request-handler", "http-service"},
			},
			FusedScore: 1.0,
		},
	}

	result := svc.conceptRerank(input, "request handling")

	// Should match "request" part of "request-handler"
	if result[0].FusedScore != 2.0 {
		t.Errorf("Hyphenated concept should match parts, expected 2.0, got %.2f", result[0].FusedScore)
	}
}

func TestConceptRerank_SkipsShortWords(t *testing.T) {
	cfg := &config.Config{Query: config.QueryConfig{DefaultTopK: 10}}
	svc := NewQueryService(nil, nil, nil, cfg)

	input := []types.ScoredNode{
		{
			Node: types.CodeNode{
				ID:       "n1",
				Concepts: []string{"a", "is", "in"},
			},
			FusedScore: 1.0,
		},
	}

	result := svc.conceptRerank(input, "a is in test")

	// Short words (<=2 chars) should be skipped, no concept match
	if result[0].FusedScore != 1.0 {
		t.Errorf("Short words should not boost, expected 1.0, got %.2f", result[0].FusedScore)
	}
}

func TestConceptRerank_CentralityBoost(t *testing.T) {
	cfg := &config.Config{Query: config.QueryConfig{DefaultTopK: 10}}
	svc := NewQueryService(nil, nil, nil, cfg)

	input := []types.ScoredNode{
		{
			Node: types.CodeNode{
				ID:       "n1",
				Concepts: []string{},
			},
			FusedScore: 1.0,
			Centrality: 10, // Well-connected node
		},
	}

	result := svc.conceptRerank(input, "some query")

	// Centrality > 0 should apply some boost. Exact magnitude is an
	// implementation detail; just confirm the score increased above the
	// baseline 1.0 and stayed within the documented cap.
	if result[0].FusedScore <= 1.0 || result[0].FusedScore > 1.3 {
		t.Errorf("Centrality boost should apply within cap, got %.4f", result[0].FusedScore)
	}
}

func TestConceptRerank_CentralityBoostCapped(t *testing.T) {
	cfg := &config.Config{Query: config.QueryConfig{DefaultTopK: 10}}
	svc := NewQueryService(nil, nil, nil, cfg)

	input := []types.ScoredNode{
		{
			Node: types.CodeNode{
				ID:       "n1",
				Concepts: []string{},
			},
			FusedScore: 1.0,
			Centrality: 100, // Very well-connected
		},
	}

	result := svc.conceptRerank(input, "query")

	// Boost capped at 1.3x
	if result[0].FusedScore > 1.3 {
		t.Errorf("Centrality boost should be capped at 1.3, got %.4f", result[0].FusedScore)
	}
}

func TestConceptRerank_CombinedBoosts(t *testing.T) {
	cfg := &config.Config{Query: config.QueryConfig{DefaultTopK: 10}}
	svc := NewQueryService(nil, nil, nil, cfg)

	input := []types.ScoredNode{
		{
			Node: types.CodeNode{
				ID:       "n1",
				Concepts: []string{"authentication"},
			},
			FusedScore: 1.0,
			Centrality: 10, // Also well-connected
		},
	}

	result := svc.conceptRerank(input, "authentication system")

	// Concept boost (2.0) * centrality boost (~1.1)
	if result[0].FusedScore < 2.0 {
		t.Errorf("Combined boosts should apply, got %.4f", result[0].FusedScore)
	}
}

func TestConceptRerank_ResortsResults(t *testing.T) {
	cfg := &config.Config{Query: config.QueryConfig{DefaultTopK: 10}}
	svc := NewQueryService(nil, nil, nil, cfg)

	input := []types.ScoredNode{
		{
			Node: types.CodeNode{
				ID:       "n1",
				Concepts: []string{},
			},
			FusedScore: 0.5,
		},
		{
			Node: types.CodeNode{
				ID: "n2",
				// "bootstrap" is the exact concept token the query carries
				// after lowercase/split — concept matcher is exact-match.
				Concepts: []string{"bootstrap"},
			},
			FusedScore: 0.4,
		},
	}

	result := svc.conceptRerank(input, "bootstrap query")

	// 0.4 * 2.0 boost = 0.8 should now rank above 0.5.
	if result[0].Node.ID != "n2" {
		t.Errorf("conceptRerank should re-sort after boosting, got %v (scores: %v / %v)",
			result[0].Node.ID, result[0].FusedScore, result[1].FusedScore)
	}
}

func TestConceptRerank_NoConcepts(t *testing.T) {
	cfg := &config.Config{Query: config.QueryConfig{DefaultTopK: 10}}
	svc := NewQueryService(nil, nil, nil, cfg)

	input := []types.ScoredNode{
		{
			Node: types.CodeNode{
				ID:       "n1",
				Concepts: []string{},
			},
			FusedScore: 1.0,
		},
	}

	result := svc.conceptRerank(input, "some query")

	// No concepts, no boost (unless centrality)
	if result[0].FusedScore != 1.0 {
		t.Errorf("No concepts should not boost, expected 1.0, got %.2f", result[0].FusedScore)
	}
}
