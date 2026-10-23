package order

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

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

		fee := in.ShippingFeeSatang
		if in.ShippingRates != nil {
			fee = in.ShippingRates.FeeFor(subtotal)
		}
		// Checked here, on the prices just locked, so nothing can change between check and save.
		if in.ExpectedTotalSatang != nil && *in.ExpectedTotalSatang != subtotal+fee {
			return &TotalChangedError{Expected: *in.ExpectedTotalSatang, SubtotalSatang: subtotal,
				ShippingFeeSatang: fee, TotalSatang: subtotal + fee}
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
			ShippingFeeSatang: fee,
			TotalSatang:       subtotal + fee,
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
	var reason *CancelReason
	if to == StatusCancelled {
		r := CancelByMerchant
		reason = &r
	}
	return s.transition(ctx, tenantID, id, to, reason, nil)
}

// ExpireLimit caps how many orders one ExpireUnpaid call handles; the next run takes the rest.
const ExpireLimit = 100

// ExpireUnpaid cancels storefront orders that are still pending although they were placed
// before cutoff, and puts their stock back. It returns how many orders it cancelled.
//
// Each order is cancelled in its own transaction and tenant scope, so one failure does not undo
// the others; failures are collected and returned together. Merchant-entered orders (chat,
// manual) never expire: the merchant has talked to that customer and decides.
func (s *Service) ExpireUnpaid(ctx context.Context, cutoff time.Time) (int, error) {
	refs, err := s.repo.ExpiredStorefront(ctx, s.pool, cutoff, ExpireLimit)
	if err != nil {
		return 0, err
	}

	reason := CancelPaymentTimeout
	// Re-checked under the row lock: between the search above and the lock, the merchant may
	// have marked the order as paid. Without this check the worker would cancel a paid order
	// (paid -> cancelled is a valid transition for a merchant).
	stillExpired := func(o Order) error {
		if o.Status != StatusPending || o.Channel != ChannelStorefront || !o.CreatedAt.Before(cutoff) {
			return errNoLongerExpired
		}
		return nil
	}

	var cancelled int
	var errs []error
	for _, ref := range refs {
		if ctx.Err() != nil { // shutting down: stop between orders, never in the middle of one
			break
		}
		_, err := s.transition(ctx, ref.TenantID, ref.ID, StatusCancelled, &reason, stillExpired)
		switch {
		case err == nil:
			cancelled++
		case errors.Is(err, errNoLongerExpired):
			// Paid (or otherwise handled) in the meantime: nothing to do.
		default:
			errs = append(errs, fmt.Errorf("order %s: %w", ref.ID, err))
		}
	}
	return cancelled, errors.Join(errs...)
}

var errNoLongerExpired = errors.New("order: no longer expired")

// transition is the one place that changes an order's status. check, if not nil, runs on the
// locked row before anything changes; an error from it aborts the transition.
func (s *Service) transition(ctx context.Context, tenantID, id uuid.UUID, to Status, reason *CancelReason, check func(Order) error) (Order, error) {
	var updated Order
	err := s.inTenant(ctx, tenantID, func(tx pgx.Tx) error {
		// Lock the order row first: a second, concurrent "cancel" waits here, then sees
		// status=cancelled and fails the CanBecome check, so stock is never returned twice.
		current, err := s.repo.Get(ctx, tx, tenantID, id, true)
		if err != nil {
			return err
		}
		if check != nil {
			if err := check(current); err != nil {
				return err
			}
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

		if updated, err = s.repo.SetStatus(ctx, tx, tenantID, id, to, reason); err != nil {
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
