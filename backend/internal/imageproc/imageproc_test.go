package imageproc

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

// twoColor makes a w×h image: the left half red, the right half blue.
func twoColor(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			c := color.RGBA{255, 0, 0, 255}
			if x >= w/2 {
				c = color.RGBA{0, 0, 255, 255}
			}
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

func jpegBytes(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// withEXIF inserts an APP1 EXIF segment holding an Orientation tag (and a fake GPS-looking
// marker string, to check that metadata does not survive) right after the JPEG's SOI marker.
func withEXIF(jpg []byte, orientation uint16) []byte {
	tiff := []byte("MM\x00\x2a\x00\x00\x00\x08") // big-endian TIFF, IFD0 at offset 8
	ifd := make([]byte, 2+12+4)
	binary.BigEndian.PutUint16(ifd[0:], 1)      // one entry
	binary.BigEndian.PutUint16(ifd[2:], 0x0112) // Orientation
	binary.BigEndian.PutUint16(ifd[4:], 3)      // SHORT
	binary.BigEndian.PutUint32(ifd[6:], 1)      // count
	binary.BigEndian.PutUint16(ifd[10:], orientation)
	payload := append([]byte("Exif\x00\x00"), append(tiff, ifd...)...)
	payload = append(payload, []byte("GPS 13.7563N 100.5018E")...)

	seg := []byte{0xFF, 0xE1, 0, 0}
	binary.BigEndian.PutUint16(seg[2:], uint16(len(payload)+2))
	seg = append(seg, payload...)

	out := append([]byte{}, jpg[:2]...)
	out = append(out, seg...)
	return append(out, jpg[2:]...)
}

func decode(t *testing.T, b []byte) image.Image {
	t.Helper()
	img, format, err := image.Decode(bytes.NewReader(b))
	if err != nil || format != "jpeg" {
		t.Fatalf("output must be a JPEG, got %q (err %v)", format, err)
	}
	return img
}

// isRed / isBlue tolerate JPEG compression noise.
func isRed(c color.Color) bool { r, g, b, _ := c.RGBA(); return r > 0xc000 && g < 0x4000 && b < 0x4000 }
func isBlue(c color.Color) bool {
	r, g, b, _ := c.RGBA()
	return b > 0xc000 && r < 0x4000 && g < 0x4000
}

func TestResizesLargePhotos(t *testing.T) {
	res, err := Process(bytes.NewReader(jpegBytes(t, twoColor(3000, 1500))), DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	if res.Width != 1200 || res.Height != 600 {
		t.Fatalf("large: got %d×%d, want 1200×600", res.Width, res.Height)
	}
	if b := decode(t, res.Thumb).Bounds(); b.Dx() != 400 || b.Dy() != 200 {
		t.Fatalf("thumb: got %v, want 400×200", b)
	}
}

func TestSmallImagesAreNotEnlarged(t *testing.T) {
	res, err := Process(bytes.NewReader(jpegBytes(t, twoColor(300, 200))), DefaultLimits)
	if err != nil || res.Width != 300 || res.Height != 200 {
		t.Fatalf("got %d×%d (err %v), want 300×200", res.Width, res.Height, err)
	}
}

func TestAppliesEXIFOrientationAndDropsMetadata(t *testing.T) {
	// A phone held turned right stores the picture sideways plus "rotate 90° clockwise".
	in := withEXIF(jpegBytes(t, twoColor(300, 200)), 6)
	res, err := Process(bytes.NewReader(in), DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	if res.Width != 200 || res.Height != 300 {
		t.Fatalf("got %d×%d, want 200×300 (width and height swapped)", res.Width, res.Height)
	}
	// Rotated clockwise, the left (red) half ends up on top.
	img := decode(t, res.Large)
	if !isRed(img.At(100, 20)) || !isBlue(img.At(100, 280)) {
		t.Fatalf("want red on top and blue at the bottom, got %v / %v", img.At(100, 20), img.At(100, 280))
	}
	if bytes.Contains(res.Large, []byte("Exif")) || bytes.Contains(res.Large, []byte("GPS")) {
		t.Fatal("EXIF metadata (and the location in it) must not survive")
	}
}

func TestOrientations(t *testing.T) {
	src := twoColor(4, 2) // red at x=0..1, blue at x=2..3
	for o, want := range map[int]struct{ w, h int }{1: {4, 2}, 3: {4, 2}, 6: {2, 4}, 8: {2, 4}} {
		got := orient(src, o)
		if got.Bounds().Dx() != want.w || got.Bounds().Dy() != want.h {
			t.Fatalf("orientation %d: got %v", o, got.Bounds())
		}
	}
	// 8 = 90° counter-clockwise: the right (blue) half ends up on top.
	if got := orient(src, 8); !isBlue(got.At(0, 0)) || !isRed(got.At(0, 3)) {
		t.Fatal("orientation 8: want blue on top, red at the bottom")
	}
	// 3 = upside down: blue moves to the left.
	if got := orient(src, 3); !isBlue(got.At(0, 0)) {
		t.Fatal("orientation 3: want blue on the left")
	}
}

func TestTransparentPNGOnWhite(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 50, 50)) // fully transparent
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	res, err := Process(&buf, DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	r, g, b, _ := decode(t, res.Large).At(25, 25).RGBA()
	if r < 0xf000 || g < 0xf000 || b < 0xf000 {
		t.Fatalf("transparent pixels must become white, got %d %d %d", r>>8, g>>8, b>>8)
	}
}

func TestRefuses(t *testing.T) {
	photo := jpegBytes(t, twoColor(20, 20))
	cases := []struct {
		name string
		data []byte
		lim  Limits
		want error
	}{
		{"too many bytes", photo, Limits{MaxBytes: 100, MaxPixels: 1 << 30}, ErrTooLarge},
		{"too many pixels (checked before decoding)", photo, Limits{MaxBytes: 1 << 20, MaxPixels: 399}, ErrTooManyPixels},
		{"a GIF", []byte("GIF89a\x01\x00\x01\x00\x00\x00\x00;"), DefaultLimits, ErrUnsupported},
		{"text named .jpg", []byte("<?php system($_GET['c']); ?>"), DefaultLimits, ErrUnsupported},
		{"a JPEG header with garbage", append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, bytes.Repeat([]byte{7}, 64)...), DefaultLimits, ErrCorrupt},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Process(bytes.NewReader(tc.data), tc.lim); !errors.Is(err, tc.want) {
				t.Fatalf("want %v, got %v", tc.want, err)
			}
		})
	}
}
