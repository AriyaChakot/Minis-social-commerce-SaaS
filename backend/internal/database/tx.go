package database

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Scope tells Postgres who is acting, so the RLS policies can filter rows.
// uuid.Nil means "not set": a zero Scope sees no tenant-owned rows at all.
type Scope struct {
	TenantID uuid.UUID // the shop being worked on
	UserID   uuid.UUID // the logged-in user
}

// InTx runs fn inside a transaction whose RLS context is set from scope.
// It commits when fn returns nil and rolls back on any error or panic.
//
// Always use InTx (never the pool directly) for tenant-owned tables: the settings are
// transaction-local, so they can never leak to the next request that reuses the connection.
func InTx(ctx context.Context, pool *pgxpool.Pool, scope Scope, fn func(tx pgx.Tx) error) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	// Rollback after a successful Commit is a no-op, so this is safe to defer unconditionally.
	defer func() { _ = tx.Rollback(context.Background()) }()

	// Both values are always set; '' means "not set" (the policies map '' to NULL).
	// The third argument true makes the settings local to this transaction.
	_, err = tx.Exec(ctx,
		`SELECT set_config('app.tenant_id', $1, true), set_config('app.user_id', $2, true)`,
		idOrEmpty(scope.TenantID), idOrEmpty(scope.UserID),
	)
	if err != nil {
		return fmt.Errorf("set rls scope: %w", err)
	}

	if err := fn(tx); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

func idOrEmpty(id uuid.UUID) string {
	if id == uuid.Nil {
		return ""
	}
	return id.String()
}
