package product_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"mscs/internal/product"
	"mscs/internal/tenant"
	"mscs/internal/testutil/dbtest"
)

func ptr[T any](v T) *T { return &v } // generic helper: pointer to any value

func newShop(t *testing.T, ctx context.Context, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	shop, err := tenant.Repository{}.Create(ctx, pool, "prod-test-"+dbtest.Suffix(), "Product Test")
	if err != nil {
		t.Fatalf("create shop: %v", err)
	}
	// ON DELETE CASCADE removes the shop's products too.
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM tenants WHERE id = $1`, shop.ID) })
	return shop.ID
}

func TestService(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.Pool(t)
	svc := product.NewService(pool)
	shopA := newShop(t, ctx, pool)
	shopB := newShop(t, ctx, pool)

	shirt, err := svc.Create(ctx, shopA, product.CreateInput{
		SKU: ptr("  SHIRT-01 "), Name: " T-Shirt ", PriceSatang: 25900, Stock: 10,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	t.Run("create trims input and sets defaults", func(t *testing.T) {
		if shirt.Name != "T-Shirt" || *shirt.SKU != "SHIRT-01" || shirt.Status != product.StatusActive {
			t.Fatalf("unexpected product: %+v", shirt)
		}
	})

	t.Run("blank name is rejected", func(t *testing.T) {
		var verr *product.ValidationError
		if _, err := svc.Create(ctx, shopA, product.CreateInput{Name: "   "}); !errors.As(err, &verr) {
			t.Fatalf("want ValidationError, got %v", err)
		}
	})

	t.Run("sku is unique per shop only", func(t *testing.T) {
		if _, err := svc.Create(ctx, shopA, product.CreateInput{SKU: ptr("SHIRT-01"), Name: "Dup"}); !errors.Is(err, product.ErrSKUTaken) {
			t.Fatalf("same shop: want ErrSKUTaken, got %v", err)
		}
		if _, err := svc.Create(ctx, shopB, product.CreateInput{SKU: ptr("SHIRT-01"), Name: "Other shop"}); err != nil {
			t.Fatalf("other shop: want success, got %v", err)
		}
	})

	t.Run("another shop cannot read the product", func(t *testing.T) {
		if _, err := svc.Get(ctx, shopB, shirt.ID); !errors.Is(err, product.ErrNotFound) {
			t.Fatalf("want ErrNotFound, got %v", err)
		}
	})

	t.Run("another shop cannot update the product", func(t *testing.T) {
		if _, err := svc.Update(ctx, shopB, shirt.ID, product.UpdateInput{PriceSatang: ptr(int64(1))}); !errors.Is(err, product.ErrNotFound) {
			t.Fatalf("want ErrNotFound, got %v", err)
		}
	})

	t.Run("partial update changes only the given fields", func(t *testing.T) {
		got, err := svc.Update(ctx, shopA, shirt.ID, product.UpdateInput{PriceSatang: ptr(int64(19900))})
		if err != nil {
			t.Fatalf("update: %v", err)
		}
		if got.PriceSatang != 19900 || got.Name != "T-Shirt" || got.Stock != 10 {
			t.Fatalf("unexpected product after update: %+v", got)
		}
		if !got.UpdatedAt.After(shirt.UpdatedAt) {
			t.Fatal("updated_at should move forward (trigger)")
		}
	})

	t.Run("search treats % literally", func(t *testing.T) {
		if _, err := svc.Create(ctx, shopA, product.CreateInput{Name: "Sale 50% off"}); err != nil {
			t.Fatalf("create: %v", err)
		}
		page, err := svc.List(ctx, shopA, product.ListParams{Query: "%"})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(page.Items) != 1 || page.Items[0].Name != "Sale 50% off" {
			t.Fatalf("want only the 50%% product, got %d items", len(page.Items))
		}
	})

	t.Run("archived products leave the default list", func(t *testing.T) {
		if err := svc.Archive(ctx, shopA, shirt.ID); err != nil {
			t.Fatalf("archive: %v", err)
		}
		active, _ := svc.List(ctx, shopA, product.ListParams{Status: ptr(product.StatusActive)})
		all, _ := svc.List(ctx, shopA, product.ListParams{})
		if contains(active.Items, shirt.ID) || !contains(all.Items, shirt.ID) {
			t.Fatal("archived product must be hidden from active list but present in all")
		}
	})
}

func TestListPagination(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.Pool(t)
	svc := product.NewService(pool)
	shop := newShop(t, ctx, pool)

	for i := 1; i <= 5; i++ {
		if _, err := svc.Create(ctx, shop, product.CreateInput{Name: fmt.Sprintf("P%d", i)}); err != nil {
			t.Fatalf("create: %v", err)
		}
	}

	var names []string
	var cursor *uuid.UUID
	pages := 0
	for {
		page, err := svc.List(ctx, shop, product.ListParams{Limit: 2, After: cursor})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		pages++
		for _, p := range page.Items {
			names = append(names, p.Name)
		}
		if page.NextCursor == nil {
			break
		}
		cursor = page.NextCursor
	}

	want := "[P5 P4 P3 P2 P1]" // newest first, every product exactly once
	if got := fmt.Sprint(names); got != want || pages != 3 {
		t.Fatalf("want %s in 3 pages, got %s in %d pages", want, got, pages)
	}
}

func contains(items []product.Product, id uuid.UUID) bool {
	for _, p := range items {
		if p.ID == id {
			return true
		}
	}
	return false
}
