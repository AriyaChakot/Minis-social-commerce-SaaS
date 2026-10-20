package product

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"mscs/internal/database"
	"mscs/internal/imageproc"
	"mscs/internal/storage"
)

// MaxImagesPerProduct keeps galleries (and storage use) reasonable.
const MaxImagesPerProduct = 5

var (
	ErrImageNotFound = errors.New("product: image not found")
	ErrTooManyImages = fmt.Errorf("product: at most %d images per product", MaxImagesPerProduct)
)

// LargeKey and ThumbKey: the two files stored for one image key.
func LargeKey(key string) string { return key + ".jpg" }
func ThumbKey(key string) string { return key + "_thumb.jpg" }

type imageRow struct {
	ID        uuid.UUID
	ProductID uuid.UUID
	Key       string
	Width     int
	Height    int
}

// imagesOf returns the images of several products, each product's in position order.
func (Repository) imagesOf(ctx context.Context, db database.DBTX, tenantID uuid.UUID, productIDs []uuid.UUID) ([]imageRow, error) {
	rows, err := db.Query(ctx,
		`SELECT id, product_id, object_key, width, height FROM product_images
		  WHERE tenant_id = $1 AND product_id = ANY($2)
		  ORDER BY product_id, position`,
		tenantID, productIDs)
	if err != nil {
		return nil, fmt.Errorf("product: images: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (imageRow, error) {
		var r imageRow
		return r, row.Scan(&r.ID, &r.ProductID, &r.Key, &r.Width, &r.Height)
	})
}

// lock row-locks the product until the transaction ends: image changes of one product then
// happen one after the other, so two uploads cannot both take the 5th place.
func (Repository) lock(ctx context.Context, db database.DBTX, tenantID, id uuid.UUID) error {
	var one int
	err := db.QueryRow(ctx, `SELECT 1 FROM products WHERE tenant_id = $1 AND id = $2 FOR UPDATE`, tenantID, id).Scan(&one)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("product: lock: %w", err)
	}
	return nil
}

// ImageService manages product photos: the files in object storage, their rows in PostgreSQL.
type ImageService struct {
	products *Service
	store    storage.Store
	limits   imageproc.Limits
	// Decoding a big photo can take ~200 MB of memory: at most 2 at a time, the rest wait.
	slots chan struct{}
}

func NewImageService(products *Service, store storage.Store) *ImageService {
	return &ImageService{products: products, store: store, limits: imageproc.DefaultLimits, slots: make(chan struct{}, 2)}
}

// Upload adds a photo at the end of the product's gallery (the first one becomes the cover).
//
// The files are written BEFORE the database row: a row must never point at a missing file. If
// the row cannot be written, the files are deleted again. (If even that fails, an unreferenced
// file stays in storage: harmless, and a cleanup job could remove it.)
func (s *ImageService) Upload(ctx context.Context, tenantID, productID uuid.UUID, file io.Reader) (Product, error) {
	// Cheap checks first: no need to process a photo for a product that cannot take it.
	p, err := s.products.Get(ctx, tenantID, productID)
	if err != nil {
		return Product{}, err
	}
	if len(p.Images) >= MaxImagesPerProduct {
		return Product{}, ErrTooManyImages
	}

	select {
	case s.slots <- struct{}{}:
	case <-ctx.Done():
		return Product{}, ctx.Err()
	}
	res, err := imageproc.Process(file, s.limits)
	<-s.slots
	if err != nil {
		return Product{}, err // imageproc.Err*: the handler explains them to the merchant
	}

	id, err := uuid.NewV7()
	if err != nil {
		return Product{}, err
	}
	key := fmt.Sprintf("tenants/%s/products/%s/%s", tenantID, productID, id)
	files := []string{LargeKey(key), ThumbKey(key)}
	if err := s.store.Put(ctx, files[0], "image/jpeg", res.Large); err != nil {
		return Product{}, err
	}
	if err := s.store.Put(ctx, files[1], "image/jpeg", res.Thumb); err != nil {
		s.removeFiles(files...)
		return Product{}, err
	}

	err = s.products.inTenant(ctx, tenantID, func(tx pgx.Tx) error {
		if err := s.products.repo.lock(ctx, tx, tenantID, productID); err != nil {
			return err
		}
		var count int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM product_images WHERE tenant_id = $1 AND product_id = $2`,
			tenantID, productID).Scan(&count); err != nil {
			return fmt.Errorf("product: count images: %w", err)
		}
		if count >= MaxImagesPerProduct { // another upload took the last place meanwhile
			return ErrTooManyImages
		}
		_, err := tx.Exec(ctx,
			`INSERT INTO product_images (id, tenant_id, product_id, position, object_key, width, height)
			 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			id, tenantID, productID, count, key, res.Width, res.Height)
		if err != nil {
			return fmt.Errorf("product: insert image: %w", err)
		}
		return nil
	})
	if err != nil {
		s.removeFiles(files...)
		return Product{}, err
	}
	return s.products.Get(ctx, tenantID, productID)
}

