package tenant_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"

	"mscs/internal/database"
	"mscs/internal/tenant"
	"mscs/internal/testutil/dbtest"
	"mscs/internal/user"
)

func TestRepository(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.Pool(t)
	repo := tenant.Repository{}
	slug := "repo-test-" + dbtest.Suffix()

	created, err := repo.Create(ctx, pool, slug, "Repo Test")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM tenants WHERE id = $1`, created.ID)
	})

	t.Run("create fills defaults", func(t *testing.T) {
		if created.Status != "active" || created.Slug != slug {
			t.Fatalf("unexpected tenant: %+v", created)
		}
	})

	t.Run("duplicate slug returns ErrSlugTaken", func(t *testing.T) {
		_, err := repo.Create(ctx, pool, slug, "Another")
		if !errors.Is(err, tenant.ErrSlugTaken) {
			t.Fatalf("want ErrSlugTaken, got %v", err)
		}
	})

	t.Run("get by slug", func(t *testing.T) {
		got, err := repo.GetBySlug(ctx, pool, slug)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if got.ID != created.ID {
			t.Fatalf("want id %s, got %s", created.ID, got.ID)
		}
	})

	t.Run("unknown slug returns ErrNotFound", func(t *testing.T) {
		_, err := repo.GetBySlug(ctx, pool, "no-such-shop-"+dbtest.Suffix())
		if !errors.Is(err, tenant.ErrNotFound) {
			t.Fatalf("want ErrNotFound, got %v", err)
		}
	})

	t.Run("add member respects RLS", func(t *testing.T) {
		u, err := user.Repository{}.Create(ctx, pool, "member-"+dbtest.Suffix()+"@example.com", "x", "Member")
		if err != nil {
			t.Fatalf("create user: %v", err)
		}
		t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, u.ID) })

		// Without a tenant scope, RLS rejects the insert.
		if err := repo.AddMember(ctx, pool, created.ID, u.ID, tenant.RoleOwner); err == nil {
			t.Fatal("want RLS error without scope, got nil")
		}

		scope := database.Scope{TenantID: created.ID}
		add := func(tx pgx.Tx) error { return repo.AddMember(ctx, tx, created.ID, u.ID, tenant.RoleOwner) }

		if err := database.InTx(ctx, pool, scope, add); err != nil {
			t.Fatalf("add member in scope: %v", err)
		}
		if err := database.InTx(ctx, pool, scope, add); !errors.Is(err, tenant.ErrAlreadyMember) {
			t.Fatalf("want ErrAlreadyMember, got %v", err)
		}
	})
}
