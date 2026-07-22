package main

import (
	"bytes"
	"image"
	"image/png"
	"sync"

	_ "embed"
)

// Dock icon (ferry on blue water). Generated with Imagine; embedded so
// `go run` works from any directory.
//
//go:embed icon.png
var iconPNG []byte

var (
	iconOnce sync.Once
	iconImg  *image.RGBA
)

// appIcon decodes the embedded PNG once (used by the snapshot test and by
// callers that want an image.Image rather than raw bytes).
func appIcon() *image.RGBA {
	iconOnce.Do(func() {
		img, err := png.Decode(bytes.NewReader(iconPNG))
		if err != nil {
			panic("ferry: decode embedded icon: " + err.Error())
		}
		// Normalize to *image.RGBA for the golden / alpha checks.
		b := img.Bounds()
		rgba := image.NewRGBA(b)
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				rgba.Set(x, y, img.At(x, y))
			}
		}
		iconImg = rgba
	})
	return iconImg
}
