package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/commit0-dev/commit0/pkg/types"
	"github.com/commit0-dev/commit0/server/internal/domain"
)

// SyncService orchestrates graph bundle export, import, pull, and push operations.
type SyncService struct {
	exporter  domain.GraphExporter
	importer  domain.GraphImporter
	codec     domain.BundleCodec
	auth      domain.SyncAuth      // nil = no auth
	transport domain.PeerTransport // nil = no QUIC (file-only mode)
	peers     domain.PeerStore     // nil = no peer management
	scope     domain.ScopeStore    // nil = no scope enforcement
	indexSvc  *IndexService        // nil = no re-embed after import
	log       *slog.Logger
}

// SetIndexService attaches the index service for re-embedding after import.
func (s *SyncService) SetIndexService(indexSvc *IndexService) {
	s.indexSvc = indexSvc
}

// Compile-time check: SyncService implements PeerHandler.
var _ domain.PeerHandler = (*SyncService)(nil)

// NewSyncService creates a sync service.
func NewSyncService(
	exporter domain.GraphExporter,
	importer domain.GraphImporter,
	codec domain.BundleCodec,
	auth domain.SyncAuth,
) *SyncService {
	return &SyncService{
		exporter: exporter,
		importer: importer,
		codec:    codec,
		auth:     auth,
		log:      slog.Default().With("service", "sync"),
	}
}

// BuildBundle creates a signed GraphBundle for a repo.
func (s *SyncService) BuildBundle(ctx context.Context, repoSlug string) (*types.GraphBundle, error) {
	bundle, err := s.exporter.ExportBundle(ctx, repoSlug)
	if err != nil {
		return nil, fmt.Errorf("build bundle: %w", err)
	}

	// Compute content hash.
	hash, err := s.codec.HashBundle(bundle)
	if err != nil {
		return nil, fmt.Errorf("build bundle: hash: %w", err)
	}
	bundle.ContentHash = hash

	// Sign if auth is configured.
	if s.auth != nil {
		sig, err := s.auth.SignBundle(hash)
		if err != nil {
			return nil, fmt.Errorf("build bundle: sign: %w", err)
		}
		bundle.Signature = sig
	}

	s.log.Info("bundle built",
		"repo", repoSlug,
		"nodes", len(bundle.Nodes),
		"edges", len(bundle.Edges),
		"hash", hash[:12],
	)

	return bundle, nil
}

// ExportToFile serializes a bundle to a file.
func (s *SyncService) ExportToFile(ctx context.Context, repoSlug, path string) error {
	bundle, err := s.BuildBundle(ctx, repoSlug)
	if err != nil {
		return err
	}

	data, err := s.codec.Encode(bundle)
	if err != nil {
		return fmt.Errorf("export to file: encode: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("export to file: write: %w", err)
	}

	s.log.Info("bundle exported", "repo", repoSlug, "path", path, "bytes", len(data))
	return nil
}

// ImportFromFile reads a bundle file and imports it.
func (s *SyncService) ImportFromFile(ctx context.Context, path string) (*types.SyncResult, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("import from file: read: %w", err)
	}

	bundle, err := s.codec.Decode(data)
	if err != nil {
		return nil, fmt.Errorf("import from file: decode: %w", err)
	}

	return s.ImportBundle(ctx, bundle)
}

// ImportFromBytes decodes CBOR bytes and imports the resulting bundle.
func (s *SyncService) ImportFromBytes(ctx context.Context, data []byte) (*types.SyncResult, error) {
	bundle, err := s.codec.Decode(data)
	if err != nil {
		return nil, fmt.Errorf("import from bytes: decode: %w", err)
	}
	return s.ImportBundle(ctx, bundle)
}

