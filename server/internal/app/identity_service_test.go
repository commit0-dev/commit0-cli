package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/commit0-dev/commit0/pkg/types"
	"github.com/commit0-dev/commit0/server/internal/domain"
)

// stubUserStore + stubTeamStore — in-memory implementations used by all
// IdentityService unit tests. Both share the same map keys for
// deterministic ordering.

type stubUserStore struct {
	users     map[string]*types.User
	upsertErr error
	getErr    error
	listErr   error
	deleteErr error
}

func newStubUserStore() *stubUserStore {
	return &stubUserStore{users: map[string]*types.User{}}
}

func (s *stubUserStore) UpsertUser(_ context.Context, u *types.User) error {
	if s.upsertErr != nil {
		return s.upsertErr
	}
	cp := *u
	s.users[u.ID] = &cp
	return nil
}
func (s *stubUserStore) GetUser(_ context.Context, id string) (*types.User, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	u, ok := s.users[id]
	if !ok {
		return nil, domain.NotFound("user " + id + " not found")
	}
	cp := *u
	return &cp, nil
}
func (s *stubUserStore) GetUserByEmail(_ context.Context, email string) (*types.User, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	for _, u := range s.users {
		if u.Email == email {
			cp := *u
			return &cp, nil
		}
	}
	return nil, domain.NotFound("user with email " + email + " not found")
}
func (s *stubUserStore) ListUsers(_ context.Context) ([]types.User, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	out := make([]types.User, 0, len(s.users))
	for _, u := range s.users {
		out = append(out, *u)
	}
	return out, nil
}
func (s *stubUserStore) DeleteUser(_ context.Context, id string) error {
	if s.deleteErr != nil {
		return s.deleteErr
	}
	delete(s.users, id)
	return nil
}

type stubTeamStore struct {
	teams       map[string]*types.Team
	memberships []types.TeamMembership
	upsertErr   error
	getErr      error
	addErr      error
}

func newStubTeamStore() *stubTeamStore {
	return &stubTeamStore{teams: map[string]*types.Team{}}
}

func (s *stubTeamStore) UpsertTeam(_ context.Context, t *types.Team) error {
	if s.upsertErr != nil {
		return s.upsertErr
	}
	cp := *t
	s.teams[t.ID] = &cp
	return nil
}
func (s *stubTeamStore) GetTeam(_ context.Context, id string) (*types.Team, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	t, ok := s.teams[id]
	if !ok {
		return nil, domain.NotFound("team " + id + " not found")
	}
	cp := *t
	return &cp, nil
}
func (s *stubTeamStore) ListTeams(_ context.Context) ([]types.Team, error) {
	out := make([]types.Team, 0, len(s.teams))
	for _, t := range s.teams {
		out = append(out, *t)
	}
	return out, nil
}
func (s *stubTeamStore) DeleteTeam(_ context.Context, id string) error {
	delete(s.teams, id)
	// Cascade memberships.
	filtered := s.memberships[:0]
	for _, m := range s.memberships {
		if m.TeamID != id {
			filtered = append(filtered, m)
		}
	}
	s.memberships = filtered
	return nil
}
func (s *stubTeamStore) AddMember(_ context.Context, m *types.TeamMembership) error {
	if s.addErr != nil {
		return s.addErr
	}
	for i, existing := range s.memberships {
		if existing.UserID == m.UserID && existing.TeamID == m.TeamID {
			s.memberships[i] = *m
			return nil
		}
	}
	s.memberships = append(s.memberships, *m)
	return nil
}
func (s *stubTeamStore) RemoveMember(_ context.Context, userID, teamID string) error {
	filtered := s.memberships[:0]
	for _, m := range s.memberships {
		if m.UserID != userID || m.TeamID != teamID {
			filtered = append(filtered, m)
		}
	}
	s.memberships = filtered
	return nil
}
func (s *stubTeamStore) ListMembers(_ context.Context, teamID string) ([]types.TeamMembership, error) {
	var out []types.TeamMembership
	for _, m := range s.memberships {
		if m.TeamID == teamID {
			out = append(out, m)
		}
	}
	return out, nil
}
func (s *stubTeamStore) ListUserTeams(_ context.Context, userID string) ([]types.TeamMembership, error) {
	var out []types.TeamMembership
	for _, m := range s.memberships {
		if m.UserID == userID {
			out = append(out, m)
		}
	}
	return out, nil
}

