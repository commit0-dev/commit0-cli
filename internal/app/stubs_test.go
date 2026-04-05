package app

import (
	"context"

	"github.com/commit0-dev/commit0/internal/domain"
	"github.com/commit0-dev/commit0/pkg/types"
)

// Stub implementations for testing

type stubGraphStore struct {
	nodes    map[string]*types.CodeNode
	nodesByQ map[string]*types.CodeNode // keyed by "slug::qualified"
	repos    map[string]*types.Repo
	traceHops []types.TraceHop
	affected  []types.AffectedNode

	// Global error — returned by all methods unless overridden below.
	err error

	// Per-method error overrides (take priority over err when non-nil).
	deleteNodesErr error // DeleteNodesByRepo
	upsertRepoErr  error // UpsertRepo
	listReposErr   error // ListRepos
	traceErr       error // TraceForward / TraceReverse
	blastRadiusErr error // BlastRadius
	upsertBatchErr error // UpsertFileBatch
}

func newStubGraphStore() *stubGraphStore {
	return &stubGraphStore{
		nodes:    make(map[string]*types.CodeNode),
		nodesByQ: make(map[string]*types.CodeNode),
		repos:    make(map[string]*types.Repo),
	}
}

func (s *stubGraphStore) UpsertNode(ctx context.Context, node *types.CodeNode) error {
	if s.err != nil {
		return s.err
	}
	s.nodes[node.ID] = node
	return nil
}

func (s *stubGraphStore) GetNode(ctx context.Context, id string) (*types.CodeNode, error) {
	if s.err != nil {
		return nil, s.err
	}
	n, ok := s.nodes[id]
	if !ok {
		return nil, domain.NotFound("node not found")
	}
	return n, nil
}

func (s *stubGraphStore) GetNodeByQualified(ctx context.Context, repo, qualified string) (*types.CodeNode, error) {
	if s.err != nil {
		return nil, s.err
	}
	key := repo + "::" + qualified
	n, ok := s.nodesByQ[key]
	if !ok {
		return nil, domain.NotFound("node not found")
	}
	return n, nil
}

func (s *stubGraphStore) DeleteNode(ctx context.Context, id string) error {
	if s.err != nil {
		return s.err
	}
	delete(s.nodes, id)
	return nil
}

func (s *stubGraphStore) DeleteNodesByRepo(ctx context.Context, repo string) error {
	if s.deleteNodesErr != nil {
		return s.deleteNodesErr
	}
	if s.err != nil {
		return s.err
	}
	return nil
}

func (s *stubGraphStore) UpsertEdge(ctx context.Context, edge *types.CodeEdge) error {
	if s.err != nil {
		return s.err
	}
	return nil
}

func (s *stubGraphStore) DeleteEdgesForNode(ctx context.Context, nodeID string) error {
	return nil
}

func (s *stubGraphStore) TraceForward(ctx context.Context, startID string, depth int) ([]types.TraceHop, error) {
	if s.traceErr != nil {
		return nil, s.traceErr
	}
	if s.err != nil {
		return nil, s.err
	}
	return s.traceHops, nil
}

func (s *stubGraphStore) TraceReverse(ctx context.Context, startID string, depth int) ([]types.TraceHop, error) {
	if s.traceErr != nil {
		return nil, s.traceErr
	}
	if s.err != nil {
		return nil, s.err
	}
	return s.traceHops, nil
}

func (s *stubGraphStore) BlastRadius(ctx context.Context, targetID string, maxDepth int) ([]types.AffectedNode, error) {
	if s.blastRadiusErr != nil {
		return nil, s.blastRadiusErr
	}
	if s.err != nil {
		return nil, s.err
	}
	return s.affected, nil
}

func (s *stubGraphStore) UpsertFileBatch(ctx context.Context, nodes []types.CodeNode, edges []types.CodeEdge) error {
	if s.upsertBatchErr != nil {
		return s.upsertBatchErr
	}
	if s.err != nil {
		return s.err
	}
	for i := range nodes {
		s.nodes[nodes[i].ID] = &nodes[i]
	}
	return nil
}

