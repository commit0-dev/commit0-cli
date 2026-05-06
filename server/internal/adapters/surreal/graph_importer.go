package surreal

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/commit0-dev/commit0/pkg/types"
	"github.com/commit0-dev/commit0/server/internal/domain"
)

// Compile-time interface check.
var _ domain.GraphImporter = (*SurrealAdapter)(nil)

// ImportBundle merges a remote GraphBundle into the local store.
// Strategy: for each node, compare ContentHash — skip if equal (identical data),
// otherwise newest-wins based on LastModifiedAt.
// Imported nodes have Embedding=nil — re-embedding is queued separately.
func (a *SurrealAdapter) ImportBundle(ctx context.Context, bundle *types.GraphBundle) (*types.SyncResult, error) {
	log := slog.Default().With("adapter", "surreal", "op", "import_bundle", "repo", bundle.RepoSlug)
	start := time.Now()

	result := &types.SyncResult{
		RepoSlug:  bundle.RepoSlug,
		Direction: "import",
	}

	// Ensure the repo exists locally.
	_, err := a.GetRepo(ctx, bundle.RepoSlug)
	if err != nil {
		// Repo doesn't exist locally — create it.
		repo := &types.Repo{
			Slug:       bundle.RepoSlug,
			RemoteURL:  bundle.RemoteURL,
			Languages:  bundle.Languages,
			LastCommit: bundle.LastCommit,
			CreatedAt:  time.Now(),
		}
		if err := a.UpsertRepo(ctx, repo); err != nil {
			return nil, fmt.Errorf("import bundle: create repo: %w", err)
		}
		log.Info("created repo from bundle")
	}

	// Import nodes — merge SyncNode into existing CodeNode, preserving local heavy fields.
	for _, sn := range bundle.Nodes {
		existing, _ := a.GetNode(ctx, sn.ID)
		if existing != nil && existing.ContentHash == sn.ContentHash {
			result.NodesSkipped++
			continue
		}
		if existing != nil && !syncNodeIsNewer(sn, *existing) {
			result.NodesSkipped++
			continue
		}

		node := syncNodeToCodeNode(sn)
		// Preserve local heavy fields (Body, Summary, Concepts, Embedding)
		// so we don't destroy data that was locally indexed or LLM-generated.
		if existing != nil {
			node.Body = existing.Body
			node.Summary = existing.Summary
			node.Concepts = existing.Concepts
			node.Embedding = existing.Embedding
		}
		if err := a.UpsertNode(ctx, &node); err != nil {
			log.Warn("import node failed", "id", sn.ID, "err", err)
			continue
		}
		result.NodesImported++
	}

	// Import edges.
	for _, se := range bundle.Edges {
		edge := syncEdgeToCodeEdge(se)
		if err := a.UpsertEdge(ctx, &edge); err != nil {
			log.Warn("import edge failed", "from", se.FromID, "to", se.ToID, "err", err)
			continue
		}
		result.EdgesImported++
	}

	result.ReEmbedQueued = result.NodesImported > 0
	elapsed := time.Since(start)
	result.Timing = types.TimingInfo{TotalMS: elapsed.Milliseconds()}

	log.Info("import complete",
		"nodes_imported", result.NodesImported,
		"nodes_skipped", result.NodesSkipped,
		"edges_imported", result.EdgesImported,
		"re_embed_queued", result.ReEmbedQueued,
		"duration_ms", elapsed.Milliseconds(),
	)

	return result, nil
}

// syncNodeIsNewer returns true if the remote SyncNode is newer than the local CodeNode.
func syncNodeIsNewer(remote types.SyncNode, local types.CodeNode) bool {
	if remote.LastModifiedAt != nil && local.LastModifiedAt != nil {
		return remote.LastModifiedAt.After(*local.LastModifiedAt)
	}
	if remote.LastModifiedAt != nil {
		return true // remote has temporal data, local doesn't
	}
	return false
}

// syncNodeToCodeNode converts a SyncNode to a CodeNode with empty heavy fields.
func syncNodeToCodeNode(sn types.SyncNode) types.CodeNode {
	return types.CodeNode{
		ID:                 sn.ID,
		Kind:               sn.Kind,
		Name:               sn.Name,
		Qualified:          sn.Qualified,
		FilePath:           sn.FilePath,
		RepoSlug:           sn.RepoSlug,
		Language:           sn.Language,
		Visibility:         sn.Visibility,
		Signature:          sn.Signature,
		Docstring:          sn.Docstring,
		ContentHash:        sn.ContentHash,
		StartLine:          sn.StartLine,
		EndLine:            sn.EndLine,
		IntroducedCommit:   sn.IntroducedCommit,
		LastModifiedCommit: sn.LastModifiedCommit,
		IntroducedAt:       sn.IntroducedAt,
		LastModifiedAt:     sn.LastModifiedAt,
		// Heavy fields intentionally empty — re-embed after import.
		// Body:      ""
		// Embedding: nil
		// Summary:   ""
		// Concepts:  nil
	}
}

// syncEdgeToCodeEdge converts a SyncEdge to a CodeEdge.
func syncEdgeToCodeEdge(se types.SyncEdge) types.CodeEdge {
	return types.CodeEdge{
		Kind:             se.Kind,
		FromID:           se.FromID,
		ToID:             se.ToID,
		CallSite:         se.CallSite,
		CallType:         se.CallType,
		IsDynamic:        se.IsDynamic,
		Metadata:         se.Metadata,
		IntroducedCommit: se.IntroducedCommit,
		IntroducedAt:     se.IntroducedAt,
		RemovedCommit:    se.RemovedCommit,
	}
}
