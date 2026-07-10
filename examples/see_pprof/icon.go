package main

import (
	_ "embed"
)

// The dock icon: a tiny flame graph, committed as an asset and embedded
// into the binary (the app runs via `go run` from arbitrary directories,
// so there's no stable path to load it from). Originally generated
// programmatically — the generator lives in git history at commit aed3569
// if it ever needs regenerating.
//
//go:embed icon.png
var iconPNG []byte