func (s *stubGraphStore) UpsertRepo(ctx context.Context, repo *types.Repo) error {
	if s.upsertRepoErr != nil {
		return s.upsertRepoErr
	}
	if s.err != nil {
		return s.err
	}
	s.repos[repo.Slug] = repo
	return nil
}

func (s *stubGraphStore) GetRepo(ctx context.Context, slug string) (*types.Repo, error) {
	if s.err != nil {
		return nil, s.err
	}
	r, ok := s.repos[slug]
	if !ok {
		return nil, domain.NotFound("repo not found")
	}
	return r, nil
}

func (s *stubGraphStore) ListRepos(ctx context.Context) ([]types.Repo, error) {
	if s.listReposErr != nil {
		return nil, s.listReposErr
	}
	if s.err != nil {
		return nil, s.err
	}
	var result []types.Repo
	for _, r := range s.repos {
		result = append(result, *r)
	}
	return result, nil
}

func (s *stubGraphStore) ApplySchema(ctx context.Context) error {
	return nil
}

func (s *stubGraphStore) GetSchemaVersion(ctx context.Context) (int, error) {
	return 1, nil
}

// ----- vector index -----

type stubVectorIndex struct {
	results []types.ScoredNode
	err     error
}

func (s *stubVectorIndex) Search(ctx context.Context, query []float32, opts domain.VectorSearchOpts) ([]types.ScoredNode, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.results, nil
}

// ----- text index -----

type stubTextIndex struct {
	results []types.ScoredNode
	err     error
}

func (s *stubTextIndex) Search(ctx context.Context, query string, opts domain.TextSearchOpts) ([]types.ScoredNode, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.results, nil
}

// ----- embedder -----

type stubEmbedder struct {
	queryVec  []float32
	batchRes  []domain.EmbedResult
	err       error // returned by both EmbedBatch and EmbedQuery
	batchErr  error // returned only by EmbedBatch (takes priority over err)
	queryErr  error // returned only by EmbedQuery (takes priority over err)
	callCount int
}

func (s *stubEmbedder) EmbedBatch(ctx context.Context, inputs []domain.EmbedInput) ([]domain.EmbedResult, error) {
	if s.batchErr != nil {
		return nil, s.batchErr
	}
	if s.err != nil {
		return nil, s.err
	}
	s.callCount++
	return s.batchRes, nil
}

func (s *stubEmbedder) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
	if s.queryErr != nil {
		return nil, s.queryErr
	}
	if s.err != nil {
		return nil, s.err
	}
	return s.queryVec, nil
}

// ----- explainer -----

type stubExplainer struct {
	chunks []domain.ExplainChunk
	err    error // returned by Explain() itself (not a chunk error)
}

func (s *stubExplainer) Explain(ctx context.Context, req domain.ExplainRequest) (<-chan domain.ExplainChunk, error) {
	if s.err != nil {
		return nil, s.err
	}
	ch := make(chan domain.ExplainChunk, len(s.chunks))
	for _, chunk := range s.chunks {
		ch <- chunk
	}
	close(ch)
	return ch, nil
}

// ----- parser -----

type stubParser struct {
	result *domain.ParsedFile
	err    error
}

func (s *stubParser) Parse(ctx context.Context, file domain.FileEntry) (*domain.ParsedFile, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.result, nil
}

func (s *stubParser) SupportedLanguages() []string {
	return []string{"go", "python", "typescript", "javascript"}
}

// ----- file walker -----

type stubFileWalker struct {
	files []domain.FileEntry
	err   error
}

func (s *stubFileWalker) Walk(ctx context.Context, repoPath string, opts domain.WalkOpts) (<-chan domain.FileEntry, <-chan error) {
	fileCh := make(chan domain.FileEntry, len(s.files))
	errCh := make(chan error, 1)

	go func() {
		for _, f := range s.files {
			fileCh <- f
		}
		close(fileCh)
		if s.err != nil {
			errCh <- s.err
		} else {
			errCh <- nil
		}
	}()

	return fileCh, errCh
}
