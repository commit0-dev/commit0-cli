package http

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/commit0-dev/commit0/pkg/types"
	"github.com/commit0-dev/commit0/server/internal/app"
	"github.com/commit0-dev/commit0/server/internal/domain"
)

// IdentityHandlers groups the user/team CRUD endpoints. They share a single
// IdentityService instance so the unit-of-work boundary is the request.
type IdentityHandlers struct {
	svc *app.IdentityService
}

// NewIdentityHandlers constructs the handler bundle.
func NewIdentityHandlers(svc *app.IdentityService) *IdentityHandlers {
	return &IdentityHandlers{svc: svc}
}

// ─── Users ────────────────────────────────────────────────────────────────

type createUserRequest struct {
	ID          string `json:"id"            binding:"required"`
	Email       string `json:"email"         binding:"required"`
	DisplayName string `json:"display_name"`
}

func (h *IdentityHandlers) handleCreateUser(c *gin.Context) {
	var req createUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, domain.Validation(err.Error()))
		return
	}
	user := &types.User{
		ID:          req.ID,
		Email:       req.Email,
		DisplayName: req.DisplayName,
	}
	if err := h.svc.CreateUser(c.Request.Context(), user); err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusCreated, user)
}

func (h *IdentityHandlers) handleGetUser(c *gin.Context) {
	id := c.Param("id")
	user, err := h.svc.GetUser(c.Request.Context(), id)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, user)
}

func (h *IdentityHandlers) handleListUsers(c *gin.Context) {
	users, err := h.svc.ListUsers(c.Request.Context())
	if err != nil {
		writeError(c, err)
		return
	}
	if users == nil {
		users = []types.User{}
	}
	c.JSON(http.StatusOK, gin.H{"users": users, "count": len(users)})
}

func (h *IdentityHandlers) handleDeleteUser(c *gin.Context) {
	id := c.Param("id")
	if err := h.svc.DeleteUser(c.Request.Context(), id); err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": id})
}

// handleWhoAmI returns the request-scoped Identity. Useful for clients to
// verify their X-User-ID is being honored before issuing real requests.
func (h *IdentityHandlers) handleWhoAmI(c *gin.Context) {
	id := domain.IdentityFrom(c.Request.Context())
	c.JSON(http.StatusOK, gin.H{
		"user_id":      id.UserID,
		"email":        id.Email,
		"display_name": id.DisplayName,
		"team_ids":     id.TeamIDs,
		"anonymous":    id.IsAnonymous(),
	})
}

// ─── Teams ────────────────────────────────────────────────────────────────

type createTeamRequest struct {
	ID          string `json:"id"          binding:"required"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

func (h *IdentityHandlers) handleCreateTeam(c *gin.Context) {
	var req createTeamRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, domain.Validation(err.Error()))
		return
	}
	team := &types.Team{
		ID:          req.ID,
		Name:        req.Name,
		Description: req.Description,
	}
	if err := h.svc.CreateTeam(c.Request.Context(), team); err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusCreated, team)
}

func (h *IdentityHandlers) handleGetTeam(c *gin.Context) {
	id := c.Param("id")
	team, err := h.svc.GetTeam(c.Request.Context(), id)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, team)
}

func (h *IdentityHandlers) handleListTeams(c *gin.Context) {
	teams, err := h.svc.ListTeams(c.Request.Context())
	if err != nil {
		writeError(c, err)
		return
	}
	if teams == nil {
		teams = []types.Team{}
	}
	c.JSON(http.StatusOK, gin.H{"teams": teams, "count": len(teams)})
}

func (h *IdentityHandlers) handleDeleteTeam(c *gin.Context) {
	id := c.Param("id")
	if err := h.svc.DeleteTeam(c.Request.Context(), id); err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": id})
}

// ─── Memberships ──────────────────────────────────────────────────────────

type addMemberRequest struct {
	UserID string `json:"user_id" binding:"required"`
	Role   string `json:"role"    binding:"required"`
}

func (h *IdentityHandlers) handleAddMember(c *gin.Context) {
	teamID := c.Param("id")
	var req addMemberRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, domain.Validation(err.Error()))
		return
	}
	m := &types.TeamMembership{
		UserID: req.UserID,
		TeamID: teamID,
		Role:   types.TeamRole(req.Role),
	}
	if err := h.svc.AddMember(c.Request.Context(), m); err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusCreated, m)
}

func (h *IdentityHandlers) handleRemoveMember(c *gin.Context) {
	teamID := c.Param("id")
	userID := c.Param("user_id")
	if err := h.svc.RemoveMember(c.Request.Context(), userID, teamID); err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"removed": userID, "team": teamID})
}

func (h *IdentityHandlers) handleListMembers(c *gin.Context) {
	teamID := c.Param("id")
	members, err := h.svc.ListMembers(c.Request.Context(), teamID)
	if err != nil {
		writeError(c, err)
		return
	}
	if members == nil {
		members = []types.TeamMembership{}
	}
	c.JSON(http.StatusOK, gin.H{"members": members, "count": len(members)})
}

// (writeError is defined in handlers.go; reused here for consistency.)
