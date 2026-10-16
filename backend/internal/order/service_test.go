package order_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mscs/internal/database"
	"mscs/internal/order"
	"mscs/internal/product"
	"mscs/internal/tenant"
	"mscs/internal/testutil/dbtest"
)

type fixture struct {
	ctx      context.Context
	pool     *pgxpool.Pool
	orders   *order.Service
	products *product.Service
	shop     uuid.UUID
	user     uuid.UUID
}

func setup(t *testing.T) *fixture {
	t.Helper()
	ctx := context.Background()
	pool := dbtest.Pool(t)
	f := &fixture{ctx: ctx, pool: pool, orders: order.NewService(pool), products: product.NewService(pool)}
	f.shop = newShop(t, f)

	err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash, display_name) VALUES ($1, 'x', 'Clerk') RETURNING id`,
		"clerk-"+dbtest.Suffix()+"@example.com").Scan(&f.user)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, f.user) })
	return f
}

// newShop creates a tenant. Cleanup must delete its orders first (orders -> tenants is
// ON DELETE RESTRICT), inside the tenant's RLS scope, then the tenant itself.
func newShop(t *testing.T, f *fixture) uuid.UUID {
	t.Helper()
	shop, err := tenant.Repository{}.Create(f.ctx, f.pool, "order-test-"+dbtest.Suffix(), "Order Test")
	if err != nil {
		t.Fatalf("create shop: %v", err)
	}
	t.Cleanup(func() {
		ctx := context.Background()
		_ = database.InTx(ctx, f.pool, database.Scope{TenantID: shop.ID}, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `DELETE FROM orders WHERE tenant_id = $1`, shop.ID)
			return err
		})
		_, _ = f.pool.Exec(ctx, `DELETE FROM tenants WHERE id = $1`, shop.ID)
	})
	return shop.ID
}

func (f *fixture) newProduct(t *testing.T, shop uuid.UUID, name string, price int64, stock int) product.Product {
	t.Helper()
	p, err := f.products.Create(f.ctx, shop, product.CreateInput{Name: name, PriceSatang: price, Stock: stock})
	if err != nil {
		t.Fatalf("create product: %v", err)
	}
	return p
}

func (f *fixture) stock(t *testing.T, shop, id uuid.UUID) int {
	t.Helper()
	p, err := f.products.Get(f.ctx, shop, id)
	if err != nil {
		t.Fatalf("get product: %v", err)
	}
	return p.Stock
}

func (f *fixture) order(items ...order.ItemInput) order.CreateInput {
	return order.CreateInput{CustomerName: "Somchai", CustomerPhone: "0812345678", ShippingFeeSatang: 5000, Items: items}
}

func TestCreateOrder(t *testing.T) {
	f := setup(t)
	shirt := f.newProduct(t, f.shop, "Shirt", 25900, 10)
	mug := f.newProduct(t, f.shop, "Mug", 9900, 3)

	o, err := f.orders.Create(f.ctx, f.shop, f.user, f.order(
		order.ItemInput{ProductID: shirt.ID, Quantity: 2},
		order.ItemInput{ProductID: mug.ID, Quantity: 1},
	))
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	t.Run("totals are computed from product prices", func(t *testing.T) {
		// 2 x 259.00 + 1 x 99.00 = 617.00, + 50.00 shipping = 667.00
		if o.SubtotalSatang != 61700 || o.TotalSatang != 66700 || o.Status != order.StatusPending {
			t.Fatalf("unexpected order: subtotal=%d total=%d status=%s", o.SubtotalSatang, o.TotalSatang, o.Status)
		}
		if o.OrderNumber != 1001 || len(o.Items) != 2 {
			t.Fatalf("want order #1001 with 2 items, got #%d with %d", o.OrderNumber, len(o.Items))
		}
	})

	t.Run("stock is taken", func(t *testing.T) {
		if s := f.stock(t, f.shop, shirt.ID); s != 8 {
			t.Fatalf("shirt stock: want 8, got %d", s)
		}
	})

	t.Run("price change later does not touch the order (snapshot)", func(t *testing.T) {
		newPrice := int64(1)
		if _, err := f.products.Update(f.ctx, f.shop, shirt.ID, product.UpdateInput{PriceSatang: &newPrice}); err != nil {
			t.Fatalf("update price: %v", err)
		}
		got, err := f.orders.Get(f.ctx, f.shop, o.ID)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if got.TotalSatang != 66700 {
			t.Fatalf("total changed after price change: %d", got.TotalSatang)
		}
	})

	t.Run("next order gets the next number", func(t *testing.T) {
		o2, err := f.orders.Create(f.ctx, f.shop, f.user, f.order(order.ItemInput{ProductID: mug.ID, Quantity: 1}))
		if err != nil || o2.OrderNumber != 1002 {
			t.Fatalf("want #1002, got #%d err=%v", o2.OrderNumber, err)
		}
	})

	t.Run("not enough stock fails and takes nothing", func(t *testing.T) {
		before := f.stock(t, f.shop, shirt.ID)
		_, err := f.orders.Create(f.ctx, f.shop, f.user, f.order(
			order.ItemInput{ProductID: shirt.ID, Quantity: 1},
			order.ItemInput{ProductID: mug.ID, Quantity: 99},
		))
		var stockErr *order.InsufficientStockError
		if !errors.As(err, &stockErr) || stockErr.ProductID != mug.ID || stockErr.Available != 1 {
			t.Fatalf("want InsufficientStockError for mug (1 left), got %v", err)
		}
		if after := f.stock(t, f.shop, shirt.ID); after != before {
			t.Fatalf("shirt stock changed from %d to %d despite failed order", before, after)
		}
	})

	t.Run("archived product cannot be ordered", func(t *testing.T) {
		if err := f.products.Archive(f.ctx, f.shop, mug.ID); err != nil {
			t.Fatalf("archive: %v", err)
		}
		_, err := f.orders.Create(f.ctx, f.shop, f.user, f.order(order.ItemInput{ProductID: mug.ID, Quantity: 1}))
		var unavail *order.ProductUnavailableError
		if !errors.As(err, &unavail) {
			t.Fatalf("want ProductUnavailableError, got %v", err)
		}
	})

	t.Run("another shop's product cannot be ordered", func(t *testing.T) {
		otherShop := newShop(t, f)
		foreign := f.newProduct(t, otherShop, "Foreign", 100, 100)
		_, err := f.orders.Create(f.ctx, f.shop, f.user, f.order(order.ItemInput{ProductID: foreign.ID, Quantity: 1}))
		var unavail *order.ProductUnavailableError
		if !errors.As(err, &unavail) {
			t.Fatalf("want ProductUnavailableError, got %v", err)
		}
	})

	t.Run("duplicate product lines are rejected", func(t *testing.T) {
		_, err := f.orders.Create(f.ctx, f.shop, f.user, f.order(
			order.ItemInput{ProductID: shirt.ID, Quantity: 1},
			order.ItemInput{ProductID: shirt.ID, Quantity: 1},
		))
		var verr *order.ValidationError
		if !errors.As(err, &verr) {
			t.Fatalf("want ValidationError, got %v", err)
		}
	})

	t.Run("search by order number and by customer", func(t *testing.T) {
		byNumber, err := f.orders.List(f.ctx, f.shop, order.ListParams{Query: "#1001"})
		if err != nil || len(byNumber.Items) != 1 || byNumber.Items[0].OrderNumber != 1001 {
			t.Fatalf("search #1001: got %+v err=%v", byNumber.Items, err)
		}
		byName, err := f.orders.List(f.ctx, f.shop, order.ListParams{Query: "somCHAI"})
		if err != nil || len(byName.Items) != 2 {
			t.Fatalf("search name: want 2, got %d err=%v", len(byName.Items), err)
		}
	})
}

func TestTransitions(t *testing.T) {
	f := setup(t)
	shirt := f.newProduct(t, f.shop, "Shirt", 1000, 100) // enough for every subtest's order
	// place takes the subtest's own t: calling Fatal on the parent t from a subtest is not allowed.
	place := func(t *testing.T) order.Order {
		t.Helper()
		o, err := f.orders.Create(f.ctx, f.shop, f.user, f.order(order.ItemInput{ProductID: shirt.ID, Quantity: 4}))
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		return o
	}

	t.Run("happy path stamps each step", func(t *testing.T) {
		o := place(t)
		for _, to := range []order.Status{order.StatusPaid, order.StatusShipped, order.StatusCompleted} {
			var err error
			if o, err = f.orders.Transition(f.ctx, f.shop, o.ID, to, tenant.RoleStaff); err != nil {
				t.Fatalf("-> %s: %v", to, err)
			}
		}
		if o.PaidAt == nil || o.ShippedAt == nil || o.CompletedAt == nil || o.CancelledAt != nil {
			t.Fatalf("timestamps not set as expected: %+v", o)
		}
	})

	t.Run("invalid jumps are refused", func(t *testing.T) {
		o := place(t)
		var terr *order.TransitionError
		if _, err := f.orders.Transition(f.ctx, f.shop, o.ID, order.StatusShipped, tenant.RoleOwner); !errors.As(err, &terr) {
			t.Fatalf("pending -> shipped: want TransitionError, got %v", err)
		}
	})

	t.Run("staff cannot cancel", func(t *testing.T) {
		o := place(t)
		if _, err := f.orders.Transition(f.ctx, f.shop, o.ID, order.StatusCancelled, tenant.RoleStaff); !errors.Is(err, order.ErrCancelForbidden) {
			t.Fatalf("want ErrCancelForbidden, got %v", err)
		}
	})

	t.Run("cancel puts stock back exactly once", func(t *testing.T) {
		o := place(t)
		before := f.stock(t, f.shop, shirt.ID)
		cancelled, err := f.orders.Transition(f.ctx, f.shop, o.ID, order.StatusCancelled, tenant.RoleOwner)
		if err != nil {
			t.Fatalf("cancel: %v", err)
		}
		if cancelled.CancelReason == nil || *cancelled.CancelReason != order.CancelByMerchant {
			t.Fatalf("want cancel reason %q, got %v", order.CancelByMerchant, cancelled.CancelReason)
		}
		var terr *order.TransitionError
		if _, err := f.orders.Transition(f.ctx, f.shop, o.ID, order.StatusCancelled, tenant.RoleOwner); !errors.As(err, &terr) {
			t.Fatalf("second cancel: want TransitionError, got %v", err)
		}
		if after := f.stock(t, f.shop, shirt.ID); after != before+4 {
			t.Fatalf("stock: want %d, got %d", before+4, after)
		}
	})
}

// TestConcurrentOrdersNeverOversell fires many orders at the same time for a product with
// little stock. Without row locks, several could read "stock = 5" at once and all succeed.
func TestConcurrentOrdersNeverOversell(t *testing.T) {
	f := setup(t)
	const stock, buyers = 5, 20
	hot := f.newProduct(t, f.shop, "Limited edition", 99900, stock)

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		numbers = map[int64]bool{}
		soldOut int
		other   []error
	)
	for range buyers {
		wg.Add(1)
		go func() { // each buyer runs in its own goroutine, all at once
			defer wg.Done()
			o, err := f.orders.Create(f.ctx, f.shop, f.user, f.order(order.ItemInput{ProductID: hot.ID, Quantity: 1}))

			mu.Lock() // the map and counters are shared between goroutines
			defer mu.Unlock()
			var stockErr *order.InsufficientStockError
			switch {
			case err == nil:
				numbers[o.OrderNumber] = true
			case errors.As(err, &stockErr):
				soldOut++
			default:
				other = append(other, err)
			}
		}()
	}
	wg.Wait()

	if len(other) > 0 {
		t.Fatalf("unexpected errors: %v", other)
	}
	if len(numbers) != stock || soldOut != buyers-stock {
		t.Fatalf("want %d sold with unique numbers and %d sold out, got %d sold, %d sold out",
			stock, buyers-stock, len(numbers), soldOut)
	}
	if s := f.stock(t, f.shop, hot.ID); s != 0 {
		t.Fatalf("final stock: want 0, got %d", s)
	}
}

// TestConcurrentCancelReturnsStockOnce: ten simultaneous cancels of the same order.
func TestConcurrentCancelReturnsStockOnce(t *testing.T) {
	f := setup(t)
	p := f.newProduct(t, f.shop, "Shirt", 1000, 10)
	o, err := f.orders.Create(f.ctx, f.shop, f.user, f.order(order.ItemInput{ProductID: p.ID, Quantity: 3}))
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	succeeded := 0
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := f.orders.Transition(f.ctx, f.shop, o.ID, order.StatusCancelled, tenant.RoleOwner)
			if err == nil {
				mu.Lock()
				succeeded++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if succeeded != 1 {
		t.Fatalf("want exactly 1 successful cancel, got %d", succeeded)
	}
	if s := f.stock(t, f.shop, p.ID); s != 10 {
		t.Fatalf("stock: want 10 (returned once), got %d", s)
	}
}

// TestExpireUnpaid checks which orders the worker's expiry job cancels.
//
// The job looks across ALL shops, and the tests share one database (possibly with dev data), so
// the test orders are backdated to the year 2000 and the cutoff is 2000-01-02: no real order can
// be that old, so the test never touches anything but its own orders.
func TestExpireUnpaid(t *testing.T) {
	f := setup(t)
	tee := f.newProduct(t, f.shop, "Tee", 1000, 100)
	cutoff := time.Date(2000, 1, 2, 0, 0, 0, 0, time.UTC)

	guest := func(t *testing.T) order.Order {
		t.Helper()
		in := f.order(order.ItemInput{ProductID: tee.ID, Quantity: 3})
		in.Channel = order.ChannelStorefront
		o, _, err := f.orders.PlaceGuestOrder(f.ctx, f.shop, in)
		if err != nil {
			t.Fatalf("place guest order: %v", err)
		}
		return o
	}
	backdate := func(t *testing.T, o order.Order) {
		t.Helper()
		err := database.InTx(f.ctx, f.pool, database.Scope{TenantID: f.shop}, func(tx pgx.Tx) error {
			_, err := tx.Exec(f.ctx, `UPDATE orders SET created_at = '2000-01-01' WHERE id = $1`, o.ID)
			return err
		})
		if err != nil {
			t.Fatalf("backdate: %v", err)
		}
	}
	get := func(t *testing.T, o order.Order) order.Order {
		t.Helper()
		got, err := f.orders.Get(f.ctx, f.shop, o.ID)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		return got
	}

	t.Run("only old, unpaid storefront orders expire", func(t *testing.T) {
		expired := guest(t)
		backdate(t, expired)

		recent := guest(t) // placed just now: still inside its payment window

		paid := guest(t)
		backdate(t, paid)
		if _, err := f.orders.Transition(f.ctx, f.shop, paid.ID, order.StatusPaid, tenant.RoleStaff); err != nil {
			t.Fatalf("pay: %v", err)
		}

		manual, err := f.orders.Create(f.ctx, f.shop, f.user, f.order(order.ItemInput{ProductID: tee.ID, Quantity: 3}))
		if err != nil {
			t.Fatalf("create manual: %v", err)
		}
		backdate(t, manual) // merchant-entered orders never expire, however old

		before := f.stock(t, f.shop, tee.ID)
		n, err := f.orders.ExpireUnpaid(f.ctx, cutoff)
		if err != nil || n != 1 {
			t.Fatalf("want 1 order expired, got %d (err %v)", n, err)
		}

		got := get(t, expired)
		if got.Status != order.StatusCancelled || got.CancelReason == nil || *got.CancelReason != order.CancelPaymentTimeout {
			t.Fatalf("expired order: want cancelled/payment_timeout, got %s/%v", got.Status, got.CancelReason)
		}
		if after := f.stock(t, f.shop, tee.ID); after != before+3 {
			t.Fatalf("stock: want %d back on the shelf, got %d", before+3, after)
		}
		for _, o := range []order.Order{recent, paid, manual} {
			if s := get(t, o).Status; s == order.StatusCancelled {
				t.Fatalf("order #%d (%s) must not expire", o.OrderNumber, o.Channel)
			}
		}

		if n, err := f.orders.ExpireUnpaid(f.ctx, cutoff); err != nil || n != 0 {
			t.Fatalf("second run: want nothing left to do, got %d (err %v)", n, err)
		}
	})

	t.Run("an order paid while the job runs is not cancelled", func(t *testing.T) {
		o := guest(t)
		backdate(t, o)

		// The merchant's "mark as paid" holds the row lock: the job finds the order (it is
		// still pending), then waits for the lock, and must see the new status once it gets it.
		tx, err := f.pool.Begin(f.ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(context.Background()) }()
		if err := database.SetScope(f.ctx, tx, database.Scope{TenantID: f.shop}); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(f.ctx, `SELECT 1 FROM orders WHERE id = $1 FOR UPDATE`, o.ID); err != nil {
			t.Fatal(err)
		}

		type result struct {
			n   int
			err error
		}
		done := make(chan result)
		go func() {
			n, err := f.orders.ExpireUnpaid(f.ctx, cutoff)
			done <- result{n, err}
		}()
		time.Sleep(200 * time.Millisecond) // let the job find the order and block on the lock

		if _, err := tx.Exec(f.ctx, `UPDATE orders SET status = 'paid', paid_at = now() WHERE id = $1`, o.ID); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(f.ctx); err != nil {
			t.Fatal(err)
		}

		if r := <-done; r.err != nil || r.n != 0 {
			t.Fatalf("want nothing cancelled, got %d (err %v)", r.n, r.err)
		}
		if s := get(t, o).Status; s != order.StatusPaid {
			t.Fatalf("want the order to stay paid, got %s", s)
		}
	})
}
