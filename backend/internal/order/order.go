// Package order owns orders: creating them (with stock), their lifecycle, and listing them.
package order

import (
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
)

// Order is one customer purchase in one shop.
type Order struct {
	ID                uuid.UUID     `json:"id"`
	TenantID          uuid.UUID     `json:"tenant_id"`
	OrderNumber       int64         `json:"order_number"` // human-friendly, per shop: 1001, 1002, ...
	CustomerName      string        `json:"customer_name"`
	CustomerPhone     string        `json:"customer_phone"`
	ShippingAddress   string        `json:"shipping_address"`
	Channel           Channel       `json:"channel"`
	Note              string        `json:"note"`
	Status            Status        `json:"status"`
	SubtotalSatang    int64         `json:"subtotal_satang"`
	ShippingFeeSatang int64         `json:"shipping_fee_satang"`
	TotalSatang       int64         `json:"total_satang"`
	CreatedBy         *uuid.UUID    `json:"created_by"`
	PaidAt            *time.Time    `json:"paid_at"`
	ShippedAt         *time.Time    `json:"shipped_at"`
	CompletedAt       *time.Time    `json:"completed_at"`
	CancelledAt       *time.Time    `json:"cancelled_at"`
	CancelReason      *CancelReason `json:"cancel_reason"` // set exactly when Status is cancelled
	CreatedAt         time.Time     `json:"created_at"`
	UpdatedAt         time.Time     `json:"updated_at"`
	Items             []Item        `json:"items,omitempty"` // filled by Get/Create, left out of lists
}

// Item is one order line. Name, SKU and price are a snapshot taken when the order was placed.
type Item struct {
	ID              uuid.UUID `json:"id"`
	ProductID       uuid.UUID `json:"product_id"`
	ProductName     string    `json:"product_name"`
	SKU             *string   `json:"sku"`
	UnitPriceSatang int64     `json:"unit_price_satang"`
	Quantity        int       `json:"quantity"`
	LineTotalSatang int64     `json:"line_total_satang"`
}

// Channel is where the order came from.
type Channel string

const (
	ChannelManual     Channel = "manual"
	ChannelLine       Channel = "line"
	ChannelFacebook   Channel = "facebook"
	ChannelInstagram  Channel = "instagram"
	ChannelStorefront Channel = "storefront"
)

var channels = []Channel{ChannelManual, ChannelLine, ChannelFacebook, ChannelInstagram, ChannelStorefront}

// Status is a step in the order lifecycle.
type Status string

const (
	StatusPending   Status = "pending"
	StatusPaid      Status = "paid"
	StatusShipped   Status = "shipped"
	StatusCompleted Status = "completed"
	StatusCancelled Status = "cancelled"
)

// transitions is the order state machine: for each status, the statuses it may move to.
//
//	pending ──► paid ──► shipped ──► completed
//	   │          │
//	   └──────────┴──► cancelled   (stock goes back on the shelf)
//
// completed and cancelled are final: they have no outgoing transitions.
var transitions = map[Status][]Status{
	StatusPending: {StatusPaid, StatusCancelled},
	StatusPaid:    {StatusShipped, StatusCancelled},
	StatusShipped: {StatusCompleted},
}

// CanBecome reports whether the state machine allows moving from s to next.
func (s Status) CanBecome(next Status) bool {
	return slices.Contains(transitions[s], next)
}

// CancelReason says who or what cancelled an order.
type CancelReason string

const (
	CancelByMerchant     CancelReason = "merchant"        // a shop member cancelled it
	CancelPaymentTimeout CancelReason = "payment_timeout" // unpaid storefront order, expired by the worker
)

// CreateInput is a new order as entered by a merchant (or, later, the storefront).
type CreateInput struct {
	CustomerName      string
	CustomerPhone     string
	ShippingAddress   string
	Channel           Channel
	Note              string
	ShippingFeeSatang int64
	Items             []ItemInput
}

type ItemInput struct {
	ProductID uuid.UUID
	Quantity  int
}

type ListParams struct {
	Status *Status
	Query  string // customer name/phone contains, or an exact order number
	After  *uuid.UUID
	Limit  int
}

type Page struct {
	Items      []Order    `json:"items"`
	NextCursor *uuid.UUID `json:"next_cursor"`
}

// ---------------------------------------------------------------------------
// Errors. Struct errors carry data the API returns to the client (which product, how many left).
// ---------------------------------------------------------------------------

var (
	ErrNotFound        = errors.New("order: not found")
	ErrCancelForbidden = errors.New("order: only owners and admins can cancel orders")
)

type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("order: invalid %s: %s", e.Field, e.Message)
}

// ProductUnavailableError: the product does not exist in this shop, or is archived.
type ProductUnavailableError struct {
	ProductID uuid.UUID
}

func (e *ProductUnavailableError) Error() string {
	return fmt.Sprintf("order: product %s is not available", e.ProductID)
}

type InsufficientStockError struct {
	ProductID uuid.UUID
	Requested int
	Available int
}

func (e *InsufficientStockError) Error() string {
	return fmt.Sprintf("order: product %s: requested %d, only %d in stock", e.ProductID, e.Requested, e.Available)
}

type TransitionError struct {
	From Status
	To   Status
}

func (e *TransitionError) Error() string {
	return fmt.Sprintf("order: cannot change status from %s to %s", e.From, e.To)
}
