package main

import (
	"fmt"
	"strconv"

	app "go.hasen.dev/shirei/giobackend"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/tw"
	. "go.hasen.dev/shirei/widgets"
)

func main() {
	app.SetupWindow("°C to °F", 300, 200)
	app.Run(frameFn)
}

var input string

func frameFn() {
	ModAttrs(Spacing(10))
	Label("Celcius:")
	TextInput(&input)

	var label string
	out, err := strconv.ParseFloat(input, 32)
	if err != nil {
		label = "..."
	} else {
		label = fmt.Sprintf("%.2f Fahrenheit", out*9/5+32)
	}
	Label(label)
}
