//go:build ignore

// Generates see_exe's dock icon: a rounded "binary file" tile containing a
// small treemap — one big slate block (the stdlib) and a few hued blocks
// (the deps), echoing the app's header bar.
//
//	go run gen_icon.go icon.png
package main

import (
	"image"
	"image/color"
	"image/png"
	"os"
)

type rrect struct{ x0, y0, x1, y1, r int }

func (r rrect) contains(x, y int) bool {
	if x < r.x0 || x >= r.x1 || y < r.y0 || y >= r.y1 {
		return false
	}
	// corner circle test
	cx, cy := 0, 0
	if x < r.x0+r.r {
		cx = r.x0 + r.r - x
	} else if x >= r.x1-r.r {
		cx = x - (r.x1 - r.r - 1)
	}
	if y < r.y0+r.r {
		cy = r.y0 + r.r - y
	} else if y >= r.y1-r.r {
		cy = y - (r.y1 - r.r - 1)
	}
	return cx*cx+cy*cy <= r.r*r.r
}

func main() {
	const S = 512
	img := image.NewNRGBA(image.Rect(0, 0, S, S))

	outer := rrect{32, 32, 480, 480, 72}
	inner := rrect{46, 46, 466, 466, 58}

	border := color.NRGBA{0x3E, 0x47, 0x53, 0xFF}
	paper := color.NRGBA{0xF4, 0xF6, 0xF8, 0xFF}

	for y := 0; y < S; y++ {
		for x := 0; x < S; x++ {
			switch {
			case inner.contains(x, y):
				img.SetNRGBA(x, y, paper)
			case outer.contains(x, y):
				img.SetNRGBA(x, y, border)
			}
		}
	}

	blocks := []struct {
		x0, y0, x1, y1 int
		c              color.NRGBA
	}{
		{88, 88, 268, 424, color.NRGBA{0x8E, 0x9B, 0xAC, 0xFF}},   // stdlib: big slate
		{286, 88, 424, 246, color.NRGBA{0xB0, 0x7C, 0xD8, 0xFF}},  // purple
		{286, 264, 348, 424, color.NRGBA{0x62, 0x8F, 0xEA, 0xFF}}, // blue
		{366, 264, 424, 338, color.NRGBA{0x4C, 0xBF, 0xAE, 0xFF}}, // teal
		{366, 356, 424, 424, color.NRGBA{0xC7, 0xCE, 0xD7, 0xFF}}, // small gray
	}
	for _, b := range blocks {
		for y := b.y0; y < b.y1; y++ {
			for x := b.x0; x < b.x1; x++ {
				img.SetNRGBA(x, y, b.c)
			}
		}
	}

	f, err := os.Create(os.Args[1])
	if err != nil {
		panic(err)
	}
	if err := png.Encode(f, img); err != nil {
		panic(err)
	}
	f.Close()
}
