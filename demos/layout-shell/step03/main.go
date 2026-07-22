// Layout tutorial step 03: body splits into server rail + rest (Row).
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
	app.SetupWindow("Layout shell — step 03", winW, winH)
	app.Run(frame)
}

func frame() {
	// frameFn runs inside the engine root (window-sized column, clipped).
	Container(Attrs(Expand, FixHeight(48), Background(280, 45, 42, 1)), func() {})

	// Body is a row: fixed-width rail + growing rest.
	Container(Attrs(Row, Grow(1), Expand), func() {
		Container(Attrs(FixWidth(72), Expand, Background(260, 40, 32, 1)), func() {})
		Container(Attrs(Grow(1), Expand, Background(210, 25, 72, 1)), func() {})
	})
}
