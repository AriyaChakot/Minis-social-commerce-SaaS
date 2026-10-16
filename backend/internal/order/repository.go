package order

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mscs/internal/database"
)

// Repository reads and writes orders, order_items and order_counters (all with RLS:
// db must be a transaction scoped to the tenant). Queries also filter on tenant_id explicitly.
type Repository struct{}

const orderColumns = `id, tenant_id, order_number, customer_name, customer_phone, shipping_address,
	channel, note, status, subtotal_satang, shipping_fee_satang, total_satang, created_by,
	paid_at, shipped_at, completed_at, cancelled_at, cancel_reason, created_at, updated_at`

func scanOrder(row pgx.Row) (Order, error) {
	var o Order
	err := row.Scan(&o.ID, &o.TenantID, &o.OrderNumber, &o.CustomerName, &o.CustomerPhone, &o.ShippingAddress,
		&o.Channel, &o.Note, &o.Status, &o.SubtotalSatang, &o.ShippingFeeSatang, &o.TotalSatang, &o.CreatedBy,
		&o.PaidAt, &o.ShippedAt, &o.CompletedAt, &o.CancelledAt, &o.CancelReason, &o.CreatedAt, &o.UpdatedAt)
	return o, err
}

// NextNumber atomically takes the shop's next order number (first order: 1001).
// The counter row stays locked until the transaction ends, so numbers are unique and,
// because a rollback also undoes the increment, gap-free.
func (Repository) NextNumber(ctx context.Context, db database.DBTX, tenantID uuid.UUID) (int64, error) {
	var n int64
	err := db.QueryRow(ctx,
		`INSERT INTO order_counters (tenant_id, last_number) VALUES ($1, 1001)
		 ON CONFLICT (tenant_id) DO UPDATE SET last_number = order_counters.last_number + 1
		 RETURNING last_number`,
		tenantID,
	).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("order: next number: %w", err)
	}
	return n, nil
}

// Insert saves o and fills in the columns the database generates (id, status, timestamps...).
// trackingTokenHash may be nil (no public tracking link).
func (Repository) Insert(ctx context.Context, db database.DBTX, o *Order, trackingTokenHash []byte) error {
	saved, err := scanOrder(db.QueryRow(ctx,
		`INSERT INTO orders (tenant_id, order_number, customer_name, customer_phone, shipping_address,
		                     channel, note, subtotal_satang, shipping_fee_satang, total_satang, created_by,
		                     tracking_token_hash)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		 RETURNING `+orderColumns,
		o.TenantID, o.OrderNumber, o.CustomerName, o.CustomerPhone, o.ShippingAddress,
		o.Channel, o.Note, o.SubtotalSatang, o.ShippingFeeSatang, o.TotalSatang, o.CreatedBy,
		trackingTokenHash,
	))
	if err != nil {
		return fmt.Errorf("order: insert: %w", err)
	}
	*o = saved
	return nil
}

func (Repository) InsertItem(ctx context.Context, db database.DBTX, tenantID, orderID uuid.UUID, it *Item) error {
	err := db.QueryRow(ctx,
		`INSERT INTO order_items (tenant_id, order_id, product_id, product_name, sku,
		                          unit_price_satang, quantity, line_total_satang)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 RETURNING id`,
		tenantID, orderID, it.ProductID, it.ProductName, it.SKU, it.UnitPriceSatang, it.Quantity, it.LineTotalSatang,
	).Scan(&it.ID)
	if err != nil {
		return fmt.Errorf("order: insert item: %w", err)
	}
	return nil
}

