package app

import (
	"context"
	"testing"
	"time"

	"github.com/commit0-dev/commit0/pkg/types"
	"github.com/commit0-dev/commit0/server/internal/config"
	"github.com/commit0-dev/commit0/server/internal/domain"
)

// fakeStreamingEmbedder implements domain.Embedder for testing.
type fakeStreamingEmbedder struct {
	query []float32
	err   error
}

func (f *fakeStreamingEmbedder) EmbedQuery(ctx context.Context, text string) ([]float32, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.query == nil {
		// Return a dummy 256-dim vector
		f.query = make([]float32, 256)
		for i := range f.query {
			f.query[i] = 0.5
		}
	}
	return f.query, nil
}

func (f *fakeStreamingEmbedder) EmbedBatch(ctx context.Context, inputs []domain.EmbedInput) ([]domain.EmbedResult, error) {
	if f.err != nil {
		return nil, f.err
	}
	results := make([]domain.EmbedResult, len(inputs))
	for i := range inputs {
		results[i].Vector = make([]float32, 256)
		for j := range results[i].Vector {
			results[i].Vector[j] = 0.5
		}
	}
	return results, nil
}

// fakeStreamingGraph implements domain.OpenCodeGraph for testing.
type fakeStreamingGraph struct {
	vectorHits []types.ScoredNode
	ftsHits    []types.ScoredNode
	neighbors  *domain.Neighborhood
	err        error
}

func (f *fakeStreamingGraph) VectorSearch(ctx context.Context, query []float32, opts domain.VectorSearchOpts) ([]types.ScoredNode, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.vectorHits, nil
}

func (f *fakeStreamingGraph) TextSearch(ctx context.Context, query string, opts domain.TextSearchOpts) ([]types.ScoredNode, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.ftsHits, nil
}

func (f *fakeStreamingGraph) Neighbors(ctx context.Context, nodeID string) (*domain.Neighborhood, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.neighbors, nil
}

func (f *fakeStreamingGraph) PutNode(ctx context.Context, node *types.CodeNode) error { return nil }
func (f *fakeStreamingGraph) GetNode(ctx context.Context, id string) (*types.CodeNode, error) {
	return nil, nil
}
func (f *fakeStreamingGraph) FindNode(ctx context.Context, repo, qualified string) (*types.CodeNode, error) {
	return nil, nil
}
func (f *fakeStreamingGraph) DeleteNode(ctx context.Context, id string) error          { return nil }
func (f *fakeStreamingGraph) PutEdge(ctx context.Context, edge *types.CodeEdge) error  { return nil }
func (f *fakeStreamingGraph) DeleteEdgesFrom(ctx context.Context, nodeID string) error { return nil }
func (f *fakeStreamingGraph) PutBatch(ctx context.Context, nodes []types.CodeNode, edges []types.CodeEdge) error {
	return nil
}
func (f *fakeStreamingGraph) DeleteByRepo(ctx context.Context, repo string) error { return nil }
func (f *fakeStreamingGraph) DeleteByFile(ctx context.Context, repo, filePath string) error {
	return nil
}
func (f *fakeStreamingGraph) TraverseGraph(ctx context.Context, startID string, edgeLabels []string, direction string, maxDepth int) ([]types.TraceHop, error) {
	return nil, nil
}
func (f *fakeStreamingGraph) GetNodeEmbedding(ctx context.Context, nodeID string) ([]float32, error) {
	return nil, nil
}
func (f *fakeStreamingGraph) ListNodes(ctx context.Context, repo string, opts domain.ListOpts) ([]types.CodeNode, error) {
	return nil, nil
}
func (f *fakeStreamingGraph) ListEdges(ctx context.Context, repo string, labels []string) ([]types.CodeEdge, error) {
	return nil, nil
}
func (f *fakeStreamingGraph) ListFilePaths(ctx context.Context, repo string) ([]string, error) {
	return nil, nil
}
func (f *fakeStreamingGraph) PutRepo(ctx context.Context, repo *types.Repo) error { return nil }
func (f *fakeStreamingGraph) GetRepo(ctx context.Context, slug string) (*types.Repo, error) {
	return nil, nil
}
func (f *fakeStreamingGraph) ListRepos(ctx context.Context) ([]types.Repo, error) { return nil, nil }
func (f *fakeStreamingGraph) DeleteRepo(ctx context.Context, slug string) error   { return nil }
func (f *fakeStreamingGraph) FindRepoByRemoteURL(ctx context.Context, url string) (*types.Repo, error) {
	return nil, nil
}
func (f *fakeStreamingGraph) UpdateRepoIndexedAt(ctx context.Context, slug string, t time.Time) error {
	return nil
}
func (f *fakeStreamingGraph) ApplySchema(ctx context.Context) error { return nil }

