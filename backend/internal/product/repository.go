package product

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mscs/internal/database"
)

// Repository reads and writes products. products has RLS, so db must be a transaction
// scoped to the tenant (database.InTx).
//
// Every query ALSO filters on tenant_id explicitly. RLS is the safety net, not the main rule:
// the explicit filter documents intent, works even for a superuser, and lets the planner
// use the (tenant_id, id) index.
type Repository struct{}

const productColumns = `id, tenant_id, sku, name, description, price_satang, stock, status, created_at, updated_at`

func scanProduct(row pgx.Row) (Product, error) {
	var p Product
	err := row.Scan(&p.ID, &p.TenantID, &p.SKU, &p.Name, &p.Description,
		&p.PriceSatang, &p.Stock, &p.Status, &p.CreatedAt, &p.UpdatedAt)
	return p, err
}

// mapWriteError converts constraint violations into domain errors.
func mapWriteError(err error, op string) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, pgx.ErrNoRows):
		return ErrNotFound
	case database.IsUniqueViolation(err, "products_tenant_sku_key"):
		return ErrSKUTaken
	default:
		return fmt.Errorf("product: %s: %w", op, err)
	}
}

func (Repository) Create(ctx context.Context, db database.DBTX, tenantID uuid.UUID, in CreateInput) (Product, error) {
	p, err := scanProduct(db.QueryRow(ctx,
		`INSERT INTO products (tenant_id, sku, name, description, price_satang, stock)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING `+productColumns,
		tenantID, in.SKU, in.Name, in.Description, in.PriceSatang, in.Stock,
	))
	return p, mapWriteError(err, "create")
}

func (Repository) Get(ctx context.Context, db database.DBTX, tenantID, id uuid.UUID) (Product, error) {
	p, err := scanProduct(db.QueryRow(ctx,
		`SELECT `+productColumns+` FROM products WHERE tenant_id = $1 AND id = $2`,
		tenantID, id,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return Product{}, ErrNotFound
	}
	if err != nil {
		return Product{}, fmt.Errorf("product: get: %w", err)
	}
	return p, nil
}

// List returns up to p.Limit products, newest first, using keyset pagination:
// "WHERE id < cursor" instead of OFFSET, so page 1000 is as fast as page 1.
// (Ordering by id works because uuidv7 ids grow with creation time.)
func (Repository) List(ctx context.Context, db database.DBTX, tenantID uuid.UUID, p ListParams) ([]Product, error) {
	// One fixed query: each optional filter is switched off by passing NULL.
	// Arguments are always bound ($n), never concatenated into the SQL string.
	var query *string
	if p.Query != "" {
		q := database.ContainsPattern(p.Query)
		query = &q
	}
	rows, err := db.Query(ctx,
		`SELECT `+productColumns+`
		   FROM products
		  WHERE tenant_id = $1
		    AND ($2::text IS NULL OR status = $2)
		    AND ($3::text IS NULL OR name ILIKE $3)
		    AND ($4::uuid IS NULL OR id < $4)
		  ORDER BY id DESC
		  LIMIT $5`,
		tenantID, p.Status, query, p.After, p.Limit,
	)
	if err != nil {
		return nil, fmt.Errorf("product: list: %w", err)
	}
	defer rows.Close()

	items := []Product{}
	for rows.Next() {
		prod, err := scanProduct(rows)
		if err != nil {
			return nil, fmt.Errorf("product: scan: %w", err)
		}
		items = append(items, prod)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("product: list: %w", err)
	}
	return items, nil
}

// LockForOrder loads the given products and row-locks them (SELECT ... FOR UPDATE) until the
// transaction ends, so no other transaction can change their stock in the meantime.
// Rows are locked in id order: two orders that share products then always lock in the same
// order, which prevents deadlocks.
func (Repository) LockForOrder(ctx context.Context, db database.DBTX, tenantID uuid.UUID, ids []uuid.UUID) ([]Product, error) {
	rows, err := db.Query(ctx,
		`SELECT `+productColumns+`
		   FROM products
		  WHERE tenant_id = $1 AND id = ANY($2)
		  ORDER BY id
		  FOR UPDATE`,
		tenantID, ids,
	)
	if err != nil {
		return nil, fmt.Errorf("product: lock: %w", err)
	}
	defer rows.Close()

	var items []Product
	for rows.Next() {
		p, err := scanProduct(rows)
		if err != nil {
			return nil, fmt.Errorf("product: scan: %w", err)
		}
		items = append(items, p)
	}
	return items, rows.Err()
}

// AdjustStock adds delta (negative to take stock out). The products_stock_non_negative CHECK
// is the last line of defense: even a buggy caller cannot push stock below zero.
func (Repository) AdjustStock(ctx context.Context, db database.DBTX, tenantID, id uuid.UUID, delta int) error {
	tag, err := db.Exec(ctx,
		`UPDATE products SET stock = stock + $3 WHERE tenant_id = $1 AND id = $2`,
		tenantID, id, delta,
	)
	if err != nil {
		return fmt.Errorf("product: adjust stock: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return ErrNotFound
	}
	return nil
}

// Update applies a partial update. COALESCE($n, column) keeps the old value when the argument
// is NULL, i.e. when the Go pointer is nil.
func (Repository) Update(ctx context.Context, db database.DBTX, tenantID, id uuid.UUID, in UpdateInput) (Product, error) {
	p, err := scanProduct(db.QueryRow(ctx,
		`UPDATE products SET
		     sku          = COALESCE($3, sku),
		     name         = COALESCE($4, name),
		     description  = COALESCE($5, description),
		     price_satang = COALESCE($6, price_satang),
		     stock        = COALESCE($7, stock),
		     status       = COALESCE($8, status)
		  WHERE tenant_id = $1 AND id = $2
		  RETURNING `+productColumns,
		tenantID, id, in.SKU, in.Name, in.Description, in.PriceSatang, in.Stock, in.Status,
	))
	return p, mapWriteError(err, "update")
}
