package main

import (
	"fmt"

	app "go.hasen.dev/shirei/app"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

func main() {
	app.SetupWindow("My App", 300, 100)
	app.Run(RootView)
}

var count int

func RootView() {
	Container(Attrs(Viewport, Background(220, 10, 97, 1)), func() {
		Container(Attrs(Row, CrossMid, Pad(20), Gap(10)), func() {
			Label(fmt.Sprintf("Counter: %d", count))
			if Button(SymIPlus, "Increment") {
				count++
			}
		})
	})
}

