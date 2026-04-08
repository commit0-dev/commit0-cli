package surreal

import (
	"context"
	"fmt"
	"time"

	surrealdb "github.com/surrealdb/surrealdb.go"
	"github.com/surrealdb/surrealdb.go/pkg/models"

	"github.com/commit0-dev/commit0/internal/domain"
	"github.com/commit0-dev/commit0/pkg/types"
)

// MemoryAdapter wraps SurrealAdapter to implement domain.MemoryStore.
type MemoryAdapter struct{ *SurrealAdapter }

// AsMemoryStore returns a domain.MemoryStore view of this adapter.
func (a *SurrealAdapter) AsMemoryStore() domain.MemoryStore { return &MemoryAdapter{a} }

// Compile-time check.
var _ domain.MemoryStore = (*MemoryAdapter)(nil)

type memoryRow struct {
	ID         *models.RecordID `json:"id"`
	Tier       string           `json:"tier"`
	SessionID  string           `json:"session_id"`
	RepoSlug   string           `json:"repo_slug"`
	Content    string           `json:"content"`
	Concepts   []string         `json:"concepts"`
	Embedding  []float32        `json:"embedding"`
	TokenCount int              `json:"token_count"`
	CreatedAt  time.Time        `json:"created_at"`
}

func (r memoryRow) toMemoryEntry() types.MemoryEntry {
	id := ""
	if r.ID != nil {
		id = fmt.Sprintf("memory:%v", r.ID.ID)
	}
	return types.MemoryEntry{
		ID:         id,
		Tier:       types.MemoryTier(r.Tier),
		SessionID:  r.SessionID,
		RepoSlug:   r.RepoSlug,
		Content:    r.Content,
		Concepts:   r.Concepts,
		Embedding:  r.Embedding,
		TokenCount: r.TokenCount,
		CreatedAt:  r.CreatedAt,
	}
}

// StoreMemory persists a memory entry.
func (m *MemoryAdapter) StoreMemory(ctx context.Context, entry *types.MemoryEntry) error {
	const q = `CREATE memory CONTENT {
		tier:        $tier,
		session_id:  $session_id,
		repo_slug:   $repo_slug,
		content:     $content,
		concepts:    $concepts,
		embedding:   $embedding,
		token_count: $token_count
	};`

	var embedding any = models.None
	if len(entry.Embedding) > 0 {
		embedding = entry.Embedding
	}
	var concepts any = models.None
	if len(entry.Concepts) > 0 {
		concepts = entry.Concepts
	}
	var sessionID any = models.None
	if entry.SessionID != "" {
		sessionID = entry.SessionID
	}

	_, err := surrealdb.Query[any](ctx, m.db, q, map[string]any{
		"tier":        string(entry.Tier),
		"session_id":  sessionID,
		"repo_slug":   entry.RepoSlug,
		"content":     entry.Content,
		"concepts":    concepts,
		"embedding":   embedding,
		"token_count": entry.TokenCount,
	})
	if err != nil {
		return fmt.Errorf("store memory: %w", err)
	}
	return nil
}

// RetrieveMemories returns top-K memories relevant to the query via HNSW vector search.
func (m *MemoryAdapter) RetrieveMemories(ctx context.Context, repoSlug string, queryVec []float32, topK int) ([]types.MemoryEntry, error) {
	if len(queryVec) == 0 || topK <= 0 {
		return nil, nil
	}

	const q = `SELECT *, vector::similarity::cosine(embedding, $vec) AS score
		FROM memory
		WHERE repo_slug = $repo AND tier = "persistent" AND embedding IS NOT NONE
		ORDER BY score DESC
		LIMIT $k;`

	results, err := surrealdb.Query[[]memoryRow](ctx, m.db, q, map[string]any{
		"vec":  queryVec,
		"repo": repoSlug,
		"k":    topK,
	})
	if err != nil {
		return nil, fmt.Errorf("retrieve memories: %w", err)
	}
	if results == nil || len(*results) == 0 {
		return nil, nil
	}

	var entries []types.MemoryEntry
	for _, r := range (*results)[0].Result {
		entries = append(entries, r.toMemoryEntry())
	}
	return entries, nil
}

// ListSessionMemories returns all memories for a session, ordered by creation time.
func (m *MemoryAdapter) ListSessionMemories(ctx context.Context, sessionID string) ([]types.MemoryEntry, error) {
	const q = `SELECT * FROM memory WHERE session_id = $sid ORDER BY created_at ASC;`

	results, err := surrealdb.Query[[]memoryRow](ctx, m.db, q, map[string]any{
		"sid": sessionID,
	})
	if err != nil {
		return nil, fmt.Errorf("list session memories: %w", err)
	}
	if results == nil || len(*results) == 0 {
		return nil, nil
	}

	var entries []types.MemoryEntry
	for _, r := range (*results)[0].Result {
		entries = append(entries, r.toMemoryEntry())
	}
	return entries, nil
}

// DeleteSessionMemories removes all memories for a session.
func (m *MemoryAdapter) DeleteSessionMemories(ctx context.Context, sessionID string) error {
	const q = `DELETE FROM memory WHERE session_id = $sid;`

	_, err := surrealdb.Query[any](ctx, m.db, q, map[string]any{
		"sid": sessionID,
	})
	if err != nil {
		return fmt.Errorf("delete session memories: %w", err)
	}
	return nil
}
