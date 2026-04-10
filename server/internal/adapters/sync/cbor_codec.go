// Package sync provides adapters for P2P graph sync: serialization, authentication.
package sync

import (
	"crypto/sha256"
	"fmt"
	"sort"

	"github.com/fxamacker/cbor/v2"

	"github.com/commit0-dev/commit0/pkg/types"
	"github.com/commit0-dev/commit0/server/internal/domain"
)

// Compile-time interface check.
var _ domain.BundleCodec = (*CBORCodec)(nil)

// CBORCodec implements BundleCodec using canonical CBOR (RFC 7049 §3.9).
// Produces deterministic bytes for the same logical bundle, enabling reliable content hashing.
type CBORCodec struct {
	encMode cbor.EncMode
	decMode cbor.DecMode
}

// NewCBORCodec creates a codec using canonical CBOR encoding.
func NewCBORCodec() (*CBORCodec, error) {
	em, err := cbor.CanonicalEncOptions().EncMode()
	if err != nil {
		return nil, fmt.Errorf("cbor enc mode: %w", err)
	}
	dm, err := cbor.DecOptions{}.DecMode()
	if err != nil {
		return nil, fmt.Errorf("cbor dec mode: %w", err)
	}
	return &CBORCodec{encMode: em, decMode: dm}, nil
}

// Encode serializes a GraphBundle to canonical CBOR bytes.
func (c *CBORCodec) Encode(bundle *types.GraphBundle) ([]byte, error) {
	data, err := c.encMode.Marshal(bundle)
	if err != nil {
		return nil, fmt.Errorf("cbor encode: %w", err)
	}
	return data, nil
}

// Decode deserializes a GraphBundle from CBOR bytes.
func (c *CBORCodec) Decode(data []byte) (*types.GraphBundle, error) {
	var bundle types.GraphBundle
	if err := c.decMode.Unmarshal(data, &bundle); err != nil {
		return nil, types.BundleCorrupt(fmt.Sprintf("cbor decode: %v", err))
	}
	return &bundle, nil
}

// HashBundle computes a deterministic SHA-256 hash of the bundle's nodes and edges.
// The hash covers only Nodes + Edges (sorted deterministically), not metadata like
// CreatedAt or Signature — so the same graph data always produces the same hash.
func (c *CBORCodec) HashBundle(bundle *types.GraphBundle) (string, error) {
	// Sort nodes by ID for deterministic ordering.
	sorted := make([]types.SyncNode, len(bundle.Nodes))
	copy(sorted, bundle.Nodes)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })

	// Sort edges by FromID+Kind+ToID.
	sortedEdges := make([]types.SyncEdge, len(bundle.Edges))
	copy(sortedEdges, bundle.Edges)
	sort.Slice(sortedEdges, func(i, j int) bool {
		ki := string(sortedEdges[i].Kind) + sortedEdges[i].FromID + sortedEdges[i].ToID
		kj := string(sortedEdges[j].Kind) + sortedEdges[j].FromID + sortedEdges[j].ToID
		return ki < kj
	})

	// Encode nodes + edges together for hashing.
	hashInput := struct {
		Nodes []types.SyncNode `cbor:"1,keyasint"`
		Edges []types.SyncEdge `cbor:"2,keyasint"`
	}{
		Nodes: sorted,
		Edges: sortedEdges,
	}

	data, err := c.encMode.Marshal(hashInput)
	if err != nil {
		return "", fmt.Errorf("hash bundle: cbor encode: %w", err)
	}

	hash := sha256.Sum256(data)
	return fmt.Sprintf("%x", hash), nil
}