// ─── User tests ────────────────────────────────────────────────────────────

func TestIdentityServiceCreateUserHappyPath(t *testing.T) {
	users := newStubUserStore()
	svc := NewIdentityService(users, nil)

	u := &types.User{ID: "alice", Email: "alice@example.com"}
	if err := svc.CreateUser(context.Background(), u); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if u.DisplayName != "alice" {
		t.Errorf("DisplayName default = %s, want alice", u.DisplayName)
	}
	if u.CreatedAt.IsZero() || u.UpdatedAt.IsZero() {
		t.Errorf("timestamps not stamped")
	}
	if _, ok := users.users["alice"]; !ok {
		t.Errorf("user not persisted")
	}
}

func TestIdentityServiceCreateUserPreservesExplicitDisplayName(t *testing.T) {
	users := newStubUserStore()
	svc := NewIdentityService(users, nil)
	u := &types.User{ID: "alice", Email: "alice@example.com", DisplayName: "Alice Smith"}
	if err := svc.CreateUser(context.Background(), u); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if u.DisplayName != "Alice Smith" {
		t.Errorf("explicit display name overwritten: %s", u.DisplayName)
	}
}

func TestIdentityServiceCreateUserValidation(t *testing.T) {
	users := newStubUserStore()
	svc := NewIdentityService(users, nil)
	cases := []struct {
		name string
		user *types.User
	}{
		{"nil", nil},
		{"empty id", &types.User{ID: "", Email: "alice@example.com"}},
		{"id starts with digit", &types.User{ID: "1alice", Email: "a@b.co"}},
		{"id contains uppercase", &types.User{ID: "Alice", Email: "a@b.co"}},
		{"id with underscore", &types.User{ID: "alice_smith", Email: "a@b.co"}},
		{"id too long", &types.User{ID: "a" + makeString(64, 'b'), Email: "a@b.co"}},
		{"missing @ in email", &types.User{ID: "alice", Email: "alice"}},
		{"too short email", &types.User{ID: "alice", Email: "@"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := svc.CreateUser(context.Background(), tc.user)
			if err == nil {
				t.Errorf("expected validation error")
				return
			}
			var dom *domain.DomainError
			if !errors.As(err, &dom) || dom.Code != domain.ErrValidation {
				t.Errorf("expected validation error, got %v", err)
			}
		})
	}
}

func TestIdentityServiceCreateUserNilStore(t *testing.T) {
	svc := NewIdentityService(nil, nil)
	err := svc.CreateUser(context.Background(), &types.User{ID: "alice", Email: "a@b.co"})
	if err == nil {
		t.Fatal("expected error when store is nil")
	}
	var dom *domain.DomainError
	if !errors.As(err, &dom) || dom.Code != domain.ErrUnavailable {
		t.Errorf("expected ErrUnavailable, got %v", err)
	}
}

func TestIdentityServiceGetUser(t *testing.T) {
	users := newStubUserStore()
	users.users["alice"] = &types.User{ID: "alice", Email: "a@b.co"}
	svc := NewIdentityService(users, nil)

	got, err := svc.GetUser(context.Background(), "alice")
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if got.ID != "alice" {
		t.Errorf("GetUser ID = %s, want alice", got.ID)
	}

	_, err = svc.GetUser(context.Background(), "bob")
	if err == nil {
		t.Errorf("GetUser for missing user should error")
	}

	// Nil store path.
	if _, err := NewIdentityService(nil, nil).GetUser(context.Background(), "x"); err == nil {
		t.Errorf("nil store should error")
	}
}