func TestQueryStreamHappyPath(t *testing.T) {
	ctx := context.Background()
	cfg := &config.Config{
		Query: config.QueryConfig{
			DefaultTopK: 10,
			MinScore:    0.0,
		},
	}

	// Create fake adapters.
	vectorHits := []types.ScoredNode{
		{
			Node: types.CodeNode{
				ID:        "func:pkg.A",
				Qualified: "pkg.A",
				Name:      "A",
				Kind:      types.NodeFunction,
			},
			VectorScore: 0.9,
		},
		{
			Node: types.CodeNode{
				ID:        "func:pkg.B",
				Qualified: "pkg.B",
				Name:      "B",
				Kind:      types.NodeFunction,
			},
			VectorScore: 0.7,
		},
	}

	ftsHits := []types.ScoredNode{
		{
			Node: types.CodeNode{
				ID:        "func:pkg.C",
				Qualified: "pkg.C",
				Name:      "C",
				Kind:      types.NodeFunction,
			},
			FTSScore: 0.8,
		},
	}

	embedder := &fakeStreamingEmbedder{
		query: make([]float32, 256),
	}
	graph := &fakeStreamingGraph{
		vectorHits: vectorHits,
		ftsHits:    ftsHits,
		neighbors: &domain.Neighborhood{
			Callers: []domain.NeighborNode{},
			Callees: []domain.NeighborNode{},
		},
	}

	qs := NewQueryService(embedder, graph, nil, cfg)

	// Run streaming query.
	events := make(chan types.QueryEvent, 256)
	done := make(chan error, 1)
	go func() {
		done <- qs.QueryStream(ctx, QueryRequest{
			Question: "test query",
			RepoSlug: "test-repo",
			TopK:     10,
		}, events)
	}()

	// Collect events.
	var eventTypes []types.QueryEventType
	for evt := range events {
		eventTypes = append(eventTypes, evt.Type)
	}

	err := <-done
	if err != nil {
		t.Fatalf("QueryStream failed: %v", err)
	}

	// Verify critical events in order (with flexible hit ordering due to concurrency).
	// embedding_done → (vector_hit* + fts_hit mixed) → fused → expanded → reranked → done
	if len(eventTypes) < 6 {
		t.Errorf("expected at least 6 events (embedding_done, 2+ hits, fused, expanded, reranked, done), got %d", len(eventTypes))
	}

	if eventTypes[0] != types.QueryEventEmbeddingDone {
		t.Errorf("first event: expected embedding_done, got %s", eventTypes[0])
	}

	// Count vector/fts hits and verify they appear before fused.
	hitCount := 0
	fuseIndex := -1
	for i, evt := range eventTypes {
		if evt == types.QueryEventVectorHit || evt == types.QueryEventFTSHit {
			hitCount++
		}
		if evt == types.QueryEventFused {
			fuseIndex = i
			break
		}
	}
	if fuseIndex == -1 {
		t.Fatal("expected fused event")
	}
	if hitCount != 3 {
		t.Errorf("expected 3 hit events (2 vector + 1 fts), got %d", hitCount)
	}

	// Verify final order
	if eventTypes[len(eventTypes)-1] != types.QueryEventDone {
		t.Errorf("last event: expected done, got %s", eventTypes[len(eventTypes)-1])
	}

	// Check that reranked appears before done
	rerankeIndex := -1
	for i := len(eventTypes) - 1; i >= 0; i-- {
		if eventTypes[i] == types.QueryEventReranked {
			rerankeIndex = i
			break
		}
	}
	if rerankeIndex == -1 {
		t.Fatal("expected reranked event")
	}
	if rerankeIndex >= len(eventTypes)-1 {
		t.Errorf("reranked should come before done")
	}
}

