package surreal

import (
	"context"
	"time"

	"github.com/commit0-dev/commit0/pkg/types"
	"github.com/commit0-dev/commit0/server/internal/domain"
)

// openCodeGraphAdapter wraps SurrealAdapter to implement the OpenCodeGraph interface.
// Each method delegates to the existing GraphStore/VectorIndex/TextIndex methods.
type openCodeGraphAdapter struct {
	a *SurrealAdapter
}

// Compile-time interface check.
var _ domain.OpenCodeGraph = (*openCodeGraphAdapter)(nil)

// AsOpenCodeGraph returns the SurrealAdapter as an OpenCodeGraph implementation.
func (a *SurrealAdapter) AsOpenCodeGraph() domain.OpenCodeGraph {
	return &openCodeGraphAdapter{a: a}
}

// ── Node CRUD ─────────────────────────────────────────

func (g *openCodeGraphAdapter) PutNode(ctx context.Context, node *types.CodeNode) error {
	return g.a.UpsertNode(ctx, node)
}

func (g *openCodeGraphAdapter) GetNode(ctx context.Context, id string) (*types.CodeNode, error) {
	return g.a.GetNode(ctx, id)
}

func (g *openCodeGraphAdapter) FindNode(ctx context.Context, repo, qualified string) (*types.CodeNode, error) {
	return g.a.GetNodeByQualified(ctx, repo, qualified)
}

func (g *openCodeGraphAdapter) DeleteNode(ctx context.Context, id string) error {
	return g.a.DeleteNode(ctx, id)
}

// ── Edge CRUD ─────────────────────────────────────────

func (g *openCodeGraphAdapter) PutEdge(ctx context.Context, edge *types.CodeEdge) error {
	return g.a.UpsertEdge(ctx, edge)
}

func (g *openCodeGraphAdapter) DeleteEdgesFrom(ctx context.Context, nodeID string) error {
	return g.a.DeleteEdgesForNode(ctx, nodeID)
}

// ── Batch ─────────────────────────────────────────────

func (g *openCodeGraphAdapter) PutBatch(ctx context.Context, nodes []types.CodeNode, edges []types.CodeEdge) error {
	return g.a.UpsertFileBatch(ctx, nodes, edges)
}

func (g *openCodeGraphAdapter) DeleteByRepo(ctx context.Context, repo string) error {
	return g.a.DeleteNodesByRepo(ctx, repo)
}

func (g *openCodeGraphAdapter) DeleteByFile(ctx context.Context, repo, filePath string) error {
	return g.a.DeleteNodesByFile(ctx, repo, filePath)
}

// ── Traversal ─────────────────────────────────────────

func (g *openCodeGraphAdapter) TraverseGraph(ctx context.Context, startID string, edgeLabels []string, direction string, maxDepth int) ([]types.TraceHop, error) {
	return g.a.TraverseGraph(ctx, startID, edgeLabels, direction, maxDepth)
}

func (g *openCodeGraphAdapter) Neighbors(ctx context.Context, nodeID string) (*domain.Neighborhood, error) {
	return g.a.GetNeighborhood(ctx, nodeID)
}

// ── Search ────────────────────────────────────────────

func (g *openCodeGraphAdapter) GetNodeEmbedding(ctx context.Context, nodeID string) ([]float32, error) {
	return g.a.GetNodeEmbedding(ctx, nodeID)
}

func (g *openCodeGraphAdapter) VectorSearch(ctx context.Context, query []float32, opts domain.VectorSearchOpts) ([]types.ScoredNode, error) {
	return g.a.VectorSearch(ctx, query, opts)
}

func (g *openCodeGraphAdapter) TextSearch(ctx context.Context, query string, opts domain.TextSearchOpts) ([]types.ScoredNode, error) {
	return g.a.TextSearch(ctx, query, opts)
}

// ── Listing ───────────────────────────────────────────

func (g *openCodeGraphAdapter) ListNodes(ctx context.Context, repo string, opts domain.ListOpts) ([]types.CodeNode, error) {
	// Dispatch based on which opts are set
	if opts.IDsOnly {
		ids, err := g.a.ListNodeIDs(ctx, repo)
		if err != nil {
			return nil, err
		}
		nodes := make([]types.CodeNode, len(ids))
		for i, id := range ids {
			nodes[i] = types.CodeNode{ID: id}
		}
		return nodes, nil
	}
	if opts.FilePath != "" {
		return g.a.ListNodesByFile(ctx, repo, opts.FilePath)
	}
	if len(opts.Concepts) > 0 {
		limit := opts.Limit
		if limit <= 0 {
			limit = 100
		}
		return g.a.ListNodesByConcepts(ctx, repo, opts.Concepts, limit)
	}
	return g.a.ListAllNodes(ctx, repo)
}

func (g *openCodeGraphAdapter) ListEdges(ctx context.Context, repo string, labels []string) ([]types.CodeEdge, error) {
	if len(labels) == 1 && labels[0] == "route" {
		return g.a.ListRoutes(ctx, repo)
	}
	return g.a.ListAllEdges(ctx, repo)
}

func (g *openCodeGraphAdapter) ListFilePaths(ctx context.Context, repo string) ([]string, error) {
	return g.a.ListFilePaths(ctx, repo)
}

// ── Repo ──────────────────────────────────────────────

func (g *openCodeGraphAdapter) PutRepo(ctx context.Context, repo *types.Repo) error {
	return g.a.UpsertRepo(ctx, repo)
}

func (g *openCodeGraphAdapter) GetRepo(ctx context.Context, slug string) (*types.Repo, error) {
	return g.a.GetRepo(ctx, slug)
}

func (g *openCodeGraphAdapter) ListRepos(ctx context.Context) ([]types.Repo, error) {
	return g.a.ListRepos(ctx)
}

func (g *openCodeGraphAdapter) DeleteRepo(ctx context.Context, slug string) error {
	// DeleteNodesByRepo deletes the repo record itself (via DELETE type::record),
	// and REFERENCE ON DELETE CASCADE removes all nodes+edges.
	return g.a.DeleteNodesByRepo(ctx, slug)
}

func (g *openCodeGraphAdapter) FindRepoByRemoteURL(ctx context.Context, url string) (*types.Repo, error) {
	return g.a.FindRepoByRemoteURL(ctx, url)
}

func (g *openCodeGraphAdapter) UpdateRepoIndexedAt(ctx context.Context, slug string, t time.Time) error {
	return g.a.UpdateRepoIndexedAt(ctx, slug, t)
}

// ── Schema ────────────────────────────────────────────

func (g *openCodeGraphAdapter) ApplySchema(ctx context.Context) error {
	return g.a.ApplySchema(ctx)
}
