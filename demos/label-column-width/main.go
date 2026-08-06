package main

import (
	"fmt"
	"os"

	"go.hasen.dev/generic"
	"go.hasen.dev/shirei/app"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

const winW, winH = 600, 500

func main() {
	if len(os.Args) >= 3 && os.Args[1] == "--png" {
		if err := RenderToPNG(os.Args[2], winW, winH, appView); err != nil {
			fmt.Fprintln(os.Stderr, "render to png failed:", err)
			os.Exit(1)
		}
		return
	}
	app.SetupWindow("Manually Aligned Column Width", winW, winH)
	app.Run(appView)
}

type DataRow struct {
	label  string
	height float32
}

var rows = []DataRow{
	{"Test", 40},
	{"Label", 120},
	{"Long Label", 80},
}

func appView() {
	Container(Attrs(Expand), func() {
		var maxLabelWidth = UseWithDefault("max-label-width", float32(300))
		var computedMaxWidth = float32(0)
		var delIdx = -1
		for idx, row := range rows {
			Container(Attrs(Row, Expand), func() {

				// Label column (the column whose size we want to align)
				Container(Attrs(MinWidth(*maxLabelWidth), Grow(0), Background(0, 0, 70, 1)), func() {
					// Open a new container so that we can isolate this container's sizing
					// from the parent that has a MinWidth applied.
					//
					// Because we want to get the resolved size of this element, and if
					// MinWidth is set on it, the size will always be at least MinWidth
					//
					// This isolation lets can measure this element without being influenced
					// by the MinWidth
					Container(Attrs(), func() {
						Label(row.label)
					})
					width := GetLastSize()[0] // GetLastSize returns the size of the container that just closed
					if computedMaxWidth < width {
						computedMaxWidth = width
					}
				})

				// content — stand in for some generic other column(s) with potentially
				// variable heights that we cannot control; so we just use an empty box
				// with a set height
				Container(Attrs(Grow(1)), func() {
					Element(Attrs(Grow(1), Expand, MinWidth(400), MinHeight(row.height), Background(float32(idx)*50, 80, 60, 1)))
				})

				Spacer(6)
				if Button(SymBoxCross, "") {
					delIdx = idx
				}
			})
		}

		if delIdx != -1 {
			generic.RemoveAt(&rows, delIdx, 1)
		}

		if *maxLabelWidth != computedMaxWidth {
			fmt.Printf("stored: %f, actual: %f \n", *maxLabelWidth, computedMaxWidth)
			*maxLabelWidth = computedMaxWidth
			RequestStabilize()
		}
	})
	Filler(1)
	Container(Attrs(Spacing(20)), func() {
		Label("New Row:", FontWeight(WeightBold))
		Container(Attrs(), func() {
			var next = Use[DataRow]("data-row")
			Label("Label:")
			TextInput(&next.label)
			Label("Height:")
			Slider(&next.height, SliderAttrs{
				Min: 0, Max: 200, Step: 1, Width: 200,
			})
			Label(fmt.Sprintf("%.1fpx", next.height))
			var enabled = len(next.label) > 0
			if Button(SymPlus, "Add Row") {
				if enabled {
					rows = append(rows, *next)
					next = new(DataRow)
				}
			}
		})
	})
}
