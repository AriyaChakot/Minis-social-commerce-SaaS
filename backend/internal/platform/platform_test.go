package platform_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"mscs/internal/auth"
	"mscs/internal/platform"
	"mscs/internal/testutil/dbtest"
)

// newShop registers a merchant (user + shop + owner membership) and, as the database owner,
// gives the shop one product and three orders: paid 1,000 satang, cancelled 500, pending 300.
// Everything is deleted when the test ends.
func newShop(t *testing.T, ctx context.Context, pool, owner *pgxpool.Pool) auth.RegisterResult {
	t.Helper()
	suffix := dbtest.Suffix()
	res, err := auth.NewService(pool).Register(ctx, auth.RegisterInput{
		Email: "platform-" + suffix + "@example.com", Password: "password-123",
		DisplayName: "Owner", ShopName: "Platform Test " + suffix, ShopSlug: "platform-test-" + suffix,
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	t.Cleanup(func() {
		bg := context.Background()
		_, _ = owner.Exec(bg, `DELETE FROM platform_audit_log WHERE tenant_id = $1 OR actor_id = $2`, res.Tenant.ID, res.User.ID)
		_, _ = owner.Exec(bg, `DELETE FROM orders WHERE tenant_id = $1`, res.Tenant.ID)
		_, _ = owner.Exec(bg, `DELETE FROM tenants WHERE id = $1`, res.Tenant.ID)
		_, _ = owner.Exec(bg, `DELETE FROM users WHERE id = $1`, res.User.ID)
	})

	_, err = owner.Exec(ctx, `
		INSERT INTO products (tenant_id, name, price_satang, stock) VALUES ($1, 'Shirt', 1000, 5);
	`, res.Tenant.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = owner.Exec(ctx, `
		INSERT INTO orders (tenant_id, order_number, customer_name, status, cancel_reason, cancelled_at, subtotal_satang, total_satang, paid_at)
		VALUES ($1, 1, 'A', 'paid', NULL, NULL, 1000, 1000, now()),
		       ($1, 2, 'B', 'cancelled', 'merchant', now(), 500, 500, NULL),
		       ($1, 3, 'C', 'pending', NULL, NULL, 300, 300, NULL)`, res.Tenant.ID)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestPlatform(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.Pool(t)
	owner := dbtest.OwnerPool(t)
	svc := platform.NewService(pool)

	shop := newShop(t, ctx, pool, owner)
	admin := newShop(t, ctx, pool, owner).User // an account that will become a platform admin

	t.Run("the API role cannot make anyone a platform admin", func(t *testing.T) {
		_, err := pool.Exec(ctx, `INSERT INTO platform_admins (user_id) VALUES ($1)`, admin.ID)
		if err == nil {
			t.Fatal("mscs_app must not be allowed to insert into platform_admins")
		}
	})

	t.Run("only listed users are admins", func(t *testing.T) {
		if ok, err := svc.IsAdmin(ctx, admin.ID); err != nil || ok {
			t.Fatalf("before: want false, got %v (err %v)", ok, err)
		}
		if _, err := owner.Exec(ctx, `INSERT INTO platform_admins (user_id) VALUES ($1)`, admin.ID); err != nil {
			t.Fatal(err)
		}
		if ok, err := svc.IsAdmin(ctx, admin.ID); err != nil || !ok {
			t.Fatalf("after: want true, got %v (err %v)", ok, err)
		}
	})

	t.Run("the shop list has numbers from behind RLS", func(t *testing.T) {
		list, err := svc.Tenants(ctx, platform.TenantFilter{Query: shop.Tenant.Slug})
		if err != nil || len(list) != 1 {
			t.Fatalf("want 1 shop, got %d (err %v)", len(list), err)
		}
		got := list[0]
		if got.OwnerEmail != shop.User.Email || got.MemberCount != 1 || got.ProductCount != 1 ||
			got.OrderCount != 3 || got.RevenueSatang != 1000 || got.LastOrderAt == nil {
			t.Fatalf("got %+v", got)
		}
		if _, err := svc.Tenants(ctx, platform.TenantFilter{Status: "deleted"}); err == nil {
			t.Fatal("an unknown status filter must be refused")
		}
	})

	t.Run("overview: 30 days, today included, with today's orders", func(t *testing.T) {
		o, err := svc.Overview(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(o.Daily) != platform.OverviewDays || o.Tenants.Total < 2 || o.Users < 2 {
			t.Fatalf("got %d days, %d shops, %d users", len(o.Daily), o.Tenants.Total, o.Users)
		}
		today := time.Now().In(time.FixedZone("Asia/Bangkok", 7*3600)).Format(time.DateOnly)
		last := o.Daily[len(o.Daily)-1]
		// >=: the dev database may hold other orders from today.
		if last.Day != today || last.Orders < 6 || last.RevenueSatang < 2000 {
			t.Fatalf("today: got %+v, want day %s with at least our 6 orders", last, today)
		}
	})

	t.Run("suspend and reactivate, each recorded", func(t *testing.T) {
		id := shop.Tenant.ID
		var verr *platform.ValidationError
		if _, err := svc.Suspend(ctx, admin.ID, id, " x "); !errors.As(err, &verr) {
			t.Fatalf("short reason: want a validation error, got %v", err)
		}

		got, err := svc.Suspend(ctx, admin.ID, id, "  ขายสินค้าผิดกฎหมาย  ")
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != "suspended" || got.SuspendedReason != "ขายสินค้าผิดกฎหมาย" || got.SuspendedAt == nil {
			t.Fatalf("got %+v", got)
		}
		if _, err := svc.Suspend(ctx, admin.ID, id, "again please"); !errors.Is(err, platform.ErrAlreadySuspended) {
			t.Fatalf("second suspend: want ErrAlreadySuspended, got %v", err)
		}

		got, err = svc.Reactivate(ctx, admin.ID, id)
		if err != nil || got.Status != "active" || got.SuspendedReason != "" || got.SuspendedAt != nil {
			t.Fatalf("reactivate: got %+v (err %v)", got, err)
		}
		if _, err := svc.Reactivate(ctx, admin.ID, id); !errors.Is(err, platform.ErrNotSuspended) {
			t.Fatalf("second reactivate: want ErrNotSuspended, got %v", err)
		}
		if _, err := svc.Suspend(ctx, admin.ID, uuid.New(), "no such shop"); !errors.Is(err, platform.ErrTenantNotFound) {
			t.Fatalf("unknown shop: want ErrTenantNotFound, got %v", err)
		}

		log, err := svc.Audit(ctx)
		if err != nil || len(log) < 2 {
			t.Fatalf("want at least 2 audit entries, got %d (err %v)", len(log), err)
		}
		// Newest first: the reactivation, then the suspension (failed attempts are not recorded).
		if log[0].Action != "tenant.reactivate" || log[1].Action != "tenant.suspend" ||
			log[1].Reason != "ขายสินค้าผิดกฎหมาย" || log[1].ActorEmail != admin.Email || log[1].TenantID != id {
			t.Fatalf("got %+v / %+v", log[0], log[1])
		}
	})

	t.Run("the audit log cannot be rewritten by the API", func(t *testing.T) {
		if _, err := pool.Exec(ctx, `DELETE FROM platform_audit_log WHERE actor_id = $1`, admin.ID); err == nil {
			t.Fatal("mscs_app must not be allowed to delete audit entries")
		}
		if _, err := pool.Exec(ctx, `UPDATE platform_audit_log SET reason = 'x' WHERE actor_id = $1`, admin.ID); err == nil {
			t.Fatal("mscs_app must not be allowed to change audit entries")
		}
	})
}
