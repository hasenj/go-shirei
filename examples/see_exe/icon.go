package main

import (
	_ "embed"
)

// The dock icon: a small treemap inside a rounded file tile — one big slate
// block (the stdlib) and a few hued blocks (the deps), echoing the header
// bar. Committed as an asset and embedded (the app runs from arbitrary
// directories, so there's no stable path to load it from). Regenerate with
// `go run gen_icon.go icon.png`.
//
//go:embed icon.png
var iconPNG []byte