func TestIdentityServiceGetUserByEmail(t *testing.T) {
	users := newStubUserStore()
	users.users["alice"] = &types.User{ID: "alice", Email: "alice@x.io"}
	svc := NewIdentityService(users, nil)

	got, err := svc.GetUserByEmail(context.Background(), "alice@x.io")
	if err != nil || got.ID != "alice" {
		t.Errorf("GetUserByEmail = %+v, %v", got, err)
	}
	if _, err := svc.GetUserByEmail(context.Background(), "missing@x.io"); err == nil {
		t.Errorf("missing email should error")
	}
	if _, err := NewIdentityService(nil, nil).GetUserByEmail(context.Background(), "x"); err == nil {
		t.Errorf("nil store should error")
	}
}

func TestIdentityServiceListUsers(t *testing.T) {
	users := newStubUserStore()
	users.users["alice"] = &types.User{ID: "alice"}
	users.users["bob"] = &types.User{ID: "bob"}
	svc := NewIdentityService(users, nil)

	got, err := svc.ListUsers(context.Background())
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("ListUsers = %d, want 2", len(got))
	}

	// Nil store returns nil.
	got, err = NewIdentityService(nil, nil).ListUsers(context.Background())
	if err != nil || got != nil {
		t.Errorf("nil store: got %+v, %v", got, err)
	}
}

func TestIdentityServiceDeleteUser(t *testing.T) {
	users := newStubUserStore()
	users.users["alice"] = &types.User{ID: "alice"}
	svc := NewIdentityService(users, nil)
	if err := svc.DeleteUser(context.Background(), "alice"); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	if _, ok := users.users["alice"]; ok {
		t.Errorf("user not removed")
	}

	// Nil store errors.
	if err := NewIdentityService(nil, nil).DeleteUser(context.Background(), "x"); err == nil {
		t.Errorf("nil store should error")
	}
}

// ─── Team tests ────────────────────────────────────────────────────────────

func TestIdentityServiceCreateTeam(t *testing.T) {
	teams := newStubTeamStore()
	svc := NewIdentityService(nil, teams)
	team := &types.Team{ID: "platform"}
	if err := svc.CreateTeam(context.Background(), team); err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	if team.Name != "platform" {
		t.Errorf("default name = %s, want platform", team.Name)
	}
	if team.CreatedAt.IsZero() {
		t.Errorf("CreatedAt not stamped")
	}

	// Validation paths.
	cases := []*types.Team{nil, {ID: ""}, {ID: "Bad"}, {ID: "a_bad"}}
	for _, c := range cases {
		if err := svc.CreateTeam(context.Background(), c); err == nil {
			t.Errorf("expected validation error for %+v", c)
		}
	}

	// Nil store errors.
	if err := NewIdentityService(nil, nil).CreateTeam(context.Background(), &types.Team{ID: "x"}); err == nil {
		t.Errorf("nil store should error")
	}
}

func TestIdentityServiceGetListDeleteTeam(t *testing.T) {
	teams := newStubTeamStore()
	teams.teams["platform"] = &types.Team{ID: "platform", Name: "Platform"}
	svc := NewIdentityService(nil, teams)

	if got, err := svc.GetTeam(context.Background(), "platform"); err != nil || got.ID != "platform" {
		t.Errorf("GetTeam = %+v, %v", got, err)
	}
	if _, err := svc.GetTeam(context.Background(), "missing"); err == nil {
		t.Errorf("missing team should error")
	}
	if _, err := NewIdentityService(nil, nil).GetTeam(context.Background(), "x"); err == nil {
		t.Errorf("nil store should error")
	}

	got, err := svc.ListTeams(context.Background())
	if err != nil || len(got) != 1 {
		t.Errorf("ListTeams = %d, %v", len(got), err)
	}
	if _, err := NewIdentityService(nil, nil).ListTeams(context.Background()); err != nil {
		t.Errorf("nil store should be ok for List")
	}

	if err := svc.DeleteTeam(context.Background(), "platform"); err != nil {
		t.Errorf("DeleteTeam: %v", err)
	}
	if err := NewIdentityService(nil, nil).DeleteTeam(context.Background(), "x"); err == nil {
		t.Errorf("nil store should error on Delete")
	}
}

// ─── Membership tests ─────────────────────────────────────────────────────

