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
	// mediaBaseURL + object key = an image URL (see WithMediaBaseURL).
	mediaBaseURL string
}

// Option configures a Service.
type Option func(*Service)

// WithMediaBaseURL sets where browsers load product photos from: the API's /media route, or
// a CDN in front of it. Default: the API's route on the same origin.
func WithMediaBaseURL(u string) Option {
	return func(s *Service) { s.mediaBaseURL = u }
}

func NewService(pool *pgxpool.Pool, opts ...Option) *Service {
	s := &Service{pool: pool, mediaBaseURL: "/api/v1/media/"}
	for _, o := range opts {
		o(s)
	}
	return s
}

// withImages fills the Images of the given products, in one query (not one per product).
func (s *Service) withImages(ctx context.Context, db pgx.Tx, tenantID uuid.UUID, items []Product) error {
	if len(items) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, len(items))
	for i, p := range items {
		ids[i] = p.ID
	}
	rows, err := s.repo.imagesOf(ctx, db, tenantID, ids)
	if err != nil {
		return err
	}
	byProduct := map[uuid.UUID][]Image{}
	for _, r := range rows {
		byProduct[r.ProductID] = append(byProduct[r.ProductID], Image{
			ID:       r.ID,
			URL:      s.mediaBaseURL + LargeKey(r.Key),
			ThumbURL: s.mediaBaseURL + ThumbKey(r.Key),
			Width:    r.Width,
			Height:   r.Height,
		})
	}
	for i := range items {
		items[i].Images = byProduct[items[i].ID]
		if items[i].Images == nil {
			items[i].Images = []Image{}
		}
	}
	return nil
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
	p.Images = []Image{}
	return p, err
}

func (s *Service) Get(ctx context.Context, tenantID, id uuid.UUID) (Product, error) {
	var p Product
	err := s.inTenant(ctx, tenantID, func(tx pgx.Tx) error {
		var err error
		if p, err = s.repo.Get(ctx, tx, tenantID, id); err != nil {
			return err
		}
		one := []Product{p}
		err = s.withImages(ctx, tx, tenantID, one)
		p = one[0]
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
		if items, err = s.repo.List(ctx, tx, tenantID, params); err != nil {
			return err
		}
		return s.withImages(ctx, tx, tenantID, items)
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
		if p, err = s.repo.Update(ctx, tx, tenantID, id, in); err != nil {
			return err
		}
		one := []Product{p}
		err = s.withImages(ctx, tx, tenantID, one)
		p = one[0]
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
