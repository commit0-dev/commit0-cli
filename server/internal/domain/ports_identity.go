package domain

import (
	"context"

	"github.com/commit0-dev/commit0/pkg/types"
)

// ---------------------------------------------------------------------------
// Identity — request-scoped principal
// ---------------------------------------------------------------------------

// Identity is the request-scoped principal carried through context.Context.
// It collapses User + active team memberships into a single, immutable value
// suitable for fan-out to events, services, and storage adapters.
//
// Anonymous requests carry the zero-value Identity{} — service code must
// guard against that with `if id.UserID == "" { ... }`.
type Identity struct {
	UserID      string
	Email       string
	DisplayName string
	// TeamIDs lists every team the user belongs to, in alphabetical order.
	// Used to populate access_scope filters and to expand "team:*" scopes
	// during graph queries.
	TeamIDs []string
}

// IsAnonymous reports whether the identity carries no user — the request
// either omitted the X-User-ID header or carried a value the IdentityStore
// could not resolve.
func (i Identity) IsAnonymous() bool {
	return i.UserID == ""
}

// AuthorID returns the value to stamp on event AuthorID fields. Falls back
// to the platform sentinel `system` for anonymous requests so events still
// have a non-empty author column for indexing and analytics.
func (i Identity) AuthorID() string {
	if i.UserID == "" {
		return types.SystemUserID
	}
	return i.UserID
}

type identityKey struct{}

// WithIdentity returns a derived context carrying the given Identity.
// Use only at the request edge (HTTP middleware) — services receive the
// derived context unchanged.
func WithIdentity(ctx context.Context, id Identity) context.Context {
	return context.WithValue(ctx, identityKey{}, id)
}

// IdentityFrom extracts the Identity carried in ctx. Returns the zero value
// (Identity{}) when no identity is attached — that's a valid state and
// callers must handle it (anonymous request).
func IdentityFrom(ctx context.Context) Identity {
	if v, ok := ctx.Value(identityKey{}).(Identity); ok {
		return v
	}
	return Identity{}
}

// ---------------------------------------------------------------------------
// User Store
// ---------------------------------------------------------------------------

// UserStore manages persistent User records.
type UserStore interface {
	UpsertUser(ctx context.Context, user *types.User) error
	GetUser(ctx context.Context, id string) (*types.User, error)
	GetUserByEmail(ctx context.Context, email string) (*types.User, error)
	ListUsers(ctx context.Context) ([]types.User, error)
	DeleteUser(ctx context.Context, id string) error
}

// ---------------------------------------------------------------------------
// Team Store
// ---------------------------------------------------------------------------

// TeamStore manages persistent Team and TeamMembership records.
//
// Membership operations are idempotent: AddMember on an existing membership
// updates the role; RemoveMember on a missing membership returns nil.
type TeamStore interface {
	UpsertTeam(ctx context.Context, team *types.Team) error
	GetTeam(ctx context.Context, id string) (*types.Team, error)
	ListTeams(ctx context.Context) ([]types.Team, error)
	DeleteTeam(ctx context.Context, id string) error

	AddMember(ctx context.Context, membership *types.TeamMembership) error
	RemoveMember(ctx context.Context, userID, teamID string) error
	ListMembers(ctx context.Context, teamID string) ([]types.TeamMembership, error)
	ListUserTeams(ctx context.Context, userID string) ([]types.TeamMembership, error)
}
