package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"mscs/internal/order"
)

// OrderHandler exposes the order use cases under /api/v1/tenants/:tenantID/orders.
// Every shop member may create and process orders; the service decides who may cancel.
type OrderHandler struct {
	svc *order.Service
}

func NewOrderHandler(svc *order.Service) *OrderHandler {
	return &OrderHandler{svc: svc}
}

type createOrderRequest struct {
	CustomerName      string `json:"customer_name" binding:"required,max=200"`
	CustomerPhone     string `json:"customer_phone" binding:"max=30"`
	ShippingAddress   string `json:"shipping_address" binding:"max=1000"`
	Channel           string `json:"channel" binding:"omitempty,oneof=manual line facebook instagram storefront"`
	Note              string `json:"note" binding:"max=2000"`
	ShippingFeeSatang int64  `json:"shipping_fee_satang" binding:"min=0"`
	// dive = also validate every element of the slice with the rules on orderItemRequest.
	Items []orderItemRequest `json:"items" binding:"required,min=1,max=100,dive"`
}

type orderItemRequest struct {
	ProductID uuid.UUID `json:"product_id" binding:"required"`
	Quantity  int       `json:"quantity" binding:"required,min=1,max=10000"`
}

type transitionRequest struct {
	Status string `json:"status" binding:"required,oneof=paid shipped completed cancelled"`
}

// List handles GET /orders?status=pending&q=somchai&limit=20&cursor=<id>
func (h *OrderHandler) List(c *gin.Context) {
	t, _ := currentTenant(c)

	var params order.ListParams
	switch status := c.DefaultQuery("status", "all"); status {
	case "all":
	case "pending", "paid", "shipped", "completed", "cancelled":
		s := order.Status(status)
		params.Status = &s
	default:
		writeError(c, http.StatusBadRequest, "invalid_query", "unknown status")
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

// Get handles GET /orders/:orderID
func (h *OrderHandler) Get(c *gin.Context) {
	t, _ := currentTenant(c)
	id, ok := orderID(c)
	if !ok {
		return
	}
	o, err := h.svc.Get(c.Request.Context(), t.ID, id)
	if err != nil {
		writeOrderError(c, err)
		return
	}
	c.JSON(http.StatusOK, o)
}

// Create handles POST /orders
func (h *OrderHandler) Create(c *gin.Context) {
	t, _ := currentTenant(c)
	var req createOrderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeBindError(c, err)
		return
	}

	in := order.CreateInput{
		CustomerName:      req.CustomerName,
		CustomerPhone:     req.CustomerPhone,
		ShippingAddress:   req.ShippingAddress,
		Channel:           order.Channel(req.Channel),
		Note:              req.Note,
		ShippingFeeSatang: req.ShippingFeeSatang,
		Items:             make([]order.ItemInput, len(req.Items)),
	}
	for i, it := range req.Items {
		in.Items[i] = order.ItemInput{ProductID: it.ProductID, Quantity: it.Quantity}
	}

	o, err := h.svc.Create(c.Request.Context(), t.ID, currentUser(c).ID, in)
	if err != nil {
		writeOrderError(c, err)
		return
	}
	c.JSON(http.StatusCreated, o)
}

// Transition handles POST /orders/:orderID/status  {"status": "paid"}
func (h *OrderHandler) Transition(c *gin.Context) {
	t, role := currentTenant(c)
	id, ok := orderID(c)
	if !ok {
		return
	}
	var req transitionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeBindError(c, err)
		return
	}

	o, err := h.svc.Transition(c.Request.Context(), t.ID, id, order.Status(req.Status), role)
	if err != nil {
		writeOrderError(c, err)
		return
	}
	c.JSON(http.StatusOK, o)
}

func orderID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("orderID"))
	if err != nil {
		writeError(c, http.StatusNotFound, "order_not_found", "order not found")
		return uuid.Nil, false
	}
	return id, true
}

func writeOrderError(c *gin.Context, err error) {
	var (
		verr     *order.ValidationError
		stockErr *order.InsufficientStockError
		unavail  *order.ProductUnavailableError
		transErr *order.TransitionError
		totalErr *order.TotalChangedError
	)
	switch {
	case errors.As(err, &verr):
		writeError(c, http.StatusBadRequest, "validation_failed", verr.Message,
			fieldError{Field: verr.Field, Rule: "format"})
	case errors.As(err, &stockErr):
		writeErrorDetails(c, http.StatusConflict, "insufficient_stock", "not enough stock",
			gin.H{"product_id": stockErr.ProductID, "requested": stockErr.Requested, "available": stockErr.Available})
	case errors.As(err, &unavail):
		writeErrorDetails(c, http.StatusConflict, "product_unavailable", "a product is not available for sale",
			gin.H{"product_id": unavail.ProductID})
	case errors.As(err, &totalErr):
		writeErrorDetails(c, http.StatusConflict, "total_changed", "prices or shipping rates have changed",
			gin.H{"subtotal_satang": totalErr.SubtotalSatang, "shipping_fee_satang": totalErr.ShippingFeeSatang,
				"total_satang": totalErr.TotalSatang})
	case errors.As(err, &transErr):
		writeErrorDetails(c, http.StatusConflict, "invalid_transition", transErr.Error(),
			gin.H{"from": transErr.From, "to": transErr.To})
	case errors.Is(err, order.ErrCancelForbidden):
		writeError(c, http.StatusForbidden, "forbidden", "only owners and admins can cancel orders")
	case errors.Is(err, order.ErrNotFound):
		writeError(c, http.StatusNotFound, "order_not_found", "order not found")
	default:
		writeInternal(c, err)
	}
}
