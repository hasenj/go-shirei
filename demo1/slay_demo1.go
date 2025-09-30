package main

import (
	app "go.hasen.dev/slay/giobackend"

	. "go.hasen.dev/slay"
	. "go.hasen.dev/slay/tw"
)

func main() {
	app.SetupWindow("Layout DEMO", 800, 600)
	app.Run(frameFn)
}

const g = 4

var brdr = Compose(Bo(0, 0, 0, 0.5), BW(1))

var contSize = Vec2{220, 140}
var clsBtn = TW(MinSize(14, 14), BG(240, 50, 50, 1), BR(2), brdr)
var contProps = Compose(Row, Wrap, Gap(g), Pad(g), BG(0, 0, 80, 1), MinSizeV(contSize), brdr)

func orientationLabel(row bool) string {
	if row {
		return "row"
	} else {
		return "column"
	}
}

func alignmentLabel(a Alignment) string {
	switch a {
	case AlignUnset:
		return "default"
	case AlignStart:
		return "start"
	case AlignMiddle:
		return "middle"
	case AlignEnd:
		return "end"
	}
	return "--"
}

func demoLayoutGrid(row bool, mainAlign Alignment, crossAlign Alignment, n ...int) {
	attrs := TW(contProps, RowF(row), MA(mainAlign), CA(crossAlign))
	Layout(TW(), func() {
		Label(orientationLabel(row))
		Label("main: " + alignmentLabel(mainAlign))
		Label("cross: " + alignmentLabel(crossAlign))
		ac := !row
		Layout(attrs, func() {
			for _, c := range n {
				Layout(TW(Gap(g), RowF(ac)), func() {
					for range c {
						Element(clsBtn)
					}
				})
			}
		})
	})
}

func frameFn() {
	Layout(TW(Row, Clip, Wrap, Pad2(g*2, 40), Gap(g*2), MaxSizeV(WindowSize), brdr), func() {
		ScrollOnInput()

		Layout(TW(), func() {
			Label("row")
			Label("default")
			Label("default")
			Layout(TW(contProps), func() {
				Element(clsBtn)
				Element(clsBtn)
				Layout(TWW(clsBtn, Float(20, 50), Pad2(2, 4)), func() {
					Label("Floating!", Clr(0, 0, 90, 1))
				})
			})
		})

		Layout(TW(), func() {
			Label("row")
			Label("main: default")
			Label("main: default")
			Layout(TW(contProps, MaxWidth(contSize[0])), func() {
				for range 20 {
					Element(clsBtn)
				}
			})
		})

		Layout(TW(), func() {
			Label("row")
			Label("main: middle")
			Label("cross: default")
			Layout(TW(contProps, MA(AlignMiddle), MaxWidth(contSize[0])), func() {
				for range 20 {
					Element(clsBtn)
				}
			})
		})

		Layout(TW(), func() {
			Label("row")
			Label("main: default")
			Label("cross: middle")
			Layout(TW(contProps, CA(AlignMiddle), MaxWidth(contSize[0])), func() {
				for range 20 {
					Element(clsBtn)
				}
			})
		})

		demoLayoutGrid(true, AlignEnd, AlignMiddle, 1, 2, 3)
		demoLayoutGrid(false, AlignEnd, AlignMiddle, 1, 2, 3)
		demoLayoutGrid(true, AlignMiddle, AlignMiddle, 2, 2)
		demoLayoutGrid(false, AlignMiddle, AlignMiddle, 1, 1)

		Layout(TW(), func() {
			Label("row")
			Label("main: middle")
			Label("cross: middle")
			Layout(TW(contProps, CA(AlignMiddle), MA(AlignMiddle)), func() {
				Element(clsBtn)
			})

		})

		Layout(TW(), func() {
			Label("row")
			Label("main: middle")
			Label("cross: middle")
			Layout(TW(contProps, Center), func() {
				Layout(TW(Gap(g)), func() {
					Element(clsBtn)
				})
				Layout(TW(Gap(g)), func() {
					Element(clsBtn)
					Element(clsBtn)
				})
			})
		})

		Layout(TW(), func() {
			Label("row")
			Label("default")
			Label("default")
			Layout(TW(contProps), func() {
				Element(clsBtn)
			})
		})

		Layout(TW(), func() {
			Label("row")
			Label("main: middle")
			Label("cross: end")
			Layout(TW(contProps, CA(AlignEnd), MA(AlignMiddle)), func() {
				Element(clsBtn)
			})
		})
	})
}
