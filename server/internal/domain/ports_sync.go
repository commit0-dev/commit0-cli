package domain

import (
	"context"

	"github.com/commit0-dev/commit0/pkg/types"
)

// ---------------------------------------------------------------------------
// P2P Graph Sync — Port Interfaces
// ---------------------------------------------------------------------------

// PeerStore persists remote peer registrations and sync state.
type PeerStore interface {
	UpsertPeer(ctx context.Context, peer *types.PeerInfo) error
	GetPeer(ctx context.Context, name string) (*types.PeerInfo, error)
	ListPeers(ctx context.Context) ([]types.PeerInfo, error)
	DeletePeer(ctx context.Context, name string) error
}

// ScopeStore manages the set of repos the local server syncs.
type ScopeStore interface {
	AddToScope(ctx context.Context, repoSlug string) error
	RemoveFromScope(ctx context.Context, repoSlug string) error
	ListScope(ctx context.Context) ([]types.SyncScope, error)
	IsInScope(ctx context.Context, repoSlug string) (bool, error)
}

// BundleCodec serializes/deserializes graph bundles to/from bytes.
type BundleCodec interface {
	// Encode serializes a bundle to canonical CBOR, optionally compressed.
	Encode(bundle *types.GraphBundle) ([]byte, error)
	// Decode deserializes a bundle from CBOR bytes.
	Decode(data []byte) (*types.GraphBundle, error)
	// HashBundle computes a deterministic SHA-256 content hash for integrity.
	HashBundle(bundle *types.GraphBundle) (string, error)
}

// GraphExporter extracts the syncable graph skeleton from the local store.
type GraphExporter interface {
	// ExportBundle builds a full graph bundle for a repo (no Body/Embedding/Summary/Concepts).
	ExportBundle(ctx context.Context, repoSlug string) (*types.GraphBundle, error)
	// ExportManifest builds a lightweight manifest for change detection.
	ExportManifest(ctx context.Context, repoSlug string) (*types.SyncManifest, error)
}

// GraphImporter merges remote graph data into the local store.
type GraphImporter interface {
	// ImportBundle imports a full graph bundle, merging with existing data.
	// Nodes with matching ContentHash are skipped; conflicts use newest-wins.
	// Returns the import result including count of imported/skipped nodes.
	ImportBundle(ctx context.Context, bundle *types.GraphBundle) (*types.SyncResult, error)
}

// SyncAuth handles authentication and authorization for sync operations.
// Built-in: passphrase HMAC. Vendors can implement PKI, OIDC, SAML, etc.
type SyncAuth interface {
	// SignBundle produces an integrity signature for a bundle's ContentHash.
	SignBundle(contentHash string) (string, error)
	// VerifyBundle checks the integrity signature.
	VerifyBundle(contentHash, signature string) error
}
