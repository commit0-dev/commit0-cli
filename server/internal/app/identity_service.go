package app

import (
	"context"
	"errors"
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/commit0-dev/commit0/pkg/types"
	"github.com/commit0-dev/commit0/server/internal/domain"
)

// IdentityService orchestrates User/Team CRUD and resolves request-time
// identities by composing UserStore + TeamStore.
//
// Gracefully degrades when stores are nil: every operation returns the
// zero value or a domain.NotFound so the surrounding HTTP layer can keep
// running even on a single-tenant deployment without identity persistence.
type IdentityService struct {
	users domain.UserStore
	teams domain.TeamStore
	log   *slog.Logger
}

// NewIdentityService constructs an IdentityService.
func NewIdentityService(users domain.UserStore, teams domain.TeamStore) *IdentityService {
	return &IdentityService{
		users: users,
		teams: teams,
		log:   slog.Default().With("service", "identity"),
	}
}

// idValidRE constrains user/team IDs to lowercase alphanumerics + hyphen,
// 2-64 chars, starting with a letter. Rejects empty, whitespace, capital
// letters, underscores, slashes, and other separators that would conflict
// with URL routing.
var idValidRE = regexp.MustCompile(`^[a-z][a-z0-9-]{1,63}$`)

// CreateUser persists a new user (or updates an existing one with the
// same ID). Returns domain.Validation when the ID or email are malformed.
func (s *IdentityService) CreateUser(ctx context.Context, user *types.User) error {
	if s.users == nil {
		return domain.Unavailable("identity store not configured")
	}
	if user == nil {
		return domain.Validation("user is nil")
	}
	if !idValidRE.MatchString(user.ID) {
		return domain.Validation("user.id must match [a-z][a-z0-9-]{1,63}")
	}
	if !strings.Contains(user.Email, "@") || len(user.Email) < 3 {
		return domain.Validation("user.email must be a valid email address")
	}
	if user.DisplayName == "" {
		user.DisplayName = user.ID
	}
	now := time.Now()
	if user.CreatedAt.IsZero() {
		user.CreatedAt = now
	}
	user.UpdatedAt = now
	return s.users.UpsertUser(ctx, user)
}

// GetUser fetches a user by ID. Returns domain.NotFound when absent.
func (s *IdentityService) GetUser(ctx context.Context, id string) (*types.User, error) {
	if s.users == nil {
		return nil, domain.NotFound("identity store not configured")
	}
	return s.users.GetUser(ctx, id)
}

// GetUserByEmail fetches a user by email. Returns domain.NotFound when absent.
func (s *IdentityService) GetUserByEmail(ctx context.Context, email string) (*types.User, error) {
	if s.users == nil {
		return nil, domain.NotFound("identity store not configured")
	}
	return s.users.GetUserByEmail(ctx, email)
}

// ListUsers returns every user, sorted by ID.
func (s *IdentityService) ListUsers(ctx context.Context) ([]types.User, error) {
	if s.users == nil {
		return nil, nil
	}
	return s.users.ListUsers(ctx)
}

// DeleteUser removes a user. Cascade to memberships happens at the storage
// layer (SurrealDB REFERENCE ON DELETE CASCADE).
func (s *IdentityService) DeleteUser(ctx context.Context, id string) error {
	if s.users == nil {
		return domain.Unavailable("identity store not configured")
	}
	return s.users.DeleteUser(ctx, id)
}

// CreateTeam persists a new team (or updates an existing one).
func (s *IdentityService) CreateTeam(ctx context.Context, team *types.Team) error {
	if s.teams == nil {
		return domain.Unavailable("identity store not configured")
	}
	if team == nil {
		return domain.Validation("team is nil")
	}
	if !idValidRE.MatchString(team.ID) {
		return domain.Validation("team.id must match [a-z][a-z0-9-]{1,63}")
	}
	if team.Name == "" {
		team.Name = team.ID
	}
	if team.CreatedAt.IsZero() {
		team.CreatedAt = time.Now()
	}
	return s.teams.UpsertTeam(ctx, team)
}

