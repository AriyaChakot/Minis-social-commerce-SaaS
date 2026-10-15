package order

import (
	"context"
	"slices"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mscs/internal/database"
	"mscs/internal/product"
	"mscs/internal/securetoken"
	"mscs/internal/tenant"
)

const (
	DefaultPageSize = 20
	MaxPageSize     = 100
	MaxItems        = 100
)

// Service implements the order use cases for one tenant at a time.
// It uses product.Repository for stock instead of touching the products table itself:
// the product package stays the only owner of product SQL.
type Service struct {
	pool     *pgxpool.Pool
	repo     Repository
	products product.Repository
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool}
}

func (s *Service) inTenant(ctx context.Context, tenantID uuid.UUID, fn func(tx pgx.Tx) error) error {
	return database.InTx(ctx, s.pool, database.Scope{TenantID: tenantID}, fn)
}

// Create records an order entered by a shop member (createdBy) in the merchant app.
func (s *Service) Create(ctx context.Context, tenantID, createdBy uuid.UUID, in CreateInput) (Order, error) {
	return s.place(ctx, tenantID, &createdBy, in, nil)
}

// PlaceGuestOrder records an order placed by a customer without an account (the storefront).
// It returns the order and a tracking token: the customer's only way to view the order later.
// The token is returned exactly once; only its hash is stored.
func (s *Service) PlaceGuestOrder(ctx context.Context, tenantID uuid.UUID, in CreateInput) (Order, string, error) {
	token := securetoken.New()
	o, err := s.place(ctx, tenantID, nil, in, securetoken.Hash(token))
	if err != nil {
		return Order{}, "", err
	}
	return o, token, nil
}

// Track finds the order that a guest's tracking token belongs to, with its lines.
func (s *Service) Track(ctx context.Context, tenantID uuid.UUID, token string) (Order, error) {
	var o Order
	err := s.inTenant(ctx, tenantID, func(tx pgx.Tx) error {
		var err error
		if o, err = s.repo.GetByTrackingToken(ctx, tx, tenantID, securetoken.Hash(token)); err != nil {
			return err
		}
		o.Items, err = s.repo.Items(ctx, tx, tenantID, o.ID)
		return err
	})
	return o, err
}

// place creates an order and takes the items out of stock, all in one transaction:
//
//  1. lock the products (FOR UPDATE) so nobody else can sell the same units meanwhile
//  2. check each product is active and has enough stock
//  3. copy name/SKU/price into the order lines (snapshot) and compute the totals
//  4. take the stock, number the order, save order + lines
//
// If any step fails, the transaction rolls back and no stock is taken.
// createdBy is nil for guest orders; trackingTokenHash is nil for merchant orders.
func (s *Service) place(ctx context.Context, tenantID uuid.UUID, createdBy *uuid.UUID, in CreateInput, trackingTokenHash []byte) (Order, error) {
	if err := normalizeCreate(&in); err != nil {
		return Order{}, err
	}

	ids := make([]uuid.UUID, len(in.Items))
	for i, it := range in.Items {
		ids[i] = it.ProductID
	}

	var created Order
	err := s.inTenant(ctx, tenantID, func(tx pgx.Tx) error {
		locked, err := s.products.LockForOrder(ctx, tx, tenantID, ids)
		if err != nil {
			return err
		}
		byID := make(map[uuid.UUID]product.Product, len(locked))
		for _, p := range locked {
			byID[p.ID] = p
		}

		items := make([]Item, 0, len(in.Items))
		var subtotal int64
		for _, req := range in.Items {
			p, ok := byID[req.ProductID]
			if !ok || p.Status != product.StatusActive {
				return &ProductUnavailableError{ProductID: req.ProductID}
			}
			if p.Stock < req.Quantity {
				return &InsufficientStockError{ProductID: p.ID, Requested: req.Quantity, Available: p.Stock}
			}
			line := p.PriceSatang * int64(req.Quantity)
			subtotal += line
			items = append(items, Item{
				ProductID:       p.ID,
				ProductName:     p.Name,
				SKU:             p.SKU,
				UnitPriceSatang: p.PriceSatang,
				Quantity:        req.Quantity,
				LineTotalSatang: line,
			})
		}

		for _, it := range items {
			if err := s.products.AdjustStock(ctx, tx, tenantID, it.ProductID, -it.Quantity); err != nil {
				return err
			}
		}

		number, err := s.repo.NextNumber(ctx, tx, tenantID)
		if err != nil {
			return err
		}

		created = Order{
			TenantID:          tenantID,
			OrderNumber:       number,
			CustomerName:      in.CustomerName,
			CustomerPhone:     in.CustomerPhone,
			ShippingAddress:   in.ShippingAddress,
			Channel:           in.Channel,
			Note:              in.Note,
			SubtotalSatang:    subtotal,
			ShippingFeeSatang: in.ShippingFeeSatang,
			TotalSatang:       subtotal + in.ShippingFeeSatang,
			CreatedBy:         createdBy,
		}
		if err := s.repo.Insert(ctx, tx, &created, trackingTokenHash); err != nil {
			return err
		}
		for i := range items {
			if err := s.repo.InsertItem(ctx, tx, tenantID, created.ID, &items[i]); err != nil {
				return err
			}
		}
		created.Items = items
		return nil
	})
	return created, err
}

