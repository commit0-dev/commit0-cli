package app

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/commit0-dev/commit0/internal/domain"
)

// Session represents a multi-turn conversation session.
type Session struct {
	CreatedAt time.Time
	UpdatedAt time.Time
	ID        string
	RepoSlug  string
	Messages  []Message
}

// Message represents a single message in a session.
type Message struct {
	Timestamp time.Time
	Role      string
	Content   string
}

// SessionService manages conversation sessions.
type SessionService struct {
	sessions map[string]*Session
	counter  int64
	mu       sync.RWMutex
}

// NewSessionService creates a new session service.
func NewSessionService() *SessionService {
	return &SessionService{
		sessions: make(map[string]*Session),
		counter:  0,
	}
}

// CreateSession creates a new conversation session.
func (ss *SessionService) CreateSession(ctx context.Context, repoSlug string) (*Session, error) {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	ss.counter++
	id := fmt.Sprintf("sess-%d", ss.counter)
	now := timeNow()

	session := &Session{
		ID:        id,
		RepoSlug:  repoSlug,
		Messages:  []Message{},
		CreatedAt: now,
		UpdatedAt: now,
	}

	ss.sessions[id] = session
	return session, nil
}

// AppendMessage adds a message to a session.
func (ss *SessionService) AppendMessage(ctx context.Context, sessionID, role, content string) (*Session, error) {
	// Validate role
	if role != "user" && role != "assistant" {
		return nil, domain.Validation(fmt.Sprintf("invalid role: %s (must be 'user' or 'assistant')", role))
	}

	ss.mu.Lock()
	defer ss.mu.Unlock()

	session, exists := ss.sessions[sessionID]
	if !exists {
		return nil, domain.NotFound(fmt.Sprintf("session %s not found", sessionID))
	}

	// Append message
	session.Messages = append(session.Messages, Message{
		Role:      role,
		Content:   content,
		Timestamp: timeNow(),
	})

	// Update timestamp
	session.UpdatedAt = timeNow()

	return session, nil
}

// GetSession retrieves a session by ID.
func (ss *SessionService) GetSession(ctx context.Context, sessionID string) (*Session, error) {
	ss.mu.RLock()
	defer ss.mu.RUnlock()

	session, exists := ss.sessions[sessionID]
	if !exists {
		return nil, domain.NotFound(fmt.Sprintf("session %s not found", sessionID))
	}

	// Return a copy to prevent external mutations
	return &Session{
		ID:        session.ID,
		RepoSlug:  session.RepoSlug,
		Messages:  append([]Message{}, session.Messages...),
		CreatedAt: session.CreatedAt,
		UpdatedAt: session.UpdatedAt,
	}, nil
}

// ListSessions lists sessions, optionally filtered by repo.
func (ss *SessionService) ListSessions(ctx context.Context, repoSlug string) ([]Session, error) {
	ss.mu.RLock()
	defer ss.mu.RUnlock()

	var result []Session
	for _, session := range ss.sessions {
		// Filter by repo if specified
		if repoSlug != "" && session.RepoSlug != repoSlug {
			continue
		}
		// Copy session
		result = append(result, Session{
			ID:        session.ID,
			RepoSlug:  session.RepoSlug,
			Messages:  append([]Message{}, session.Messages...),
			CreatedAt: session.CreatedAt,
			UpdatedAt: session.UpdatedAt,
		})
	}

	// Sort by CreatedAt descending
	sort.Slice(result, func(i, j int) bool {
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})

	return result, nil
}

// DeleteSession removes a session.
func (ss *SessionService) DeleteSession(ctx context.Context, sessionID string) error {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	if _, exists := ss.sessions[sessionID]; !exists {
		return domain.NotFound(fmt.Sprintf("session %s not found", sessionID))
	}

	delete(ss.sessions, sessionID)
	return nil
}

// timeNow is a helper to get current time (used for testing).
func timeNow() time.Time {
	return time.Now()
}
