package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/commit0-dev/commit0/pkg/types"
	"github.com/commit0-dev/commit0/server/internal/domain"
)

// SyncService orchestrates graph bundle export and import operations.
type SyncService struct {
	exporter domain.GraphExporter
	importer domain.GraphImporter
	codec    domain.BundleCodec
	auth     domain.SyncAuth // nil = no auth (bundles unsigned)
	log      *slog.Logger
}

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
	s.log.Info("bundle imported",
		"repo", bundle.RepoSlug,
		"nodes_imported", result.NodesImported,
		"nodes_skipped", result.NodesSkipped,
		"edges_imported", result.EdgesImported,
	)

	return result, nil
}

// Manifest returns the sync manifest for a repo.
func (s *SyncService) Manifest(ctx context.Context, repoSlug string) (*types.SyncManifest, error) {
	manifest, err := s.exporter.ExportManifest(ctx, repoSlug)
	if err != nil {
		return nil, fmt.Errorf("manifest: %w", err)
	}
	return manifest, nil
}