// GetTeam fetches a team by ID.
func (s *IdentityService) GetTeam(ctx context.Context, id string) (*types.Team, error) {
	if s.teams == nil {
		return nil, domain.NotFound("identity store not configured")
	}
	return s.teams.GetTeam(ctx, id)
}

// ListTeams returns every team, sorted by ID.
func (s *IdentityService) ListTeams(ctx context.Context) ([]types.Team, error) {
	if s.teams == nil {
		return nil, nil
	}
	return s.teams.ListTeams(ctx)
}

// DeleteTeam removes a team and cascades to memberships.
func (s *IdentityService) DeleteTeam(ctx context.Context, id string) error {
	if s.teams == nil {
		return domain.Unavailable("identity store not configured")
	}
	return s.teams.DeleteTeam(ctx, id)
}

// AddMember adds a user to a team with the given role. Both the user
// and team must exist; the role must be a valid TeamRole.
func (s *IdentityService) AddMember(ctx context.Context, m *types.TeamMembership) error {
	if s.teams == nil || s.users == nil {
		return domain.Unavailable("identity store not configured")
	}
	if m == nil {
		return domain.Validation("membership is nil")
	}
	if m.UserID == "" || m.TeamID == "" {
		return domain.Validation("membership.user_id and team_id are required")
	}
	if m.Role != types.TeamRoleOwner && m.Role != types.TeamRoleMember {
		return domain.Validation("membership.role must be owner or member")
	}
	// Verify user + team exist (fail fast with a helpful message instead of
	// a foreign-key error from the database).
	if _, err := s.users.GetUser(ctx, m.UserID); err != nil {
		var domErr *domain.DomainError
		if errors.As(err, &domErr) && domErr.Code == domain.ErrNotFound {
			return domain.Validation("user " + m.UserID + " does not exist")
		}
		return err
	}
	if _, err := s.teams.GetTeam(ctx, m.TeamID); err != nil {
		var domErr *domain.DomainError
		if errors.As(err, &domErr) && domErr.Code == domain.ErrNotFound {
			return domain.Validation("team " + m.TeamID + " does not exist")
		}
		return err
	}
	if m.JoinedAt.IsZero() {
		m.JoinedAt = time.Now()
	}
	return s.teams.AddMember(ctx, m)
}

// RemoveMember removes a user from a team. Returns nil even if the
// membership did not exist (idempotent).
func (s *IdentityService) RemoveMember(ctx context.Context, userID, teamID string) error {
	if s.teams == nil {
		return domain.Unavailable("identity store not configured")
	}
	if userID == "" || teamID == "" {
		return domain.Validation("user_id and team_id are required")
	}
	return s.teams.RemoveMember(ctx, userID, teamID)
}

// ListMembers returns every member of the given team, sorted by user ID.
func (s *IdentityService) ListMembers(ctx context.Context, teamID string) ([]types.TeamMembership, error) {
	if s.teams == nil {
		return nil, nil
	}
	return s.teams.ListMembers(ctx, teamID)
}

// Resolve loads the request-time Identity for the given userID.
// Returns the zero-value Identity{} when userID is empty (anonymous) or
// the user does not exist (best-effort: the request continues with no
// attribution rather than failing outright).
func (s *IdentityService) Resolve(ctx context.Context, userID string) Identity {
	if userID == "" || s.users == nil {
		return Identity{}
	}
	user, err := s.users.GetUser(ctx, userID)
	if err != nil {
		s.log.Debug("identity resolve: user not found", "user_id", userID, "err", err)
		return Identity{}
	}
	id := Identity{
		UserID:      user.ID,
		Email:       user.Email,
		DisplayName: user.DisplayName,
	}
	if s.teams != nil {
		memberships, err := s.teams.ListUserTeams(ctx, userID)
		if err == nil {
			teams := make([]string, 0, len(memberships))
			for _, m := range memberships {
				teams = append(teams, m.TeamID)
			}
			sort.Strings(teams)
			id.TeamIDs = teams
		}
	}
	return id
}

// Identity is the orchestrator-facing alias of domain.Identity. Re-exported
// here so HTTP handlers and middleware can construct/inspect identities
// without importing domain.
type Identity = domain.Identity
