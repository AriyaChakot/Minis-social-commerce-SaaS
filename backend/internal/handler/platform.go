package handler

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"mscs/internal/auth"
	"mscs/internal/platform"
	"mscs/internal/ratelimit"
)

// The platform console is a browser-only app (React SPA) with no server of its own, so unlike
// the merchant app (whose Nuxt server keeps the token and sends it as a Bearer header) the API
// itself stores the session in a cookie:
//   - HttpOnly: JavaScript cannot read it, so an XSS bug cannot steal the session.
//   - Secure: HTTPS only (browsers also accept it on http://localhost for development).
//   - SameSite=Strict: never sent on a request started by another site (CSRF defense #1).
//   - Path=/api/v1/platform: never sent to the rest of the API.
//
// CSRF defense #2: state-changing requests must carry the X-MSCS-Platform header. Another site
// cannot add a custom header to a cross-origin request without a CORS preflight, which this API
// never approves.
const (
	platformCookie     = "mscs_platform"
	platformCookiePath = "/api/v1/platform"
	platformCSRFHeader = "X-MSCS-Platform"
)

type PlatformHandler struct {
	auth         *auth.Service
	svc          *platform.Service
	loginByEmail *ratelimit.Limiter
}

func NewPlatformHandler(authSvc *auth.Service, svc *platform.Service, loginByEmail *ratelimit.Limiter) *PlatformHandler {
	return &PlatformHandler{auth: authSvc, svc: svc, loginByEmail: loginByEmail}
}

func setPlatformCookie(c *gin.Context, token string, expires time.Time) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     platformCookie,
		Value:    token,
		Path:     platformCookiePath,
		Expires:  expires,
		MaxAge:   int(time.Until(expires).Seconds()),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	})
}

func clearPlatformCookie(c *gin.Context) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name: platformCookie, Path: platformCookiePath, MaxAge: -1,
		HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode,
	})
}

// requireCSRFHeader is CSRF defense #2 (see above), for every method that changes something.
func requireCSRFHeader(c *gin.Context) bool {
	switch c.Request.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}
	if c.GetHeader(platformCSRFHeader) == "" {
		writeError(c, http.StatusForbidden, "csrf_failed", "missing "+platformCSRFHeader+" header")
		return false
	}
	return true
}

// RequirePlatformAdmin lets through only a valid platform session of a platform admin.
func RequirePlatformAdmin(authSvc *auth.Service, svc *platform.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !requireCSRFHeader(c) {
			return
		}
		token, _ := c.Cookie(platformCookie)
		u, err := authSvc.Authenticate(c.Request.Context(), token)
		if errors.Is(err, auth.ErrUnauthenticated) {
			writeError(c, http.StatusUnauthorized, "unauthenticated", "login required")
			return
		}
		if err != nil {
			writeInternal(c, err)
			return
		}
		// Checked on every request, not only at login: removing someone from platform_admins
		// takes effect immediately.
		ok, err := svc.IsAdmin(c.Request.Context(), u.ID)
		if err != nil {
			writeInternal(c, err)
			return
		}
		if !ok {
			writeError(c, http.StatusForbidden, "forbidden", "platform admins only")
			return
		}
		c.Set(ctxUserKey, u)
		c.Next()
	}
}

// Login handles POST /api/v1/platform/login: the same accounts as the merchant app, but only
// platform admins get a console session.
func (h *PlatformHandler) Login(c *gin.Context) {
	if !requireCSRFHeader(c) {
		return
	}
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeBindError(c, err)
		return
	}
	if !allow(c, h.loginByEmail, "login:"+strings.ToLower(strings.TrimSpace(req.Email))) {
		return
	}

	u, sess, err := h.auth.Login(c.Request.Context(), req.Email, req.Password)
	if errors.Is(err, auth.ErrInvalidCredentials) {
		writeError(c, http.StatusUnauthorized, "invalid_credentials", "invalid email or password")
		return
	}
	if err != nil {
		writeInternal(c, err)
		return
	}
	ok, err := h.svc.IsAdmin(c.Request.Context(), u.ID)
	if err == nil && !ok {
		// A merchant account: its password is right, but it gets no console session.
		err = h.auth.Logout(c.Request.Context(), sess.Token)
		if err == nil {
			writeError(c, http.StatusForbidden, "not_platform_admin", "this account is not a platform admin")
			return
		}
	}
	if err != nil {
		writeInternal(c, err)
		return
	}

	setPlatformCookie(c, sess.Token, sess.ExpiresAt)
	// The token is in the cookie only, never in the body: JavaScript never sees it.
	c.JSON(http.StatusOK, gin.H{"user": u})
}

