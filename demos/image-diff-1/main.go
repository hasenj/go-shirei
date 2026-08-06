package main

// image-diff-1: layered snapshot diff with a slider that fades shared pixels.
//
// Precompute once into two images:
//   - same layer: only pixels that match in A and B (elsewhere alpha 0)
//   - diff layer: only pixels that differ (red highlight, elsewhere alpha 0)
//
// Paint stacks same (below, Trans from slider) then diff (above, opaque).
// No per-frame pixel work after load.
//
//	go run ./demos/image-diff-1
//
// Images are the widget_gallery golden/actual pair from a forced mismatch.

import (
	"bytes"
	_ "embed"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"log"

	"go.hasen.dev/shirei/app"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

//go:embed a.png
var aPNG []byte

//go:embed b.png
var bPNG []byte

func main() {
	if err := prepareLayers(); err != nil {
		log.Fatal(err)
	}
	app.SetupWindow("Image Diff 1 — layered opacity", 1100, 780)
	app.Run(root)
}

var (
	imgA, imgB       *image.RGBA
	sameLayer        *image.RGBA
	diffLayer        *image.RGBA
	diffCount        int
	// sameVis: 1 = shared pixels fully visible, 0 = fully faded (only diffs remain).
	sameVis float32 = 0.35
)

func prepareLayers() error {
	var err error
	imgA, err = decodeRGBA(aPNG)
	if err != nil {
		return fmt.Errorf("a.png: %w", err)
	}
	imgB, err = decodeRGBA(bPNG)
	if err != nil {
		return fmt.Errorf("b.png: %w", err)
	}
	sameLayer, diffLayer, diffCount = splitSameAndDiff(imgA, imgB)
	return nil
}

func decodeRGBA(data []byte) (*image.RGBA, error) {
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if r, ok := img.(*image.RGBA); ok && r.Bounds().Min == (image.Point{}) {
		return r, nil
	}
	b := img.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(dst, dst.Bounds(), img, b.Min, draw.Src)
	return dst, nil
}

// splitSameAndDiff builds two overlay-ready RGBA images from A (reference) and B.
// Same pixels copy A; differing pixels go on the diff layer as opaque red.
func splitSameAndDiff(a, b *image.RGBA) (same, diff *image.RGBA, nDiff int) {
	w := a.Bounds().Dx()
	h := a.Bounds().Dy()
	if bw := b.Bounds().Dx(); bw > w {
		w = bw
	}
	if bh := b.Bounds().Dy(); bh > h {
		h = bh
	}
	same = image.NewRGBA(image.Rect(0, 0, w, h))
	diff = image.NewRGBA(image.Rect(0, 0, w, h))
	// transparent by default (zero value)

	aw, ah := a.Bounds().Dx(), a.Bounds().Dy()
	bw, bh := b.Bounds().Dx(), b.Bounds().Dy()
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			inA := x < aw && y < ah
			inB := x < bw && y < bh
			var ca, cb color.RGBA
			if inA {
				i := a.PixOffset(x, y)
				ca = color.RGBA{a.Pix[i], a.Pix[i+1], a.Pix[i+2], a.Pix[i+3]}
			}
			if inB {
				i := b.PixOffset(x, y)
				cb = color.RGBA{b.Pix[i], b.Pix[i+1], b.Pix[i+2], b.Pix[i+3]}
			}
			if inA && inB && ca == cb {
				si := same.PixOffset(x, y)
				same.Pix[si+0] = ca.R
				same.Pix[si+1] = ca.G
				same.Pix[si+2] = ca.B
				same.Pix[si+3] = 255
			} else {
				nDiff++
				di := diff.PixOffset(x, y)
				diff.Pix[di+0] = 220
				diff.Pix[di+1] = 40
				diff.Pix[di+2] = 40
				diff.Pix[di+3] = 255
			}
		}
	}
	return same, diff, nDiff
}

func root() {
	ModAttrs(Viewport, Background(220, 8, 96, 1), Pad(20), Gap(14))
	ScrollOnInput()

	Label("Layered image diff", FontWeight(WeightBold), FontSize(18))
	Label("Below: identical pixels only. Above: diffs only (red). Slider sets Trans on the below layer.",
		FontSize(13), TextColor(0, 0, 40, 1))

	total := sameLayer.Bounds().Dx() * sameLayer.Bounds().Dy()
	Label(fmt.Sprintf("%d × %d · %d differing pixels (%.2f%%)",
		sameLayer.Bounds().Dx(), sameLayer.Bounds().Dy(),
		diffCount, 100*float64(diffCount)/float64(total)),
		FontSize(12), TextColor(0, 0, 45, 1))

	Container(Attrs(Row, CrossMid, Gap(12)), func() {
		Label("Shared visibility", FontSize(13), FontWeight(WeightSemibold))
		Slider(&sameVis, SliderAttrs{Min: 0, Max: 1, Width: 280})
		Label(fmt.Sprintf("%.0f%%", sameVis*100), FontSize(13), TextColor(0, 0, 35, 1))
		if ButtonExt("Hide shared", ButtonAttrs{}, DefaultButtonLook()) {
			sameVis = 0
		}
		if ButtonExt("Full context", ButtonAttrs{}, DefaultButtonLook()) {
			sameVis = 1
		}
		if ButtonExt("Dim (35%)", ButtonAttrs{}, DefaultButtonLook()) {
			sameVis = 0.35
		}
	})

	// Main layered view — precomputed layers; only Trans changes per frame.
	viewW, viewH := float32(720), float32(512)
	Container(Attrs(Gap(6)), func() {
		Label("Layered (same below + diffs above)", FontSize(12), FontWeight(WeightBold), TextColor(0, 0, 30, 1))
		// Checker-ish muted board so faded shared pixels don't vanish into white.
		Container(Attrs(FixSize(viewW, viewH), Clip, Corners(6),
			Background(0, 0, 88, 1),
			BorderWidth(1), BorderColor(0, 0, 75, 1),
		), func() {
			// Trans: 0 = opaque, 1 = invisible. sameVis is "how much shared we keep".
			trans := 1 - sameVis
			if trans < 0 {
				trans = 0
			}
			if trans > 1 {
				trans = 1
			}
			// Below: identical pixels; fade via container Transparency.
			Container(Attrs(Float(0, 0), FixSize(viewW, viewH), Trans(trans), NoAnimate, ClickThrough), func() {
				ImageView(UseImage("image-diff-1/same", sameLayer), Vec2{viewW, viewH})
			})
			// Above: diff pixels always full strength.
			Container(Attrs(Float(0, 0), FixSize(viewW, viewH), NoAnimate, ClickThrough), func() {
				ImageView(UseImage("image-diff-1/diff", diffLayer), Vec2{viewW, viewH})
			})
		})
	})

	// Reference: raw A / B for orientation.
	thumb := Vec2{280, 200}
	Container(Attrs(Row, Gap(12), CrossAlign(AlignStart)), func() {
		thumbCard("A (golden)", "image-diff-1/a", imgA, thumb)
		thumbCard("B (actual)", "image-diff-1/b", imgB, thumb)
	})

	ScrollBars()
}

func thumbCard(title, key string, img *image.RGBA, max Vec2) {
	Container(Attrs(Gap(4)), func() {
		Label(title, FontSize(11), FontWeight(WeightBold), TextColor(0, 0, 35, 1))
		Container(Attrs(FixSize(max[0], max[1]), Clip, Corners(4),
			Background(0, 0, 92, 1), BorderWidth(1), BorderColor(0, 0, 80, 1)), func() {
			ImageView(UseImage(key, img), max)
		})
	})
}
