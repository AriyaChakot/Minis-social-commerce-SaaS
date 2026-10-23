package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"mscs/internal/team"
	"mscs/internal/tenant"
	"mscs/internal/user"
)

// TeamHandler exposes shop members and invitations.
type TeamHandler struct {
	svc *team.Service
}

func NewTeamHandler(svc *team.Service) *TeamHandler {
	return &TeamHandler{svc: svc}
}

// Team handles GET /tenants/:tenantID/team (owner/admin): members and pending invitations.
func (h *TeamHandler) Team(c *gin.Context) {
	t, _ := currentTenant(c)
	members, invitations, err := h.svc.Team(c.Request.Context(), t.ID)
	if err != nil {
		writeInternal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"members": members, "invitations": invitations, "you": currentUser(c).ID})
}

type inviteRequest struct {
	Email string      `json:"email" binding:"required,max=254"`
	Role  tenant.Role `json:"role" binding:"required"`
}

// Invite handles POST /tenants/:tenantID/invitations (owner/admin). The token is in this answer
// only: the frontend turns it into the link to send.
func (h *TeamHandler) Invite(c *gin.Context) {
	t, role := currentTenant(c)
	var req inviteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeBindError(c, err)
		return
	}
	inv, token, err := h.svc.Invite(c.Request.Context(), t.ID, currentUser(c).ID, role, req.Email, req.Role)
	if err != nil {
		writeTeamError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"invitation": inv, "token": token})
}

// Revoke handles DELETE /tenants/:tenantID/invitations/:invitationID (owner/admin).
func (h *TeamHandler) Revoke(c *gin.Context) {
	t, role := currentTenant(c)
	id, err := uuid.Parse(c.Param("invitationID"))
	if err != nil {
		writeTeamError(c, team.ErrInvitationNotFound)
		return
	}
	if err := h.svc.Revoke(c.Request.Context(), t.ID, role, id); err != nil {
		writeTeamError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// RemoveMember handles DELETE /tenants/:tenantID/members/:userID (owner/admin).
func (h *TeamHandler) RemoveMember(c *gin.Context) {
	t, role := currentTenant(c)
	id, err := uuid.Parse(c.Param("userID"))
	if err != nil {
		writeTeamError(c, team.ErrMemberNotFound)
		return
	}
	if err := h.svc.RemoveMember(c.Request.Context(), t.ID, currentUser(c).ID, role, id); err != nil {
		writeTeamError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// Preview handles GET /invitations/:token (public: the link is the permission).
func (h *TeamHandler) Preview(c *gin.Context) {
	p, err := h.svc.Preview(c.Request.Context(), c.Param("token"))
	if err != nil {
		writeTeamError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"invitation": p})
}

// Accept handles POST /invitations/:token/accept (logged in): join with the current account.
func (h *TeamHandler) Accept(c *gin.Context) {
	p, err := h.svc.Accept(c.Request.Context(), c.Param("token"), currentUser(c))
	if err != nil {
		writeTeamError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"invitation": p})
}

type invitedRegisterRequest struct {
	DisplayName string `json:"display_name" binding:"required,max=100"`
	Password    string `json:"password" binding:"required,min=8,max=128"`
}

// Register handles POST /invitations/:token/register (public): create the invited account,
// join the shop and log in. Same answer shape as /auth/register, so the BFF can set the cookie.
func (h *TeamHandler) Register(c *gin.Context) {
	var req invitedRegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeBindError(c, err)
		return
	}
	u, p, sess, err := h.svc.Register(c.Request.Context(), c.Param("token"),
		team.RegisterInput{DisplayName: req.DisplayName, Password: req.Password})
	if err != nil {
		writeTeamError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"user": u, "invitation": p, "session": sess})
}

func writeTeamError(c *gin.Context, err error) {
	var verr *team.ValidationError
	switch {
	case errors.As(err, &verr):
		writeError(c, http.StatusBadRequest, "validation_failed", verr.Message,
			fieldError{Field: verr.Field, Rule: "format"})
	case errors.Is(err, team.ErrInvitationNotFound):
		writeError(c, http.StatusNotFound, "invitation_not_found", "this invitation link is not valid (it may have been cancelled)")
	case errors.Is(err, team.ErrInvitationExpired):
		// 410 Gone: it existed, it is over.
		writeError(c, http.StatusGone, "invitation_expired", "this invitation link has expired")
	case errors.Is(err, team.ErrInvitationUsed):
		writeError(c, http.StatusGone, "invitation_used", "this invitation link has already been used")
	case errors.Is(err, team.ErrShopUnavailable):
		writeError(c, http.StatusGone, "tenant_suspended", "this shop is suspended")
	case errors.Is(err, team.ErrWrongAccount):
		writeError(c, http.StatusForbidden, "invitation_wrong_account", "this invitation is for another email address")
	case errors.Is(err, team.ErrAlreadyMember):
		writeError(c, http.StatusConflict, "already_member", "already a member of this shop")
	case errors.Is(err, team.ErrMemberNotFound):
		writeError(c, http.StatusNotFound, "member_not_found", "member not found")
	case errors.Is(err, team.ErrNotAllowed):
		writeError(c, http.StatusForbidden, "forbidden", "your role cannot do this")
	case errors.Is(err, user.ErrEmailTaken):
		writeError(c, http.StatusConflict, "email_taken", "this email already has an account: log in to accept")
	default:
		writeInternal(c, err)
	}
}
