package app

import (
	"context"
	"errors"
	"testing"

	"github.com/commit0-dev/commit0/server/internal/config"
	"github.com/commit0-dev/commit0/server/internal/domain"
	"github.com/commit0-dev/commit0/pkg/types"
)

func TestQueryServiceQueryEmptyQuestion(t *testing.T) {
	cfg := &config.Config{Query: config.QueryConfig{DefaultTopK: 10}}
	svc := NewQueryService(nil, nil, nil, nil, nil, cfg)

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

	vectorIdx := &stubVectorIndex{
		results: []types.ScoredNode{
			{
				Node:        types.CodeNode{ID: "n1", Qualified: "pkg.Func1"},
				VectorScore: 0.9,
				FusedScore:  0.9,
			},
		},
	}

	textIdx := &stubTextIndex{
		results: []types.ScoredNode{},
	}

	cfg := &config.Config{
		Query: config.QueryConfig{
			DefaultTopK:  10,
			MinScore:     0.5,
			RRFKConstant: 60,
		},
	}

	svc := NewQueryService(embedder, vectorIdx, textIdx, nil, nil, cfg)

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

	vectorIdx := &stubVectorIndex{results: []types.ScoredNode{}}
	textIdx := &stubTextIndex{results: []types.ScoredNode{}}

	cfg := &config.Config{Query: config.QueryConfig{DefaultTopK: 10}}
	svc := NewQueryService(embedder, vectorIdx, textIdx, nil, nil, cfg)

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

	vectorIdx := &stubVectorIndex{
		err: domain.Timeout("timeout", nil),
	}

	textIdx := &stubTextIndex{results: []types.ScoredNode{}}

	cfg := &config.Config{Query: config.QueryConfig{DefaultTopK: 10}}
	svc := NewQueryService(embedder, vectorIdx, textIdx, nil, nil, cfg)

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
	vectorIdx := &stubVectorIndex{results: []types.ScoredNode{}}
	textIdx := &stubTextIndex{
		err: domain.Timeout("fts timeout", nil),
	}

	cfg := &config.Config{Query: config.QueryConfig{DefaultTopK: 10}}
	svc := NewQueryService(embedder, vectorIdx, textIdx, nil, nil, cfg)

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
	vectorIdx := &stubVectorIndex{results: []types.ScoredNode{}}
	textIdx := &stubTextIndex{results: []types.ScoredNode{}}

	cfg := &config.Config{Query: config.QueryConfig{DefaultTopK: 5}}
	svc := NewQueryService(embedder, vectorIdx, textIdx, nil, nil, cfg)

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
	vectorIdx := &stubVectorIndex{results: []types.ScoredNode{}}
	textIdx := &stubTextIndex{results: []types.ScoredNode{}}

	cfg := &config.Config{Query: config.QueryConfig{DefaultTopK: 5, MinScore: 0.7}}
	svc := NewQueryService(embedder, vectorIdx, textIdx, nil, nil, cfg)

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
	// Vector returns 5 results but TopK=2 → should be truncated to 2
	vectorIdx := &stubVectorIndex{
		results: []types.ScoredNode{
			{Node: types.CodeNode{ID: "n1"}, FusedScore: 0.9},
			{Node: types.CodeNode{ID: "n2"}, FusedScore: 0.8},
			{Node: types.CodeNode{ID: "n3"}, FusedScore: 0.7},
			{Node: types.CodeNode{ID: "n4"}, FusedScore: 0.6},
			{Node: types.CodeNode{ID: "n5"}, FusedScore: 0.5},
		},
	}
	textIdx := &stubTextIndex{results: []types.ScoredNode{}}

	cfg := &config.Config{Query: config.QueryConfig{DefaultTopK: 10}}
	svc := NewQueryService(embedder, vectorIdx, textIdx, nil, nil, cfg)

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
	vectorIdx := &stubVectorIndex{
		results: []types.ScoredNode{
			{Node: types.CodeNode{ID: "n1", Qualified: "pkg.Func"}, FusedScore: 0.9},
		},
	}
	textIdx := &stubTextIndex{results: []types.ScoredNode{}}

	explainer := &stubExplainer{
		chunks: []domain.ExplainChunk{
			{Text: "This function ", Done: false},
			{Text: "handles auth", Done: true},
		},
	}

	cfg := &config.Config{Query: config.QueryConfig{DefaultTopK: 10}}
	svc := NewQueryService(embedder, vectorIdx, textIdx, nil, explainer, cfg)

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
	vectorIdx := &stubVectorIndex{results: []types.ScoredNode{}}
	textIdx := &stubTextIndex{results: []types.ScoredNode{}}
	explainer := &stubExplainer{
		err: errors.New("llm unavailable"),
	}

	cfg := &config.Config{Query: config.QueryConfig{DefaultTopK: 10}}
	svc := NewQueryService(embedder, vectorIdx, textIdx, nil, explainer, cfg)

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

func TestNewQueryServiceWithNonNilStore(t *testing.T) {
	// When store != nil, NewQueryService creates an internal DataFlowService.
	// This covers the `if store != nil { qs.flowSvc = ... }` branch.
	store := newStubGraphStore()
	cfg := &config.Config{Query: config.QueryConfig{DefaultTopK: 10}}
	svc := NewQueryService(nil, nil, nil, store, nil, cfg)
	if svc.flowSvc == nil {
		t.Error("flowSvc should be set when store is non-nil")
	}
}

func TestQueryServiceQueryWithExplainerChunkError(t *testing.T) {
	embedder := &stubEmbedder{queryVec: []float32{0.1}}
	vectorIdx := &stubVectorIndex{results: []types.ScoredNode{}}
	textIdx := &stubTextIndex{results: []types.ScoredNode{}}
	explainer := &stubExplainer{
		chunks: []domain.ExplainChunk{
			{Error: errors.New("stream interrupted")},
		},
	}

	cfg := &config.Config{Query: config.QueryConfig{DefaultTopK: 10}}
	svc := NewQueryService(embedder, vectorIdx, textIdx, nil, explainer, cfg)

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