// ImportBundle verifies and imports a GraphBundle.
func (s *SyncService) ImportBundle(ctx context.Context, bundle *types.GraphBundle) (*types.SyncResult, error) {
	// Verify content hash.
	hash, err := s.codec.HashBundle(bundle)
	if err != nil {
		return nil, fmt.Errorf("import bundle: hash: %w", err)
	}
	if bundle.ContentHash != "" && bundle.ContentHash != hash {
		return nil, types.BundleCorrupt(fmt.Sprintf(
			"content hash mismatch: expected %s, got %s", bundle.ContentHash, hash))
	}

	// Verify signature if auth is configured and bundle is signed.
	if s.auth != nil && bundle.Signature != "" {
		if err := s.auth.VerifyBundle(hash, bundle.Signature); err != nil {
			return nil, fmt.Errorf("import bundle: verify: %w", err)
		}
	}

	result, err := s.importer.ImportBundle(ctx, bundle)
	if err != nil {
		return nil, fmt.Errorf("import bundle: %w", err)
	}

	result.Direction = "import"

	// Trigger async re-embed for imported nodes.
	if result.NodesImported > 0 && s.indexSvc != nil {
		go func() {
			reembedResult, err := s.indexSvc.ReembedNeighborhood(context.Background(), bundle.RepoSlug, nil)
			if err != nil {
				s.log.Warn("re-embed after import failed", "repo", bundle.RepoSlug, "err", err)
			} else {
				s.log.Info("re-embed after import complete",
					"repo", bundle.RepoSlug,
					"updated", reembedResult.NodesUpdated,
					"skipped", reembedResult.Skipped,
				)
			}
		}()
		result.ReEmbedQueued = true
	}

	s.log.Info("bundle imported",
		"repo", bundle.RepoSlug,
		"nodes_imported", result.NodesImported,
		"nodes_skipped", result.NodesSkipped,
		"edges_imported", result.EdgesImported,
		"re_embed_queued", result.ReEmbedQueued,
	)

	return result, nil
}

// Manifest returns the sync manifest for a repo, including the ContentHash.
func (s *SyncService) Manifest(ctx context.Context, repoSlug string) (*types.SyncManifest, error) {
	manifest, err := s.exporter.ExportManifest(ctx, repoSlug)
	if err != nil {
		return nil, fmt.Errorf("manifest: %w", err)
	}

	// Compute ContentHash by building the full bundle and hashing it.
	// This is more expensive than a lightweight manifest but ensures
	// the hash reflects the actual graph data.
	bundle, err := s.exporter.ExportBundle(ctx, repoSlug)
	if err != nil {
		return manifest, nil // return manifest without hash if export fails
	}
	hash, err := s.codec.HashBundle(bundle)
	if err != nil {
		return manifest, nil
	}
	manifest.ContentHash = hash

	return manifest, nil
}

// SetTransport attaches the QUIC transport for pull/push operations.
func (s *SyncService) SetTransport(transport domain.PeerTransport, peers domain.PeerStore, scope domain.ScopeStore) {
	s.transport = transport
	s.peers = peers
	s.scope = scope
}

