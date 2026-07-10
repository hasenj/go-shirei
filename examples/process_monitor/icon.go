package main

// The app icon, drawn in code: three usage bars on a dark rounded tile —
// the same motif and palette as the app's header and CPU bars. Drawn with
// plain image ops rather than a shirei render because the icon needs real
// alpha outside the rounded tile (the software renderer clears to opaque
// white), and pixel-level drawing keeps it free of asset files.

import (
	"image"
	"image/color"
	"math"
)

const iconSize = 256

func appIcon() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, iconSize, iconSize))

	// tile: dark slate blue, like the app header (hsl 220, 25%, 18%)
	fillRoundedRect(img, 16, 16, 224, 224, 52, color.NRGBA{34, 42, 57, 255})

	// bars, bottom-aligned on a shared baseline: teal (the memory gauge),
	// orange (the CPU bars), light slate (an idle track)
	const barW, gap, baseline = 40, 22, 196
	x := (iconSize - 3*barW - 2*gap) / 2
	fillRoundedRect(img, x, baseline-88, barW, 88, 12, color.NRGBA{60, 221, 221, 255})
	fillRoundedRect(img, x+barW+gap, baseline-136, barW, 136, 12, color.NRGBA{224, 96, 41, 255})
	fillRoundedRect(img, x+2*(barW+gap), baseline-56, barW, 56, 12, color.NRGBA{176, 186, 203, 255})

	return img
}

// fillRoundedRect alpha-blends an antialiased rounded rectangle onto img,
// using the signed distance to the rounded-rect boundary for edge coverage.
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
			if cover <= 0 {
				continue
			}
			a := cover * float64(c.A) / 255
			blendPixel(img, px, py, c, a)
		}
	}
}

// blendPixel composites the color over the pixel at coverage-scaled alpha a.
// img.Pix is premultiplied RGBA, so "source over" is src*a + dst*(1-a) on
// every channel.
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
