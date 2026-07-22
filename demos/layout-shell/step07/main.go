// Layout tutorial step 07: server rail content (column of icons).
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

// Stable dummy "servers" (letter + hue).
var servers = []struct {
	letter string
	hue    float32
}{
	{"A", 10},
	{"B", 40},
	{"C", 120},
	{"D", 200},
	{"E", 280},
	{"F", 320},
}

func main() {
	if len(os.Args) >= 3 && os.Args[1] == "--png" {
		if err := RenderToPNG(os.Args[2], winW, winH, frame); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	app.SetupWindow("Layout shell — step 07", winW, winH)
	app.Run(frame)
}

func frame() {
	// frameFn runs inside the engine root (window-sized column, clipped).
	Container(Attrs(Expand, FixHeight(48), Background(280, 45, 42, 1), Center), func() {
		Label("top bar", FontSize(16), FontWeight(WeightSemibold), TextColor(0, 0, 100, 1))
	})

	Container(Attrs(Row, Grow(1), Expand), func() {
		// Server rail: vertical stack of fixed-size tiles.
		Container(Attrs(FixWidth(72), Expand, Background(260, 40, 32, 1), Pad(8), Gap(8)), func() {
			for _, s := range servers {
				Container(Attrs(FixSize(48, 48), Corners(16), Background(s.hue, 55, 50, 1), Center), func() {
					Label(s.letter, FontSize(18), FontWeight(WeightBold), TextColor(0, 0, 100, 1))
				})
			}
		})

		Container(Attrs(Row, Grow(1), Expand), func() {
			Container(Attrs(FixWidth(240), Expand, Background(220, 30, 52, 1), Center), func() {
				Label("channels", FontSize(16), FontWeight(WeightSemibold), TextColor(0, 0, 100, 1))
			})
			// Keep main subdivided (from step 05) while we fill the server rail.
			Container(Attrs(Grow(1), Expand, Background(200, 20, 78, 1)), func() {
				Container(Attrs(Expand, FixHeight(48), Background(200, 25, 70, 1), Center), func() {
					Label("header", FontSize(14), FontWeight(WeightSemibold), TextColor(0, 0, 100, 1))
				})
				Container(Attrs(Grow(1), Expand, Background(200, 15, 82, 1), Center), func() {
					Label("messages", FontSize(14), FontWeight(WeightSemibold), TextColor(0, 0, 100, 1))
				})
				Container(Attrs(Expand, FixHeight(56), Background(200, 30, 65, 1), Center), func() {
					Label("compose", FontSize(14), FontWeight(WeightSemibold), TextColor(0, 0, 100, 1))
				})
			})
			Container(Attrs(FixWidth(220), Expand, Background(150, 35, 48, 1), Center), func() {
				Label("members", FontSize(16), FontWeight(WeightSemibold), TextColor(0, 0, 100, 1))
			})
		})
	})
}
