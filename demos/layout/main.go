package main

import (
	"fmt"
	"os"

	"go.hasen.dev/shirei/app"

	. "go.hasen.dev/shirei"
)

const winW, winH = 800, 600

func main() {
	if len(os.Args) >= 3 && os.Args[1] == "--png" {
		if err := RenderToPNG(os.Args[2], winW, winH, frameFn); err != nil {
			fmt.Fprintln(os.Stderr, "render to png failed:", err)
			os.Exit(1)
		}
		return
	}
	app.SetupWindow("Layout DEMO", winW, winH)
	app.Run(frameFn)
}

const g = 4

var brdr = ComposeAttrs(BorderColor(0, 0, 0, 0.5), BorderWidth(1))

var contSize = Vec2{220, 140}
var clsBtn = Attrs(MinSize(14, 14), Background(240, 50, 50, 1), Corners(2), brdr)
var contProps = ComposeAttrs(Row, Wrap, Gap(g), Pad(g), Background(0, 0, 80, 1), MinSizeVec(contSize), brdr)

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
	attrs := Attrs(contProps, RowF(row), MainAlign(mainAlign), CrossAlign(crossAlign))
	Container(Attrs(), func() {
		Label(orientationLabel(row))
		Label("main: " + alignmentLabel(mainAlign))
		Label("cross: " + alignmentLabel(crossAlign))
		ac := !row
		Container(attrs, func() {
			for _, c := range n {
				Container(Attrs(Gap(g), RowF(ac)), func() {
					for range c {
						Element(clsBtn)
					}
				})
			}
		})
	})
}

func frameFn() {
	Container(Attrs(Row, Clip, Wrap, Pad2(g*2, 40), Gap(g*2), MaxSizeVec(GetHost().WindowSize), brdr), func() {
		ScrollOnInput()

		Container(Attrs(), func() {
			Label("row")
			Label("default")
			Label("default")
			Container(Attrs(contProps), func() {
				Element(clsBtn)
				Element(clsBtn)
				Container(AttrsWith(clsBtn, Float(20, 50), Pad2(2, 4)), func() {
					Label("Floating!", TextColor(0, 0, 90, 1))
				})
			})
		})

		Container(Attrs(), func() {
			Label("row")
			Label("main: default")
			Label("main: default")
			Container(Attrs(contProps, MaxWidth(contSize[0])), func() {
				for range 20 {
					Element(clsBtn)
				}
			})
		})

		Container(Attrs(), func() {
			Label("row")
			Label("main: middle")
			Label("cross: default")
			Container(Attrs(contProps, MainAlign(AlignMiddle), MaxWidth(contSize[0])), func() {
				for range 20 {
					Element(clsBtn)
				}
			})
		})

		Container(Attrs(), func() {
			Label("row")
			Label("main: default")
			Label("cross: middle")
			Container(Attrs(contProps, CrossAlign(AlignMiddle), MaxWidth(contSize[0])), func() {
				for range 20 {
					Element(clsBtn)
				}
			})
		})

		demoLayoutGrid(true, AlignEnd, AlignMiddle, 1, 2, 3)
		demoLayoutGrid(false, AlignEnd, AlignMiddle, 1, 2, 3)
		demoLayoutGrid(true, AlignMiddle, AlignMiddle, 2, 2)
		demoLayoutGrid(false, AlignMiddle, AlignMiddle, 1, 1)

		Container(Attrs(), func() {
			Label("row")
			Label("main: middle")
			Label("cross: middle")
			Container(Attrs(contProps, CrossAlign(AlignMiddle), MainAlign(AlignMiddle)), func() {
				Element(clsBtn)
			})

		})

		Container(Attrs(), func() {
			Label("row")
			Label("main: middle")
			Label("cross: middle")
			Container(Attrs(contProps, Center), func() {
				Container(Attrs(Gap(g)), func() {
					Element(clsBtn)
				})
				Container(Attrs(Gap(g)), func() {
					Element(clsBtn)
					Element(clsBtn)
				})
			})
		})

		Container(Attrs(), func() {
			Label("row")
			Label("default")
			Label("default")
			Container(Attrs(contProps), func() {
				Element(clsBtn)
			})
		})

		Container(Attrs(), func() {
			Label("row")
			Label("main: middle")
			Label("cross: end")
			Container(Attrs(contProps, CrossAlign(AlignEnd), MainAlign(AlignMiddle)), func() {
				Element(clsBtn)
			})
		})
	})
}
