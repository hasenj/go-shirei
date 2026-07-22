// Layout tutorial step 01: paint the engine root.
//
//	go run . --png out.png
package main

import (
	"fmt"
	"os"

	"go.hasen.dev/shirei/app"

	. "go.hasen.dev/shirei"
)

const winW, winH = 1100, 720

func main() {
	if len(os.Args) >= 3 && os.Args[1] == "--png" {
		if err := RenderToPNG(os.Args[2], winW, winH, frame); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	app.SetupWindow("Layout shell — step 01", winW, winH)
	app.Run(frame)
}

func frame() {
	// frameFn already runs inside an engine-created root: Min/Max/resolved size
	// = WindowSize, Clip = true. You do not add a Viewport "window root".
	// Style the current container (the root) before adding children:
	ModAttrs(Background(220, 20, 88, 1))
}
