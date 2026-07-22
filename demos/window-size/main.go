// window-size: probe WindowSize vs soft keyboard / orientation (iOS-focused).
//
//   - Single-line + 6-row multi-line text fields
//
//   - Floating [Width×Height] readout (monospace, top-right)
//
//   - Dummy filler so content is taller than the keyboard-up viewport but
//     shorter than the full safe-area viewport — whole page scrolls so a
//     scrollbar should appear when the keyboard shrinks WindowSize.
//
//     go run ./demos/window-size
package main

import (
	"fmt"

	"go.hasen.dev/shirei/app"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

// Phone-ish desktop default; on iOS the host is full-screen safe area.
const winW, winH = 390, 720

var (
	singleLine = "single line"
	multiLine  = "multi-line\nsecond row\n"
)

func main() {
	app.SetupWindow("Window Size", winW, winH)
	app.Run(RootView)
}

func RootView() {
	ws := GetHost().WindowSize
	fieldW := ws[0] - 28 // viewport pad 14+14
	if fieldW < 200 {
		fieldW = 200
	}

	// Viewport is already Extrinsic+Clip+Expand — scroll on this level.
	Container(Attrs(Viewport, Expand, Background(220, 8, 96, 1), Pad(14), Gap(12)), func() {
		ScrollOnInput()
		ScrollBars()

		Label("Window size probe", FontSize(18), FontWeight(WeightBold),
			TextColor(220, 20, 18, 1))
		Label("Focus a field to open the keyboard. Content is sized so a "+
			"scrollbar appears only while the keyboard is up.",
			FontSize(11), TextColor(0, 0, 45, 1))

		Container(Attrs(Gap(6)), func() {
			Label("Single line", FontSize(12), FontWeight(WeightSemibold),
				TextColor(0, 0, 30, 1))
			attrs := DefaultTextInputAttrs()
			attrs.MinWidth = fieldW
			attrs.NoAutoFocus = true
			TextInputExt(&singleLine, attrs)
		})

		Container(Attrs(Gap(6)), func() {
			Label("Multi-line (6 rows)", FontSize(12), FontWeight(WeightSemibold),
				TextColor(0, 0, 30, 1))
			attrs := DefaultMultilineTextInputAttrs()
			attrs.Rows = 6
			attrs.MaxLines = 0
			attrs.MinWidth = fieldW
			attrs.NoAutoFocus = true
			TextInputExt(&multiLine, attrs)
		})

		Label("Filler (scroll when keyboard is up)", FontSize(12),
			FontWeight(WeightSemibold), TextColor(0, 0, 30, 1))

		// A few filler blocks: with fields above, total height should exceed
		// a keyboard-up viewport but stay under a full keyboard-down one.
		for i := 0; i < 4; i++ {
			Container(Attrs(Expand, Pad(10), Gap(2), Corners(8),
				Background(0, 0, 100, 1), BorderWidth(1), BorderColor(0, 0, 88, 1)), func() {
				Label(fmt.Sprintf("Filler block %d of 4", i+1),
					FontSize(13), FontWeight(WeightSemibold))
				Label("Tall enough that the page overflows only while the "+
					"soft keyboard shrinks GetHost().WindowSize.",
					FontSize(11), TextColor(0, 0, 45, 1))
			})
		}

		Label(fmt.Sprintf("End of page · layout GetHost().WindowSize %.0f×%.0f", ws[0], ws[1]),
			FontSize(10), TextColor(0, 0, 55, 1))

		// Floating size HUD — top-right (Float ignores scroll offset for paint
		// position relative to the viewport).
		const hudPad float32 = 8
		const hudW float32 = 92
		const hudH float32 = 26
		x := ws[0] - hudW - hudPad
		if x < hudPad {
			x = hudPad
		}
		Container(Attrs(Float(x, hudPad), FixSize(hudW, hudH), Pad2(3, 6),
			Corners(6), Background(220, 25, 18, 0.8), Center), func() {
			Label(fmt.Sprintf("[%.0fx%.0f]", ws[0], ws[1]),
				FontSize(11), FontWeight(WeightSemibold),
				TextColor(0, 0, 100, 1), Fonts(Monospace...))
		})
	})
}
