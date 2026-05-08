package types

import "time"

// User represents an authenticated principal acting on the platform.
//
// Identity is the foundation for multi-tenant collaboration: every event,
// every node, every edge can be attributed back to a specific user. This
// closes the platform-readiness gap A1 (no concept of users, teams, or
// shared state).
//
// User IDs are short slugs (lowercase, alphanumeric + hyphens) chosen by
// the system administrator at registration time. Email is the canonical
// identifier for human-facing flows; UserID is the canonical identifier
// for system-facing flows (events, attribution, audit).
type User struct {
	ID          string    `json:"id"`
	Email       string    `json:"email"`
	DisplayName string    `json:"display_name"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at,omitzero"`
}

// Team represents a group of users with shared access to repositories
// and knowledge. Teams provide the scope boundary for access_scope on
// graph nodes and edges.
type Team struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

// TeamRole classifies a user's role within a team.
type TeamRole string

const (
	// TeamRoleOwner has full administrative privileges over the team —
	// can add/remove members, change team settings, delete the team.
	TeamRoleOwner TeamRole = "owner"
	// TeamRoleMember can read and contribute to team-scoped resources
	// but cannot modify membership or team settings.
	TeamRoleMember TeamRole = "member"
)

// TeamMembership records a user's membership in a team with a role.
// (UserID, TeamID) is unique; a user holds at most one role per team.
type TeamMembership struct {
	UserID   string    `json:"user_id"`
	TeamID   string    `json:"team_id"`
	Role     TeamRole  `json:"role"`
	JoinedAt time.Time `json:"joined_at"`
}

// AccessScope classifies the visibility of a node or edge.
//
// "public"  — visible to anyone with access to the repository
// "team:<team-id>"  — visible only to members of the named team
// "user:<user-id>"  — visible only to the named user (private notes)
//
// Default for code nodes and edges is "public" (existing behavior).
// Knowledge nodes can be created with team or user scope to support
// private decisions, runbooks, or postmortems.
type AccessScope = string

const (
	// AccessScopePublic indicates a node/edge is visible repository-wide.
	AccessScopePublic AccessScope = "public"
)

// SystemUserID is the AuthorID used for events emitted by the platform
// itself (background indexers, cleanup jobs) when no human user is in
// the request context.
const SystemUserID = "system"
