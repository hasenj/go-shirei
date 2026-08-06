// Command behavior_runner is a list-only GUI for shirei/behavior_test programs.
//
//	go run ./cmd/behavior_runner          # from the shirei module root
//
// Discovers behavior_test/*/main.go (skips btmode). On Run all, builds every
// package ahead via `go build` while tests still execute one-by-one
// (--window --drive --close). Double-click a row for the log and re-run modes.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"go.hasen.dev/shirei/app"
)

func main() {
	root, err := findShireiRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	tests, err := discoverTests(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if len(tests) == 0 {
		fmt.Fprintln(os.Stderr, "no behavior_test packages found under", root)
		os.Exit(1)
	}

	state := newAppState(root, tests)
	app.SetupWindow("behavior_runner", 640, 720)
	app.Run(state.rootView)
}

func findShireiRoot() (string, error) {
	start, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := start
	for {
		bt := filepath.Join(dir, "behavior_test")
		mod := filepath.Join(dir, "go.mod")
		if st, err := os.Stat(bt); err == nil && st.IsDir() {
			if _, err := os.Stat(mod); err == nil {
				return dir, nil
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("could not find shirei root (dir with behavior_test/ + go.mod) from %s", start)
}
