package widgets

import (
	"image"
	"testing"

	. "go.hasen.dev/shirei"
)

// SoftRenderer BGRA vs RGBA paths must agree after ToRGBA (PixelOrder design).
func TestPixelOrderToRGBAParity(t *testing.T) {
	scene := func() {
		ModAttrs(Background(220, 80, 50, 1), Pad(20), Gap(8))
		Element(Attrs(MinSize(80, 40), Background(140, 70, 45, 1), Corners(8)))
		Element(Attrs(MinSize(60, 20), Background(30, 90, 55, 0.7)))
	}

	render := func(order [4]uint8) *image.RGBA {
		// Headless via public RenderToImage mutates host; set order around it.
		h := GetHost()
		prev := h.PixelOrder
		h.PixelOrder = order
		defer func() { h.PixelOrder = prev }()
		return RenderToImage(200, 120, func() {
			scene()
		})
	}

	a := render(PixelOrderBGRA)
	b := render(PixelOrderRGBA)
	if a.Bounds() != b.Bounds() {
		t.Fatalf("bounds %v vs %v", a.Bounds(), b.Bounds())
	}
	n := 0
	for i := range a.Pix {
		if a.Pix[i] != b.Pix[i] {
			n++
			if n <= 6 {
				t.Logf("byte %d: %d vs %d", i, a.Pix[i], b.Pix[i])
			}
		}
	}
	if n > 0 {
		t.Fatalf("%d bytes differ between BGRA and RGBA SoftRenderer paths", n)
	}
}