func TestQueryStreamEmptyQuestion(t *testing.T) {
	ctx := context.Background()
	cfg := &config.Config{
		Query: config.QueryConfig{
			DefaultTopK: 10,
			MinScore:    0.0,
		},
	}

	embedder := &fakeStreamingEmbedder{}
	graph := &fakeStreamingGraph{}
	qs := NewQueryService(embedder, graph, nil, cfg)

	events := make(chan types.QueryEvent, 256)
	err := qs.QueryStream(ctx, QueryRequest{
		Question: "", // Empty question
		RepoSlug: "test-repo",
	}, events)

	if err == nil {
		t.Fatal("expected validation error for empty question")
	}

	// Should have emitted an error event.
	evt, ok := <-events
	if !ok {
		t.Fatal("channel closed without error event")
	}
	if evt.Type != types.QueryEventError {
		t.Errorf("expected error event, got %s", evt.Type)
	}
}

func TestQueryStreamVectorSearchError(t *testing.T) {
	ctx := context.Background()
	cfg := &config.Config{
		Query: config.QueryConfig{
			DefaultTopK: 10,
			MinScore:    0.0,
		},
	}

	embedder := &fakeStreamingEmbedder{}
	graph := &fakeStreamingGraph{
		err: domain.NotFound("test error"),
	}
	qs := NewQueryService(embedder, graph, nil, cfg)

	events := make(chan types.QueryEvent, 256)
	err := qs.QueryStream(ctx, QueryRequest{
		Question: "test query",
		RepoSlug: "test-repo",
	}, events)

	if err == nil {
		t.Fatal("expected error from QueryStream")
	}
}

// fakeStreamingExplainer implements domain.LLMExplainer for the streaming
// query test. Drives both the structured (ExplainStructured) and the chunked
// (Explain) paths so QueryStream's explanation branches all get exercised.
type fakeStreamingExplainer struct {
	chunks       []domain.ExplainChunk
	chunkErr     error
	structuredOK bool
	rawJSON      []byte
}

func (f *fakeStreamingExplainer) Explain(ctx context.Context, req domain.ExplainRequest) (<-chan domain.ExplainChunk, error) {
	if f.chunkErr != nil {
		return nil, f.chunkErr
	}
	ch := make(chan domain.ExplainChunk, len(f.chunks)+1)
	for _, c := range f.chunks {
		ch <- c
	}
	close(ch)
	return ch, nil
}

func (f *fakeStreamingExplainer) ExplainStructured(ctx context.Context, req domain.ExplainRequest) ([]byte, error) {
	if f.structuredOK {
		return f.rawJSON, nil
	}
	return nil, domain.NotFound("no structured")
}

// TestQueryStreamWithChunkedExplainer drives the streaming explanation path:
// ExplainStructured returns no result, so the function falls through to
// Explain which yields tokens. Each non-empty chunk emits an
// explanation_token event; the Done flag terminates the loop cleanly.
func TestQueryStreamWithChunkedExplainer(t *testing.T) {
	ctx := context.Background()
	cfg := &config.Config{
		Query: config.QueryConfig{DefaultTopK: 10, MinScore: 0.0},
	}

	vectorHits := []types.ScoredNode{{
		Node:        types.CodeNode{ID: "func:pkg.A", Qualified: "pkg.A", Kind: types.NodeFunction},
		VectorScore: 0.9,
	}}
	embedder := &fakeStreamingEmbedder{query: make([]float32, 256)}
	graph := &fakeStreamingGraph{vectorHits: vectorHits, neighbors: &domain.Neighborhood{}}
	explainer := &fakeStreamingExplainer{
		chunks: []domain.ExplainChunk{
			{Text: "first "},
			{Text: "second "},
			{Text: "third", Done: true},
		},
	}

	qs := NewQueryService(embedder, graph, explainer, cfg)
	events := make(chan types.QueryEvent, 256)
	err := qs.QueryStream(ctx, QueryRequest{
		Question: "test query",
		RepoSlug: "test-repo",
		TopK:     10,
		// NoExplain false → exercises explainer branch
	}, events)
	if err != nil {
		t.Fatalf("QueryStream: %v", err)
	}

	tokens := 0
	var doneEvt *types.QueryEvent
	for evt := range events {
		if evt.Type == types.QueryEventExplanationToken {
			tokens++
		}
		if evt.Type == types.QueryEventDone {
			e := evt
			doneEvt = &e
		}
	}
	if tokens != 3 {
		t.Errorf("expected 3 explanation_token events, got %d", tokens)
	}
	if doneEvt == nil || doneEvt.Done == nil {
		t.Fatal("expected terminal done event with payload")
	}
	if doneEvt.Done.Explanation == "" {
		t.Errorf("expected accumulated explanation, got empty")
	}
}

