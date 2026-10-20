package handler

import (
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"mscs/internal/auth"
	"mscs/internal/tenant"
	"mscs/internal/user"
)

// Keys for values that middleware stores on the gin.Context for later handlers.
const (
	ctxUserKey   = "auth.user"
	ctxTenantKey = "auth.tenant"
	ctxRoleKey   = "auth.role"
)

// RequireAuth rejects the request with 401 unless it carries a valid session token:
//
//	Authorization: Bearer <token>
func RequireAuth(svc *auth.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		u, err := svc.Authenticate(c.Request.Context(), bearerToken(c))
		if errors.Is(err, auth.ErrUnauthenticated) {
			writeError(c, http.StatusUnauthorized, "unauthenticated", "login required")
			return
		}
		if err != nil {
			writeInternal(c, err)
			return
		}
		c.Set(ctxUserKey, u)
		c.Next()
	}
}

// RequireTenantMember must run after RequireAuth on routes with a :tenantID path parameter.
// It answers 404 (not 403) when the user is not a member, so outsiders cannot even
// confirm that a tenant id exists.
func RequireTenantMember(svc *auth.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		tenantID, err := uuid.Parse(c.Param("tenantID"))
		if err != nil {
			writeError(c, http.StatusNotFound, "tenant_not_found", "tenant not found")
			return
		}

		t, role, err := svc.TenantAccess(c.Request.Context(), tenantID, currentUser(c).ID)
		if errors.Is(err, tenant.ErrNotMember) || errors.Is(err, tenant.ErrNotFound) {
			writeError(c, http.StatusNotFound, "tenant_not_found", "tenant not found")
			return
		}
		if err != nil {
			writeInternal(c, err)
			return
		}
		// 403 with the reason: the member knows the shop, they should know why it is closed.
		if t.Status == tenant.StatusSuspended {
			writeErrorDetails(c, http.StatusForbidden, "tenant_suspended", "this shop is suspended by the platform",
				gin.H{"reason": t.SuspendedReason})
			return
		}
		c.Set(ctxTenantKey, t)
		c.Set(ctxRoleKey, role)
		c.Next()
	}
}

// RequireRole must run after RequireTenantMember. It answers 403 when the member's role is
// not in the allowed list. (403, not 404: the user is a member, so the shop's existence is no secret.)
func RequireRole(allowed ...tenant.Role) gin.HandlerFunc {
	return func(c *gin.Context) {
		_, role := currentTenant(c)
		if !slices.Contains(allowed, role) {
			writeError(c, http.StatusForbidden, "forbidden", "your role cannot do this")
			return
		}
		c.Next()
	}
}

// bearerToken extracts the token from "Authorization: Bearer <token>", or returns "".
func bearerToken(c *gin.Context) string {
	token, ok := strings.CutPrefix(c.GetHeader("Authorization"), "Bearer ")
	if !ok {
		return ""
	}
	return strings.TrimSpace(token)
}

// The getters below may only be used on routes behind the matching middleware;
// MustGet panics otherwise, which gin.Recovery turns into a 500 (a programming error, not a user error).

func currentUser(c *gin.Context) user.User {
	return c.MustGet(ctxUserKey).(user.User)
}

func currentTenant(c *gin.Context) (tenant.Tenant, tenant.Role) {
	return c.MustGet(ctxTenantKey).(tenant.Tenant), c.MustGet(ctxRoleKey).(tenant.Role)
}
