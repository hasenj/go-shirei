// window-ctrl demo: control window placement, centering, and minimum dimensions.
//
//	go run ./shirei/demos/window-ctrl
package main

import (
	"fmt"

	"go.hasen.dev/shirei/app"
	"go.hasen.dev/shirei/ext/window"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

const winW, winH = 580, 440

var (
	minW float32 = 400
	minH float32 = 300
)

func main() {
	app.SetupWindow("Window Control Demo", winW, winH)

	// Enforce initial minimum size and center the window on launch
	window.SetMinSize(minW, minH)
	window.Center()

	app.Run(RootView)
}

func RootView() {
	// Dynamically update enforced minimum size
	window.SetMinSize(minW, minH)

	ws := GetHost().WindowSize

	Container(Attrs(Viewport, Expand, Background(220, 8, 96, 1), Pad(20), Gap(14)), func() {
		Label("Window Control Extension", FontSize(20), FontWeight(WeightBold), TextColor(220, 20, 18, 1))
		Label("Enforces minimum dimensions, centering, and placement across desktop platforms via EscapeHatchBackendContext.",
			FontSize(13), TextColor(0, 0, 40, 1))

		Container(Attrs(Row, Gap(16), CrossMid), func() {
			Container(Attrs(Background(0, 0, 100, 1), Pad(12), Corners(6), BorderWidth(1), BorderColor(0, 0, 80, 1)), func() {
				Label(fmt.Sprintf("Current Window:  %.0f × %.0f pt", ws[0], ws[1]),
					FontSize(13), FontWeight(WeightMedium))
			})
			Container(Attrs(Background(210, 30, 92, 1), Pad(12), Corners(6), BorderWidth(1), BorderColor(210, 40, 75, 1)), func() {
				Label(fmt.Sprintf("Enforced Min:  %.0f × %.0f pt", minW, minH),
					FontSize(13), FontWeight(WeightBold), TextColor(210, 60, 25, 1))
			})
		})

		Label("Placement actions:", FontSize(14), FontWeight(WeightMedium))
		Container(Attrs(Row, Gap(10), CrossMid), func() {
			if Button(NoIcon, "Center Window") {
				window.Center()
			}
			if Button(NoIcon, "Position (100, 100)") {
				window.Position(100, 100)
			}
			if Button(NoIcon, "Position (400, 200)") {
				window.Position(400, 200)
			}
		})

		Label("Preset minimums:", FontSize(14), FontWeight(WeightMedium))
		Container(Attrs(Row, Gap(10), CrossMid), func() {
			if Button(NoIcon, "300 × 200") {
				minW, minH = 300, 200
			}
			if Button(NoIcon, "400 × 300") {
				minW, minH = 400, 300
			}
			if Button(NoIcon, "500 × 400") {
				minW, minH = 500, 400
			}
		})

		Label("Adjust minimum size:", FontSize(14), FontWeight(WeightMedium))
		Container(Attrs(Row, Gap(10), CrossMid), func() {
			if Button(SymIMinus, "Width -50") && minW > 150 {
				minW -= 50
			}
			if Button(SymIPlus, "Width +50") {
				minW += 50
			}
			if Button(SymIMinus, "Height -50") && minH > 150 {
				minH -= 50
			}
			if Button(SymIPlus, "Height +50") {
				minH += 50
			}
		})

		Label("Try resizing the window smaller than the enforced minimum size, or clicking placement buttons.",
			FontSize(12), FontStyle(StyleItalic), TextColor(0, 0, 50, 1))
	})
}