// TestQueryStreamWithStructuredExplainer covers the path where
// ExplainStructured returns valid JSON — QueryStream short-circuits the
// chunked Explain branch.
func TestQueryStreamWithStructuredExplainer(t *testing.T) {
	ctx := context.Background()
	cfg := &config.Config{
		Query: config.QueryConfig{DefaultTopK: 10, MinScore: 0.0},
	}
	vectorHits := []types.ScoredNode{{
		Node:        types.CodeNode{ID: "func:pkg.A", Qualified: "pkg.A", Kind: types.NodeFunction},
		VectorScore: 0.9,
	}}
	embedder := &fakeStreamingEmbedder{query: make([]float32, 256)}
	graph := &fakeStreamingGraph{vectorHits: vectorHits, neighbors: &domain.Neighborhood{}}
	explainer := &fakeStreamingExplainer{
		structuredOK: true,
		rawJSON:      []byte(`{"overview":"structured ok","key_findings":[]}`),
	}

	qs := NewQueryService(embedder, graph, explainer, cfg)
	events := make(chan types.QueryEvent, 256)
	err := qs.QueryStream(ctx, QueryRequest{
		Question: "test query",
		RepoSlug: "test-repo",
		TopK:     10,
	}, events)
	if err != nil {
		t.Fatalf("QueryStream: %v", err)
	}
	var done *types.QueryEvent
	tokenCount := 0
	for evt := range events {
		if evt.Type == types.QueryEventExplanationToken {
			tokenCount++
		}
		if evt.Type == types.QueryEventDone {
			e := evt
			done = &e
		}
	}
	if tokenCount != 0 {
		t.Errorf("structured path should NOT emit token events, got %d", tokenCount)
	}
	if done == nil || done.Done == nil || done.Done.StructuredExplanation == nil {
		t.Fatalf("expected structured explanation in done event, got %+v", done)
	}
	if done.Done.Explanation != "structured ok" {
		t.Errorf("expected overview as flat explanation, got %q", done.Done.Explanation)
	}
}

// TestQueryStreamFilePathFilter exercises the file-path prefix filter.
func TestQueryStreamFilePathFilter(t *testing.T) {
	ctx := context.Background()
	cfg := &config.Config{Query: config.QueryConfig{DefaultTopK: 10, MinScore: 0.0}}

	hits := []types.ScoredNode{
		{Node: types.CodeNode{ID: "function:a", Qualified: "a", FilePath: "server/a.go", Kind: types.NodeFunction}, VectorScore: 0.9},
		{Node: types.CodeNode{ID: "function:b", Qualified: "b", FilePath: "client/b.go", Kind: types.NodeFunction}, VectorScore: 0.8},
	}
	embedder := &fakeStreamingEmbedder{query: make([]float32, 256)}
	graph := &fakeStreamingGraph{vectorHits: hits, neighbors: &domain.Neighborhood{}}
	qs := NewQueryService(embedder, graph, nil, cfg)

	events := make(chan types.QueryEvent, 256)
	err := qs.QueryStream(ctx, QueryRequest{
		Question: "test query",
		RepoSlug: "test-repo",
		TopK:     10,
		FilePath: "server/", // only "server/a.go" should survive
	}, events)
	if err != nil {
		t.Fatalf("QueryStream: %v", err)
	}
	var done *types.QueryEvent
	for evt := range events {
		if evt.Type == types.QueryEventDone {
			e := evt
			done = &e
		}
	}
	if done == nil || done.Done == nil {
		t.Fatal("missing done event")
	}
	for _, n := range done.Done.Nodes {
		if n.Node.FilePath != "server/a.go" {
			t.Errorf("file-path filter let through %q", n.Node.FilePath)
		}
	}
}

