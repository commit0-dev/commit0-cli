package surreal

import (
	"context"
	"fmt"
	"time"

	surrealdb "github.com/surrealdb/surrealdb.go"

	"github.com/commit0-dev/commit0/pkg/types"
	"github.com/commit0-dev/commit0/server/internal/domain"
)

// ScopeStoreAdapter wraps SurrealAdapter to implement domain.ScopeStore.
type ScopeStoreAdapter struct{ *SurrealAdapter }

// Compile-time interface check.
var _ domain.ScopeStore = (*ScopeStoreAdapter)(nil)

// AsScopeStore returns a ScopeStore adapter.
func (a *SurrealAdapter) AsScopeStore() *ScopeStoreAdapter {
	return &ScopeStoreAdapter{a}
}

type scopeRow struct {
	RepoSlug string `json:"repo_slug"`
	AddedAt  string `json:"added_at"`
}

func (s *ScopeStoreAdapter) AddToScope(ctx context.Context, repoSlug string) error {
	q := `UPSERT sync_scope SET repo_slug = $slug, added_at = time::now() WHERE repo_slug = $slug;`
	_, err := surrealdb.Query[any](ctx, s.db, q, map[string]any{"slug": repoSlug})
	if err != nil {
		return fmt.Errorf("add to scope %s: %w", repoSlug, err)
	}
	return nil
}

func (s *ScopeStoreAdapter) RemoveFromScope(ctx context.Context, repoSlug string) error {
	q := `DELETE FROM sync_scope WHERE repo_slug = $slug;`
	_, err := surrealdb.Query[any](ctx, s.db, q, map[string]any{"slug": repoSlug})
	if err != nil {
		return fmt.Errorf("remove from scope %s: %w", repoSlug, err)
	}
	return nil
}

func (s *ScopeStoreAdapter) ListScope(ctx context.Context) ([]types.SyncScope, error) {
	q := `SELECT * FROM sync_scope ORDER BY repo_slug;`
	results, err := surrealdb.Query[[]scopeRow](ctx, s.db, q, nil)
	if err != nil {
		return nil, fmt.Errorf("list scope: %w", err)
	}
	if results == nil || len(*results) == 0 {
		return nil, nil
	}
	var scopes []types.SyncScope
	for _, r := range (*results)[0].Result {
		sc := types.SyncScope{RepoSlug: r.RepoSlug}
		if t, err := time.Parse(time.RFC3339, r.AddedAt); err == nil {
			sc.AddedAt = t
		}
		scopes = append(scopes, sc)
	}
	return scopes, nil
}

func (s *ScopeStoreAdapter) IsInScope(ctx context.Context, repoSlug string) (bool, error) {
	q := `SELECT count() AS c FROM sync_scope WHERE repo_slug = $slug GROUP ALL;`
	type countRow struct {
		C int `json:"c"`
	}
	results, err := surrealdb.Query[[]countRow](ctx, s.db, q, map[string]any{"slug": repoSlug})
	if err != nil {
		return false, fmt.Errorf("is in scope %s: %w", repoSlug, err)
	}
	if results == nil || len(*results) == 0 || len((*results)[0].Result) == 0 {
		return false, nil
	}
	return (*results)[0].Result[0].C > 0, nil
}
