// Command shirei_tester is an IDE-style snapshot test runner (usually for
// Shirei UI tests, works for any Go module that follows the same patterns).
//
//	# from a module or monorepo workspace root:
//	go run ./cmd/shirei_tester
//	go run ./cmd/shirei_tester /path/to/module
//
// Discovers packages by walking the filesystem for snapshot markers in
// *_test.go, lists Test* from source, runs with `go test -json` and optional
// SHIREI_SNAP_REPORT for golden/actual/diff + Accept.
package main

import (
	"fmt"
	"os"

	"go.hasen.dev/shirei/app"
)

func main() {
	start, _ := os.Getwd()
	if len(os.Args) > 1 && !stringsHasDash(os.Args[1]) {
		start = os.Args[1]
	}
	root, err := findScanRoot(start)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	state := &AppState{
		Root: root, SelPkg: 0, SelTest: -1, SelSnap: -1,
		// Diff highlight is a full-res pixel pass — opt-in via checkbox.
		ShowWipeDiffHL: false, Scanning: true,
	}
	// Discover off the main path so the window opens immediately.
	// (go list + per-package test listing used to block for several seconds.)
	go state.scanPackages(root)

	app.SetupWindow("shirei_tester", 1100, 720)
	app.Run(func() {
		state.rootView()
	})
}

func stringsHasDash(s string) bool {
	return len(s) > 0 && s[0] == '-'
}
