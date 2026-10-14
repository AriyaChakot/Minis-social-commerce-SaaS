package database

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mscs/internal/testutil/dbtest"
)

// Integration test: it needs a migrated Postgres and DATABASE_URL pointing at the
// restricted app role (mscs_app), because a superuser would bypass RLS.
// Without DATABASE_URL the test is skipped, so `go test ./...` still passes anywhere.
func TestInTx_RLS(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.Pool(t)

	shopA, shopB, user := seed(t, ctx, pool)

	countMembers := func(scope Scope) int {
		t.Helper()
		var n int
		err := InTx(ctx, pool, scope, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT count(*) FROM tenant_members`).Scan(&n)
		})
		if err != nil {
			t.Fatalf("count members: %v", err)
		}
		return n
	}

	addMember := func(scope Scope, tenantID uuid.UUID) error {
		return InTx(ctx, pool, scope, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx,
				`INSERT INTO tenant_members (tenant_id, user_id) VALUES ($1, $2)`, tenantID, user)
			return err
		})
	}

	t.Run("insert into own tenant succeeds", func(t *testing.T) {
		if err := addMember(Scope{TenantID: shopA}, shopA); err != nil {
			t.Fatalf("want success, got %v", err)
		}
	})

	t.Run("insert into another tenant is blocked", func(t *testing.T) {
		if err := addMember(Scope{TenantID: shopA}, shopB); err == nil {
			t.Fatal("want RLS error, got nil")
		}
	})

	t.Run("empty scope sees nothing", func(t *testing.T) {
		if n := countMembers(Scope{}); n != 0 {
			t.Fatalf("want 0 rows, got %d", n)
		}
	})

	t.Run("other tenant sees nothing", func(t *testing.T) {
		if n := countMembers(Scope{TenantID: shopB}); n != 0 {
			t.Fatalf("want 0 rows, got %d", n)
		}
	})

	t.Run("user scope sees own memberships", func(t *testing.T) {
		if n := countMembers(Scope{UserID: user}); n != 1 {
			t.Fatalf("want 1 row, got %d", n)
		}
	})

	t.Run("error from fn rolls back", func(t *testing.T) {
		errBoom := errors.New("boom")
		err := InTx(ctx, pool, Scope{TenantID: shopB}, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx,
				`INSERT INTO tenant_members (tenant_id, user_id) VALUES ($1, $2)`, shopB, user); err != nil {
				return err
			}
			return errBoom
		})
		if !errors.Is(err, errBoom) {
			t.Fatalf("want errBoom, got %v", err)
		}
		if n := countMembers(Scope{TenantID: shopB}); n != 0 {
			t.Fatalf("want rollback (0 rows), got %d", n)
		}
	})
}

// seed creates two tenants and one user with unique values, and removes them when the test ends.
func seed(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (shopA, shopB, user uuid.UUID) {
	t.Helper()
	suffix := dbtest.Suffix()

	// tenants and users have no RLS, so no scope is needed here.
	err := InTx(ctx, pool, Scope{}, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx,
			`INSERT INTO tenants (slug, name) VALUES ($1, 'Test A') RETURNING id`, "test-a-"+suffix).Scan(&shopA); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx,
			`INSERT INTO tenants (slug, name) VALUES ($1, 'Test B') RETURNING id`, "test-b-"+suffix).Scan(&shopB); err != nil {
			return err
		}
		return tx.QueryRow(ctx,
			`INSERT INTO users (email, password_hash, display_name) VALUES ($1, 'x', 'Test') RETURNING id`,
			"test-"+suffix+"@example.com").Scan(&user)
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	t.Cleanup(func() {
		// ON DELETE CASCADE removes the memberships too.
		_ = InTx(ctx, pool, Scope{}, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `DELETE FROM tenants WHERE id IN ($1, $2)`, shopA, shopB)
			if err != nil {
				return err
			}
			_, err = tx.Exec(ctx, `DELETE FROM users WHERE id = $1`, user)
			return err
		})
	})
	return shopA, shopB, user
}
