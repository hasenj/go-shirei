// Package iconimg loads and prepares window-icon images for the native
// backends: decode to straight-alpha NRGBA, downsample, square-pad. Each
// backend converts to its platform pixel layout itself (BGRA DIB, ARGB
// cardinals, premultiplied shm, ...).
package iconimg

import (
	"bytes"
	"image"
	"image/draw"
	_ "image/gif"  // register the formats an icon file may reasonably use
	_ "image/jpeg"
	_ "image/png"
	"os"
)

// LoadNRGBA reads and decodes an image file into straight-alpha RGBA (NRGBA),
// the alpha convention icon surfaces use. Returns nil on any failure.
func LoadNRGBA(path string) *image.NRGBA {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	src, _, err := image.Decode(f)
	if err != nil {
		return nil
	}
	return FromImage(src)
}

// DecodeBytes decodes encoded image bytes (PNG etc., e.g. from go:embed) into
// straight-alpha NRGBA. Returns nil on any failure.
func DecodeBytes(data []byte) *image.NRGBA {
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil
	}
	return FromImage(src)
}

// FromImage converts any image.Image into straight-alpha NRGBA (a fresh copy,
// so later mutation of the source cannot affect the icon). Returns nil for a
// nil or empty image.
func FromImage(src image.Image) *image.NRGBA {
	if src == nil {
		return nil
	}
	b := src.Bounds()
	if b.Dx() <= 0 || b.Dy() <= 0 {
		return nil
	}
	dst := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(dst, dst.Bounds(), src, b.Min, draw.Src)
	return dst
}

// ShrinkToFit downsamples (nearest sample — fine at icon display sizes) so
// neither side exceeds maxSide; images already small enough pass through.
func ShrinkToFit(img *image.NRGBA, maxSide int) *image.NRGBA {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	if w <= maxSide && h <= maxSide {
		return img
	}
	nw, nh := maxSide, h*maxSide/w
	if h > w {
		nw, nh = w*maxSide/h, maxSide
	}
	nw, nh = max(nw, 1), max(nh, 1)
	out := image.NewNRGBA(image.Rect(0, 0, nw, nh))
	for y := 0; y < nh; y++ {
		sy := y * h / nh
		for x := 0; x < nw; x++ {
			sx := x * w / nw
			copy(out.Pix[y*out.Stride+x*4:y*out.Stride+x*4+4],
				img.Pix[sy*img.Stride+sx*4:sy*img.Stride+sx*4+4])
		}
	}
	return out
}

// PadSquare centers the image on a transparent square canvas whose side is the
// larger dimension (xdg-toplevel-icon requires square buffers); already-square
// images pass through.
func PadSquare(img *image.NRGBA) *image.NRGBA {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	if w == h {
		return img
	}
	side := max(w, h)
	out := image.NewNRGBA(image.Rect(0, 0, side, side))
	off := image.Pt((side-w)/2, (side-h)/2)
	draw.Draw(out, img.Bounds().Add(off), img, image.Point{}, draw.Src)
	return out
}
