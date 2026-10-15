package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// GetTenant handles GET /api/v1/tenants/:tenantID (behind RequireAuth + RequireTenantMember).
// The middleware already loaded the tenant and checked membership, so there is nothing left to query.
func GetTenant(c *gin.Context) {
	t, role := currentTenant(c)
	c.JSON(http.StatusOK, gin.H{"tenant": t, "role": role})
}
