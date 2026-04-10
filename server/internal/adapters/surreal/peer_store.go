package surreal

import (
	"context"
	"fmt"
	"time"

	surrealdb "github.com/surrealdb/surrealdb.go"
	"github.com/surrealdb/surrealdb.go/pkg/models"

	"github.com/commit0-dev/commit0/pkg/types"
	"github.com/commit0-dev/commit0/server/internal/domain"
)

// PeerStoreAdapter wraps SurrealAdapter to implement domain.PeerStore.
type PeerStoreAdapter struct{ *SurrealAdapter }

// Compile-time interface check.
var _ domain.PeerStore = (*PeerStoreAdapter)(nil)

// AsPeerStore returns a PeerStore adapter.
func (a *SurrealAdapter) AsPeerStore() *PeerStoreAdapter {
	return &PeerStoreAdapter{a}
}

type peerRow struct {
	ID         *models.RecordID `json:"id"`
	Name       string           `json:"name"`
	Endpoint   string           `json:"endpoint"`
	APIURL     string           `json:"api_url"`
	AddedAt    string           `json:"added_at"`
	LastSyncAt *string          `json:"last_sync_at"`
}

func (p *PeerStoreAdapter) UpsertPeer(ctx context.Context, peer *types.PeerInfo) error {
	q := `UPSERT peer SET
		name       = $name,
		endpoint   = $endpoint,
		api_url    = $api_url,
		added_at   = $added_at
	WHERE name = $name;`

	params := map[string]any{
		"name":     peer.Name,
		"endpoint": peer.Endpoint,
		"api_url":  peer.APIURL,
		"added_at": peer.AddedAt.Format(time.RFC3339),
	}

	_, err := surrealdb.Query[any](ctx, p.db, q, params)
	if err != nil {
		return fmt.Errorf("upsert peer %s: %w", peer.Name, err)
	}
	return nil
}

func (p *PeerStoreAdapter) GetPeer(ctx context.Context, name string) (*types.PeerInfo, error) {
	q := `SELECT * FROM peer WHERE name = $name LIMIT 1;`
	results, err := surrealdb.Query[[]peerRow](ctx, p.db, q, map[string]any{"name": name})
	if err != nil {
		return nil, fmt.Errorf("get peer %s: %w", name, err)
	}
	if results == nil || len(*results) == 0 || len((*results)[0].Result) == 0 {
		return nil, domain.NotFound(fmt.Sprintf("peer %q not found", name))
	}
	return rowToPeerInfo((*results)[0].Result[0]), nil
}

func (p *PeerStoreAdapter) ListPeers(ctx context.Context) ([]types.PeerInfo, error) {
	q := `SELECT * FROM peer ORDER BY name;`
	results, err := surrealdb.Query[[]peerRow](ctx, p.db, q, nil)
	if err != nil {
		return nil, fmt.Errorf("list peers: %w", err)
	}
	if results == nil || len(*results) == 0 {
		return nil, nil
	}
	var peers []types.PeerInfo
	for _, r := range (*results)[0].Result {
		peers = append(peers, *rowToPeerInfo(r))
	}
	return peers, nil
}

func (p *PeerStoreAdapter) DeletePeer(ctx context.Context, name string) error {
	q := `DELETE FROM peer WHERE name = $name;`
	_, err := surrealdb.Query[any](ctx, p.db, q, map[string]any{"name": name})
	if err != nil {
		return fmt.Errorf("delete peer %s: %w", name, err)
	}
	return nil
}

func rowToPeerInfo(r peerRow) *types.PeerInfo {
	peer := &types.PeerInfo{
		Name:     r.Name,
		Endpoint: r.Endpoint,
		APIURL:   r.APIURL,
	}
	if t, err := time.Parse(time.RFC3339, r.AddedAt); err == nil {
		peer.AddedAt = t
	}
	if r.LastSyncAt != nil {
		if t, err := time.Parse(time.RFC3339, *r.LastSyncAt); err == nil {
			peer.LastSyncAt = &t
		}
	}
	return peer
}
