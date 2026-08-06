// small is the landing-page sample: a name field, Hello button, and a
// background toggle. Same program shown in the judi.systems/shirei hero.
//
//	go run ./demos/small
//	go run ./cmd/shirei_web -o ../static-sites/judi.systems/shirei/try ./demos/small
package main

import (
	"fmt"
	"os"

	"go.hasen.dev/shirei/app"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

const winW, winH = 400, 200

func main() {
	if len(os.Args) >= 3 && os.Args[1] == "--png" {
		if err := RenderToPNG(os.Args[2], winW, winH, RootView); err != nil {
			fmt.Fprintln(os.Stderr, "render to png failed:", err)
			os.Exit(1)
		}
		return
	}
	app.SetupWindow("Small Demo", winW, winH)
	app.Run(RootView)
}

var name string
var response = "Please give me your name"
var colorBG bool = true

func RootView() {
	ModAttrs(Gap(10), Background(220, 20, 97, 1))
	if colorBG {
		ModAttrs(Background(220, 20, 94, 1))
	}

	Container(Attrs(Pad(20), Gap(10)), func() {
		Label("Name:", FontWeight(WeightBold))
		Container(Attrs(Row, CrossMid, Gap(10)), func() {
			TextInput(&name)
			if Button(SymInfo, "Hello") {
				if name == "" {
					response = "Uh, sorry who are you again?"
				} else {
					response = "Well, hello " + name + "!"
				}
			}
		})
		Label(response)
	})

	Container(Attrs(Expand, Row, CrossMid, Pad2(10, 30), Gap(10), Background(0, 0, 98, 1),
		BoxShadow(2), BorderWidth(1), BorderColor(0, 0, 0, 1)), func() {
		ToggleSwitch(&colorBG)
		Label("Subtle Color Background")
	})
}
