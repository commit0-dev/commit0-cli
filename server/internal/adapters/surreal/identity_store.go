package surreal

import (
	"context"
	"fmt"
	"sort"
	"time"

	surrealdb "github.com/surrealdb/surrealdb.go"

	"github.com/commit0-dev/commit0/pkg/types"
	"github.com/commit0-dev/commit0/server/internal/domain"
)

// IdentityStoreAdapter implements domain.UserStore + domain.TeamStore on
// top of the SurrealDB connection. The two ports share an adapter because
// they share a transaction-coherent storage layer (one DB, three tables).
type IdentityStoreAdapter struct{ *SurrealAdapter }

// Compile-time interface checks.
var (
	_ domain.UserStore = (*IdentityStoreAdapter)(nil)
	_ domain.TeamStore = (*IdentityStoreAdapter)(nil)
)

// AsUserStore returns the adapter as a UserStore.
func (a *SurrealAdapter) AsUserStore() *IdentityStoreAdapter {
	return &IdentityStoreAdapter{a}
}

// AsTeamStore returns the adapter as a TeamStore. Returns the same value
// as AsUserStore — the adapter satisfies both ports — so callers can wire
// them independently without extra round-trips to the database.
func (a *SurrealAdapter) AsTeamStore() *IdentityStoreAdapter {
	return &IdentityStoreAdapter{a}
}

// ─── User row ─────────────────────────────────────────────────────────────

type userRow struct {
	ID          string `json:"user_id"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

func (a *IdentityStoreAdapter) UpsertUser(ctx context.Context, user *types.User) error {
	q := `UPSERT user SET
		user_id      = $user_id,
		email        = $email,
		display_name = $display_name,
		created_at   = $created_at,
		updated_at   = $updated_at
	WHERE user_id = $user_id;`
	params := map[string]any{
		"user_id":      user.ID,
		"email":        user.Email,
		"display_name": user.DisplayName,
		"created_at":   user.CreatedAt.UTC().Format(time.RFC3339),
		"updated_at":   user.UpdatedAt.UTC().Format(time.RFC3339),
	}
	_, err := surrealdb.Query[any](ctx, a.db, q, params)
	if err != nil {
		return fmt.Errorf("upsert user %s: %w", user.ID, err)
	}
	return nil
}

func (a *IdentityStoreAdapter) GetUser(ctx context.Context, id string) (*types.User, error) {
	q := `SELECT * FROM user WHERE user_id = $id LIMIT 1;`
	results, err := surrealdb.Query[[]userRow](ctx, a.db, q, map[string]any{"id": id})
	if err != nil {
		return nil, fmt.Errorf("get user %s: %w", id, err)
	}
	if results == nil || len(*results) == 0 || len((*results)[0].Result) == 0 {
		return nil, domain.NotFound(fmt.Sprintf("user %q not found", id))
	}
	return rowToUser((*results)[0].Result[0]), nil
}

func (a *IdentityStoreAdapter) GetUserByEmail(ctx context.Context, email string) (*types.User, error) {
	q := `SELECT * FROM user WHERE email = $email LIMIT 1;`
	results, err := surrealdb.Query[[]userRow](ctx, a.db, q, map[string]any{"email": email})
	if err != nil {
		return nil, fmt.Errorf("get user by email %s: %w", email, err)
	}
	if results == nil || len(*results) == 0 || len((*results)[0].Result) == 0 {
		return nil, domain.NotFound(fmt.Sprintf("user with email %q not found", email))
	}
	return rowToUser((*results)[0].Result[0]), nil
}

func (a *IdentityStoreAdapter) ListUsers(ctx context.Context) ([]types.User, error) {
	q := `SELECT * FROM user ORDER BY user_id;`
	results, err := surrealdb.Query[[]userRow](ctx, a.db, q, nil)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	if results == nil || len(*results) == 0 {
		return nil, nil
	}
	out := make([]types.User, 0, len((*results)[0].Result))
	for _, r := range (*results)[0].Result {
		out = append(out, *rowToUser(r))
	}
	return out, nil
}

func (a *IdentityStoreAdapter) DeleteUser(ctx context.Context, id string) error {
	// Cascade: remove memberships first, then the user row.
	if _, err := surrealdb.Query[any](ctx, a.db, `DELETE FROM team_membership WHERE user_id = $id;`, map[string]any{"id": id}); err != nil {
		return fmt.Errorf("delete memberships for user %s: %w", id, err)
	}
	if _, err := surrealdb.Query[any](ctx, a.db, `DELETE FROM user WHERE user_id = $id;`, map[string]any{"id": id}); err != nil {
		return fmt.Errorf("delete user %s: %w", id, err)
	}
	return nil
}

func rowToUser(r userRow) *types.User {
	u := &types.User{
		ID:          r.ID,
		Email:       r.Email,
		DisplayName: r.DisplayName,
	}
	if t, err := time.Parse(time.RFC3339, r.CreatedAt); err == nil {
		u.CreatedAt = t
	}
	if t, err := time.Parse(time.RFC3339, r.UpdatedAt); err == nil {
		u.UpdatedAt = t
	}
	return u
}

// ─── Team row ─────────────────────────────────────────────────────────────

type teamRow struct {
	ID          string `json:"team_id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	CreatedAt   string `json:"created_at"`
}

func (a *IdentityStoreAdapter) UpsertTeam(ctx context.Context, team *types.Team) error {
	q := `UPSERT team SET
		team_id     = $team_id,
		name        = $name,
		description = $description,
		created_at  = $created_at
	WHERE team_id = $team_id;`
	params := map[string]any{
		"team_id":     team.ID,
		"name":        team.Name,
		"description": team.Description,
		"created_at":  team.CreatedAt.UTC().Format(time.RFC3339),
	}
	_, err := surrealdb.Query[any](ctx, a.db, q, params)
	if err != nil {
		return fmt.Errorf("upsert team %s: %w", team.ID, err)
	}
	return nil
}

