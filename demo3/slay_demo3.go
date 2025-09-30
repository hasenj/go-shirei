package main

import (
	"fmt"

	app "go.hasen.dev/slay/giobackend"

	. "go.hasen.dev/slay"
	. "go.hasen.dev/slay/tw"
	. "go.hasen.dev/slay/widgets"
)

func main() {
	app.SetupWindow("Slay DEMO", 800, 600)
	app.Run(frameFn)
}

var num = 0

var name = "Taro"
var email = "taro@example.com"
var address = "حسن عارف الجودي"
var active = true

func frameFn() {
	ModAttrs(Gap(10), Pad(10), BG(0, 0, 90, 1), MinSize(1200, 800))

	Layout(TW(Row), func() {
		Label("Name:")
		Label(name)
	})
	Layout(TW(Row), func() {
		Label("Email:")
		Label(email)
	})

	TextInput(&name)
	TextInput(&email)
	TextInput(&address)

	Layout(TW(Row, Pad(10), Gap(20)), func() {
		if Button(0, "-") {
			num--
		}
		if Button(0, "+") {
			num++
		}
		num = max(0, min(num, 10))
		Layout(TW(Row, CA(AlignMiddle), Expand), func() {
			Label(fmt.Sprintf("%d", num))
		})
	})
}
