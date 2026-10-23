package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"mscs/internal/tenant"
)

// GetTenant handles GET /api/v1/tenants/:tenantID (behind RequireAuth + RequireTenantMember).
// The middleware already loaded the tenant and checked membership, so there is nothing left to query.
func GetTenant(c *gin.Context) {
	t, role := currentTenant(c)
	c.JSON(http.StatusOK, gin.H{"tenant": t, "role": role})
}

// ShippingHandler saves a shop's shipping rates.
type ShippingHandler struct {
	pool  *pgxpool.Pool
	repos tenant.Repository
}

func NewShippingHandler(pool *pgxpool.Pool) *ShippingHandler {
	return &ShippingHandler{pool: pool}
}

type shippingRequest struct {
	// Pointers so that "missing" and 0 differ: fee_satang is required, free_from_satang null = off.
	FeeSatang      *int64 `json:"fee_satang" binding:"required"`
	FreeFromSatang *int64 `json:"free_from_satang"`
}

// Update handles PUT /tenants/:tenantID/shipping (owner/admin). New rates apply to orders placed
// from now on; existing orders keep the fee they were placed with.
func (h *ShippingHandler) Update(c *gin.Context) {
	t, _ := currentTenant(c)
	var req shippingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeBindError(c, err)
		return
	}
	rates := tenant.ShippingRates{FeeSatang: *req.FeeSatang, FreeFromSatang: req.FreeFromSatang}
	var verr *tenant.ValidationError
	if err := rates.Validate(); errors.As(err, &verr) {
		writeError(c, http.StatusBadRequest, "validation_failed", verr.Message,
			fieldError{Field: verr.Field, Rule: "range"})
		return
	}
	// tenants has no RLS (it is the list of shops itself): the membership and role checks
	// were done by the middleware, and the UPDATE is by this shop's id only.
	updated, err := h.repos.UpdateShipping(c.Request.Context(), h.pool, t.ID, rates)
	if err != nil {
		writeInternal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"tenant": updated})
}
