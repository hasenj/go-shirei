package main

import (
	"image"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
)

// loadRGBA loads a PNG path into *image.RGBA.
func loadRGBA(path string) (*image.RGBA, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, err := png.Decode(f)
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

func acceptGolden(golden, actual string) error {
	data, err := os.ReadFile(actual)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(golden), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(golden, data, 0o644); err != nil {
		return err
	}
	_ = os.Remove(actual)
	return nil
}
