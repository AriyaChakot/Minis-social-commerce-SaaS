package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"mscs/internal/auth"
	"mscs/internal/tenant"
	"mscs/internal/user"
)

// AuthHandler exposes the auth use cases over HTTP.
// It only translates HTTP <-> service calls; the rules live in auth.Service.
type AuthHandler struct {
	svc *auth.Service
}

func NewAuthHandler(svc *auth.Service) *AuthHandler {
	return &AuthHandler{svc: svc}
}

// Request bodies. `binding` rules (go-playground/validator) check the shape of the input;
// business rules such as the slug format are checked by the service.
type registerRequest struct {
	Email       string `json:"email" binding:"required,email,max=254"`
	Password    string `json:"password" binding:"required,min=8,max=128"`
	DisplayName string `json:"display_name" binding:"required,max=100"`
	ShopName    string `json:"shop_name" binding:"required,max=100"`
	ShopSlug    string `json:"shop_slug" binding:"required,min=3,max=63"`
}

type loginRequest struct {
	Email    string `json:"email" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// Register handles POST /api/v1/auth/register.
func (h *AuthHandler) Register(c *gin.Context) {
	var req registerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeBindError(c, err)
		return
	}

	res, err := h.svc.Register(c.Request.Context(), auth.RegisterInput{
		Email:       req.Email,
		Password:    req.Password,
		DisplayName: req.DisplayName,
		ShopName:    req.ShopName,
		ShopSlug:    req.ShopSlug,
	})

	var verr *auth.ValidationError
	switch {
	case err == nil:
		c.JSON(http.StatusCreated, res)
	case errors.As(err, &verr):
		writeError(c, http.StatusBadRequest, "validation_failed", verr.Message,
			fieldError{Field: verr.Field, Rule: "format"})
	case errors.Is(err, user.ErrEmailTaken):
		writeError(c, http.StatusConflict, "email_taken", "this email is already registered")
	case errors.Is(err, tenant.ErrSlugTaken):
		writeError(c, http.StatusConflict, "slug_taken", "this shop URL is already taken")
	default:
		writeInternal(c, err)
	}
}

// Login handles POST /api/v1/auth/login.
func (h *AuthHandler) Login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeBindError(c, err)
		return
	}

	u, sess, err := h.svc.Login(c.Request.Context(), req.Email, req.Password)
	switch {
	case err == nil:
		c.JSON(http.StatusOK, gin.H{"user": u, "session": sess})
	case errors.Is(err, auth.ErrInvalidCredentials):
		writeError(c, http.StatusUnauthorized, "invalid_credentials", "invalid email or password")
	default:
		writeInternal(c, err)
	}
}

// Logout handles POST /api/v1/auth/logout (behind RequireAuth).
func (h *AuthHandler) Logout(c *gin.Context) {
	if err := h.svc.Logout(c.Request.Context(), bearerToken(c)); err != nil {
		writeInternal(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// Me handles GET /api/v1/me (behind RequireAuth): the user plus the shops they can open.
func (h *AuthHandler) Me(c *gin.Context) {
	u := currentUser(c)
	memberships, err := h.svc.Memberships(c.Request.Context(), u.ID)
	if err != nil {
		writeInternal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"user": u, "memberships": memberships})
}