// Pull fetches a repo's graph from a remote peer: scope check → manifest compare → transfer → import.
func (s *SyncService) Pull(ctx context.Context, peerName, repoSlug string) (*types.SyncResult, error) {
	if s.transport == nil {
		return nil, fmt.Errorf("pull: QUIC transport not configured")
	}

	// Check scope.
	if s.scope != nil {
		inScope, err := s.scope.IsInScope(ctx, repoSlug)
		if err != nil {
			return nil, fmt.Errorf("pull: scope check: %w", err)
		}
		if !inScope {
			return nil, types.OutOfScope(fmt.Sprintf("repo %q not in sync scope — use 'scope add' first", repoSlug))
		}
	}

	// Resolve peer.
	peer, err := s.peers.GetPeer(ctx, peerName)
	if err != nil {
		return nil, fmt.Errorf("pull: %w", err)
	}

	// Compare manifests.
	remoteManifest, err := s.transport.PullManifest(ctx, peer, repoSlug)
	if err != nil {
		return nil, fmt.Errorf("pull: manifest: %w", err)
	}

	localManifest, _ := s.exporter.ExportManifest(ctx, repoSlug)
	if localManifest != nil && localManifest.ContentHash == remoteManifest.ContentHash {
		s.log.Info("pull: already up to date", "repo", repoSlug, "peer", peerName)
		return &types.SyncResult{
			RepoSlug:  repoSlug,
			Direction: "pull",
			PeerName:  peerName,
		}, nil
	}

	// Pull full bundle.
	bundle, err := s.transport.PullBundle(ctx, peer, repoSlug)
	if err != nil {
		return nil, fmt.Errorf("pull: bundle: %w", err)
	}

	result, err := s.ImportBundle(ctx, bundle)
	if err != nil {
		return nil, fmt.Errorf("pull: import: %w", err)
	}
	result.Direction = "pull"
	result.PeerName = peerName

	s.log.Info("pull complete",
		"repo", repoSlug, "peer", peerName,
		"nodes", result.NodesImported, "skipped", result.NodesSkipped,
	)
	return result, nil
}

// Push sends a repo's graph to a remote peer.
func (s *SyncService) Push(ctx context.Context, peerName, repoSlug string) (*types.SyncResult, error) {
	if s.transport == nil {
		return nil, fmt.Errorf("push: QUIC transport not configured")
	}

	peer, err := s.peers.GetPeer(ctx, peerName)
	if err != nil {
		return nil, fmt.Errorf("push: %w", err)
	}

	bundle, err := s.BuildBundle(ctx, repoSlug)
	if err != nil {
		return nil, fmt.Errorf("push: build bundle: %w", err)
	}

	result, err := s.transport.PushBundle(ctx, peer, bundle)
	if err != nil {
		return nil, fmt.Errorf("push: %w", err)
	}
	result.Direction = "push"
	result.PeerName = peerName

	s.log.Info("push complete", "repo", repoSlug, "peer", peerName, "nodes", result.NodesImported)
	return result, nil
}

// --- PeerHandler implementation (server side of QUIC transport) ---

func (s *SyncService) HandleManifestRequest(ctx context.Context, repoSlug string) (*types.SyncManifest, error) {
	return s.Manifest(ctx, repoSlug)
}

func (s *SyncService) HandleBundleRequest(ctx context.Context, repoSlug string) ([]byte, error) {
	bundle, err := s.BuildBundle(ctx, repoSlug)
	if err != nil {
		return nil, err
	}
	return s.codec.Encode(bundle)
}

func (s *SyncService) HandleDeltaRequest(ctx context.Context, repoSlug, baseCommit string) ([]byte, error) {
	// Delta not yet implemented — fall back to full bundle.
	return s.HandleBundleRequest(ctx, repoSlug)
}

func (s *SyncService) HandlePushBundle(ctx context.Context, data []byte) (*types.SyncResult, error) {
	return s.ImportFromBytes(ctx, data)
}

// --- Auto-sync & notifications ---

// NotifyPeers sends a lightweight notification to all registered peers
// that a repo has been updated. Peers with auto-pull enabled will pull automatically.
func (s *SyncService) NotifyPeers(ctx context.Context, repoSlug string) {
	if s.peers == nil || s.transport == nil {
		return
	}
	peers, err := s.peers.ListPeers(ctx)
	if err != nil {
		s.log.Warn("notify peers: list failed", "err", err)
		return
	}
	for _, peer := range peers {
		manifest, err := s.exporter.ExportManifest(ctx, repoSlug)
		if err != nil {
			continue
		}
		s.log.Info("notifying peer", "peer", peer.Name, "repo", repoSlug,
			"nodes", manifest.NodeCount, "commit", manifest.LastCommit)
		// In a full implementation, this would push the manifest via QUIC.
		// For now, peers poll or manually pull.
	}
}
