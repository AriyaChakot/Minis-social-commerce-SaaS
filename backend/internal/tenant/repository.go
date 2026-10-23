package tenant

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mscs/internal/database"
)

// Repository reads and writes tenants in Postgres.
// It holds no connection: every method takes a database.DBTX, so the caller chooses
// the pool or a transaction (and, through database.InTx, the RLS scope).
type Repository struct{}

const tenantColumns = `id, slug, name, status, suspended_reason, shipping_fee_satang,
	free_shipping_from_satang, created_at, updated_at`

func scanTenant(row pgx.Row) (Tenant, error) {
	var t Tenant
	err := row.Scan(&t.ID, &t.Slug, &t.Name, &t.Status, &t.SuspendedReason, &t.Shipping.FeeSatang,
		&t.Shipping.FreeFromSatang, &t.CreatedAt, &t.UpdatedAt)
	return t, err
}

// Create inserts a new active tenant.
func (Repository) Create(ctx context.Context, db database.DBTX, slug, name string) (Tenant, error) {
	t, err := scanTenant(db.QueryRow(ctx,
		`INSERT INTO tenants (slug, name) VALUES ($1, $2) RETURNING `+tenantColumns,
		slug, name,
	))
	if database.IsUniqueViolation(err, "tenants_slug_key") {
		return Tenant{}, ErrSlugTaken
	}
	if err != nil {
		return Tenant{}, fmt.Errorf("tenant: create: %w", err)
	}
	return t, nil
}

// GetBySlug finds a tenant by its URL slug.
func (Repository) GetBySlug(ctx context.Context, db database.DBTX, slug string) (Tenant, error) {
	t, err := scanTenant(db.QueryRow(ctx,
		`SELECT `+tenantColumns+` FROM tenants WHERE slug = $1`,
		slug,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return Tenant{}, ErrNotFound
	}
	if err != nil {
		return Tenant{}, fmt.Errorf("tenant: get by slug: %w", err)
	}
	return t, nil
}

// GetByID finds a tenant by its id.
func (Repository) GetByID(ctx context.Context, db database.DBTX, id uuid.UUID) (Tenant, error) {
	t, err := scanTenant(db.QueryRow(ctx,
		`SELECT `+tenantColumns+` FROM tenants WHERE id = $1`,
		id,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return Tenant{}, ErrNotFound
	}
	if err != nil {
		return Tenant{}, fmt.Errorf("tenant: get by id: %w", err)
	}
	return t, nil
}

// UpdateShipping saves the shop's shipping rates (already validated) and returns the shop.
func (Repository) UpdateShipping(ctx context.Context, db database.DBTX, id uuid.UUID, r ShippingRates) (Tenant, error) {
	t, err := scanTenant(db.QueryRow(ctx,
		`UPDATE tenants SET shipping_fee_satang = $2, free_shipping_from_satang = $3
		  WHERE id = $1 RETURNING `+tenantColumns,
		id, r.FeeSatang, r.FreeFromSatang,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return Tenant{}, ErrNotFound
	}
	if err != nil {
		return Tenant{}, fmt.Errorf("tenant: update shipping: %w", err)
	}
	return t, nil
}

// MemberRole returns the user's role in the tenant, or ErrNotMember.
// db must be a transaction scoped to tenantID (database.InTx), because tenant_members has RLS.
func (Repository) MemberRole(ctx context.Context, db database.DBTX, tenantID, userID uuid.UUID) (Role, error) {
	var role Role
	err := db.QueryRow(ctx,
		`SELECT role FROM tenant_members WHERE tenant_id = $1 AND user_id = $2`,
		tenantID, userID,
	).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotMember
	}
	if err != nil {
		return "", fmt.Errorf("tenant: member role: %w", err)
	}
	return role, nil
}

// ListMemberships returns every tenant the user belongs to, ordered by name.
// db must be a transaction scoped to userID (database.InTx): the member_self_read policy
// is what lets one user see their memberships across tenants.
func (Repository) ListMemberships(ctx context.Context, db database.DBTX, userID uuid.UUID) ([]Membership, error) {
	rows, err := db.Query(ctx,
		`SELECT t.id, t.slug, t.name, t.status, t.suspended_reason, t.shipping_fee_satang,
		        t.free_shipping_from_satang, t.created_at, t.updated_at, m.role
		   FROM tenant_members m
		   JOIN tenants t ON t.id = m.tenant_id
		  WHERE m.user_id = $1
		  ORDER BY t.name`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("tenant: list memberships: %w", err)
	}
	defer rows.Close()

	// Start with an empty (not nil) slice so it encodes as [] instead of null in JSON.
	memberships := []Membership{}
	for rows.Next() {
		var m Membership
		t := &m.Tenant
		if err := rows.Scan(&t.ID, &t.Slug, &t.Name, &t.Status, &t.SuspendedReason, &t.Shipping.FeeSatang,
			&t.Shipping.FreeFromSatang, &t.CreatedAt, &t.UpdatedAt, &m.Role); err != nil {
			return nil, fmt.Errorf("tenant: scan membership: %w", err)
		}
		memberships = append(memberships, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("tenant: list memberships: %w", err)
	}
	return memberships, nil
}

// AddMember links a user to a tenant with a role.
// tenant_members has RLS: db must be a transaction scoped to tenantID (database.InTx).
func (Repository) AddMember(ctx context.Context, db database.DBTX, tenantID, userID uuid.UUID, role Role) error {
	_, err := db.Exec(ctx,
		`INSERT INTO tenant_members (tenant_id, user_id, role) VALUES ($1, $2, $3)`,
		tenantID, userID, role,
	)
	if database.IsUniqueViolation(err, "tenant_members_pkey") {
		return ErrAlreadyMember
	}
	if err != nil {
		return fmt.Errorf("tenant: add member: %w", err)
	}
	return nil
}
