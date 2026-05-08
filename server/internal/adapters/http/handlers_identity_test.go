package http

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/commit0-dev/commit0/pkg/types"
	"github.com/commit0-dev/commit0/server/internal/app"
	"github.com/commit0-dev/commit0/server/internal/domain"
)

// stubUserStoreHTTP / stubTeamStoreHTTP — local test doubles for the
// http package. Mirrors the app-package stubs but separately scoped so
// the two test suites don't fight over names.

type stubUserStoreHTTP struct {
	users map[string]*types.User
}

func newStubUserStoreHTTP() *stubUserStoreHTTP {
	return &stubUserStoreHTTP{users: map[string]*types.User{}}
}

func (s *stubUserStoreHTTP) UpsertUser(_ context.Context, u *types.User) error {
	cp := *u
	s.users[u.ID] = &cp
	return nil
}
func (s *stubUserStoreHTTP) GetUser(_ context.Context, id string) (*types.User, error) {
	u, ok := s.users[id]
	if !ok {
		return nil, domain.NotFound("user " + id + " not found")
	}
	cp := *u
	return &cp, nil
}
func (s *stubUserStoreHTTP) GetUserByEmail(_ context.Context, _ string) (*types.User, error) {
	return nil, domain.NotFound("not implemented in stub")
}
func (s *stubUserStoreHTTP) ListUsers(_ context.Context) ([]types.User, error) {
	out := make([]types.User, 0, len(s.users))
	for _, u := range s.users {
		out = append(out, *u)
	}
	return out, nil
}
func (s *stubUserStoreHTTP) DeleteUser(_ context.Context, id string) error {
	delete(s.users, id)
	return nil
}

type stubTeamStoreHTTP struct {
	teams       map[string]*types.Team
	memberships []types.TeamMembership
}

func newStubTeamStoreHTTP() *stubTeamStoreHTTP {
	return &stubTeamStoreHTTP{teams: map[string]*types.Team{}}
}

func (s *stubTeamStoreHTTP) UpsertTeam(_ context.Context, t *types.Team) error {
	cp := *t
	s.teams[t.ID] = &cp
	return nil
}
func (s *stubTeamStoreHTTP) GetTeam(_ context.Context, id string) (*types.Team, error) {
	t, ok := s.teams[id]
	if !ok {
		return nil, domain.NotFound("team " + id + " not found")
	}
	cp := *t
	return &cp, nil
}
func (s *stubTeamStoreHTTP) ListTeams(_ context.Context) ([]types.Team, error) {
	out := make([]types.Team, 0, len(s.teams))
	for _, t := range s.teams {
		out = append(out, *t)
	}
	return out, nil
}
func (s *stubTeamStoreHTTP) DeleteTeam(_ context.Context, id string) error {
	delete(s.teams, id)
	return nil
}
func (s *stubTeamStoreHTTP) AddMember(_ context.Context, m *types.TeamMembership) error {
	for i, existing := range s.memberships {
		if existing.UserID == m.UserID && existing.TeamID == m.TeamID {
			s.memberships[i] = *m
			return nil
		}
	}
	s.memberships = append(s.memberships, *m)
	return nil
}
func (s *stubTeamStoreHTTP) RemoveMember(_ context.Context, userID, teamID string) error {
	filtered := s.memberships[:0]
	for _, m := range s.memberships {
		if m.UserID != userID || m.TeamID != teamID {
			filtered = append(filtered, m)
		}
	}
	s.memberships = filtered
	return nil
}
func (s *stubTeamStoreHTTP) ListMembers(_ context.Context, teamID string) ([]types.TeamMembership, error) {
	var out []types.TeamMembership
	for _, m := range s.memberships {
		if m.TeamID == teamID {
			out = append(out, m)
		}
	}
	return out, nil
}
func (s *stubTeamStoreHTTP) ListUserTeams(_ context.Context, userID string) ([]types.TeamMembership, error) {
	var out []types.TeamMembership
	for _, m := range s.memberships {
		if m.UserID == userID {
			out = append(out, m)
		}
	}
	return out, nil
}

