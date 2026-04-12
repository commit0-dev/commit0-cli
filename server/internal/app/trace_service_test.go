package app

import (
	"context"
	"errors"
	"testing"

	"github.com/commit0-dev/commit0/server/internal/config"
	"github.com/commit0-dev/commit0/server/internal/domain"
	"github.com/commit0-dev/commit0/pkg/types"
)

func TestTraceServiceTraceSuccess(t *testing.T) {
	store := newStubGraphStore()
	store.nodesByQ["my-repo::pkg.Handler"] = &types.CodeNode{
		ID:        "f1",
		Qualified: "pkg.Handler",
		Kind:      types.NodeFunction,
	}
	store.traceHops = []types.TraceHop{
		{
			Depth: 0,
			Node:  types.CodeNode{ID: "f1", Qualified: "pkg.Handler"},
		},
	}

	cfg := &config.Config{}
	svc := NewTraceService(store, nil, nil, cfg)

	result, err := svc.Trace(context.Background(), TraceRequest{
		Symbol:    "pkg.Handler",
		RepoSlug:  "my-repo",
		Direction: "forward",
	})

	if err != nil {
		t.Fatalf("Trace failed: %v", err)
	}

	if result.Direction != "forward" {
		t.Errorf("Direction = %s, want forward", result.Direction)
	}
}

func TestTraceServiceTraceEmptySymbol(t *testing.T) {
	cfg := &config.Config{}
	svc := NewTraceService(nil, nil, nil, cfg)

	_, err := svc.Trace(context.Background(), TraceRequest{
		Symbol:    "",
		RepoSlug:  "my-repo",
		Direction: "forward",
	})

	if err == nil {
		t.Errorf("Trace should fail with empty symbol")
	}
}

func TestTraceServiceTraceEmptyRepoSlug(t *testing.T) {
	cfg := &config.Config{}
	svc := NewTraceService(nil, nil, nil, cfg)

	_, err := svc.Trace(context.Background(), TraceRequest{
		Symbol:    "pkg.Func",
		RepoSlug:  "",
		Direction: "forward",
	})

	if err == nil {
		t.Errorf("Trace should fail with empty repo slug")
	}
	var domErr *domain.DomainError
	if !errors.As(err, &domErr) || domErr.Code != domain.ErrValidation {
		t.Errorf("expected validation error, got %v", err)
	}
}

func TestTraceServiceTraceInvalidDirection(t *testing.T) {
	store := newStubGraphStore()
	store.nodesByQ["my-repo::pkg.Func"] = &types.CodeNode{ID: "f1"}

	cfg := &config.Config{}
	svc := NewTraceService(store, nil, nil, cfg)

	_, err := svc.Trace(context.Background(), TraceRequest{
		Symbol:    "pkg.Func",
		RepoSlug:  "my-repo",
		Direction: "invalid",
	})

	if err == nil {
		t.Errorf("Trace should fail with invalid direction")
	}
}

func TestTraceServiceTraceDefaultDepth(t *testing.T) {
	store := newStubGraphStore()
	store.nodesByQ["repo::pkg.Func"] = &types.CodeNode{ID: "f1", Qualified: "pkg.Func"}
	store.traceHops = []types.TraceHop{}

	cfg := &config.Config{}
	svc := NewTraceService(store, nil, nil, cfg)

	// Depth=0 should default to 5 (no error)
	result, err := svc.Trace(context.Background(), TraceRequest{
		Symbol:    "pkg.Func",
		RepoSlug:  "repo",
		Direction: "forward",
		Depth:     0,
	})

	if err != nil {
		t.Fatalf("Trace failed: %v", err)
	}
	if result == nil {
		t.Error("result should not be nil")
	}
}

func TestTraceServiceTraceReverse(t *testing.T) {
	store := newStubGraphStore()
	store.nodesByQ["my-repo::pkg.Handler"] = &types.CodeNode{
		ID:        "f1",
		Qualified: "pkg.Handler",
	}
	store.traceHops = []types.TraceHop{
		{Depth: 0, Node: types.CodeNode{ID: "f1", Qualified: "pkg.Handler"}},
	}

	cfg := &config.Config{}
	svc := NewTraceService(store, nil, nil, cfg)

	result, err := svc.Trace(context.Background(), TraceRequest{
		Symbol:    "pkg.Handler",
		RepoSlug:  "my-repo",
		Direction: "reverse",
	})

	if err != nil {
		t.Fatalf("Trace reverse failed: %v", err)
	}
	if result.Direction != "reverse" {
		t.Errorf("Direction = %s, want reverse", result.Direction)
	}
}

