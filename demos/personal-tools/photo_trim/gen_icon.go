//go:build ignore

package main

import (
	"log"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

func main() {
	GetHost().TransparentBackground = true
	err := RenderToPNG("icon.png", 512, 512, func() {
		Container(Attrs(Viewport, Center), func() {
			Container(Attrs(
				FixSize(412, 412), Corners(92),
				Background(208, 72, 48, 1), Grad(-18, 8, -10, 0), Center,
			), func() {
				Icon(TypCamera, FontSize(224), TextColor(0, 0, 99, 1))
			})
		})
	})
	if err != nil {
		log.Fatal(err)
	}
}