// Get returns one order with its lines.
func (s *Service) Get(ctx context.Context, tenantID, id uuid.UUID) (Order, error) {
	var o Order
	err := s.inTenant(ctx, tenantID, func(tx pgx.Tx) error {
		var err error
		if o, err = s.repo.Get(ctx, tx, tenantID, id, false); err != nil {
			return err
		}
		o.Items, err = s.repo.Items(ctx, tx, tenantID, id)
		return err
	})
	return o, err
}

// List returns one page of orders (without lines). A numeric query also matches the order number.
func (s *Service) List(ctx context.Context, tenantID uuid.UUID, params ListParams) (Page, error) {
	if params.Limit <= 0 {
		params.Limit = DefaultPageSize
	}
	params.Limit = min(params.Limit, MaxPageSize)
	params.Query = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(params.Query), "#"))

	var orderNumber *int64
	if n, err := strconv.ParseInt(params.Query, 10, 64); err == nil {
		orderNumber = &n
	}

	want := params.Limit
	params.Limit++ // one extra row tells us whether there is a next page

	var orders []Order
	err := s.inTenant(ctx, tenantID, func(tx pgx.Tx) error {
		var err error
		orders, err = s.repo.List(ctx, tx, tenantID, params, orderNumber)
		return err
	})
	if err != nil {
		return Page{}, err
	}

	page := Page{Items: orders}
	if len(orders) > want {
		page.Items = orders[:want]
		last := page.Items[want-1].ID
		page.NextCursor = &last
	}
	return page, nil
}

// Transition moves an order along the state machine. Cancelling puts the stock back.
// actorRole is the caller's role in the shop: staff may process orders but not cancel them.
func (s *Service) Transition(ctx context.Context, tenantID, id uuid.UUID, to Status, actorRole tenant.Role) (Order, error) {
	if to == StatusCancelled && actorRole == tenant.RoleStaff {
		return Order{}, ErrCancelForbidden
	}

	var updated Order
	err := s.inTenant(ctx, tenantID, func(tx pgx.Tx) error {
		// Lock the order row first: a second, concurrent "cancel" waits here, then sees
		// status=cancelled and fails the CanBecome check, so stock is never returned twice.
		current, err := s.repo.Get(ctx, tx, tenantID, id, true)
		if err != nil {
			return err
		}
		if !current.Status.CanBecome(to) {
			return &TransitionError{From: current.Status, To: to}
		}

		items, err := s.repo.Items(ctx, tx, tenantID, id)
		if err != nil {
			return err
		}
		if to == StatusCancelled {
			for _, it := range items { // ordered by product_id: same lock order as Create
				if err := s.products.AdjustStock(ctx, tx, tenantID, it.ProductID, it.Quantity); err != nil {
					return err
				}
			}
		}

		if updated, err = s.repo.SetStatus(ctx, tx, tenantID, id, to); err != nil {
			return err
		}
		updated.Items = items
		return nil
	})
	return updated, err
}

// normalizeCreate trims text fields, applies defaults and checks the business rules
// that do not need the database.
func normalizeCreate(in *CreateInput) error {
	in.CustomerName = strings.TrimSpace(in.CustomerName)
	in.CustomerPhone = strings.TrimSpace(in.CustomerPhone)
	in.ShippingAddress = strings.TrimSpace(in.ShippingAddress)
	in.Note = strings.TrimSpace(in.Note)
	if in.Channel == "" {
		in.Channel = ChannelManual
	}

	switch {
	case in.CustomerName == "":
		return &ValidationError{"customer_name", "must not be blank"}
	case !slices.Contains(channels, in.Channel):
		return &ValidationError{"channel", "unknown channel"}
	case in.ShippingFeeSatang < 0:
		return &ValidationError{"shipping_fee_satang", "must not be negative"}
	case len(in.Items) == 0:
		return &ValidationError{"items", "an order needs at least one item"}
	case len(in.Items) > MaxItems:
		return &ValidationError{"items", "too many items"}
	}

	seen := make(map[uuid.UUID]bool, len(in.Items))
	for _, it := range in.Items {
		if it.Quantity <= 0 {
			return &ValidationError{"items", "quantity must be at least 1"}
		}
		if seen[it.ProductID] {
			return &ValidationError{"items", "each product may appear only once; increase its quantity instead"}
		}
		seen[it.ProductID] = true
	}
	return nil
}