func TestTraceServiceTraceForwardError(t *testing.T) {
	store := newStubGraphStore()
	store.nodesByQ["repo::pkg.Func"] = &types.CodeNode{ID: "f1", Qualified: "pkg.Func"}
	store.traceErr = domain.Timeout("graph timeout", nil)

	cfg := &config.Config{}
	svc := NewTraceService(store, nil, nil, cfg)

	_, err := svc.Trace(context.Background(), TraceRequest{
		Symbol:    "pkg.Func",
		RepoSlug:  "repo",
		Direction: "forward",
	})

	if err == nil {
		t.Errorf("Trace should fail when store.TraceForward fails")
	}
}

func TestTraceServiceTraceReverseError(t *testing.T) {
	store := newStubGraphStore()
	store.nodesByQ["repo::pkg.Func"] = &types.CodeNode{ID: "f1", Qualified: "pkg.Func"}
	store.traceErr = domain.Timeout("graph timeout", nil)

	cfg := &config.Config{}
	svc := NewTraceService(store, nil, nil, cfg)

	_, err := svc.Trace(context.Background(), TraceRequest{
		Symbol:    "pkg.Func",
		RepoSlug:  "repo",
		Direction: "reverse",
	})

	if err == nil {
		t.Errorf("Trace should fail when store.TraceReverse fails")
	}
}

func TestTraceServiceTraceNotFound(t *testing.T) {
	store := newStubGraphStore()
	cfg := &config.Config{}
	svc := NewTraceService(store, nil, nil, cfg)

	_, err := svc.Trace(context.Background(), TraceRequest{
		Symbol:    "nonexistent",
		RepoSlug:  "my-repo",
		Direction: "forward",
	})

	if err == nil {
		t.Errorf("Trace should fail for non-existent symbol")
	}
}

func TestTraceServiceTraceWithExplainerSuccess(t *testing.T) {
	store := newStubGraphStore()
	store.nodesByQ["repo::pkg.Func"] = &types.CodeNode{
		ID: "f1", Qualified: "pkg.Func", FilePath: "main.go", Body: "func Func() {}",
	}
	store.traceHops = []types.TraceHop{
		{Depth: 0, Node: types.CodeNode{ID: "f1", Qualified: "pkg.Func"}},
	}

	explainer := &stubExplainer{
		chunks: []domain.ExplainChunk{
			{Text: "call chain is ", Done: false},
			{Text: "clear", Done: true},
		},
	}

	cfg := &config.Config{}
	svc := NewTraceService(store, nil, explainer, cfg)

	result, err := svc.Trace(context.Background(), TraceRequest{
		Symbol:    "pkg.Func",
		RepoSlug:  "repo",
		Direction: "forward",
	})

	if err != nil {
		t.Fatalf("Trace failed: %v", err)
	}
	if result.Explanation != "call chain is clear" {
		t.Errorf("Explanation = %q, want 'call chain is clear'", result.Explanation)
	}
}

func TestTraceServiceTraceWithExplainerFails(t *testing.T) {
	store := newStubGraphStore()
	store.nodesByQ["repo::pkg.Func"] = &types.CodeNode{ID: "f1", Qualified: "pkg.Func"}
	store.traceHops = []types.TraceHop{}

	explainer := &stubExplainer{
		err: errors.New("explainer unavailable"),
	}

	cfg := &config.Config{}
	svc := NewTraceService(store, nil, explainer, cfg)

	// Explainer failure is non-fatal
	result, err := svc.Trace(context.Background(), TraceRequest{
		Symbol:    "pkg.Func",
		RepoSlug:  "repo",
		Direction: "forward",
	})

	if err != nil {
		t.Fatalf("Trace should succeed even when explainer fails, got: %v", err)
	}
	if result.Explanation != "" {
		t.Errorf("Explanation should be empty on explainer error")
	}
}

func TestTraceServiceTraceWithExplainerChunkError(t *testing.T) {
	store := newStubGraphStore()
	store.nodesByQ["repo::pkg.Func"] = &types.CodeNode{ID: "f1", Qualified: "pkg.Func"}
	store.traceHops = []types.TraceHop{}

	explainer := &stubExplainer{
		chunks: []domain.ExplainChunk{
			{Error: errors.New("stream failed")},
		},
	}

	cfg := &config.Config{}
	svc := NewTraceService(store, nil, explainer, cfg)

	result, err := svc.Trace(context.Background(), TraceRequest{
		Symbol:    "pkg.Func",
		RepoSlug:  "repo",
		Direction: "forward",
	})

	if err != nil {
		t.Fatalf("Trace should succeed even with chunk error, got: %v", err)
	}
	if result.Explanation != "" {
		t.Errorf("Explanation should be empty on chunk error")
	}
}

