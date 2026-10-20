// Package imageproc turns an uploaded photo into the files the shop serves: a large JPEG and a
// thumbnail. Every upload is decoded and re-encoded, never stored as sent:
//
//   - The type is detected from the bytes, not from the file name or the Content-Type header,
//     which the client chooses (an "image.jpg" can be anything).
//   - Width × height is checked BEFORE decoding: a tiny file can claim to be 100,000 × 100,000
//     pixels and would need tens of GB of memory ("decompression bomb").
//   - Re-encoding drops all metadata. Phone photos carry EXIF, often with the GPS position of
//     where they were taken (for a home seller: their home). Customers must not get it.
//   - The EXIF orientation is applied first, otherwise phone photos would come out sideways.
//   - Transparent PNGs are flattened onto white (JPEG has no transparency).
package imageproc

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"net/http"

	// Decoders register themselves with the image package.
	_ "image/png"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

var (
	ErrTooLarge      = errors.New("imageproc: file too large")
	ErrUnsupported   = errors.New("imageproc: not a JPEG, PNG or WebP image")
	ErrTooManyPixels = errors.New("imageproc: image has too many pixels")
	ErrCorrupt       = errors.New("imageproc: image cannot be decoded")
)

// Output sizes: the longest side, in pixels. Smaller images are not enlarged.
const (
	LargeSize = 1200
	ThumbSize = 400
)

type Limits struct {
	MaxBytes  int64
	MaxPixels int
}

// DefaultLimits fit photos from current phones (up to ~50 megapixels).
var DefaultLimits = Limits{MaxBytes: 15 << 20, MaxPixels: 50_000_000}

type Result struct {
	Large, Thumb  []byte // JPEG
	Width, Height int    // of Large
}

func Process(r io.Reader, lim Limits) (Result, error) {
	data, err := io.ReadAll(io.LimitReader(r, lim.MaxBytes+1))
	if err != nil {
		return Result{}, err
	}
	if int64(len(data)) > lim.MaxBytes {
		return Result{}, ErrTooLarge
	}
	switch http.DetectContentType(data) {
	case "image/jpeg", "image/png", "image/webp":
	default:
		return Result{}, ErrUnsupported
	}

	// The header only: dimensions without decoding the pixels.
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return Result{}, ErrCorrupt
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return Result{}, ErrCorrupt
	}
	if cfg.Width*cfg.Height > lim.MaxPixels {
		return Result{}, ErrTooManyPixels
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return Result{}, ErrCorrupt
	}

	orientation := 1
	if format == "jpeg" {
		orientation = jpegOrientation(data)
	}

	// Resize first (cheap to rotate a small image), then rotate.
	large := orient(fit(img, LargeSize), orientation)
	thumb := fit(large, ThumbSize)

	var res Result
	if res.Large, err = encode(large, 85); err != nil {
		return Result{}, err
	}
	if res.Thumb, err = encode(thumb, 80); err != nil {
		return Result{}, err
	}
	b := large.Bounds()
	res.Width, res.Height = b.Dx(), b.Dy()
	return res, nil
}

func encode(img image.Image, quality int) ([]byte, error) {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// fit scales img so its longest side is at most maxSide, onto a white background (which also
// flattens transparency). It always returns a new image.
func fit(img image.Image, maxSide int) *image.RGBA {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if longest := max(w, h); longest > maxSide {
		w = max(1, w*maxSide/longest)
		h = max(1, h*maxSide/longest)
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(dst, dst.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	// Catmull-Rom: a high-quality filter for downscaling photos.
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, b, draw.Over, nil)
	return dst
}

// orient applies an EXIF orientation (1-8) so the image displays upright without it.
func orient(src *image.RGBA, o int) *image.RGBA {
	if o < 2 || o > 8 {
		return src
	}
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	dw, dh := w, h
	if o >= 5 { // 5-8 turn the image a quarter: width and height swap
		dw, dh = h, w
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := range dh {
		for x := range dw {
			var sx, sy int
			switch o {
			case 2: // mirrored
				sx, sy = w-1-x, y
			case 3: // upside down
				sx, sy = w-1-x, h-1-y
			case 4: // mirrored upside down
				sx, sy = x, h-1-y
			case 5: // mirrored, turned
				sx, sy = y, x
			case 6: // camera held turned right: rotate 90° clockwise
				sx, sy = y, h-1-x
			case 7:
				sx, sy = w-1-y, h-1-x
			case 8: // camera held turned left: rotate 90° counter-clockwise
				sx, sy = w-1-y, x
			}
			dst.SetRGBA(x, y, src.RGBAAt(sx, sy))
		}
	}
	return dst
}

// jpegOrientation reads the EXIF Orientation tag (0x0112) of a JPEG; 1 (upright) if absent.
//
// A JPEG is a list of segments (0xFF, marker, 2-byte length, data). EXIF is in the APP1 segment:
// "Exif\0\0", then a small TIFF file whose first directory (IFD0) holds the orientation.
func jpegOrientation(data []byte) int {
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return 1
	}
	for i := 2; i+4 <= len(data); {
		if data[i] != 0xFF {
			return 1
		}
		marker := data[i+1]
		if marker == 0xDA || marker == 0xD9 { // image data starts: no EXIF before it
			return 1
		}
		size := int(binary.BigEndian.Uint16(data[i+2:]))
		if size < 2 || i+2+size > len(data) {
			return 1
		}
		seg := data[i+4 : i+2+size]
		if marker == 0xE1 && len(seg) > 14 && string(seg[:6]) == "Exif\x00\x00" {
			return tiffOrientation(seg[6:])
		}
		i += 2 + size
	}
	return 1
}

func tiffOrientation(t []byte) int {
	var bo binary.ByteOrder
	switch string(t[:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return 1
	}
	if len(t) < 8 || bo.Uint16(t[2:]) != 42 {
		return 1
	}
	ifd := int(bo.Uint32(t[4:]))
	if ifd+2 > len(t) {
		return 1
	}
	n := int(bo.Uint16(t[ifd:]))
	for k := range n {
		e := ifd + 2 + 12*k
		if e+12 > len(t) {
			return 1
		}
		if bo.Uint16(t[e:]) == 0x0112 { // Orientation, type SHORT: the value is the first 2 bytes
			if o := int(bo.Uint16(t[e+8:])); o >= 1 && o <= 8 {
				return o
			}
			return 1
		}
	}
	return 1
}