func TestIdentityServiceAddMember(t *testing.T) {
	users := newStubUserStore()
	users.users["alice"] = &types.User{ID: "alice"}
	teams := newStubTeamStore()
	teams.teams["platform"] = &types.Team{ID: "platform"}
	svc := NewIdentityService(users, teams)

	m := &types.TeamMembership{UserID: "alice", TeamID: "platform", Role: types.TeamRoleOwner}
	if err := svc.AddMember(context.Background(), m); err != nil {
		t.Fatalf("AddMember: %v", err)
	}
	if m.JoinedAt.IsZero() {
		t.Errorf("JoinedAt not stamped")
	}
	if len(teams.memberships) != 1 {
		t.Errorf("membership not persisted")
	}

	// Idempotent — AddMember with same user/team replaces role.
	m2 := &types.TeamMembership{UserID: "alice", TeamID: "platform", Role: types.TeamRoleMember}
	if err := svc.AddMember(context.Background(), m2); err != nil {
		t.Fatalf("AddMember idempotent: %v", err)
	}
	if len(teams.memberships) != 1 || teams.memberships[0].Role != types.TeamRoleMember {
		t.Errorf("idempotent AddMember should update role, got %+v", teams.memberships)
	}
}

func TestIdentityServiceAddMemberValidation(t *testing.T) {
	users := newStubUserStore()
	users.users["alice"] = &types.User{ID: "alice"}
	teams := newStubTeamStore()
	teams.teams["platform"] = &types.Team{ID: "platform"}
	svc := NewIdentityService(users, teams)

	cases := []struct {
		name string
		m    *types.TeamMembership
	}{
		{"nil", nil},
		{"missing user id", &types.TeamMembership{TeamID: "platform", Role: types.TeamRoleMember}},
		{"missing team id", &types.TeamMembership{UserID: "alice", Role: types.TeamRoleMember}},
		{"invalid role", &types.TeamMembership{UserID: "alice", TeamID: "platform", Role: "admin"}},
		{"missing user", &types.TeamMembership{UserID: "ghost", TeamID: "platform", Role: types.TeamRoleMember}},
		{"missing team", &types.TeamMembership{UserID: "alice", TeamID: "ghost", Role: types.TeamRoleMember}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := svc.AddMember(context.Background(), tc.m); err == nil {
				t.Errorf("expected validation error")
			}
		})
	}

	// Nil store path.
	if err := NewIdentityService(nil, nil).AddMember(context.Background(), &types.TeamMembership{UserID: "a", TeamID: "b", Role: types.TeamRoleMember}); err == nil {
		t.Errorf("nil store should error")
	}
}

func TestIdentityServiceAddMemberPropagatesStoreError(t *testing.T) {
	users := newStubUserStore()
	users.users["alice"] = &types.User{ID: "alice"}
	teams := newStubTeamStore()
	teams.teams["platform"] = &types.Team{ID: "platform"}
	users.getErr = errors.New("db down")
	svc := NewIdentityService(users, teams)

	m := &types.TeamMembership{UserID: "alice", TeamID: "platform", Role: types.TeamRoleMember}
	if err := svc.AddMember(context.Background(), m); err == nil {
		t.Errorf("expected upstream error to propagate")
	}
}

func TestIdentityServiceRemoveMember(t *testing.T) {
	teams := newStubTeamStore()
	teams.memberships = []types.TeamMembership{{UserID: "alice", TeamID: "platform", Role: types.TeamRoleMember}}
	svc := NewIdentityService(newStubUserStore(), teams)
	if err := svc.RemoveMember(context.Background(), "alice", "platform"); err != nil {
		t.Fatalf("RemoveMember: %v", err)
	}
	if len(teams.memberships) != 0 {
		t.Errorf("membership not removed")
	}

	// Idempotent — removing missing membership is OK.
	if err := svc.RemoveMember(context.Background(), "alice", "platform"); err != nil {
		t.Errorf("idempotent RemoveMember: %v", err)
	}

	// Validation.
	if err := svc.RemoveMember(context.Background(), "", "platform"); err == nil {
		t.Errorf("missing user id should error")
	}
	if err := svc.RemoveMember(context.Background(), "alice", ""); err == nil {
		t.Errorf("missing team id should error")
	}

	// Nil store.
	if err := NewIdentityService(nil, nil).RemoveMember(context.Background(), "a", "b"); err == nil {
		t.Errorf("nil store should error")
	}
}

