package app

import (
	"context"
	"testing"

	"github.com/commit0-dev/commit0/internal/domain"
)

func TestSessionServiceCreateSession(t *testing.T) {
	svc := NewSessionService()
	ctx := context.Background()

	session, err := svc.CreateSession(ctx, "my-repo")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	if session.ID == "" {
		t.Errorf("Session ID should not be empty")
	}

	if session.RepoSlug != "my-repo" {
		t.Errorf("RepoSlug = %s, want my-repo", session.RepoSlug)
	}

	if len(session.Messages) != 0 {
		t.Errorf("New session should have no messages, got %d", len(session.Messages))
	}
}

func TestSessionServiceAppendMessage(t *testing.T) {
	svc := NewSessionService()
	ctx := context.Background()

	session, err := svc.CreateSession(ctx, "my-repo")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	err = svc.AppendMessage(ctx, session.ID, "user", "Hello")
	if err != nil {
		t.Fatalf("AppendMessage failed: %v", err)
	}

	// Verify via GetSession since AppendMessage no longer returns the session.
	session, err = svc.GetSession(ctx, session.ID)
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}
	if len(session.Messages) != 1 {
		t.Errorf("Expected 1 message, got %d", len(session.Messages))
	}

	if session.Messages[0].Role != "user" {
		t.Errorf("Message Role = %s, want user", session.Messages[0].Role)
	}

	if session.Messages[0].Content != "Hello" {
		t.Errorf("Message Content = %s, want Hello", session.Messages[0].Content)
	}
}

func TestSessionServiceAppendMessageInvalidRole(t *testing.T) {
	svc := NewSessionService()
	ctx := context.Background()

	session, _ := svc.CreateSession(ctx, "my-repo")

	err := svc.AppendMessage(ctx, session.ID, "invalid", "text")
	if err == nil {
		t.Errorf("AppendMessage should fail with invalid role")
	}

	domErr, ok := err.(*domain.DomainError)
	if !ok {
		t.Errorf("error type = %T, want *DomainError", err)
	}

	if domErr.Code != domain.ErrValidation {
		t.Errorf("error code = %s, want validation", domErr.Code)
	}
}

func TestSessionServiceAppendMessageUnknownSession(t *testing.T) {
	svc := NewSessionService()
	ctx := context.Background()

	err := svc.AppendMessage(ctx, "unknown-id", "user", "text")
	if err == nil {
		t.Errorf("AppendMessage should fail for unknown session")
	}

	domErr, ok := err.(*domain.DomainError)
	if !ok {
		t.Errorf("error type = %T, want *DomainError", err)
	}

	if domErr.Code != domain.ErrNotFound {
		t.Errorf("error code = %s, want not_found", domErr.Code)
	}
}

func TestSessionServiceGetSession(t *testing.T) {
	svc := NewSessionService()
	ctx := context.Background()

	created, _ := svc.CreateSession(ctx, "my-repo")
	svc.AppendMessage(ctx, created.ID, "user", "Hello")

	retrieved, err := svc.GetSession(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}

	if len(retrieved.Messages) != 1 {
		t.Errorf("Retrieved session has %d messages, want 1", len(retrieved.Messages))
	}

	// Verify it's a copy, not the same reference
	if &retrieved == &created {
		t.Errorf("GetSession should return a copy, not original")
	}
}

func TestSessionServiceListSessions(t *testing.T) {
	svc := NewSessionService()
	ctx := context.Background()

	svc.CreateSession(ctx, "repo1")
	svc.CreateSession(ctx, "repo1")
	svc.CreateSession(ctx, "repo2")

	allSessions, err := svc.ListSessions(ctx, "")
	if err != nil {
		t.Fatalf("ListSessions failed: %v", err)
	}

	if len(allSessions) != 3 {
		t.Errorf("ListSessions returned %d sessions, want 3", len(allSessions))
	}

	repo1Sessions, err := svc.ListSessions(ctx, "repo1")
	if err != nil {
		t.Fatalf("ListSessions with filter failed: %v", err)
	}

	if len(repo1Sessions) != 2 {
		t.Errorf("ListSessions for repo1 returned %d sessions, want 2", len(repo1Sessions))
	}

	for _, s := range repo1Sessions {
		if s.RepoSlug != "repo1" {
			t.Errorf("Session RepoSlug = %s, want repo1", s.RepoSlug)
		}
	}
}

func TestSessionServiceDeleteSession(t *testing.T) {
	svc := NewSessionService()
	ctx := context.Background()

	session, _ := svc.CreateSession(ctx, "my-repo")

	err := svc.DeleteSession(ctx, session.ID)
	if err != nil {
		t.Fatalf("DeleteSession failed: %v", err)
	}

	_, err = svc.GetSession(ctx, session.ID)
	if err == nil {
		t.Errorf("GetSession should fail after DeleteSession")
	}
}

func TestSessionServiceDeleteSessionNotFound(t *testing.T) {
	svc := NewSessionService()
	ctx := context.Background()

	err := svc.DeleteSession(ctx, "unknown-id")
	if err == nil {
		t.Errorf("DeleteSession should fail for unknown session")
	}
}