// Logout handles POST /api/v1/platform/logout.
func (h *PlatformHandler) Logout(c *gin.Context) {
	token, _ := c.Cookie(platformCookie)
	if err := h.auth.Logout(c.Request.Context(), token); err != nil {
		writeInternal(c, err)
		return
	}
	clearPlatformCookie(c)
	c.Status(http.StatusNoContent)
}

// Me handles GET /api/v1/platform/me: who is logged in to the console.
func (h *PlatformHandler) Me(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"user": currentUser(c)})
}

// Overview handles GET /api/v1/platform/overview.
func (h *PlatformHandler) Overview(c *gin.Context) {
	o, err := h.svc.Overview(c.Request.Context())
	if err != nil {
		writeInternal(c, err)
		return
	}
	c.JSON(http.StatusOK, o)
}

// Tenants handles GET /api/v1/platform/tenants?q=&status=.
func (h *PlatformHandler) Tenants(c *gin.Context) {
	list, err := h.svc.Tenants(c.Request.Context(), platform.TenantFilter{Query: c.Query("q"), Status: c.Query("status")})
	if err != nil {
		writePlatformError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": list})
}

// Audit handles GET /api/v1/platform/audit: the latest platform actions.
func (h *PlatformHandler) Audit(c *gin.Context) {
	list, err := h.svc.Audit(c.Request.Context())
	if err != nil {
		writeInternal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": list})
}

type suspendRequest struct {
	Reason string `json:"reason" binding:"required,max=2000"`
}

// Suspend handles POST /api/v1/platform/tenants/:tenantID/suspend.
func (h *PlatformHandler) Suspend(c *gin.Context) {
	id, ok := platformTenantID(c)
	if !ok {
		return
	}
	var req suspendRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeBindError(c, err)
		return
	}
	t, err := h.svc.Suspend(c.Request.Context(), currentUser(c).ID, id, req.Reason)
	if err != nil {
		writePlatformError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"tenant": t})
}

// Reactivate handles POST /api/v1/platform/tenants/:tenantID/reactivate.
func (h *PlatformHandler) Reactivate(c *gin.Context) {
	id, ok := platformTenantID(c)
	if !ok {
		return
	}
	t, err := h.svc.Reactivate(c.Request.Context(), currentUser(c).ID, id)
	if err != nil {
		writePlatformError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"tenant": t})
}

func platformTenantID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("tenantID"))
	if err != nil {
		writeError(c, http.StatusNotFound, "tenant_not_found", "tenant not found")
		return uuid.Nil, false
	}
	return id, true
}

func writePlatformError(c *gin.Context, err error) {
	var verr *platform.ValidationError
	switch {
	case errors.As(err, &verr):
		writeError(c, http.StatusBadRequest, "validation_failed", verr.Message,
			fieldError{Field: verr.Field, Rule: "format"})
	case errors.Is(err, platform.ErrTenantNotFound):
		writeError(c, http.StatusNotFound, "tenant_not_found", "tenant not found")
	case errors.Is(err, platform.ErrAlreadySuspended):
		writeError(c, http.StatusConflict, "already_suspended", "the shop is already suspended")
	case errors.Is(err, platform.ErrNotSuspended):
		writeError(c, http.StatusConflict, "not_suspended", "the shop is not suspended")
	default:
		writeInternal(c, err)
	}
}
