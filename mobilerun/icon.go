package main

import (
	"bytes"
	_ "embed"
	"image"
	"image/draw"
	_ "image/png"
)

// Dock icon: Shirei flower logo over a phone silhouette. Generated with
// Imagine from the project logo; embedded so `go run ./mobilerun` works
// from any directory.
//
//go:embed icon.png
var iconPNG []byte

// appIcon is the embedded dock icon as RGBA for in-UI ImageView.
var appIcon *image.RGBA

func init() {
	img, _, err := image.Decode(bytes.NewReader(iconPNG))
	if err != nil {
		return
	}
	b := img.Bounds()
	appIcon = image.NewRGBA(b)
	draw.Draw(appIcon, b, img, b.Min, draw.Src)
}
