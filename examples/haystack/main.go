// haystack: a "find in files" utility built on shirei.
//
// Point it at a folder, type a search term, and matching lines stream into a
// virtual list as they are found. File headers group context snippets with
// copy-path and open-in-editor actions. Matching uses go.hasen.dev/textsearch
// (pure Go, no ripgrep/grep subprocess); the GUI
// fans file work across a worker pool in the background so the UI never blocks.
//
// Usage:
//
//	haystack                       open the GUI on the current directory
//	haystack -png out.png          render one headless frame
//	haystack -gitignore -query func [path]
//	                               open the GUI and start that search
package main

import (
	"flag"
	"fmt"
	"os"

	app "go.hasen.dev/shirei/app"

	. "go.hasen.dev/shirei"
)

const (
	winW = 1200
	winH = 820
)

func main() {
	cwd, _ := os.Getwd()
	appData.pathInput = cwd
	appData.editors = detectEditors()

	png := flag.String("png", "", "write one settled frame to PATH and exit")
	gitignore := flag.Bool("gitignore", false, "honor .gitignore")
	query := flag.String("query", "", "start this search")
	flag.Parse()
	if flag.NArg() > 0 {
		appData.pathInput = flag.Arg(0)
	}
	appData.gitignore = *gitignore
	appData.query = *query

	if *png != "" {
		if appData.query != "" {
			// Run to completion synchronously so the frame has results, and
			// open it as the active tab.
			s := searchSync(currentParams())
			appData.searches = []*Search{s}
			appData.active = s
		}
		if err := RenderToPNG(*png, winW, winH, RootView); err != nil {
			fmt.Fprintln(os.Stderr, "render to png failed:", err)
			os.Exit(1)
		}
		return
	}

	if appData.query != "" {
		runNewSearch(currentParams())
	}

	app.SetupWindow("haystack", winW, winH)
	app.SetupIconBytes(iconPNG)
	app.SetupDrive()
	app.Run(RootView)
}
