package surreal

import (
	"context"
	"fmt"
	"time"

	surrealdb "github.com/surrealdb/surrealdb.go"
	"github.com/surrealdb/surrealdb.go/pkg/models"

	"github.com/commit0-dev/commit0/server/internal/app"
)

// SessionAdapter wraps SurrealAdapter for chat session persistence.
type SessionAdapter struct{ *SurrealAdapter }

// AsSessionStore returns a session store view of this adapter.
func (a *SurrealAdapter) AsSessionStore() app.SessionStore { return &SessionAdapter{a} }

// CreateSession creates a new chat session in SurrealDB.
func (sa *SessionAdapter) CreateSession(ctx context.Context, repoSlug string) (*app.Session, error) {
	type createRow struct {
		ID        *models.RecordID `json:"id"`
		RepoSlug  string           `json:"repo_slug"`
		CreatedAt time.Time        `json:"created_at"`
		UpdatedAt time.Time        `json:"updated_at"`
	}

	q := `CREATE chat_session CONTENT { repo_slug: $repo, created_at: time::now(), updated_at: time::now() };`
	results, err := surrealdb.Query[[]createRow](ctx, sa.db, q, map[string]any{"repo": repoSlug})
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	if results == nil || len(*results) == 0 || len((*results)[0].Result) == 0 {
		return nil, fmt.Errorf("create session: empty result")
	}

	row := (*results)[0].Result[0]
	id := recordIDToString(row.ID)

	return &app.Session{
		ID:        id,
		RepoSlug:  repoSlug,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
	}, nil
}

// AppendMessage adds a message to a session.
func (sa *SessionAdapter) AppendMessage(ctx context.Context, sessionID, role, content string) error {
	q := `CREATE chat_message CONTENT { session_id: $sid, role: $role, content: $content, created_at: time::now() };`
	_, err := surrealdb.Query[any](ctx, sa.db, q, map[string]any{
		"sid":     sessionID,
		"role":    role,
		"content": content,
	})
	if err != nil {
		return fmt.Errorf("append message: %w", err)
	}

	// Update session's updated_at using the full record ID.
	uq := `UPDATE type::record($id) SET updated_at = time::now();`
	_, _ = surrealdb.Query[any](ctx, sa.db, uq, map[string]any{"id": sessionID})

	return nil
}

// GetSession retrieves a session with all its messages.
func (sa *SessionAdapter) GetSession(ctx context.Context, sessionID string) (*app.Session, error) {
	type sessRow struct {
		ID        *models.RecordID `json:"id"`
		RepoSlug  string           `json:"repo_slug"`
		CreatedAt time.Time        `json:"created_at"`
		UpdatedAt time.Time        `json:"updated_at"`
	}
	type msgRow struct {
		Role      string    `json:"role"`
		Content   string    `json:"content"`
		CreatedAt time.Time `json:"created_at"`
	}

	// Get session using full record ID.
	sq := `SELECT * FROM type::record($id);`
	sessResults, err := surrealdb.Query[[]sessRow](ctx, sa.db, sq, map[string]any{"id": sessionID})
	if err != nil {
		return nil, fmt.Errorf("get session: %w", err)
	}
	if sessResults == nil || len(*sessResults) == 0 || len((*sessResults)[0].Result) == 0 {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}

	row := (*sessResults)[0].Result[0]

	// Get messages by session_id string match.
	mq := `SELECT * FROM chat_message WHERE session_id = $sid ORDER BY created_at ASC;`
	msgResults, err := surrealdb.Query[[]msgRow](ctx, sa.db, mq, map[string]any{"sid": sessionID})
	if err != nil {
		return nil, fmt.Errorf("get messages: %w", err)
	}

	sess := &app.Session{
		ID:        sessionID,
		RepoSlug:  row.RepoSlug,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
	}

	if msgResults != nil && len(*msgResults) > 0 {
		for _, m := range (*msgResults)[0].Result {
			sess.Messages = append(sess.Messages, app.Message{
				Role:      m.Role,
				Content:   m.Content,
				Timestamp: m.CreatedAt,
			})
		}
	}

	return sess, nil
}

// ListSessions returns recent sessions for a repo, sorted by updated_at DESC.
// Does NOT load messages — caller should use GetSession for full content.
func (sa *SessionAdapter) ListSessions(ctx context.Context, repoSlug string) ([]*app.Session, error) {
	type sessRow struct {
		ID        *models.RecordID `json:"id"`
		RepoSlug  string           `json:"repo_slug"`
		CreatedAt time.Time        `json:"created_at"`
		UpdatedAt time.Time        `json:"updated_at"`
	}

	q := `SELECT * FROM chat_session WHERE repo_slug = $repo ORDER BY updated_at DESC LIMIT 10;`
	results, err := surrealdb.Query[[]sessRow](ctx, sa.db, q, map[string]any{"repo": repoSlug})
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	if results == nil || len(*results) == 0 {
		return nil, nil
	}

	var sessions []*app.Session
	for _, row := range (*results)[0].Result {
		sessions = append(sessions, &app.Session{
			ID:        recordIDToString(row.ID),
			RepoSlug:  row.RepoSlug,
			CreatedAt: row.CreatedAt,
			UpdatedAt: row.UpdatedAt,
		})
	}

	return sessions, nil
}

// DeleteSession removes a session and its messages.
func (sa *SessionAdapter) DeleteSession(ctx context.Context, sessionID string) error {
	// Delete messages first.
	mq := `DELETE FROM chat_message WHERE session_id = $sid;`
	_, _ = surrealdb.Query[any](ctx, sa.db, mq, map[string]any{"sid": sessionID})

	// Delete session using full record ID.
	sq := `DELETE type::record($id);`
	_, err := surrealdb.Query[any](ctx, sa.db, sq, map[string]any{"id": sessionID})
	return err
}

// recordIDToString converts a SurrealDB RecordID to "table:id" string format.
func recordIDToString(id *models.RecordID) string {
	if id == nil {
		return ""
	}
	return fmt.Sprintf("%s:%s", id.Table, id.ID)
}
