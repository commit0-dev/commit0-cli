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
