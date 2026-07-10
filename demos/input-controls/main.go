package main

import (
	"fmt"

	"go.hasen.dev/shirei/app"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

func main() {
	app.SetupWindow("Shirei demo", 800, 600)
	app.Run(frameFn)
}

var num = 0

var name = "Taro"
var email = "taro@example.com"
var address = "حسن عارف الجودي"
var active = true

var opt = "A"
var seg = 10

func frameFn() {
	ModAttrs(Gap(10), Pad(10), Background(0, 0, 90, 1), MinSize(1200, 800))

	Container(Attrs(Row), func() {
		Label("Name:")
		Label(name)
	})
	Container(Attrs(Row), func() {
		Label("Email:")
		Label(email)
	})

	TextInput(&name)
	TextInput(&email)
	TextInput(&address)

	Container(Attrs(Row, Pad(10), Gap(20)), func() {
		if Button(0, "-") {
			num--
		}
		if Button(0, "+") {
			num++
		}
		num = max(0, min(num, 10))
		Container(Attrs(Row, CrossAlign(AlignMiddle), Expand), func() {
			Label(fmt.Sprintf("%d", num))
		})
	})

	Container(Attrs(Spacing(20)), func() {
		OptionButton(&opt, "First Option", "A")
		OptionButton(&opt, "Second Option", "B")
		OptionButton(&opt, "Third Option", "C")
	})

	Container(Attrs(Spacing(20)), func() {
		SegmentedControl(&seg, Cell("X", 10), Cell("Y", 20), Cell("Z", 30))
	})
}