// Get loads one order. With forUpdate, the row stays locked until the transaction ends,
// so two requests cannot change the same order's status at the same time.
func (Repository) Get(ctx context.Context, db database.DBTX, tenantID, id uuid.UUID, forUpdate bool) (Order, error) {
	sql := `SELECT ` + orderColumns + ` FROM orders WHERE tenant_id = $1 AND id = $2`
	if forUpdate {
		sql += ` FOR UPDATE`
	}
	o, err := scanOrder(db.QueryRow(ctx, sql, tenantID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Order{}, ErrNotFound
	}
	if err != nil {
		return Order{}, fmt.Errorf("order: get: %w", err)
	}
	return o, nil
}

// GetByTrackingToken finds an order by the hash of its public tracking token.
func (Repository) GetByTrackingToken(ctx context.Context, db database.DBTX, tenantID uuid.UUID, tokenHash []byte) (Order, error) {
	o, err := scanOrder(db.QueryRow(ctx,
		`SELECT `+orderColumns+` FROM orders WHERE tenant_id = $1 AND tracking_token_hash = $2`,
		tenantID, tokenHash,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return Order{}, ErrNotFound
	}
	if err != nil {
		return Order{}, fmt.Errorf("order: get by tracking token: %w", err)
	}
	return o, nil
}

// Items returns the order's lines ordered by product id (the same order used for locking).
func (Repository) Items(ctx context.Context, db database.DBTX, tenantID, orderID uuid.UUID) ([]Item, error) {
	rows, err := db.Query(ctx,
		`SELECT id, product_id, product_name, sku, unit_price_satang, quantity, line_total_satang
		   FROM order_items
		  WHERE tenant_id = $1 AND order_id = $2
		  ORDER BY product_id`,
		tenantID, orderID,
	)
	if err != nil {
		return nil, fmt.Errorf("order: items: %w", err)
	}
	defer rows.Close()

	items := []Item{}
	for rows.Next() {
		var it Item
		if err := rows.Scan(&it.ID, &it.ProductID, &it.ProductName, &it.SKU,
			&it.UnitPriceSatang, &it.Quantity, &it.LineTotalSatang); err != nil {
			return nil, fmt.Errorf("order: scan item: %w", err)
		}
		items = append(items, it)
	}
	return items, rows.Err()
}

// List returns orders newest first with keyset pagination (same technique as products).
func (Repository) List(ctx context.Context, db database.DBTX, tenantID uuid.UUID, p ListParams, orderNumber *int64) ([]Order, error) {
	var pattern *string
	if p.Query != "" {
		q := database.ContainsPattern(p.Query)
		pattern = &q
	}
	rows, err := db.Query(ctx,
		`SELECT `+orderColumns+`
		   FROM orders
		  WHERE tenant_id = $1
		    AND ($2::text IS NULL OR status = $2)
		    AND ($3::text IS NULL OR customer_name ILIKE $3 OR customer_phone ILIKE $3 OR order_number = $4)
		    AND ($5::uuid IS NULL OR id < $5)
		  ORDER BY id DESC
		  LIMIT $6`,
		tenantID, p.Status, pattern, orderNumber, p.After, p.Limit,
	)
	if err != nil {
		return nil, fmt.Errorf("order: list: %w", err)
	}
	defer rows.Close()

	orders := []Order{}
	for rows.Next() {
		o, err := scanOrder(rows)
		if err != nil {
			return nil, fmt.Errorf("order: scan: %w", err)
		}
		orders = append(orders, o)
	}
	return orders, rows.Err()
}

// SetStatus moves the order to a new status and stamps the matching *_at column.
// reason must be set when to is cancelled, and nil otherwise (a CHECK constraint enforces it).
// The caller must have checked the transition (Status.CanBecome) under a row lock.
func (Repository) SetStatus(ctx context.Context, db database.DBTX, tenantID, id uuid.UUID, to Status, reason *CancelReason) (Order, error) {
	o, err := scanOrder(db.QueryRow(ctx,
		`UPDATE orders SET
		     status        = $3,
		     cancel_reason = $4,
		     paid_at      = CASE WHEN $3 = 'paid'      THEN now() ELSE paid_at      END,
		     shipped_at   = CASE WHEN $3 = 'shipped'   THEN now() ELSE shipped_at   END,
		     completed_at = CASE WHEN $3 = 'completed' THEN now() ELSE completed_at END,
		     cancelled_at = CASE WHEN $3 = 'cancelled' THEN now() ELSE cancelled_at END
		  WHERE tenant_id = $1 AND id = $2
		  RETURNING `+orderColumns,
		tenantID, id, to, reason,
	))
	if err != nil {
		return Order{}, fmt.Errorf("order: set status: %w", err)
	}
	return o, nil
}

// Ref points at one order in one shop.
type Ref struct {
	TenantID uuid.UUID
	ID       uuid.UUID
}

// ExpiredStorefront lists pending storefront orders created before cutoff, in ALL shops, oldest
// first. It calls the SECURITY DEFINER function from migration 000010, the only query that may
// look across shops; it returns ids only, and db needs no tenant scope.
func (Repository) ExpiredStorefront(ctx context.Context, db database.DBTX, cutoff time.Time, limit int) ([]Ref, error) {
	rows, err := db.Query(ctx, `SELECT tenant_id, order_id FROM expired_storefront_orders($1, $2)`, cutoff, limit)
	if err != nil {
		return nil, fmt.Errorf("order: expired storefront orders: %w", err)
	}
	refs, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Ref, error) {
		var r Ref
		err := row.Scan(&r.TenantID, &r.ID)
		return r, err
	})
	if err != nil {
		return nil, fmt.Errorf("order: scan expired: %w", err)
	}
	return refs, nil
}