// TestQueryStreamTopKTruncation pushes more results than TopK to exercise
// the trim branch in QueryStream.
func TestQueryStreamTopKTruncation(t *testing.T) {
	ctx := context.Background()
	cfg := &config.Config{Query: config.QueryConfig{DefaultTopK: 3, MinScore: 0.0}}

	// Generate 8 distinct hits; TopK is 2 → output capped at 2.
	hits := make([]types.ScoredNode, 8)
	for i := range hits {
		hits[i] = types.ScoredNode{
			Node:        types.CodeNode{ID: "function:n", Qualified: "n", Kind: types.NodeFunction},
			VectorScore: 0.9 - float64(i)*0.01,
		}
	}
	embedder := &fakeStreamingEmbedder{query: make([]float32, 256)}
	graph := &fakeStreamingGraph{vectorHits: hits, neighbors: &domain.Neighborhood{}}
	qs := NewQueryService(embedder, graph, nil, cfg)

	events := make(chan types.QueryEvent, 256)
	err := qs.QueryStream(ctx, QueryRequest{
		Question: "test query",
		RepoSlug: "test-repo",
		TopK:     2, // smaller than fused size to trigger truncation
	}, events)
	if err != nil {
		t.Fatalf("QueryStream: %v", err)
	}
	var done *types.QueryEvent
	for evt := range events {
		if evt.Type == types.QueryEventDone {
			e := evt
			done = &e
		}
	}
	if done == nil || done.Done == nil {
		t.Fatal("missing done event")
	}
	if len(done.Done.Nodes) > 2 {
		t.Errorf("TopK trim missed: got %d nodes, want <= 2", len(done.Done.Nodes))
	}
}

// TestQueryStreamChunkError covers the explain-chunk-error branch in
// QueryStream — when the LLM streams a chunk whose Error is non-nil, the
// loop must break cleanly without panicking and the final done event still
// land.
func TestQueryStreamChunkError(t *testing.T) {
	ctx := context.Background()
	cfg := &config.Config{Query: config.QueryConfig{DefaultTopK: 10, MinScore: 0.0}}
	vectorHits := []types.ScoredNode{{
		Node:        types.CodeNode{ID: "func:pkg.A", Qualified: "pkg.A", Kind: types.NodeFunction},
		VectorScore: 0.9,
	}}
	embedder := &fakeStreamingEmbedder{query: make([]float32, 256)}
	graph := &fakeStreamingGraph{vectorHits: vectorHits, neighbors: &domain.Neighborhood{}}
	explainer := &fakeStreamingExplainer{
		chunks: []domain.ExplainChunk{
			{Text: "partial "},
			{Error: domain.NotFound("provider gave up")},
			{Text: "should not arrive"},
		},
	}

	qs := NewQueryService(embedder, graph, explainer, cfg)
	events := make(chan types.QueryEvent, 256)
	err := qs.QueryStream(ctx, QueryRequest{Question: "q", RepoSlug: "r", TopK: 10}, events)
	if err != nil {
		t.Fatalf("QueryStream: %v", err)
	}
	tokens := 0
	var done *types.QueryEvent
	for evt := range events {
		if evt.Type == types.QueryEventExplanationToken {
			tokens++
		}
		if evt.Type == types.QueryEventDone {
			e := evt
			done = &e
		}
	}
	if tokens != 1 {
		t.Errorf("expected 1 token before chunk error, got %d", tokens)
	}
	if done == nil {
		t.Fatal("expected done event even after chunk error")
	}
}

