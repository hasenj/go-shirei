package main

// The app icon, drawn by the app itself: shirei renders a little scene
// — a white ferry on a blue sea in a dock-style rounded tile — into
// pixels once at startup (RunGUI hands it to app.SetupIconImage), and
// the corners are punched transparent afterwards. No asset files; the
// icon is code like everything else, and the golden pins it.

import (
	"image"
	"math"
	"sync"

	. "go.hasen.dev/shirei"
)

const iconSize = 512
const iconCorner = 116 // ≈ the macOS dock tile curvature

// AppIconView draws the 512×512 scene with plain shirei containers:
// gradient sea-sky, sun, a two-deck ferry, three waves.
func AppIconView() {
	white := func(a f32) AttrsFn { return Background(0, 0, 100, a) }
	sea := Background(215, 64, 44, 1)
	Container(Attrs(Viewport, sea, Grad(0, 8, -14, 0)), func() {
		// sun, high right
		Element(Attrs(Float(342, 64), FixWidth(70), FixHeight(70), Corners(35), white(0.9)))
		// cabin deck with three windows
		Container(Attrs(Float(176, 218), FixWidth(160), FixHeight(72), Corners(12), white(1)), func() {
			for i := range 3 {
				Element(Attrs(Float(20+f32(i)*45, 21), FixWidth(30), FixHeight(30), Corners(7), sea))
			}
		})
		// hull, rounded toward the waterline
		Element(Attrs(Float(126, 288), FixWidth(260), FixHeight(82), Corners4(10, 10, 42, 42), white(1)))
		// waves, staggered
		Element(Attrs(Float(62, 398), FixWidth(96), FixHeight(16), Corners(8), white(0.55)))
		Element(Attrs(Float(206, 412), FixWidth(116), FixHeight(16), Corners(8), white(0.4)))
		Element(Attrs(Float(370, 398), FixWidth(84), FixHeight(16), Corners(8), white(0.55)))
	})
}

var iconOnce sync.Once
var iconImg *image.RGBA

func appIcon() *image.RGBA {
	iconOnce.Do(func() {
		iconImg = RenderToImage(iconSize, iconSize, AppIconView)
		punchCorners(iconImg, iconCorner)
	})
	return iconImg
}

// punchCorners makes everything outside the rounded square transparent
// (the renderer clears opaque): outside each corner circle, pixels zero
// out; a ~1px band around the arc blends for a soft edge. Premultiplied,
// so all four channels scale together.
func punchCorners(img *image.RGBA, r int) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	rf := f32(r)
	for y := 0; y < r; y++ {
		for x := 0; x < r; x++ {
			// distance from the corner-circle center, top-left corner
			dx := rf - f32(x) - 0.5
			dy := rf - f32(y) - 0.5
			d := dx*dx + dy*dy
			if d <= (rf-1)*(rf-1) {
				continue
			}
			factor := f32(0)
			if d < rf*rf {
				factor = rf - f32(math.Sqrt(float64(d))) // 0..1 across the edge band
			}
			for _, pt := range [4][2]int{
				{x, y}, {w - 1 - x, y}, {x, h - 1 - y}, {w - 1 - x, h - 1 - y},
			} {
				i := img.PixOffset(b.Min.X+pt[0], b.Min.Y+pt[1])
				for c := 0; c < 4; c++ {
					img.Pix[i+c] = uint8(f32(img.Pix[i+c]) * factor)
				}
			}
		}
	}
}