func newIdentityTestRouter(svc *app.IdentityService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(IdentityMiddleware(svc))
	if svc != nil {
		ih := NewIdentityHandlers(svc)
		r.GET("/whoami", ih.handleWhoAmI)
		r.POST("/users", ih.handleCreateUser)
		r.GET("/users", ih.handleListUsers)
		r.GET("/users/:id", ih.handleGetUser)
		r.DELETE("/users/:id", ih.handleDeleteUser)
		r.POST("/teams", ih.handleCreateTeam)
		r.GET("/teams", ih.handleListTeams)
		r.GET("/teams/:id", ih.handleGetTeam)
		r.DELETE("/teams/:id", ih.handleDeleteTeam)
		r.POST("/teams/:id/members", ih.handleAddMember)
		r.GET("/teams/:id/members", ih.handleListMembers)
		r.DELETE("/teams/:id/members/:user_id", ih.handleRemoveMember)
	}
	return r
}

func doJSON(t *testing.T, r http.Handler, method, path string, body any) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	var reqBody []byte
	if body != nil {
		var err error
		reqBody, err = json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	var got map[string]any
	if rec.Body.Len() > 0 {
		_ = json.Unmarshal(rec.Body.Bytes(), &got)
	}
	return rec, got
}

func TestIdentityCreateUser(t *testing.T) {
	users := newStubUserStoreHTTP()
	teams := newStubTeamStoreHTTP()
	svc := app.NewIdentityService(users, teams)
	r := newIdentityTestRouter(svc)

	rec, body := doJSON(t, r, http.MethodPost, "/users", map[string]any{
		"id":           "alice",
		"email":        "alice@example.com",
		"display_name": "Alice",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%v", rec.Code, body)
	}
	if body["id"] != "alice" || body["email"] != "alice@example.com" {
		t.Errorf("response missing fields: %+v", body)
	}
}

func TestIdentityCreateUserValidationError(t *testing.T) {
	svc := app.NewIdentityService(newStubUserStoreHTTP(), newStubTeamStoreHTTP())
	r := newIdentityTestRouter(svc)

	rec, _ := doJSON(t, r, http.MethodPost, "/users", map[string]any{
		"id":    "Bad-ID",
		"email": "alice@example.com",
	})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("validation should return 400, got %d", rec.Code)
	}

	// Missing required fields → bind error → 400.
	rec, _ = doJSON(t, r, http.MethodPost, "/users", map[string]any{"id": "alice"})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("missing email should return 400, got %d", rec.Code)
	}
}

func TestIdentityListGetDeleteUser(t *testing.T) {
	users := newStubUserStoreHTTP()
	users.users["alice"] = &types.User{ID: "alice", Email: "a@b.co"}
	users.users["bob"] = &types.User{ID: "bob", Email: "b@b.co"}
	svc := app.NewIdentityService(users, newStubTeamStoreHTTP())
	r := newIdentityTestRouter(svc)

	rec, body := doJSON(t, r, http.MethodGet, "/users", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list users = %d, body=%v", rec.Code, body)
	}
	if int(body["count"].(float64)) != 2 {
		t.Errorf("count = %v, want 2", body["count"])
	}

	rec, body = doJSON(t, r, http.MethodGet, "/users/alice", nil)
	if rec.Code != http.StatusOK || body["id"] != "alice" {
		t.Errorf("get user: %d %+v", rec.Code, body)
	}

	rec, _ = doJSON(t, r, http.MethodGet, "/users/missing", nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("missing user should return 404, got %d", rec.Code)
	}

	rec, body = doJSON(t, r, http.MethodDelete, "/users/alice", nil)
	if rec.Code != http.StatusOK || body["deleted"] != "alice" {
		t.Errorf("delete: %d %+v", rec.Code, body)
	}
}

func TestIdentityCreateTeamHandler(t *testing.T) {
	svc := app.NewIdentityService(newStubUserStoreHTTP(), newStubTeamStoreHTTP())
	r := newIdentityTestRouter(svc)

	rec, body := doJSON(t, r, http.MethodPost, "/teams", map[string]any{
		"id":          "platform",
		"name":        "Platform",
		"description": "Owns the platform layer",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%+v", rec.Code, body)
	}
	if body["id"] != "platform" {
		t.Errorf("team id wrong: %v", body["id"])
	}

	// Invalid team id.
	rec, _ = doJSON(t, r, http.MethodPost, "/teams", map[string]any{"id": "Bad"})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("invalid id status = %d, want 400", rec.Code)
	}
}