// TestTraceServiceTraceWithChildren covers collectHopExcerpts recursive path.
func TestTraceServiceTraceWithChildren(t *testing.T) {
	store := newStubGraphStore()
	store.nodesByQ["repo::pkg.Root"] = &types.CodeNode{
		ID: "r1", Qualified: "pkg.Root", Body: "func Root() {}",
	}
	// Hops with nested children to trigger recursive collectHopExcerpts
	store.traceHops = []types.TraceHop{
		{
			Depth: 0,
			Node:  types.CodeNode{ID: "r1", Qualified: "pkg.Root"},
			Children: []types.TraceHop{
				{
					Depth: 1,
					Node:  types.CodeNode{ID: "c1", Qualified: "pkg.Child"},
					Children: []types.TraceHop{
						{Depth: 2, Node: types.CodeNode{ID: "gc1", Qualified: "pkg.GrandChild"}},
					},
				},
			},
		},
	}

	explainer := &stubExplainer{
		chunks: []domain.ExplainChunk{{Text: "deep trace", Done: true}},
	}

	cfg := &config.Config{}
	svc := NewTraceService(store, nil, explainer, cfg)

	result, err := svc.Trace(context.Background(), TraceRequest{
		Symbol:    "pkg.Root",
		RepoSlug:  "repo",
		Direction: "forward",
	})

	if err != nil {
		t.Fatalf("Trace with children failed: %v", err)
	}
	if result.Explanation != "deep trace" {
		t.Errorf("Explanation = %q, want 'deep trace'", result.Explanation)
	}
}

// TestTraceServiceResolveSymbolVectorFallback covers the vector-search fallback in resolveSymbol.
func TestTraceServiceResolveSymbolVectorFallback(t *testing.T) {
	store := newStubGraphStore()
	// No entry in nodesByQ → direct lookup fails
	// VectorSearch returns a result via stubGraphStore.vectorResults
	store.vectorResults = []types.ScoredNode{
		{Node: types.CodeNode{ID: "f1", Qualified: "pkg.Handler"}, VectorScore: 0.9},
	}
	embedder := &stubEmbedder{queryVec: []float32{0.1, 0.2, 0.3}}

	store.traceHops = []types.TraceHop{}

	cfg := &config.Config{}
	svc := NewTraceService(store, embedder, nil, cfg)

	result, err := svc.Trace(context.Background(), TraceRequest{
		Symbol:    "Handler",
		RepoSlug:  "repo",
		Direction: "forward",
	})

	if err != nil {
		t.Fatalf("Trace with vector fallback failed: %v", err)
	}
	if result == nil {
		t.Error("result should not be nil")
	}
}

func TestTraceServiceResolveSymbolVectorFallbackEmbedFails(t *testing.T) {
	store := newStubGraphStore()
	embedder := &stubEmbedder{
		queryErr: domain.RateLimit("embed rate limited"),
	}
	cfg := &config.Config{}
	svc := NewTraceService(store, embedder, nil, cfg)

	_, err := svc.Trace(context.Background(), TraceRequest{
		Symbol:    "Handler",
		RepoSlug:  "repo",
		Direction: "forward",
	})

	if err == nil {
		t.Errorf("Trace should fail when embed query fails")
	}
}

func TestTraceServiceResolveSymbolVectorFallbackSearchFails(t *testing.T) {
	store := newStubGraphStore()
	embedder := &stubEmbedder{queryVec: []float32{0.1}}

	cfg := &config.Config{}
	svc := NewTraceService(store, embedder, nil, cfg)

	_, err := svc.Trace(context.Background(), TraceRequest{
		Symbol:    "Handler",
		RepoSlug:  "repo",
		Direction: "forward",
	})

	if err == nil {
		t.Errorf("Trace should fail when vector search fails")
	}
}

func TestTraceServiceResolveSymbolVectorFallbackEmpty(t *testing.T) {
	store := newStubGraphStore()
	embedder := &stubEmbedder{queryVec: []float32{0.1}}
	cfg := &config.Config{}
	svc := NewTraceService(store, embedder, nil, cfg)

	_, err := svc.Trace(context.Background(), TraceRequest{
		Symbol:    "Handler",
		RepoSlug:  "repo",
		Direction: "forward",
	})

	if err == nil {
		t.Errorf("Trace should fail when vector search returns nothing")
	}

	var domErr *domain.DomainError
	if !errors.As(err, &domErr) || domErr.Code != domain.ErrNotFound {
		t.Errorf("expected not-found error, got %v", err)
	}
}

func TestTraceServiceResolveSymbolNoVectorIdx(t *testing.T) {
	// embedder present but no vectorIdx → skip fallback → NotFound
	store := newStubGraphStore()
	embedder := &stubEmbedder{queryVec: []float32{0.1}}

	cfg := &config.Config{}
	svc := NewTraceService(store, embedder, nil, cfg)

	_, err := svc.Trace(context.Background(), TraceRequest{
		Symbol:    "Handler",
		RepoSlug:  "repo",
		Direction: "forward",
	})

	if err == nil {
		t.Errorf("Trace should fail with no vectorIdx and symbol not in store")
	}
}
