package app

import (
	"context"
	"fmt"
	"time"

	"github.com/commit0-dev/commit0/pkg/types"
	"github.com/commit0-dev/commit0/server/internal/domain"
)

// Stub implementations for testing

type stubGraphStore struct {
	traceErr              error
	err                   error
	deleteNodesErr        error
	upsertRepoErr         error
	listReposErr          error
	blastRadiusErr        error
	upsertBatchErr        error
	nodesByQ              map[string]*types.CodeNode
	repos                 map[string]*types.Repo
	nodes                 map[string]*types.CodeNode
	upsertBatchFn         func(ctx context.Context, nodes []types.CodeNode, edges []types.CodeEdge) error
	traceHops             []types.TraceHop
	affected              []types.AffectedNode
	neighborhood          *domain.Neighborhood
	dataFlowHops          []types.TraceHop
	nodeIDs               []string
	routeEdges            []types.CodeEdge
	vectorResults         []types.ScoredNode // for VectorSearch
	vectorErr             error              // for VectorSearch error
	textErr               error              // for TextSearch error
	findRepoByRemoteURLFn func(ctx context.Context, url string) (*types.Repo, error)
	listFilePathsFn       func(ctx context.Context, repoSlug string) ([]string, error)
	deleteByFileFn        func(ctx context.Context, repoSlug, filePath string) error
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

func (s *stubGraphStore) DeleteNodesByFile(ctx context.Context, repoSlug, filePath string) error {
	if s.deleteByFileFn != nil {
		return s.deleteByFileFn(ctx, repoSlug, filePath)
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

func (s *stubGraphStore) UpsertFileBatch(ctx context.Context, nodes []types.CodeNode, edges []types.CodeEdge) error {
	if s.upsertBatchFn != nil {
		return s.upsertBatchFn(ctx, nodes, edges)
	}
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

func (s *stubGraphStore) GetNeighborhood(ctx context.Context, nodeID string) (*domain.Neighborhood, error) {
	if s.err != nil {
		return nil, s.err
	}
	if s.neighborhood != nil {
		return s.neighborhood, nil
	}
	return &domain.Neighborhood{}, nil
}

func (s *stubGraphStore) TraverseGraph(_ context.Context, _ string, _ []string, direction string, _ int) ([]types.TraceHop, error) {
	if s.blastRadiusErr != nil {
		return nil, s.blastRadiusErr
	}
	if s.traceErr != nil {
		return nil, s.traceErr
	}
	if s.err != nil {
		return nil, s.err
	}
	// For reverse (blast), convert affected to hops
	if direction == "reverse" && len(s.affected) > 0 {
		hops := make([]types.TraceHop, len(s.affected))
		for i, a := range s.affected {
			hops[i] = types.TraceHop{Node: a.Node, Depth: a.HopCount}
		}
		return hops, nil
	}
	// For forward (trace), return traceHops
	if s.traceHops != nil {
		return s.traceHops, nil
	}
	if s.traceErr != nil {
		return nil, s.traceErr
	}
	return nil, s.err
}

func (s *stubGraphStore) TraceDataFlow(ctx context.Context, startID string, depth int, direction string) ([]types.TraceHop, error) {
	if s.traceErr != nil {
		return nil, s.traceErr
	}
	if s.err != nil {
		return nil, s.err
	}
	return s.dataFlowHops, nil
}

func (s *stubGraphStore) ListNodeIDs(ctx context.Context, repoSlug string) ([]string, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.nodeIDs, nil
}

func (s *stubGraphStore) ListAllNodes(_ context.Context, _ string) ([]types.CodeNode, error) {
	return nil, nil
}

func (s *stubGraphStore) ListFilePaths(ctx context.Context, repoSlug string) ([]string, error) {
	if s.listFilePathsFn != nil {
		return s.listFilePathsFn(ctx, repoSlug)
	}
	return nil, nil
}

func (s *stubGraphStore) ListAllEdges(_ context.Context, _ string) ([]types.CodeEdge, error) {
	return nil, nil
}

func (s *stubGraphStore) ListNodesByFile(_ context.Context, _, _ string) ([]types.CodeNode, error) {
	return nil, nil
}

func (s *stubGraphStore) ListNodesByConcepts(_ context.Context, _ string, _ []string, _ int) ([]types.CodeNode, error) {
	return nil, nil
}

func (s *stubGraphStore) ListRoutes(_ context.Context, _ string) ([]types.CodeEdge, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.routeEdges, nil
}

func (s *stubGraphStore) UpdateRepoIndexedAt(_ context.Context, _ string, _ time.Time) error {
	return nil
}

func (s *stubGraphStore) FindRepoByRemoteURL(ctx context.Context, url string) (*types.Repo, error) {
	if s.findRepoByRemoteURLFn != nil {
		return s.findRepoByRemoteURLFn(ctx, url)
	}
	return nil, nil
}

func (s *stubGraphStore) TraceFieldFlow(_ context.Context, _ string, _ string, _ int, _ string) ([]types.FieldFlowHop, error) {
	return nil, nil
}

func (s *stubGraphStore) FindMutations(_ context.Context, _ string, _ string) ([]types.FieldFlowHop, error) {
	return nil, nil
}

func (s *stubGraphStore) ApplySchema(ctx context.Context) error {
	return nil
}

func (s *stubGraphStore) GetSchemaVersion(ctx context.Context) (int, error) {
	return 1, nil
}

// ── OpenCodeGraph adapter methods ──────────────────────────────────────
// These delegate to the existing stubGraphStore methods so the stub satisfies
// both GraphStore and OpenCodeGraph interfaces.

func (s *stubGraphStore) PutNode(ctx context.Context, node *types.CodeNode) error {
	return s.UpsertNode(ctx, node)
}
func (s *stubGraphStore) FindNode(ctx context.Context, repo, qualified string) (*types.CodeNode, error) {
	return s.GetNodeByQualified(ctx, repo, qualified)
}
func (s *stubGraphStore) PutEdge(ctx context.Context, edge *types.CodeEdge) error {
	return s.UpsertEdge(ctx, edge)
}
func (s *stubGraphStore) DeleteEdgesFrom(ctx context.Context, nodeID string) error {
	return s.DeleteEdgesForNode(ctx, nodeID)
}
func (s *stubGraphStore) PutBatch(ctx context.Context, nodes []types.CodeNode, edges []types.CodeEdge) error {
	return s.UpsertFileBatch(ctx, nodes, edges)
}
func (s *stubGraphStore) DeleteByRepo(ctx context.Context, repo string) error {
	return s.DeleteNodesByRepo(ctx, repo)
}
func (s *stubGraphStore) DeleteByFile(ctx context.Context, repo, filePath string) error {
	return s.DeleteNodesByFile(ctx, repo, filePath)
}
func (s *stubGraphStore) Neighbors(ctx context.Context, nodeID string) (*domain.Neighborhood, error) {
	return s.GetNeighborhood(ctx, nodeID)
}
func (s *stubGraphStore) GetNodeEmbedding(_ context.Context, _ string) ([]float32, error) {
	return nil, nil
}
func (s *stubGraphStore) VectorSearch(_ context.Context, _ []float32, _ domain.VectorSearchOpts) ([]types.ScoredNode, error) {
	if s.vectorErr != nil {
		return nil, s.vectorErr
	}
	return s.vectorResults, nil
}
func (s *stubGraphStore) TextSearch(_ context.Context, _ string, _ domain.TextSearchOpts) ([]types.ScoredNode, error) {
	if s.textErr != nil {
		return nil, s.textErr
	}
	return nil, nil
}
func (s *stubGraphStore) ListNodes(_ context.Context, _ string, opts domain.ListOpts) ([]types.CodeNode, error) {
	if opts.IDsOnly && len(s.nodeIDs) > 0 {
		nodes := make([]types.CodeNode, len(s.nodeIDs))
		for i, id := range s.nodeIDs {
			nodes[i] = types.CodeNode{ID: id}
		}
		return nodes, nil
	}
	return nil, s.err
}
func (s *stubGraphStore) ListEdges(_ context.Context, _ string, labels []string) ([]types.CodeEdge, error) {
	// Return routeEdges when requesting route label (for APISurface tests)
	for _, l := range labels {
		if l == "route" && len(s.routeEdges) > 0 {
			return s.routeEdges, nil
		}
	}
	return nil, nil
}
func (s *stubGraphStore) PutRepo(ctx context.Context, repo *types.Repo) error {
	return s.UpsertRepo(ctx, repo)
}
func (s *stubGraphStore) DeleteRepo(ctx context.Context, slug string) error {
	return s.DeleteNodesByRepo(ctx, slug)
}

// Compile-time check: stubGraphStore implements OpenCodeGraph.
var _ domain.OpenCodeGraph = (*stubGraphStore)(nil)

// ----- embedder -----

type stubEmbedder struct {
	err       error
	batchErr  error
	queryErr  error
	queryVec  []float32
	batchRes  []domain.EmbedResult
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
	err            error
	chunks         []domain.ExplainChunk
	structuredJSON []byte // if set, ExplainStructured returns this
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

func (s *stubExplainer) ExplainStructured(_ context.Context, _ domain.ExplainRequest) ([]byte, error) {
	// Default: return error so tests exercise the streaming Explain() fallback path.
	// Tests that want structured output should set structuredJSON.
	if s.structuredJSON != nil {
		return s.structuredJSON, nil
	}
	return nil, fmt.Errorf("structured output not configured in stub")
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
	// Return a shallow copy so concurrent parse goroutines don't share the same
	// Nodes slice — the production code stamps RepoSlug onto each node in-place.
	cp := *s.result
	cp.Nodes = make([]types.CodeNode, len(s.result.Nodes))
	copy(cp.Nodes, s.result.Nodes)
	return &cp, nil
}

func (s *stubParser) SupportedLanguages() []string {
	return []string{"go", "python", "typescript", "javascript"}
}

// ----- file walker -----

type stubFileWalker struct {
	err   error
	files []domain.FileEntry
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