// TestQueryStreamExplainError covers the case where Explain itself returns
// an error (provider unreachable). QueryStream must keep going and emit done
// with no explanation rather than fail the whole search.
func TestQueryStreamExplainError(t *testing.T) {
	ctx := context.Background()
	cfg := &config.Config{Query: config.QueryConfig{DefaultTopK: 10, MinScore: 0.0}}
	vectorHits := []types.ScoredNode{{
		Node:        types.CodeNode{ID: "func:pkg.A", Qualified: "pkg.A", Kind: types.NodeFunction},
		VectorScore: 0.9,
	}}
	embedder := &fakeStreamingEmbedder{query: make([]float32, 256)}
	graph := &fakeStreamingGraph{vectorHits: vectorHits, neighbors: &domain.Neighborhood{}}
	explainer := &fakeStreamingExplainer{chunkErr: domain.NotFound("LLM down")}

	qs := NewQueryService(embedder, graph, explainer, cfg)
	events := make(chan types.QueryEvent, 256)
	err := qs.QueryStream(ctx, QueryRequest{Question: "q", RepoSlug: "r", TopK: 10}, events)
	if err != nil {
		t.Fatalf("QueryStream should soft-fail when LLM is down, got: %v", err)
	}
	var done *types.QueryEvent
	for evt := range events {
		if evt.Type == types.QueryEventDone {
			e := evt
			done = &e
		}
	}
	if done == nil || done.Done == nil {
		t.Fatal("expected done event")
	}
	if done.Done.Explanation != "" {
		t.Errorf("expected empty explanation when LLM fails, got %q", done.Done.Explanation)
	}
}

// TestQueryStreamWithStructuredExplainerInvalidJSON drops back to the
// chunked path when the structured response is unparsable.
func TestQueryStreamWithStructuredExplainerInvalidJSON(t *testing.T) {
	ctx := context.Background()
	cfg := &config.Config{Query: config.QueryConfig{DefaultTopK: 10, MinScore: 0.0}}
	vectorHits := []types.ScoredNode{{
		Node:        types.CodeNode{ID: "func:pkg.A", Qualified: "pkg.A", Kind: types.NodeFunction},
		VectorScore: 0.9,
	}}
	embedder := &fakeStreamingEmbedder{query: make([]float32, 256)}
	graph := &fakeStreamingGraph{vectorHits: vectorHits, neighbors: &domain.Neighborhood{}}
	explainer := &fakeStreamingExplainer{
		structuredOK: true,
		rawJSON:      []byte(`not json`),
		chunks:       []domain.ExplainChunk{{Text: "fallback ", Done: true}},
	}

	qs := NewQueryService(embedder, graph, explainer, cfg)
	events := make(chan types.QueryEvent, 256)
	err := qs.QueryStream(ctx, QueryRequest{Question: "q", RepoSlug: "r", TopK: 10}, events)
	if err != nil {
		t.Fatalf("QueryStream: %v", err)
	}
	var done *types.QueryEvent
	tokens := 0
	for evt := range events {
		if evt.Type == types.QueryEventExplanationToken {
			tokens++
		}
		if evt.Type == types.QueryEventDone {
			e := evt
			done = &e
		}
	}
	if tokens == 0 {
		t.Errorf("expected fallback chunked tokens after invalid structured JSON")
	}
	if done == nil || done.Done.StructuredExplanation != nil {
		t.Errorf("structured should be nil after parse failure, got %+v", done)
	}
}

// TestQueryStreamEmbedderError covers the embed-failure path.
func TestQueryStreamEmbedderError(t *testing.T) {
	ctx := context.Background()
	cfg := &config.Config{Query: config.QueryConfig{DefaultTopK: 10, MinScore: 0.0}}

	embedder := &fakeStreamingEmbedder{err: domain.Validation("embed boom")}
	graph := &fakeStreamingGraph{}
	qs := NewQueryService(embedder, graph, nil, cfg)

	events := make(chan types.QueryEvent, 256)
	err := qs.QueryStream(ctx, QueryRequest{Question: "q", RepoSlug: "r"}, events)
	if err == nil {
		t.Fatal("expected embed error")
	}
	// Channel must drain to a closed state, not block.
	gotErrEvent := false
	for evt := range events {
		if evt.Type == types.QueryEventError {
			gotErrEvent = true
		}
	}
	if !gotErrEvent {
		t.Errorf("expected error event in stream")
	}
}
