package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"mscs/internal/order"
	"mscs/internal/storefront"
)

// StorefrontHandler serves the public shop pages' API: /api/v1/public/shops/:slug/...
// No authentication: the slug selects the shop, and a tracking token unlocks one order.
type StorefrontHandler struct {
	svc *storefront.Service
}

func NewStorefrontHandler(svc *storefront.Service) *StorefrontHandler {
	return &StorefrontHandler{svc: svc}
}

type placeOrderRequest struct {
	CustomerName    string             `json:"customer_name" binding:"required,max=200"`
	CustomerPhone   string             `json:"customer_phone" binding:"required,max=30"`
	ShippingAddress string             `json:"shipping_address" binding:"required,max=1000"`
	Note            string             `json:"note" binding:"max=2000"`
	Items           []orderItemRequest `json:"items" binding:"required,min=1,max=100,dive"`
	// The total the customer saw. Not a price the customer sets: the server only compares it with
	// its own total and refuses the order if they differ.
	ExpectedTotalSatang *int64 `json:"expected_total_satang" binding:"omitempty,min=0"`
	// Deliberately no channel, shipping fee or prices: the server decides those.
}

// Shop handles GET /public/shops/:slug
func (h *StorefrontHandler) Shop(c *gin.Context) {
	shop, err := h.svc.Shop(c.Request.Context(), c.Param("slug"))
	if err != nil {
		writeStorefrontError(c, err)
		return
	}
	c.JSON(http.StatusOK, shop)
}

// Products handles GET /public/shops/:slug/products
func (h *StorefrontHandler) Products(c *gin.Context) {
	products, err := h.svc.Products(c.Request.Context(), c.Param("slug"))
	if err != nil {
		writeStorefrontError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": products})
}

// PlaceOrder handles POST /public/shops/:slug/orders
func (h *StorefrontHandler) PlaceOrder(c *gin.Context) {
	var req placeOrderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeBindError(c, err)
		return
	}

	in := storefront.PlaceOrderInput{
		CustomerName:        req.CustomerName,
		CustomerPhone:       req.CustomerPhone,
		ShippingAddress:     req.ShippingAddress,
		Note:                req.Note,
		Items:               make([]order.ItemInput, len(req.Items)),
		ExpectedTotalSatang: req.ExpectedTotalSatang,
	}
	for i, it := range req.Items {
		in.Items[i] = order.ItemInput{ProductID: it.ProductID, Quantity: it.Quantity}
	}

	o, token, err := h.svc.PlaceOrder(c.Request.Context(), c.Param("slug"), in)
	if err != nil {
		writeStorefrontError(c, err)
		return
	}
	// The tracking token appears in this response only; the client turns it into the
	// customer's link to the order (it cannot be recovered later).
	c.JSON(http.StatusCreated, gin.H{"order": o, "tracking_token": token})
}

// TrackOrder handles GET /public/shops/:slug/orders/:token
func (h *StorefrontHandler) TrackOrder(c *gin.Context) {
	o, err := h.svc.TrackOrder(c.Request.Context(), c.Param("slug"), c.Param("token"))
	if err != nil {
		writeStorefrontError(c, err)
		return
	}
	c.JSON(http.StatusOK, o)
}

func writeStorefrontError(c *gin.Context, err error) {
	if errors.Is(err, storefront.ErrShopNotFound) {
		writeError(c, http.StatusNotFound, "shop_not_found", "shop not found")
		return
	}
	// Order problems (validation, stock, unknown token) map exactly like in the merchant API.
	writeOrderError(c, err)
}