func TestIdentityServiceListMembers(t *testing.T) {
	teams := newStubTeamStore()
	teams.memberships = []types.TeamMembership{
		{UserID: "alice", TeamID: "platform", Role: types.TeamRoleOwner},
		{UserID: "bob", TeamID: "platform", Role: types.TeamRoleMember},
	}
	svc := NewIdentityService(newStubUserStore(), teams)

	got, err := svc.ListMembers(context.Background(), "platform")
	if err != nil {
		t.Fatalf("ListMembers: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("ListMembers = %d, want 2", len(got))
	}

	// Nil store returns nil.
	if got, err := NewIdentityService(nil, nil).ListMembers(context.Background(), "x"); err != nil || got != nil {
		t.Errorf("nil store: got %+v, %v", got, err)
	}
}

// ─── Resolve tests ────────────────────────────────────────────────────────

func TestIdentityServiceResolveAnonymous(t *testing.T) {
	svc := NewIdentityService(newStubUserStore(), newStubTeamStore())
	id := svc.Resolve(context.Background(), "")
	if !id.IsAnonymous() {
		t.Errorf("empty userID should resolve to anonymous")
	}
	if id.AuthorID() != types.SystemUserID {
		t.Errorf("AuthorID for anonymous = %s, want %s", id.AuthorID(), types.SystemUserID)
	}
}

func TestIdentityServiceResolveMissingUser(t *testing.T) {
	svc := NewIdentityService(newStubUserStore(), newStubTeamStore())
	id := svc.Resolve(context.Background(), "ghost")
	if !id.IsAnonymous() {
		t.Errorf("missing user should resolve to anonymous")
	}
}

func TestIdentityServiceResolveWithTeams(t *testing.T) {
	users := newStubUserStore()
	users.users["alice"] = &types.User{ID: "alice", Email: "a@b.co", DisplayName: "Alice"}
	teams := newStubTeamStore()
	teams.memberships = []types.TeamMembership{
		{UserID: "alice", TeamID: "zeta", Role: types.TeamRoleMember},
		{UserID: "alice", TeamID: "alpha", Role: types.TeamRoleOwner},
	}
	svc := NewIdentityService(users, teams)
	id := svc.Resolve(context.Background(), "alice")

	if id.UserID != "alice" || id.Email != "a@b.co" || id.DisplayName != "Alice" {
		t.Errorf("identity fields wrong: %+v", id)
	}
	if id.AuthorID() != "alice" {
		t.Errorf("AuthorID = %s, want alice", id.AuthorID())
	}
	if len(id.TeamIDs) != 2 || id.TeamIDs[0] != "alpha" || id.TeamIDs[1] != "zeta" {
		t.Errorf("TeamIDs not sorted alphabetically: %+v", id.TeamIDs)
	}
}

func TestIdentityServiceResolveNilUserStore(t *testing.T) {
	svc := NewIdentityService(nil, newStubTeamStore())
	id := svc.Resolve(context.Background(), "alice")
	if !id.IsAnonymous() {
		t.Errorf("nil user store should yield anonymous")
	}
}

// ─── domain.Identity helpers ──────────────────────────────────────────────

func TestIdentityWithIdentityRoundTrip(t *testing.T) {
	id := domain.Identity{UserID: "alice", Email: "a@b.co"}
	ctx := domain.WithIdentity(context.Background(), id)
	got := domain.IdentityFrom(ctx)
	if got.UserID != "alice" || got.Email != "a@b.co" {
		t.Errorf("round-trip lost data: %+v", got)
	}

	// Empty context returns zero value.
	zero := domain.IdentityFrom(context.Background())
	if !zero.IsAnonymous() {
		t.Errorf("empty ctx should be anonymous")
	}
}

// ─── Helper ────────────────────────────────────────────────────────────────

func makeString(n int, c byte) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = c
	}
	return string(b)
}

// Time hint silences unused-import detection for time during refactors.
var _ = time.Now
