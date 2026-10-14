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

const tenantColumns = `id, slug, name, status, created_at, updated_at`

func scanTenant(row pgx.Row) (Tenant, error) {
	var t Tenant
	err := row.Scan(&t.ID, &t.Slug, &t.Name, &t.Status, &t.CreatedAt, &t.UpdatedAt)
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
