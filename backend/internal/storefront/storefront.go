// Package storefront serves the public, customer-facing side of a shop: no login, addressed
// by the shop's slug. It is a facade over the tenant, product and order packages that decides
// what the public may see (public view types below, never the internal structs).
package storefront

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"mscs/internal/order"
	"mscs/internal/product"
	"mscs/internal/tenant"
)

// ErrShopNotFound covers unknown slugs and suspended shops alike.
var ErrShopNotFound = errors.New("storefront: shop not found")

// ---- Public views: only what a customer needs. ----

type Shop struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
	// Shipping lets the cart show the fee before checkout; the order is charged by the server.
	Shipping tenant.ShippingRates `json:"shipping"`
}

type Product struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	PriceSatang int64     `json:"price_satang"`
	Stock       int       `json:"stock"`
	// Photos (URLs only), the cover first.
	Images []product.Image `json:"images"`
}

type OrderLine struct {
	ProductName     string `json:"product_name"`
	UnitPriceSatang int64  `json:"unit_price_satang"`
	Quantity        int    `json:"quantity"`
	LineTotalSatang int64  `json:"line_total_satang"`
}

// Order deliberately omits phone, address, note and internal ids: anyone holding the
// tracking link can see it, so it shows only what is needed to follow the order.
type Order struct {
	OrderNumber       int64        `json:"order_number"`
	Status            order.Status `json:"status"`
	CustomerName      string       `json:"customer_name"`
	Items             []OrderLine  `json:"items"`
	SubtotalSatang    int64        `json:"subtotal_satang"`
	ShippingFeeSatang int64        `json:"shipping_fee_satang"`
	TotalSatang       int64        `json:"total_satang"`
	CreatedAt         time.Time    `json:"created_at"`
	PaidAt            *time.Time   `json:"paid_at"`
	ShippedAt         *time.Time   `json:"shipped_at"`
	CompletedAt       *time.Time   `json:"completed_at"`
	CancelledAt       *time.Time   `json:"cancelled_at"`
	// PayBy is the payment deadline while the order is pending; after it the order is cancelled.
	PayBy        *time.Time          `json:"pay_by"`
	CancelReason *order.CancelReason `json:"cancel_reason"`
}

type PlaceOrderInput struct {
	CustomerName    string
	CustomerPhone   string
	ShippingAddress string
	Note            string
	Items           []order.ItemInput
	// ExpectedTotalSatang is the total shown in the cart (optional; the storefront always sends it).
	ExpectedTotalSatang *int64
}

// ---- Service ----

type Service struct {
	pool          *pgxpool.Pool
	tenants       tenant.Repository
	products      *product.Service
	orders        *order.Service
	paymentWindow time.Duration // must match the worker's, see config.PaymentWindow
}

func NewService(pool *pgxpool.Pool, products *product.Service, orders *order.Service, paymentWindow time.Duration) *Service {
	return &Service{pool: pool, products: products, orders: orders, paymentWindow: paymentWindow}
}

// shop resolves a slug to an active tenant. tenants has no RLS, so the pool is enough;
// everything after this runs inside the tenant's RLS scope (via the product/order services).
func (s *Service) shop(ctx context.Context, slug string) (tenant.Tenant, error) {
	t, err := s.tenants.GetBySlug(ctx, s.pool, strings.ToLower(slug))
	if errors.Is(err, tenant.ErrNotFound) || (err == nil && t.Status != tenant.StatusActive) {
		return tenant.Tenant{}, ErrShopNotFound
	}
	return t, err
}

func (s *Service) Shop(ctx context.Context, slug string) (Shop, error) {
	t, err := s.shop(ctx, slug)
	if err != nil {
		return Shop{}, err
	}
	return Shop{Slug: t.Slug, Name: t.Name, Shipping: t.Shipping}, nil
}

// Products lists what is on sale. MVP limit: the newest 100 active products.
func (s *Service) Products(ctx context.Context, slug string) ([]Product, error) {
	t, err := s.shop(ctx, slug)
	if err != nil {
		return nil, err
	}
	active := product.StatusActive
	page, err := s.products.List(ctx, t.ID, product.ListParams{Status: &active, Limit: product.MaxPageSize})
	if err != nil {
		return nil, err
	}
	out := make([]Product, len(page.Items))
	for i, p := range page.Items {
		out[i] = Product{ID: p.ID, Name: p.Name, Description: p.Description, PriceSatang: p.PriceSatang, Stock: p.Stock, Images: p.Images}
	}
	return out, nil
}

// PlaceOrder creates a guest order. Prices always come from the database, and the customer
// cannot choose the channel or the shipping fee: the fee comes from the shop's shipping rates.
// With ExpectedTotalSatang, a change of price or rates since the cart was shown refuses the order
// (order.TotalChangedError) instead of charging an amount the customer did not see.
func (s *Service) PlaceOrder(ctx context.Context, slug string, in PlaceOrderInput) (Order, string, error) {
	t, err := s.shop(ctx, slug)
	if err != nil {
		return Order{}, "", err
	}
	// Without an account, the phone and address are the only way to deliver and to get in touch.
	switch {
	case strings.TrimSpace(in.CustomerPhone) == "":
		return Order{}, "", &order.ValidationError{Field: "customer_phone", Message: "must not be blank"}
	case strings.TrimSpace(in.ShippingAddress) == "":
		return Order{}, "", &order.ValidationError{Field: "shipping_address", Message: "must not be blank"}
	}

	o, token, err := s.orders.PlaceGuestOrder(ctx, t.ID, order.CreateInput{
		CustomerName:        in.CustomerName,
		CustomerPhone:       in.CustomerPhone,
		ShippingAddress:     in.ShippingAddress,
		Note:                in.Note,
		Channel:             order.ChannelStorefront,
		Items:               in.Items,
		ShippingRates:       &t.Shipping,
		ExpectedTotalSatang: in.ExpectedTotalSatang,
	})
	if err != nil {
		return Order{}, "", err
	}
	return s.publicOrder(o), token, nil
}

// TrackOrder shows a guest order to whoever holds its tracking token.
func (s *Service) TrackOrder(ctx context.Context, slug, token string) (Order, error) {
	t, err := s.shop(ctx, slug)
	if err != nil {
		return Order{}, err
	}
	o, err := s.orders.Track(ctx, t.ID, token)
	if err != nil {
		return Order{}, err
	}
	return s.publicOrder(o), nil
}

func (s *Service) publicOrder(o order.Order) Order {
	lines := make([]OrderLine, len(o.Items))
	for i, it := range o.Items {
		lines[i] = OrderLine{
			ProductName:     it.ProductName,
			UnitPriceSatang: it.UnitPriceSatang,
			Quantity:        it.Quantity,
			LineTotalSatang: it.LineTotalSatang,
		}
	}
	var payBy *time.Time
	if o.Status == order.StatusPending {
		t := o.CreatedAt.Add(s.paymentWindow)
		payBy = &t
	}
	return Order{
		OrderNumber:       o.OrderNumber,
		Status:            o.Status,
		CustomerName:      o.CustomerName,
		Items:             lines,
		SubtotalSatang:    o.SubtotalSatang,
		ShippingFeeSatang: o.ShippingFeeSatang,
		TotalSatang:       o.TotalSatang,
		CreatedAt:         o.CreatedAt,
		PaidAt:            o.PaidAt,
		ShippedAt:         o.ShippedAt,
		CompletedAt:       o.CompletedAt,
		CancelledAt:       o.CancelledAt,
		PayBy:             payBy,
		CancelReason:      o.CancelReason,
	}
}
