package main

// The app icon, drawn in code: a magnifying glass over stacked "text lines" —
// one highlighted in the app's match-yellow — on a dark rounded tile. It reads
// as "find in files" at a glance, and the magnifier silhouette survives down to
// dock size. Drawn with plain image ops (antialiased signed-distance fills)
// rather than a shirei render because the icon needs real alpha outside the
// rounded tile (the software renderer clears to opaque white), and pixel-level
// drawing keeps it free of asset files.

import (
	"image"
	"image/color"
	"math"
)

const iconSize = 256

func appIcon() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, iconSize, iconSize))

	tile := color.NRGBA{36, 42, 64, 255}         // dark indigo-slate
	textLine := color.NRGBA{196, 206, 222, 255}  // light slate
	matchLine := color.NRGBA{255, 243, 194, 255} // the app's pale-yellow match hue
	glass := color.NRGBA{236, 241, 248, 255}     // the magnifier

	fillRoundedRect(img, 16, 16, 224, 224, 52, tile)

	// stacked text lines — some sit under the lens (searched content), one
	// highlighted like a match
	lines := []struct {
		x, y, w int
		c       color.NRGBA
	}{
		{52, 72, 120, textLine},
		{52, 100, 96, matchLine},
		{52, 128, 132, textLine},
		{52, 156, 104, textLine},
	}
	for _, l := range lines {
		fillRoundedRect(img, l.x, l.y, l.w, 14, 7, l.c)
	}

	// magnifying glass
	const cx, cy, outerR, thick = 156, 116, 60, 17
	fillCircle(img, cx, cy, outerR-thick, color.NRGBA{255, 255, 255, 22}) // faint glass tint
	strokeRing(img, cx, cy, outerR, thick, glass)
	fillCapsule(img, 198, 158, 228, 188, 22, glass) // handle from the ring's outer edge

	return img
}

func fillRoundedRect(img *image.RGBA, x, y, w, h, radius int, c color.NRGBA) {
	cx := float64(x) + float64(w)/2
	cy := float64(y) + float64(h)/2
	hw := float64(w)/2 - float64(radius)
	hh := float64(h)/2 - float64(radius)
	for py := y - 1; py <= y+h; py++ {
		for px := x - 1; px <= x+w; px++ {
			dx := math.Max(math.Abs(float64(px)+0.5-cx)-hw, 0)
			dy := math.Max(math.Abs(float64(py)+0.5-cy)-hh, 0)
			dist := math.Hypot(dx, dy) - float64(radius)
			cover := math.Min(math.Max(0.5-dist, 0), 1)
			if cover > 0 {
				blendPixel(img, px, py, c, cover*float64(c.A)/255)
			}
		}
	}
}

func fillCircle(img *image.RGBA, cx, cy, r int, c color.NRGBA) {
	for py := cy - r - 1; py <= cy+r+1; py++ {
		for px := cx - r - 1; px <= cx+r+1; px++ {
			dist := math.Hypot(float64(px)+0.5-float64(cx), float64(py)+0.5-float64(cy)) - float64(r)
			cover := math.Min(math.Max(0.5-dist, 0), 1)
			if cover > 0 {
				blendPixel(img, px, py, c, cover*float64(c.A)/255)
			}
		}
	}
}

// strokeRing draws an antialiased annulus of the given centerline radius and
// thickness (the magnifier's lens rim).
func strokeRing(img *image.RGBA, cx, cy, radius, thick int, c color.NRGBA) {
	half := float64(thick) / 2
	for py := cy - radius - thick; py <= cy+radius+thick; py++ {
		for px := cx - radius - thick; px <= cx+radius+thick; px++ {
			dc := math.Abs(math.Hypot(float64(px)+0.5-float64(cx), float64(py)+0.5-float64(cy)) - float64(radius))
			cover := math.Min(math.Max(0.5-(dc-half), 0), 1)
			if cover > 0 {
				blendPixel(img, px, py, c, cover*float64(c.A)/255)
			}
		}
	}
}

// fillCapsule draws an antialiased thick line segment with round caps (the
// magnifier's handle), using the distance from each pixel to the segment.
func fillCapsule(img *image.RGBA, x0, y0, x1, y1, thick int, c color.NRGBA) {
	half := float64(thick) / 2
	ax, ay := float64(x0), float64(y0)
	bx, by := float64(x1), float64(y1)
	dx, dy := bx-ax, by-ay
	ll := dx*dx + dy*dy
	lo := int(math.Min(ax, bx) - half - 1)
	hi := int(math.Max(ax, bx) + half + 1)
	top := int(math.Min(ay, by) - half - 1)
	bot := int(math.Max(ay, by) + half + 1)
	for py := top; py <= bot; py++ {
		for px := lo; px <= hi; px++ {
			fx, fy := float64(px)+0.5, float64(py)+0.5
			t := 0.0
			if ll > 0 {
				t = math.Min(math.Max(((fx-ax)*dx+(fy-ay)*dy)/ll, 0), 1)
			}
			dist := math.Hypot(fx-(ax+t*dx), fy-(ay+t*dy)) - half
			cover := math.Min(math.Max(0.5-dist, 0), 1)
			if cover > 0 {
				blendPixel(img, px, py, c, cover*float64(c.A)/255)
			}
		}
	}
}

// blendPixel composites c over the pixel at coverage-scaled alpha a. img.Pix is
// premultiplied RGBA, so "source over" is src*a + dst*(1-a) on every channel.
func blendPixel(img *image.RGBA, x, y int, c color.NRGBA, a float64) {
	if !(image.Point{x, y}).In(img.Rect) {
		return
	}
	i := img.PixOffset(x, y)
	p := img.Pix[i : i+4 : i+4]
	p[0] = byte(float64(c.R)*a + float64(p[0])*(1-a) + 0.5)
	p[1] = byte(float64(c.G)*a + float64(p[1])*(1-a) + 0.5)
	p[2] = byte(float64(c.B)*a + float64(p[2])*(1-a) + 0.5)
	p[3] = byte(255*a + float64(p[3])*(1-a) + 0.5)
}
