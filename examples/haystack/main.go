// haystack: a "find in files" utility built on shirei.
//
// Point it at a folder, type a search term, and matching lines stream into a
// virtual list as they are found — each row is a file:line header (with copy
// and open-in-editor buttons) over a few lines of surrounding context. The
// search is pure Go (no ripgrep/grep subprocess): a naive parallel walk that
// reads text files and scans them line by line, run entirely in the
// background so the UI never blocks.
//
// Usage:
//
//	haystack                       open the GUI on the current directory
//	haystack --png out.png [query] render one headless frame (optionally
//	                               after running `query` to completion)
package main

import (
	"fmt"
	"os"

	app "go.hasen.dev/shirei/app"

	. "go.hasen.dev/shirei"
)

const (
	winW = 1000
	winH = 720
)

func main() {
	cwd, _ := os.Getwd()
	appData.pathInput = cwd
	appData.editors = detectEditors()

	if len(os.Args) >= 3 && os.Args[1] == "--png" {
		if len(os.Args) >= 4 {
			appData.query = os.Args[3]
		}
		if appData.query != "" {
			// Run to completion synchronously so the frame has results, and
			// open it as the active tab.
			s := searchSync(currentParams())
			appData.searches = []*Search{s}
			appData.active = s
		}
		if err := RenderToPNG(os.Args[2], winW, winH, RootView); err != nil {
			fmt.Fprintln(os.Stderr, "render to png failed:", err)
			os.Exit(1)
		}
		return
	}

	app.SetupWindow("haystack", winW, winH)
	app.SetupIconBytes(iconPNG)
	app.Run(RootView)
}
