// Package product owns a shop's catalog: products, prices and stock.
package product

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// Product is one item a shop sells.
type Product struct {
	ID          uuid.UUID `json:"id"`
	TenantID    uuid.UUID `json:"tenant_id"`
	SKU         *string   `json:"sku"` // pointer: a product may have no SKU (null in JSON and SQL)
	Name        string    `json:"name"`
	Description string    `json:"description"`
	PriceSatang int64     `json:"price_satang"` // 1 THB = 100 satang; integers only, never float
	Stock       int       `json:"stock"`
	Status      Status    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Status string

const (
	StatusActive   Status = "active"
	StatusArchived Status = "archived"
)

var (
	ErrNotFound = errors.New("product: not found")
	ErrSKUTaken = errors.New("product: sku already used in this shop")
)

// CreateInput holds the fields for a new product.
type CreateInput struct {
	SKU         *string
	Name        string
	Description string
	PriceSatang int64
	Stock       int
}

// UpdateInput is a partial update: a nil field means "leave unchanged".
// This is why every field is a pointer: it separates "not sent" (nil) from "set to zero" (&0).
type UpdateInput struct {
	SKU         *string
	Name        *string
	Description *string
	PriceSatang *int64
	Stock       *int
	Status      *Status
}

// ListParams filters and paginates the product list.
type ListParams struct {
	Status *Status     // nil = all statuses
	Query  string      // case-insensitive substring of the name; "" = no filter
	After  *uuid.UUID  // cursor: return products older than this id (nil = first page)
	Limit  int
}

// Page is one page of results. NextCursor is nil on the last page.
type Page struct {
	Items      []Product  `json:"items"`
	NextCursor *uuid.UUID `json:"next_cursor"`
}
