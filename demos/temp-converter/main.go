package main

import (
	"fmt"
	"os"
	"strconv"

	"go.hasen.dev/shirei/app"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

const winW, winH = 300, 200

func main() {
	if len(os.Args) >= 3 && os.Args[1] == "--png" {
		if err := RenderToPNG(os.Args[2], winW, winH, frameFn); err != nil {
			fmt.Fprintln(os.Stderr, "render to png failed:", err)
			os.Exit(1)
		}
		return
	}
	app.SetupWindow("°C to °F", winW, winH)
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
