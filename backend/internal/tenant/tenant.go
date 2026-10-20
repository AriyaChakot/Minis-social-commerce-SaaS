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
	SuspendedReason string    `json:"suspended_reason"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
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