// Delete removes a photo; the ones after it move up one place. The row goes first, then the
// files: if deleting the files fails, nothing points at them anymore.
func (s *ImageService) Delete(ctx context.Context, tenantID, productID, imageID uuid.UUID) (Product, error) {
	var key string
	err := s.products.inTenant(ctx, tenantID, func(tx pgx.Tx) error {
		if err := s.products.repo.lock(ctx, tx, tenantID, productID); err != nil {
			return err
		}
		var pos int
		err := tx.QueryRow(ctx,
			`DELETE FROM product_images WHERE tenant_id = $1 AND product_id = $2 AND id = $3
			 RETURNING object_key, position`,
			tenantID, productID, imageID).Scan(&key, &pos)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrImageNotFound
		}
		if err != nil {
			return fmt.Errorf("product: delete image: %w", err)
		}
		_, err = tx.Exec(ctx,
			`UPDATE product_images SET position = position - 1
			  WHERE tenant_id = $1 AND product_id = $2 AND position > $3`,
			tenantID, productID, pos)
		return err
	})
	if err != nil {
		return Product{}, err
	}
	s.removeFiles(LargeKey(key), ThumbKey(key))
	return s.products.Get(ctx, tenantID, productID)
}

// MakeCover moves a photo to the first place; the ones before it move down one place.
func (s *ImageService) MakeCover(ctx context.Context, tenantID, productID, imageID uuid.UUID) (Product, error) {
	err := s.products.inTenant(ctx, tenantID, func(tx pgx.Tx) error {
		if err := s.products.repo.lock(ctx, tx, tenantID, productID); err != nil {
			return err
		}
		var pos int
		err := tx.QueryRow(ctx,
			`SELECT position FROM product_images WHERE tenant_id = $1 AND product_id = $2 AND id = $3`,
			tenantID, productID, imageID).Scan(&pos)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrImageNotFound
		}
		if err != nil {
			return fmt.Errorf("product: find image: %w", err)
		}
		// Positions collide while the rows are updated one by one; the UNIQUE constraint is
		// DEFERRABLE, so it is checked at COMMIT, when they are all distinct again.
		_, err = tx.Exec(ctx,
			`UPDATE product_images
			    SET position = CASE WHEN id = $3 THEN 0 ELSE position + 1 END
			  WHERE tenant_id = $1 AND product_id = $2 AND (id = $3 OR position < $4)`,
			tenantID, productID, imageID, pos)
		return err
	})
	if err != nil {
		return Product{}, err
	}
	return s.products.Get(ctx, tenantID, productID)
}

// removeFiles deletes files in the background of a request that has already decided; a failure
// leaves an unreferenced file, which is logged, not returned.
func (s *ImageService) removeFiles(keys ...string) {
	if err := s.store.Delete(context.Background(), keys...); err != nil {
		log.Printf("product: could not delete image files %v: %v", keys, err)
	}
}
