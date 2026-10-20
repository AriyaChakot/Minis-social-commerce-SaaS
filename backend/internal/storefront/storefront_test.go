package storefront_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mscs/internal/database"
	"mscs/internal/order"
	"mscs/internal/product"
	"mscs/internal/storefront"
	"mscs/internal/tenant"
	"mscs/internal/testutil/dbtest"
)

func newShop(t *testing.T, ctx context.Context, pool *pgxpool.Pool) tenant.Tenant {
	t.Helper()
	shop, err := tenant.Repository{}.Create(ctx, pool, "store-test-"+dbtest.Suffix(), "Store Test")
	if err != nil {
		t.Fatalf("create shop: %v", err)
	}
	t.Cleanup(func() {
		ctx := context.Background()
		_ = database.InTx(ctx, pool, database.Scope{TenantID: shop.ID}, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `DELETE FROM orders WHERE tenant_id = $1`, shop.ID)
			return err
		})
		_, _ = pool.Exec(ctx, `DELETE FROM tenants WHERE id = $1`, shop.ID)
	})
	return shop
}

func TestStorefront(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.Pool(t)
	products := product.NewService(pool)
	orders := order.NewService(pool)
	svc := storefront.NewService(pool, products, orders, 24*time.Hour)

	shop := newShop(t, ctx, pool)
	other := newShop(t, ctx, pool)

	tee, err := products.Create(ctx, shop.ID, product.CreateInput{Name: "Tee", PriceSatang: 25000, Stock: 5})
	if err != nil {
		t.Fatalf("create product: %v", err)
	}
	hidden, _ := products.Create(ctx, shop.ID, product.CreateInput{Name: "Old", PriceSatang: 100, Stock: 5})
	if err := products.Archive(ctx, shop.ID, hidden.ID); err != nil {
		t.Fatalf("archive: %v", err)
	}

	t.Run("only active products are listed", func(t *testing.T) {
		list, err := svc.Products(ctx, shop.Slug)
		if err != nil || len(list) != 1 || list[0].ID != tee.ID {
			t.Fatalf("want only the active product, got %+v err=%v", list, err)
		}
	})

	t.Run("unknown slug is not found", func(t *testing.T) {
		if _, err := svc.Shop(ctx, "no-such-shop-"+dbtest.Suffix()); !errors.Is(err, storefront.ErrShopNotFound) {
			t.Fatalf("want ErrShopNotFound, got %v", err)
		}
	})

	in := storefront.PlaceOrderInput{
		CustomerName: "Guest", CustomerPhone: "0810000000", ShippingAddress: "Bangkok",
		Items: []order.ItemInput{{ProductID: tee.ID, Quantity: 2}},
	}
	placed, token, err := svc.PlaceOrder(ctx, shop.Slug, in)
	if err != nil {
		t.Fatalf("place order: %v", err)
	}

	t.Run("guest order is a storefront order with no creator and free shipping", func(t *testing.T) {
		page, err := orders.List(ctx, shop.ID, order.ListParams{})
		if err != nil || len(page.Items) != 1 {
			t.Fatalf("list: %v (%d orders)", err, len(page.Items))
		}
		o := page.Items[0]
		if o.Channel != order.ChannelStorefront || o.CreatedBy != nil || o.ShippingFeeSatang != 0 || o.TotalSatang != 50000 {
			t.Fatalf("unexpected order: %+v", o)
		}
		if placed.OrderNumber != o.OrderNumber || len(token) != 26 {
			t.Fatalf("want order #%d and a 26-char token, got #%d %q", o.OrderNumber, placed.OrderNumber, token)
		}
	})

	t.Run("the token opens the order", func(t *testing.T) {
		got, err := svc.TrackOrder(ctx, shop.Slug, token)
		if err != nil || got.OrderNumber != placed.OrderNumber || len(got.Items) != 1 {
			t.Fatalf("track: got %+v err=%v", got, err)
		}
		// Pending: the customer sees when they must pay by.
		if got.PayBy == nil || !got.PayBy.Equal(got.CreatedAt.Add(24*time.Hour)) || got.CancelReason != nil {
			t.Fatalf("want pay_by = created_at + 24h and no cancel reason, got %v / %v", got.PayBy, got.CancelReason)
		}
	})

	t.Run("a wrong token or another shop's slug finds nothing", func(t *testing.T) {
		if _, err := svc.TrackOrder(ctx, shop.Slug, "WRONGTOKENWRONGTOKENWRONG1"); !errors.Is(err, order.ErrNotFound) {
			t.Fatalf("wrong token: want ErrNotFound, got %v", err)
		}
		if _, err := svc.TrackOrder(ctx, other.Slug, token); !errors.Is(err, order.ErrNotFound) {
			t.Fatalf("other shop: want ErrNotFound, got %v", err)
		}
	})

	t.Run("phone and address are required for guests", func(t *testing.T) {
		noPhone := in
		noPhone.CustomerPhone = " "
		var verr *order.ValidationError
		if _, _, err := svc.PlaceOrder(ctx, shop.Slug, noPhone); !errors.As(err, &verr) || verr.Field != "customer_phone" {
			t.Fatalf("want ValidationError on customer_phone, got %v", err)
		}
	})

	t.Run("archived products cannot be ordered", func(t *testing.T) {
		bad := in
		bad.Items = []order.ItemInput{{ProductID: hidden.ID, Quantity: 1}}
		var unavail *order.ProductUnavailableError
		if _, _, err := svc.PlaceOrder(ctx, shop.Slug, bad); !errors.As(err, &unavail) {
			t.Fatalf("want ProductUnavailableError, got %v", err)
		}
	})

	t.Run("suspended shops disappear from the storefront", func(t *testing.T) {
		if _, err := pool.Exec(ctx, `UPDATE tenants SET status = 'suspended', suspended_at = now(), suspended_reason = 'test' WHERE id = $1`, other.ID); err != nil {
			t.Fatalf("suspend: %v", err)
		}
		if _, err := svc.Products(ctx, other.Slug); !errors.Is(err, storefront.ErrShopNotFound) {
			t.Fatalf("want ErrShopNotFound, got %v", err)
		}
	})
}
