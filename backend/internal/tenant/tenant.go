// Package tenant owns everything about tenants (shops) and their members.
package tenant

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// Tenant is one merchant account (one online shop).
type Tenant struct {
	ID     uuid.UUID `json:"id"`
	Slug   string    `json:"slug"`
	Name   string    `json:"name"`
	Status string    `json:"status"`
	// SuspendedReason: why the platform suspended the shop (empty when active), shown to its members.
	SuspendedReason string `json:"suspended_reason"`
	// Shipping is what the storefront charges for delivery.
	Shipping  ShippingRates `json:"shipping"`
	CreatedAt time.Time     `json:"created_at"`
	UpdatedAt time.Time     `json:"updated_at"`
}

// ShippingRates is how a shop charges for delivery on storefront orders: a flat fee per order,
// free once the subtotal reaches FreeFromSatang (nil: never free, unless the fee is 0).
type ShippingRates struct {
	FeeSatang      int64  `json:"fee_satang"`
	FreeFromSatang *int64 `json:"free_from_satang"`
}

// Limits, the same as the database CHECKs (migration 000016).
const (
	MaxShippingFeeSatang = 1_000_000     // ฿10,000
	MaxFreeFromSatang    = 1_000_000_000 // ฿10,000,000
)

// FeeFor is the shipping fee of an order with this subtotal. The storefront shows the same rule
// (frontend/packages/shared, shippingFeeFor), but only this one is used to charge.
func (r ShippingRates) FeeFor(subtotalSatang int64) int64 {
	if r.FreeFromSatang != nil && subtotalSatang >= *r.FreeFromSatang {
		return 0
	}
	return r.FeeSatang
}

// Validate checks the rates before they are saved.
func (r ShippingRates) Validate() error {
	switch {
	case r.FeeSatang < 0 || r.FeeSatang > MaxShippingFeeSatang:
		return &ValidationError{Field: "fee_satang", Message: "must be between 0 and 1000000"}
	case r.FreeFromSatang != nil && (*r.FreeFromSatang < 1 || *r.FreeFromSatang > MaxFreeFromSatang):
		return &ValidationError{Field: "free_from_satang", Message: "must be between 1 and 1000000000"}
	}
	return nil
}

// ValidationError is a setting the API refuses, with the field to point at.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return "tenant: invalid " + e.Field + ": " + e.Message
}

// Tenant statuses (tenants_status_valid). A suspended shop is closed: its storefront answers
// "not found" and its members cannot use the merchant app until the platform reactivates it.
const (
	StatusActive    = "active"
	StatusSuspended = "suspended"
)

// Role is a member's permission level inside one tenant.
type Role string

const (
	RoleOwner Role = "owner"
	RoleAdmin Role = "admin"
	RoleStaff Role = "staff"
)

// Membership is one tenant a user belongs to, with their role there.
type Membership struct {
	Tenant Tenant `json:"tenant"`
	Role   Role   `json:"role"`
}

// Sentinel errors: callers check them with errors.Is, without knowing anything about SQL.
var (
	ErrNotFound      = errors.New("tenant: not found")
	ErrSlugTaken     = errors.New("tenant: slug already taken")
	ErrAlreadyMember = errors.New("tenant: user is already a member")
	ErrNotMember     = errors.New("tenant: user is not a member")
)
