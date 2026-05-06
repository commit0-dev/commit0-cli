package surreal

import (
	"context"
	"fmt"
	"time"

	"github.com/commit0-dev/commit0/pkg/types"
	"github.com/commit0-dev/commit0/server/internal/domain"
)

// Compile-time interface check.
var _ domain.GraphExporter = (*SurrealAdapter)(nil)

// ExportBundle builds a full GraphBundle for a repo by querying all nodes
// and edges, stripping heavy fields (Body, Embedding, Summary, Concepts).
func (a *SurrealAdapter) ExportBundle(ctx context.Context, repoSlug string) (*types.GraphBundle, error) {
	repo, err := a.GetRepo(ctx, repoSlug)
	if err != nil {
		return nil, fmt.Errorf("export bundle: get repo: %w", err)
	}

	nodes, err := a.ListAllNodes(ctx, repoSlug)
	if err != nil {
		return nil, fmt.Errorf("export bundle: list nodes: %w", err)
	}

	edges, err := a.ListAllEdges(ctx, repoSlug)
	if err != nil {
		return nil, fmt.Errorf("export bundle: list edges: %w", err)
	}

	syncNodes := make([]types.SyncNode, 0, len(nodes))
	for _, n := range nodes {
		syncNodes = append(syncNodes, nodeToSyncNode(n))
	}

	syncEdges := make([]types.SyncEdge, 0, len(edges))
	for _, e := range edges {
		syncEdges = append(syncEdges, edgeToSyncEdge(e))
	}

	return &types.GraphBundle{
		FormatVersion: 1,
		RepoSlug:      repo.Slug,
		RemoteURL:     repo.RemoteURL,
		LastCommit:    repo.LastCommit,
		Languages:     repo.Languages,
		CreatedAt:     time.Now(),
		Nodes:         syncNodes,
		Edges:         syncEdges,
	}, nil
}

// ExportManifest builds a lightweight manifest for change detection.
// Contains no code intelligence — safe to exchange before authentication.
func (a *SurrealAdapter) ExportManifest(ctx context.Context, repoSlug string) (*types.SyncManifest, error) {
	repo, err := a.GetRepo(ctx, repoSlug)
	if err != nil {
		return nil, fmt.Errorf("export manifest: get repo: %w", err)
	}

	nodeIDs, err := a.ListNodeIDs(ctx, repoSlug)
	if err != nil {
		return nil, fmt.Errorf("export manifest: count nodes: %w", err)
	}

	edges, err := a.ListAllEdges(ctx, repoSlug)
	if err != nil {
		return nil, fmt.Errorf("export manifest: count edges: %w", err)
	}

	updatedAt := repo.CreatedAt
	if repo.LastIndexedAt != nil {
		updatedAt = *repo.LastIndexedAt
	}

	return &types.SyncManifest{
		RepoSlug:   repo.Slug,
		RemoteURL:  repo.RemoteURL,
		LastCommit: repo.LastCommit,
		NodeCount:  len(nodeIDs),
		EdgeCount:  len(edges),
		UpdatedAt:  updatedAt,
	}, nil
}

// nodeToSyncNode converts a CodeNode to a SyncNode, stripping heavy fields.
func nodeToSyncNode(n types.CodeNode) types.SyncNode {
	return types.SyncNode{
		ID:                 n.ID,
		Kind:               n.Kind,
		Name:               n.Name,
		Qualified:          n.Qualified,
		FilePath:           n.FilePath,
		RepoSlug:           n.RepoSlug,
		Language:           n.Language,
		Visibility:         n.Visibility,
		Signature:          n.Signature,
		Docstring:          n.Docstring,
		ContentHash:        n.ContentHash,
		StartLine:          n.StartLine,
		EndLine:            n.EndLine,
		IntroducedCommit:   n.IntroducedCommit,
		LastModifiedCommit: n.LastModifiedCommit,
		IntroducedAt:       n.IntroducedAt,
		LastModifiedAt:     n.LastModifiedAt,
	}
}

// edgeToSyncEdge converts a CodeEdge to a SyncEdge.
func edgeToSyncEdge(e types.CodeEdge) types.SyncEdge {
	return types.SyncEdge{
		Kind:             e.Kind,
		FromID:           e.FromID,
		ToID:             e.ToID,
		CallSite:         e.CallSite,
		CallType:         e.CallType,
		IsDynamic:        e.IsDynamic,
		Metadata:         e.Metadata,
		IntroducedCommit: e.IntroducedCommit,
		IntroducedAt:     e.IntroducedAt,
		RemovedCommit:    e.RemovedCommit,
	}
}
