package auth_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"mscs/internal/auth"
	"mscs/internal/tenant"
	"mscs/internal/testutil/dbtest"
	"mscs/internal/user"
)

func TestService(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.Pool(t)
	svc := auth.NewService(pool)

	sfx := dbtest.Suffix()
	in := auth.RegisterInput{
		Email:       "Owner-" + sfx + "@Example.com",
		Password:    "s3cret-password",
		DisplayName: "Owner",
		ShopName:    "Shop " + sfx,
		ShopSlug:    "shop-" + sfx,
	}

	reg, err := svc.Register(ctx, in)
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	cleanup(t, pool, reg.User.Email, reg.Tenant.Slug)

	t.Run("register makes the user owner of the new shop", func(t *testing.T) {
		_, role, err := svc.TenantAccess(ctx, reg.Tenant.ID, reg.User.ID)
		if err != nil || role != tenant.RoleOwner {
			t.Fatalf("want owner, got role=%q err=%v", role, err)
		}
	})

	t.Run("register is all-or-nothing", func(t *testing.T) {
		// New email but an already-taken slug: the user insert succeeds, the tenant insert fails.
		dup := in
		dup.Email = "rollback-" + sfx + "@example.com"
		_, err := svc.Register(ctx, dup)
		if !errors.Is(err, tenant.ErrSlugTaken) {
			t.Fatalf("want ErrSlugTaken, got %v", err)
		}
		// The user created earlier in that transaction must have been rolled back.
		if _, err := (user.Repository{}).GetByEmail(ctx, pool, dup.Email); !errors.Is(err, user.ErrNotFound) {
			t.Fatalf("want user rolled back (ErrNotFound), got %v", err)
		}
	})

	t.Run("register rejects a bad slug", func(t *testing.T) {
		bad := in
		bad.Email = "badslug-" + sfx + "@example.com"
		bad.ShopSlug = "Bad Slug!"
		var verr *auth.ValidationError
		if _, err := svc.Register(ctx, bad); !errors.As(err, &verr) || verr.Field != "shop_slug" {
			t.Fatalf("want ValidationError on shop_slug, got %v", err)
		}
	})

	t.Run("session token authenticates the user", func(t *testing.T) {
		u, err := svc.Authenticate(ctx, reg.Session.Token)
		if err != nil || u.ID != reg.User.ID {
			t.Fatalf("want user %s, got %s err=%v", reg.User.ID, u.ID, err)
		}
	})

	t.Run("login with wrong password or unknown email gives the same error", func(t *testing.T) {
		if _, _, err := svc.Login(ctx, in.Email, "wrong-password"); !errors.Is(err, auth.ErrInvalidCredentials) {
			t.Fatalf("wrong password: want ErrInvalidCredentials, got %v", err)
		}
		if _, _, err := svc.Login(ctx, "nobody-"+sfx+"@example.com", "whatever"); !errors.Is(err, auth.ErrInvalidCredentials) {
			t.Fatalf("unknown email: want ErrInvalidCredentials, got %v", err)
		}
	})

	t.Run("login then logout", func(t *testing.T) {
		_, sess, err := svc.Login(ctx, in.Email, in.Password)
		if err != nil {
			t.Fatalf("login: %v", err)
		}
		if err := svc.Logout(ctx, sess.Token); err != nil {
			t.Fatalf("logout: %v", err)
		}
		if _, err := svc.Authenticate(ctx, sess.Token); !errors.Is(err, auth.ErrUnauthenticated) {
			t.Fatalf("after logout: want ErrUnauthenticated, got %v", err)
		}
	})

	t.Run("memberships list the user's shops", func(t *testing.T) {
		ms, err := svc.Memberships(ctx, reg.User.ID)
		if err != nil || len(ms) != 1 || ms[0].Tenant.ID != reg.Tenant.ID {
			t.Fatalf("want 1 membership of %s, got %+v err=%v", reg.Tenant.ID, ms, err)
		}
	})

	t.Run("other users cannot access the shop", func(t *testing.T) {
		other, err := svc.Register(ctx, auth.RegisterInput{
			Email: "other-" + sfx + "@example.com", Password: "s3cret-password",
			DisplayName: "Other", ShopName: "Other", ShopSlug: "other-" + sfx,
		})
		if err != nil {
			t.Fatalf("register other: %v", err)
		}
		cleanup(t, pool, other.User.Email, other.Tenant.Slug)

		if _, _, err := svc.TenantAccess(ctx, reg.Tenant.ID, other.User.ID); !errors.Is(err, tenant.ErrNotMember) {
			t.Fatalf("want ErrNotMember, got %v", err)
		}
	})
}

// cleanup deletes the test user and shop; sessions and memberships go with them (ON DELETE CASCADE).
func cleanup(t *testing.T, pool *pgxpool.Pool, email, slug string) {
	t.Helper()
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = pool.Exec(ctx, `DELETE FROM tenants WHERE slug = $1`, slug)
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE email = $1`, email)
	})
}
