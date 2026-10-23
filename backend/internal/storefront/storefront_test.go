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

func TestStorefrontShipping(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.Pool(t)
	products := product.NewService(pool)
	orders := order.NewService(pool)
	svc := storefront.NewService(pool, products, orders, 24*time.Hour)

	shop := newShop(t, ctx, pool)
	free := int64(60000)
	if _, err := (tenant.Repository{}).UpdateShipping(ctx, pool, shop.ID, tenant.ShippingRates{FeeSatang: 4000, FreeFromSatang: &free}); err != nil {
		t.Fatalf("set shipping: %v", err)
	}
	tee, err := products.Create(ctx, shop.ID, product.CreateInput{Name: "Tee", PriceSatang: 25000, Stock: 100})
	if err != nil {
		t.Fatalf("create product: %v", err)
	}
	bag, _ := products.Create(ctx, shop.ID, product.CreateInput{Name: "Bag", PriceSatang: 30000, Stock: 100})

	guest := func(expected *int64, items ...order.ItemInput) storefront.PlaceOrderInput {
		return storefront.PlaceOrderInput{CustomerName: "Guest", CustomerPhone: "0810000000",
			ShippingAddress: "Bangkok", Items: items, ExpectedTotalSatang: expected}
	}
	total := func(v int64) *int64 { return &v }

	t.Run("the cart can show the rates", func(t *testing.T) {
		got, err := svc.Shop(ctx, shop.Slug)
		if err != nil || got.Shipping.FeeSatang != 4000 || got.Shipping.FreeFromSatang == nil || *got.Shipping.FreeFromSatang != free {
			t.Fatalf("want the shop's rates, got %+v err=%v", got.Shipping, err)
		}
	})

	for _, c := range []struct {
		name          string
		items         []order.ItemInput
		fee, wantTotl int64
	}{
		{"below the threshold pays the fee", []order.ItemInput{{ProductID: tee.ID, Quantity: 1}}, 4000, 29000},
		{"exactly the threshold ships free", []order.ItemInput{{ProductID: bag.ID, Quantity: 2}}, 0, 60000},
		{"above the threshold ships free", []order.ItemInput{{ProductID: tee.ID, Quantity: 3}}, 0, 75000},
	} {
		t.Run(c.name, func(t *testing.T) {
			o, _, err := svc.PlaceOrder(ctx, shop.Slug, guest(total(c.wantTotl), c.items...))
			if err != nil || o.ShippingFeeSatang != c.fee || o.TotalSatang != c.wantTotl {
				t.Fatalf("want fee %d total %d, got %+v err=%v", c.fee, c.wantTotl, o, err)
			}
		})
	}

	t.Run("a total the customer did not see is refused, and takes no stock", func(t *testing.T) {
		before, _ := products.Get(ctx, shop.ID, tee.ID)
		// The cart showed free shipping (25000), but the shop charges 4000 below 60000.
		_, _, err := svc.PlaceOrder(ctx, shop.Slug, guest(total(25000), order.ItemInput{ProductID: tee.ID, Quantity: 1}))
		var changed *order.TotalChangedError
		if !errors.As(err, &changed) || changed.ShippingFeeSatang != 4000 || changed.TotalSatang != 29000 {
			t.Fatalf("want TotalChangedError with the new amounts, got %v", err)
		}
		after, _ := products.Get(ctx, shop.ID, tee.ID)
		if after.Stock != before.Stock {
			t.Fatalf("stock changed from %d to %d by a refused order", before.Stock, after.Stock)
		}
	})

	t.Run("without an expected total the server's amount is used", func(t *testing.T) {
		o, _, err := svc.PlaceOrder(ctx, shop.Slug, guest(nil, order.ItemInput{ProductID: tee.ID, Quantity: 1}))
		if err != nil || o.TotalSatang != 29000 {
			t.Fatalf("want total 29000, got %+v err=%v", o, err)
		}
	})

	t.Run("the database refuses rates out of range", func(t *testing.T) {
		if _, err := pool.Exec(ctx, `UPDATE tenants SET shipping_fee_satang = -1 WHERE id = $1`, shop.ID); err == nil {
			t.Fatal("negative fee was accepted")
		}
		if _, err := pool.Exec(ctx, `UPDATE tenants SET free_shipping_from_satang = 0 WHERE id = $1`, shop.ID); err == nil {
			t.Fatal("zero threshold was accepted")
		}
	})
}
