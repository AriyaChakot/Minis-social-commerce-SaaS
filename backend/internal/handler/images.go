package handler

import (
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"mscs/internal/imageproc"
	"mscs/internal/product"
	"mscs/internal/storage"
)

// MaxUploadBytes is the request body limit of the image upload route (the photo itself may be
// up to imageproc.DefaultLimits.MaxBytes; the rest is room for the multipart framing).
const MaxUploadBytes = 16 << 20

// ImageHandler exposes product photos: uploads by the merchant, and the files themselves.
type ImageHandler struct {
	svc   *product.ImageService
	store storage.Store
}

func NewImageHandler(svc *product.ImageService, store storage.Store) *ImageHandler {
	return &ImageHandler{svc: svc, store: store}
}

// Upload handles POST /tenants/:tenantID/products/:productID/images (owner/admin), a
// multipart/form-data body with the photo in the "file" field.
func (h *ImageHandler) Upload(c *gin.Context) {
	t, _ := currentTenant(c)
	id, ok := productID(c)
	if !ok {
		return
	}
	// FormFile would first copy the whole upload to memory or a temp file; reading the part
	// directly streams it into the image decoder, which stops at its size limit.
	reader, err := c.Request.MultipartReader()
	if err != nil {
		writeError(c, http.StatusBadRequest, "validation_failed", "send the photo as multipart/form-data",
			fieldError{Field: "file", Rule: "required"})
		return
	}
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			// Usually the body hit MaxUploadBytes.
			writeImageError(c, imageproc.ErrTooLarge)
			return
		}
		if part.FormName() != "file" {
			continue
		}
		p, err := h.svc.Upload(c.Request.Context(), t.ID, id, part)
		if err != nil {
			writeImageError(c, err)
			return
		}
		c.JSON(http.StatusCreated, p)
		return
	}
	writeError(c, http.StatusBadRequest, "validation_failed", `no file in the "file" field`,
		fieldError{Field: "file", Rule: "required"})
}

// Delete handles DELETE /tenants/:tenantID/products/:productID/images/:imageID (owner/admin).
func (h *ImageHandler) Delete(c *gin.Context) {
	h.change(c, h.svc.Delete)
}

// MakeCover handles POST /tenants/:tenantID/products/:productID/images/:imageID/cover (owner/admin).
func (h *ImageHandler) MakeCover(c *gin.Context) {
	h.change(c, h.svc.MakeCover)
}

func (h *ImageHandler) change(c *gin.Context, op func(ctx context.Context, tenantID, productID, imageID uuid.UUID) (product.Product, error)) {
	t, _ := currentTenant(c)
	id, ok := productID(c)
	if !ok {
		return
	}
	imageID, err := uuid.Parse(c.Param("imageID"))
	if err != nil {
		writeImageError(c, product.ErrImageNotFound)
		return
	}
	p, err := op(c.Request.Context(), t.ID, id, imageID)
	if err != nil {
		writeImageError(c, err)
		return
	}
	c.JSON(http.StatusOK, p)
}

// Only keys this API creates: tenants/<uuid>/products/<uuid>/<uuid>[_thumb].jpg.
var mediaKey = regexp.MustCompile(`^tenants/[0-9a-f-]{36}/products/[0-9a-f-]{36}/[0-9a-f-]{36}(_thumb)?\.jpg$`)

// Serve handles GET /api/v1/media/*key: the photo files, for every browser (no login, like any
// image on a shop's website). The keys contain random ids, so they cannot be guessed or listed.
//
// A file never changes after upload (a new photo gets a new key), so browsers and any CDN in
// front of the API may cache it for a year without asking again ("immutable").
func (h *ImageHandler) Serve(c *gin.Context) {
	key := strings.TrimPrefix(c.Param("key"), "/")
	if !mediaKey.MatchString(key) {
		writeError(c, http.StatusNotFound, "not_found", "not found")
		return
	}
	body, contentType, err := h.store.Get(c.Request.Context(), key)
	if errors.Is(err, storage.ErrNotFound) {
		writeError(c, http.StatusNotFound, "not_found", "not found")
		return
	}
	if err != nil {
		writeInternal(c, err)
		return
	}
	defer body.Close()

	c.Header("Content-Type", contentType)
	c.Header("Cache-Control", "public, max-age=31536000, immutable")
	// The browser must treat it as an image, whatever the bytes look like.
	c.Header("X-Content-Type-Options", "nosniff")
	c.Status(http.StatusOK)
	if _, err := io.Copy(c.Writer, body); err != nil {
		log.Printf("media: send %s: %v", key, err) // usually the client went away
	}
}

func writeImageError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, imageproc.ErrTooLarge):
		writeError(c, http.StatusRequestEntityTooLarge, "image_too_large", "the photo is larger than 15 MB")
	case errors.Is(err, imageproc.ErrUnsupported):
		writeError(c, http.StatusUnsupportedMediaType, "image_unsupported", "only JPEG, PNG and WebP photos")
	case errors.Is(err, imageproc.ErrTooManyPixels):
		writeError(c, http.StatusUnprocessableEntity, "image_too_many_pixels", "the photo has more than 50 megapixels")
	case errors.Is(err, imageproc.ErrCorrupt):
		writeError(c, http.StatusUnprocessableEntity, "image_corrupt", "the file is not a readable image")
	case errors.Is(err, product.ErrTooManyImages):
		writeError(c, http.StatusConflict, "too_many_images", err.Error())
	case errors.Is(err, product.ErrImageNotFound):
		writeError(c, http.StatusNotFound, "image_not_found", "image not found")
	default:
		writeProductError(c, err)
	}
}
