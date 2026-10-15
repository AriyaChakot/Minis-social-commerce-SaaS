package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"mscs/internal/product"
)

// ProductHandler exposes the product use cases under /api/v1/tenants/:tenantID/products.
// The tenant always comes from the URL via RequireTenantMember, never from the request body,
// so a client cannot create a product in a shop it does not belong to.
type ProductHandler struct {
	svc *product.Service
}

func NewProductHandler(svc *product.Service) *ProductHandler {
	return &ProductHandler{svc: svc}
}

type createProductRequest struct {
	SKU         *string `json:"sku" binding:"omitempty,max=64"`
	Name        string  `json:"name" binding:"required,max=200"`
	Description string  `json:"description" binding:"max=5000"`
	// Pointer + required: the field must be present, but 0 (a free item) is allowed.
	PriceSatang *int64 `json:"price_satang" binding:"required,min=0"`
	Stock       *int   `json:"stock" binding:"omitempty,min=0"`
}

// All fields optional: only the fields present in the JSON are changed (PATCH semantics).
type updateProductRequest struct {
	SKU         *string `json:"sku" binding:"omitempty,max=64"`
	Name        *string `json:"name" binding:"omitempty,max=200"`
	Description *string `json:"description" binding:"omitempty,max=5000"`
	PriceSatang *int64  `json:"price_satang" binding:"omitempty,min=0"`
	Stock       *int    `json:"stock" binding:"omitempty,min=0"`
	Status      *string `json:"status" binding:"omitempty,oneof=active archived"`
}

// List handles GET /products?status=active|archived|all&q=...&limit=20&cursor=<id>
func (h *ProductHandler) List(c *gin.Context) {
	t, _ := currentTenant(c)

	var params product.ListParams
	switch status := c.DefaultQuery("status", "active"); status {
	case "all":
	case string(product.StatusActive), string(product.StatusArchived):
		s := product.Status(status)
		params.Status = &s
	default:
		writeError(c, http.StatusBadRequest, "invalid_query", "status must be active, archived or all")
		return
	}

	params.Query = c.Query("q")

	if raw := c.Query("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			writeError(c, http.StatusBadRequest, "invalid_query", "limit must be a positive integer")
			return
		}
		params.Limit = n
	}

	if raw := c.Query("cursor"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			writeError(c, http.StatusBadRequest, "invalid_query", "cursor is invalid")
			return
		}
		params.After = &id
	}

	page, err := h.svc.List(c.Request.Context(), t.ID, params)
	if err != nil {
		writeInternal(c, err)
		return
	}
	c.JSON(http.StatusOK, page)
}

// Get handles GET /products/:productID
func (h *ProductHandler) Get(c *gin.Context) {
	t, _ := currentTenant(c)
	id, ok := productID(c)
	if !ok {
		return
	}
	p, err := h.svc.Get(c.Request.Context(), t.ID, id)
	if err != nil {
		writeProductError(c, err)
		return
	}
	c.JSON(http.StatusOK, p)
}

// Create handles POST /products (owner/admin only)
func (h *ProductHandler) Create(c *gin.Context) {
	t, _ := currentTenant(c)
	var req createProductRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeBindError(c, err)
		return
	}

	in := product.CreateInput{
		SKU:         req.SKU,
		Name:        req.Name,
		Description: req.Description,
		PriceSatang: *req.PriceSatang, // safe: `required` guarantees non-nil
	}
	if req.Stock != nil {
		in.Stock = *req.Stock
	}

	p, err := h.svc.Create(c.Request.Context(), t.ID, in)
	if err != nil {
		writeProductError(c, err)
		return
	}
	c.JSON(http.StatusCreated, p)
}

// Update handles PATCH /products/:productID (owner/admin only)
func (h *ProductHandler) Update(c *gin.Context) {
	t, _ := currentTenant(c)
	id, ok := productID(c)
	if !ok {
		return
	}
	var req updateProductRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeBindError(c, err)
		return
	}

	in := product.UpdateInput{
		SKU:         req.SKU,
		Name:        req.Name,
		Description: req.Description,
		PriceSatang: req.PriceSatang,
		Stock:       req.Stock,
	}
	if req.Status != nil {
		s := product.Status(*req.Status)
		in.Status = &s
	}

	p, err := h.svc.Update(c.Request.Context(), t.ID, id, in)
	if err != nil {
		writeProductError(c, err)
		return
	}
	c.JSON(http.StatusOK, p)
}

// Archive handles DELETE /products/:productID (owner/admin only).
// It is a soft delete: the product is archived, not removed, because orders reference it.
func (h *ProductHandler) Archive(c *gin.Context) {
	t, _ := currentTenant(c)
	id, ok := productID(c)
	if !ok {
		return
	}
	if err := h.svc.Archive(c.Request.Context(), t.ID, id); err != nil {
		writeProductError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func productID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("productID"))
	if err != nil {
		writeError(c, http.StatusNotFound, "product_not_found", "product not found")
		return uuid.Nil, false
	}
	return id, true
}

func writeProductError(c *gin.Context, err error) {
	var verr *product.ValidationError
	switch {
	case errors.As(err, &verr):
		writeError(c, http.StatusBadRequest, "validation_failed", verr.Message,
			fieldError{Field: verr.Field, Rule: "format"})
	case errors.Is(err, product.ErrNotFound):
		writeError(c, http.StatusNotFound, "product_not_found", "product not found")
	case errors.Is(err, product.ErrSKUTaken):
		writeError(c, http.StatusConflict, "sku_taken", "this SKU is already used in your shop")
	default:
		writeInternal(c, err)
	}
}