func (a *IdentityStoreAdapter) GetTeam(ctx context.Context, id string) (*types.Team, error) {
	q := `SELECT * FROM team WHERE team_id = $id LIMIT 1;`
	results, err := surrealdb.Query[[]teamRow](ctx, a.db, q, map[string]any{"id": id})
	if err != nil {
		return nil, fmt.Errorf("get team %s: %w", id, err)
	}
	if results == nil || len(*results) == 0 || len((*results)[0].Result) == 0 {
		return nil, domain.NotFound(fmt.Sprintf("team %q not found", id))
	}
	return rowToTeam((*results)[0].Result[0]), nil
}

func (a *IdentityStoreAdapter) ListTeams(ctx context.Context) ([]types.Team, error) {
	q := `SELECT * FROM team ORDER BY team_id;`
	results, err := surrealdb.Query[[]teamRow](ctx, a.db, q, nil)
	if err != nil {
		return nil, fmt.Errorf("list teams: %w", err)
	}
	if results == nil || len(*results) == 0 {
		return nil, nil
	}
	out := make([]types.Team, 0, len((*results)[0].Result))
	for _, r := range (*results)[0].Result {
		out = append(out, *rowToTeam(r))
	}
	return out, nil
}

func (a *IdentityStoreAdapter) DeleteTeam(ctx context.Context, id string) error {
	if _, err := surrealdb.Query[any](ctx, a.db, `DELETE FROM team_membership WHERE team_id = $id;`, map[string]any{"id": id}); err != nil {
		return fmt.Errorf("delete memberships for team %s: %w", id, err)
	}
	if _, err := surrealdb.Query[any](ctx, a.db, `DELETE FROM team WHERE team_id = $id;`, map[string]any{"id": id}); err != nil {
		return fmt.Errorf("delete team %s: %w", id, err)
	}
	return nil
}

func rowToTeam(r teamRow) *types.Team {
	t := &types.Team{
		ID:          r.ID,
		Name:        r.Name,
		Description: r.Description,
	}
	if ts, err := time.Parse(time.RFC3339, r.CreatedAt); err == nil {
		t.CreatedAt = ts
	}
	return t
}

// ─── Membership row ───────────────────────────────────────────────────────

type membershipRow struct {
	UserID   string `json:"user_id"`
	TeamID   string `json:"team_id"`
	Role     string `json:"role"`
	JoinedAt string `json:"joined_at"`
}

func (a *IdentityStoreAdapter) AddMember(ctx context.Context, m *types.TeamMembership) error {
	// Idempotent: UPSERT replaces an existing (user, team) pair.
	q := `UPSERT team_membership SET
		user_id   = $user_id,
		team_id   = $team_id,
		role      = $role,
		joined_at = $joined_at
	WHERE user_id = $user_id AND team_id = $team_id;`
	params := map[string]any{
		"user_id":   m.UserID,
		"team_id":   m.TeamID,
		"role":      string(m.Role),
		"joined_at": m.JoinedAt.UTC().Format(time.RFC3339),
	}
	_, err := surrealdb.Query[any](ctx, a.db, q, params)
	if err != nil {
		return fmt.Errorf("add member %s to %s: %w", m.UserID, m.TeamID, err)
	}
	return nil
}

func (a *IdentityStoreAdapter) RemoveMember(ctx context.Context, userID, teamID string) error {
	q := `DELETE FROM team_membership WHERE user_id = $user_id AND team_id = $team_id;`
	_, err := surrealdb.Query[any](ctx, a.db, q, map[string]any{
		"user_id": userID,
		"team_id": teamID,
	})
	if err != nil {
		return fmt.Errorf("remove member %s from %s: %w", userID, teamID, err)
	}
	return nil
}

func (a *IdentityStoreAdapter) ListMembers(ctx context.Context, teamID string) ([]types.TeamMembership, error) {
	q := `SELECT * FROM team_membership WHERE team_id = $team_id ORDER BY user_id;`
	results, err := surrealdb.Query[[]membershipRow](ctx, a.db, q, map[string]any{"team_id": teamID})
	if err != nil {
		return nil, fmt.Errorf("list members of %s: %w", teamID, err)
	}
	return rowsToMemberships(results), nil
}

func (a *IdentityStoreAdapter) ListUserTeams(ctx context.Context, userID string) ([]types.TeamMembership, error) {
	q := `SELECT * FROM team_membership WHERE user_id = $user_id ORDER BY team_id;`
	results, err := surrealdb.Query[[]membershipRow](ctx, a.db, q, map[string]any{"user_id": userID})
	if err != nil {
		return nil, fmt.Errorf("list teams for user %s: %w", userID, err)
	}
	return rowsToMemberships(results), nil
}

func rowsToMemberships(results *[]surrealdb.QueryResult[[]membershipRow]) []types.TeamMembership {
	if results == nil || len(*results) == 0 {
		return nil
	}
	rows := (*results)[0].Result
	out := make([]types.TeamMembership, 0, len(rows))
	for _, r := range rows {
		m := types.TeamMembership{
			UserID: r.UserID,
			TeamID: r.TeamID,
			Role:   types.TeamRole(r.Role),
		}
		if t, err := time.Parse(time.RFC3339, r.JoinedAt); err == nil {
			m.JoinedAt = t
		}
		out = append(out, m)
	}
	// Stable sort guarantee even if the database returns unordered rows.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].TeamID != out[j].TeamID {
			return out[i].TeamID < out[j].TeamID
		}
		return out[i].UserID < out[j].UserID
	})
	return out
}
