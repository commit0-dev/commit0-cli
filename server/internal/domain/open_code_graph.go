package domain

import (
	"context"
	"time"

	"github.com/commit0-dev/commit0/pkg/types"
)

// OpenCodeGraph is the single port for all graph operations.
//
// It replaces 4 interfaces: GraphStore (26 methods), VectorIndex (1),
// TextIndex (1), FieldFlowStore (2) — consolidated into 23 methods.
//
// Design: docs/OPEN_CODE_GRAPH.md
//
// Uses existing types (CodeNode, CodeEdge, Neighborhood) for zero-friction
// service migration. GraphNode/GraphEdge types exist for SDK/future use.
type OpenCodeGraph interface {
	// ── Node CRUD ─────────────────────────────────────────
	PutNode(ctx context.Context, node *types.CodeNode) error
	GetNode(ctx context.Context, id string) (*types.CodeNode, error)
	FindNode(ctx context.Context, repo, qualified string) (*types.CodeNode, error)
	DeleteNode(ctx context.Context, id string) error

	// ── Edge CRUD ─────────────────────────────────────────
	PutEdge(ctx context.Context, edge *types.CodeEdge) error
	DeleteEdgesFrom(ctx context.Context, nodeID string) error

	// ── Batch ─────────────────────────────────────────────
	PutBatch(ctx context.Context, nodes []types.CodeNode, edges []types.CodeEdge) error
	DeleteByRepo(ctx context.Context, repo string) error
	DeleteByFile(ctx context.Context, repo, filePath string) error

	// ── Traversal ─────────────────────────────────────────
	// TraverseGraph follows edges of specified labels from startID.
	// direction: "forward" or "reverse". edgeLabels empty = ["calls"].
	TraverseGraph(ctx context.Context, startID string, edgeLabels []string, direction string, maxDepth int) ([]types.TraceHop, error)

	// Neighbors returns the immediate graph context: callers, callees,
	// data flow sources/sinks, field reads/writes.
	Neighbors(ctx context.Context, nodeID string) (*Neighborhood, error)

	// ── Search ────────────────────────────────────────────
	GetNodeEmbedding(ctx context.Context, nodeID string) ([]float32, error)
	VectorSearch(ctx context.Context, query []float32, opts VectorSearchOpts) ([]types.ScoredNode, error)
	TextSearch(ctx context.Context, query string, opts TextSearchOpts) ([]types.ScoredNode, error)

	// ── Listing ───────────────────────────────────────────
	// ListNodes returns nodes matching the filter criteria.
	// Replaces: ListAllNodes, ListNodesByFile, ListNodesByConcepts, ListNodeIDs.
	ListNodes(ctx context.Context, repo string, opts ListOpts) ([]types.CodeNode, error)

	// ListEdges returns edges for a repo, optionally filtered by label.
	// labels=nil returns all edge types.
	ListEdges(ctx context.Context, repo string, labels []string) ([]types.CodeEdge, error)

	// ListFilePaths returns distinct file paths for all nodes in a repo.
	ListFilePaths(ctx context.Context, repo string) ([]string, error)

	// ── Repo ──────────────────────────────────────────────
	PutRepo(ctx context.Context, repo *types.Repo) error
	GetRepo(ctx context.Context, slug string) (*types.Repo, error)
	ListRepos(ctx context.Context) ([]types.Repo, error)
	DeleteRepo(ctx context.Context, slug string) error
	FindRepoByRemoteURL(ctx context.Context, url string) (*types.Repo, error)
	UpdateRepoIndexedAt(ctx context.Context, slug string, t time.Time) error

	// ── Schema ────────────────────────────────────────────
	ApplySchema(ctx context.Context) error
}

// ListOpts configures the ListNodes query.
// Zero value of each field means "no filter" (return all).
type ListOpts struct {
	// Labels filters by node kind/label (e.g., ["function", "class"]).
	Labels []string
	// FilePath filters nodes by file path prefix.
	FilePath string
	// Concepts filters nodes whose concepts overlap with these tags.
	Concepts []string
	// Limit caps the number of results. 0 = unlimited.
	Limit int
	// IDsOnly returns only IDs (no body, embedding, etc.). Lightweight.
	IDsOnly bool
}
