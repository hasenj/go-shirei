package main

import (
	_ "embed"
)

// Dock icon (magnifying glass over match-highlighted lines). Generated with
// Imagine; embedded so `go run` works from any directory.
//
//go:embed icon.png
var iconPNG []byte
