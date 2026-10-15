package product

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mscs/internal/database"
)

const (
	DefaultPageSize = 20
	MaxPageSize     = 100
)

// ValidationError is a business-rule failure on one field (maps to HTTP 400).
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("product: invalid %s: %s", e.Field, e.Message)
}

// Service implements the product use cases for ONE tenant at a time:
// every method takes tenantID and runs inside database.InTx scoped to it.
type Service struct {
	pool *pgxpool.Pool
	repo Repository
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool}
}

func (s *Service) inTenant(ctx context.Context, tenantID uuid.UUID, fn func(tx pgx.Tx) error) error {
	return database.InTx(ctx, s.pool, database.Scope{TenantID: tenantID}, fn)
}

func (s *Service) Create(ctx context.Context, tenantID uuid.UUID, in CreateInput) (Product, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.Description = strings.TrimSpace(in.Description)
	in.SKU = normalizeSKU(in.SKU)
	if in.Name == "" {
		return Product{}, &ValidationError{"name", "must not be blank"}
	}

	var p Product
	err := s.inTenant(ctx, tenantID, func(tx pgx.Tx) error {
		var err error
		p, err = s.repo.Create(ctx, tx, tenantID, in)
		return err
	})
	return p, err
}

func (s *Service) Get(ctx context.Context, tenantID, id uuid.UUID) (Product, error) {
	var p Product
	err := s.inTenant(ctx, tenantID, func(tx pgx.Tx) error {
		var err error
		p, err = s.repo.Get(ctx, tx, tenantID, id)
		return err
	})
	return p, err
}

func (s *Service) List(ctx context.Context, tenantID uuid.UUID, params ListParams) (Page, error) {
	if params.Limit <= 0 {
		params.Limit = DefaultPageSize
	}
	params.Limit = min(params.Limit, MaxPageSize)
	params.Query = strings.TrimSpace(params.Query)

	// Ask for one extra row: if it comes back, there is a next page.
	want := params.Limit
	params.Limit++

	var items []Product
	err := s.inTenant(ctx, tenantID, func(tx pgx.Tx) error {
		var err error
		items, err = s.repo.List(ctx, tx, tenantID, params)
		return err
	})
	if err != nil {
		return Page{}, err
	}

	page := Page{Items: items}
	if len(items) > want {
		page.Items = items[:want]
		last := page.Items[want-1].ID
		page.NextCursor = &last
	}
	return page, nil
}

func (s *Service) Update(ctx context.Context, tenantID, id uuid.UUID, in UpdateInput) (Product, error) {
	if in.Name != nil {
		name := strings.TrimSpace(*in.Name)
		if name == "" {
			return Product{}, &ValidationError{"name", "must not be blank"}
		}
		in.Name = &name
	}
	if in.Description != nil {
		d := strings.TrimSpace(*in.Description)
		in.Description = &d
	}
	if in.SKU != nil {
		// An empty SKU here means "leave unchanged" (removing a SKU is not supported yet).
		in.SKU = normalizeSKU(in.SKU)
	}
	if in.Status != nil && *in.Status != StatusActive && *in.Status != StatusArchived {
		return Product{}, &ValidationError{"status", "must be active or archived"}
	}

	var p Product
	err := s.inTenant(ctx, tenantID, func(tx pgx.Tx) error {
		var err error
		p, err = s.repo.Update(ctx, tx, tenantID, id, in)
		return err
	})
	return p, err
}

// Archive hides a product from sale without deleting it (orders keep referencing it).
func (s *Service) Archive(ctx context.Context, tenantID, id uuid.UUID) error {
	archived := StatusArchived
	_, err := s.Update(ctx, tenantID, id, UpdateInput{Status: &archived})
	return err
}

// normalizeSKU trims spaces and turns an empty SKU into nil (stored as NULL).
func normalizeSKU(sku *string) *string {
	if sku == nil {
		return nil
	}
	v := strings.TrimSpace(*sku)
	if v == "" {
		return nil
	}
	return &v
}
