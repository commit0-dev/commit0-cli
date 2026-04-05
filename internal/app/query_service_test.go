package app

import (
	"context"
	"testing"

	"github.com/commit0-dev/commit0/internal/config"
	"github.com/commit0-dev/commit0/internal/domain"
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
		batchRes: []domain.EmbedResult{},
	}

	vectorIdx := &stubVectorIndex{
		results: []types.ScoredNode{
			{
				Node:       types.CodeNode{ID: "n1", Qualified: "pkg.Func1"},
				VectorScore: 0.9,
				FusedScore: 0.9,
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

	vectorIdx := &stubVectorIndex{
		results: []types.ScoredNode{},
	}

	textIdx := &stubTextIndex{
		results: []types.ScoredNode{},
	}

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

	textIdx := &stubTextIndex{
		results: []types.ScoredNode{},
	}

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

func TestQueryServiceQueryDefaultTopK(t *testing.T) {
	embedder := &stubEmbedder{
		queryVec: []float32{0.1},
	}

	vectorIdx := &stubVectorIndex{
		results: []types.ScoredNode{},
	}

	textIdx := &stubTextIndex{
		results: []types.ScoredNode{},
	}

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

	// Just verify it succeeds with default TopK
	if result == nil {
		t.Errorf("Query should return result")
	}
}