func TestIdentityListGetDeleteTeam(t *testing.T) {
	teams := newStubTeamStoreHTTP()
	teams.teams["platform"] = &types.Team{ID: "platform", Name: "Platform"}
	svc := app.NewIdentityService(newStubUserStoreHTTP(), teams)
	r := newIdentityTestRouter(svc)

	rec, body := doJSON(t, r, http.MethodGet, "/teams", nil)
	if rec.Code != http.StatusOK || int(body["count"].(float64)) != 1 {
		t.Errorf("list teams: %d %+v", rec.Code, body)
	}

	rec, _ = doJSON(t, r, http.MethodGet, "/teams/platform", nil)
	if rec.Code != http.StatusOK {
		t.Errorf("get team: %d", rec.Code)
	}
	rec, _ = doJSON(t, r, http.MethodGet, "/teams/missing", nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("missing team: %d", rec.Code)
	}

	rec, _ = doJSON(t, r, http.MethodDelete, "/teams/platform", nil)
	if rec.Code != http.StatusOK {
		t.Errorf("delete team: %d", rec.Code)
	}
}

func TestIdentityMembership(t *testing.T) {
	users := newStubUserStoreHTTP()
	users.users["alice"] = &types.User{ID: "alice", Email: "a@b.co"}
	teams := newStubTeamStoreHTTP()
	teams.teams["platform"] = &types.Team{ID: "platform"}
	svc := app.NewIdentityService(users, teams)
	r := newIdentityTestRouter(svc)

	// Add member.
	rec, body := doJSON(t, r, http.MethodPost, "/teams/platform/members", map[string]any{
		"user_id": "alice",
		"role":    "owner",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("add member: %d %+v", rec.Code, body)
	}

	// List members.
	rec, body = doJSON(t, r, http.MethodGet, "/teams/platform/members", nil)
	if rec.Code != http.StatusOK || int(body["count"].(float64)) != 1 {
		t.Errorf("list members: %d %+v", rec.Code, body)
	}

	// Add invalid (missing user).
	rec, _ = doJSON(t, r, http.MethodPost, "/teams/platform/members", map[string]any{
		"user_id": "ghost",
		"role":    "member",
	})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("missing user should be 400, got %d", rec.Code)
	}

	// Invalid body.
	rec, _ = doJSON(t, r, http.MethodPost, "/teams/platform/members", map[string]any{})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("missing fields should be 400, got %d", rec.Code)
	}

	// Remove.
	rec, _ = doJSON(t, r, http.MethodDelete, "/teams/platform/members/alice", nil)
	if rec.Code != http.StatusOK {
		t.Errorf("remove: %d", rec.Code)
	}

	// Idempotent remove.
	rec, _ = doJSON(t, r, http.MethodDelete, "/teams/platform/members/alice", nil)
	if rec.Code != http.StatusOK {
		t.Errorf("idempotent remove: %d", rec.Code)
	}
}

func TestIdentityWhoAmIAnonymous(t *testing.T) {
	svc := app.NewIdentityService(newStubUserStoreHTTP(), newStubTeamStoreHTTP())
	r := newIdentityTestRouter(svc)

	req := httptest.NewRequest(http.MethodGet, "/whoami", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("whoami: %d", rec.Code)
	}
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["anonymous"] != true {
		t.Errorf("expected anonymous=true, got %v", body)
	}
}

func TestIdentityWhoAmIAuthenticated(t *testing.T) {
	users := newStubUserStoreHTTP()
	users.users["alice"] = &types.User{ID: "alice", Email: "a@b.co", DisplayName: "Alice"}
	teams := newStubTeamStoreHTTP()
	teams.memberships = []types.TeamMembership{
		{UserID: "alice", TeamID: "platform", Role: types.TeamRoleMember},
	}
	svc := app.NewIdentityService(users, teams)
	r := newIdentityTestRouter(svc)

	req := httptest.NewRequest(http.MethodGet, "/whoami", nil)
	req.Header.Set(HeaderUserID, "alice")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("whoami: %d", rec.Code)
	}
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["user_id"] != "alice" || body["anonymous"] != false {
		t.Errorf("whoami body wrong: %+v", body)
	}
	if teamIDs, ok := body["team_ids"].([]any); !ok || len(teamIDs) != 1 {
		t.Errorf("team_ids wrong: %+v", body["team_ids"])
	}
}

func TestIdentityMiddlewareNilService(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(IdentityMiddleware(nil))
	called := false
	r.GET("/x", func(c *gin.Context) {
		called = true
		id := domain.IdentityFrom(c.Request.Context())
		if !id.IsAnonymous() {
			t.Errorf("expected anonymous identity with nil svc")
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if !called {
		t.Errorf("handler not called")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d", rec.Code)
	}
}
