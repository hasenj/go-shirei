package main

import (
	_ "embed"
)

// Dock icon (piano keys). Generated with Imagine; embedded so `go run`
// works from any directory.
//
//go:embed icon.png
var iconPNG []byte
