// Layout tutorial step 06: name every region (including main's three rows).
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
	app.SetupWindow("Layout shell — step 06", winW, winH)
	app.Run(frame)
}

func frame() {
	Container(Attrs(Expand, FixHeight(48), Background(280, 45, 42, 1), Center), func() {
		Label("top bar", FontSize(16), FontWeight(WeightSemibold), TextColor(0, 0, 100, 1))
	})

	Container(Attrs(Row, Grow(1), Expand), func() {
		Container(Attrs(FixWidth(72), Expand, Background(260, 40, 32, 1), Center, Pad(4)), func() {
			Label("servers", FontSize(12), FontWeight(WeightSemibold), TextColor(0, 0, 100, 1))
		})

		Container(Attrs(Row, Grow(1), Expand), func() {
			Container(Attrs(FixWidth(240), Expand, Background(220, 30, 52, 1), Center), func() {
				Label("channels", FontSize(16), FontWeight(WeightSemibold), TextColor(0, 0, 100, 1))
			})

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
