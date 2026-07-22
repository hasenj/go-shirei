// Layout tutorial step 02: top bar + body (column + Grow).
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
	app.SetupWindow("Layout shell — step 02", winW, winH)
	app.Run(frame)
}

func frame() {
	// Default container axis is column (vertical).
	// frameFn runs inside the engine root (window-sized column, clipped).
	// Top bar: fixed height, does not grow.
	Container(Attrs(Expand, FixHeight(48), Background(280, 45, 42, 1)), func() {})

	// Body: Grow(1) takes all remaining height on the main axis.
	Container(Attrs(Grow(1), Expand, Background(210, 25, 72, 1)), func() {})
}
