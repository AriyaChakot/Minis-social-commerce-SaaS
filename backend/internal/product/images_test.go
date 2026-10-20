package product_test

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"mscs/internal/imageproc"
	"mscs/internal/product"
	"mscs/internal/storage"
	"mscs/internal/testutil/dbtest"
)

// photo returns a small PNG of one color: a valid image to upload.
func photo(t *testing.T, c color.Color) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 60, 40))
	for y := range 40 {
		for x := range 60 {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func imageIDs(p product.Product) []uuid.UUID {
	ids := make([]uuid.UUID, len(p.Images))
	for i, img := range p.Images {
		ids[i] = img.ID
	}
	return ids
}

func TestImages(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.Pool(t)
	store := storage.NewMemory()
	products := product.NewService(pool, product.WithMediaBaseURL("https://cdn.example/"))
	images := product.NewImageService(products, store)

	shop := newShop(t, ctx, pool)
	other := newShop(t, ctx, pool)
	shirt, err := products.Create(ctx, shop, product.CreateInput{Name: "Shirt", PriceSatang: 100})
	if err != nil {
		t.Fatal(err)
	}
	if shirt.Images == nil || len(shirt.Images) != 0 {
		t.Fatalf("a new product has an empty (not null) image list, got %#v", shirt.Images)
	}

	var first product.Product
	t.Run("upload: processed, stored, linked", func(t *testing.T) {
		first, err = images.Upload(ctx, shop, shirt.ID, bytes.NewReader(photo(t, color.RGBA{200, 0, 0, 255})))
		if err != nil {
			t.Fatal(err)
		}
		if len(first.Images) != 1 {
			t.Fatalf("want 1 image, got %d", len(first.Images))
		}
		img := first.Images[0]
		prefix := "https://cdn.example/tenants/" + shop.String() + "/products/" + shirt.ID.String() + "/"
		if !strings.HasPrefix(img.URL, prefix) || !strings.HasSuffix(img.URL, ".jpg") ||
			img.ThumbURL != strings.TrimSuffix(img.URL, ".jpg")+"_thumb.jpg" || img.Width != 60 || img.Height != 40 {
			t.Fatalf("got %+v", img)
		}
		// Both files are in storage, as JPEG.
		keys := store.Keys()
		if len(keys) != 2 {
			t.Fatalf("want 2 files, got %v", keys)
		}
		for _, k := range keys {
			body, ct, err := store.Get(ctx, k)
			if err != nil || ct != "image/jpeg" {
				t.Fatalf("%s: %q %v", k, ct, err)
			}
			body.Close()
		}
		// Lists carry the images too.
		page, _ := products.List(ctx, shop, product.ListParams{})
		if len(page.Items) != 1 || len(page.Items[0].Images) != 1 {
			t.Fatalf("list: want the image, got %+v", page.Items)
		}
	})

	t.Run("a file that is not an image is refused and nothing is stored", func(t *testing.T) {
		_, err := images.Upload(ctx, shop, shirt.ID, strings.NewReader("<script>alert(1)</script>"))
		if !errors.Is(err, imageproc.ErrUnsupported) || len(store.Keys()) != 2 {
			t.Fatalf("want ErrUnsupported and still 2 files, got %v, %d files", err, len(store.Keys()))
		}
	})

	t.Run("at most five, even when uploads race", func(t *testing.T) {
		// 1 already there; 8 uploads at the same time for the remaining 4 places.
		var wg sync.WaitGroup
		errs := make(chan error, 8)
		for range 8 {
			wg.Go(func() {
				_, err := images.Upload(ctx, shop, shirt.ID, bytes.NewReader(photo(t, color.RGBA{0, 0, 200, 255})))
				errs <- err
			})
		}
		wg.Wait()
		close(errs)
		ok, full := 0, 0
		for err := range errs {
			switch {
			case err == nil:
				ok++
			case errors.Is(err, product.ErrTooManyImages):
				full++
			default:
				t.Fatalf("unexpected error: %v", err)
			}
		}
		p, _ := products.Get(ctx, shop, shirt.ID)
		if ok != 4 || full != 4 || len(p.Images) != 5 {
			t.Fatalf("want 4 accepted, 4 refused, 5 images; got %d, %d, %d", ok, full, len(p.Images))
		}
		// The refused uploads left no files behind: 5 images × 2 files.
		if n := len(store.Keys()); n != 10 {
			t.Fatalf("want 10 files, got %d", n)
		}
	})

	t.Run("make cover: moves to the front, the others keep their order", func(t *testing.T) {
		p, _ := products.Get(ctx, shop, shirt.ID)
		before := imageIDs(p) // a b c d e
		p, err := images.MakeCover(ctx, shop, shirt.ID, before[3])
		if err != nil {
			t.Fatal(err)
		}
		want := []uuid.UUID{before[3], before[0], before[1], before[2], before[4]} // d a b c e
		if !slices.Equal(imageIDs(p), want) {
			t.Fatalf("got %v, want %v", imageIDs(p), want)
		}
	})

	t.Run("delete: removes the files, closes the gap", func(t *testing.T) {
		p, _ := products.Get(ctx, shop, shirt.ID)
		before := imageIDs(p)
		p, err := images.Delete(ctx, shop, shirt.ID, before[1])
		if err != nil {
			t.Fatal(err)
		}
		want := slices.Delete(slices.Clone(before), 1, 2)
		if !slices.Equal(imageIDs(p), want) || len(store.Keys()) != 8 {
			t.Fatalf("got %v and %d files, want %v and 8 files", imageIDs(p), len(store.Keys()), want)
		}
		// The gap is closed: making the last one the cover still works (positions are 0..3).
		if _, err := images.MakeCover(ctx, shop, shirt.ID, want[3]); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("another shop cannot touch these photos", func(t *testing.T) {
		p, _ := products.Get(ctx, shop, shirt.ID)
		img := p.Images[0].ID
		if _, err := images.Delete(ctx, other, shirt.ID, img); !errors.Is(err, product.ErrNotFound) {
			t.Fatalf("delete from another shop: want ErrNotFound, got %v", err)
		}
		if _, err := images.Upload(ctx, other, shirt.ID, bytes.NewReader(photo(t, color.White))); !errors.Is(err, product.ErrNotFound) {
			t.Fatalf("upload from another shop: want ErrNotFound, got %v", err)
		}
		if _, err := images.MakeCover(ctx, shop, shirt.ID, uuid.New()); !errors.Is(err, product.ErrImageNotFound) {
			t.Fatalf("unknown image: want ErrImageNotFound, got %v", err)
		}
	})

	t.Run("storage failure: nothing is linked", func(t *testing.T) {
		mug, _ := products.Create(ctx, shop, product.CreateInput{Name: "Mug", PriceSatang: 100})
		store.FailPut = true
		defer func() { store.FailPut = false }()
		if _, err := images.Upload(ctx, shop, mug.ID, bytes.NewReader(photo(t, color.Black))); err == nil {
			t.Fatal("want an error when storage fails")
		}
		if p, _ := products.Get(ctx, shop, mug.ID); len(p.Images) != 0 {
			t.Fatalf("no image may point at a file that was not stored, got %d", len(p.Images))
		}
	})
}
