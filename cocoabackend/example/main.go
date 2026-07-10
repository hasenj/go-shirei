//go:build darwin

// Manual test harness for the cocoa backend. Run with:
//
//	go run ./shirei/cocoabackend/example
//
// At this stage (M0-M2) it exercises rect rendering only: solid fills, rounded
// corners, vertical gradients, borders, clipping, and transparency. No text yet.
package main

import (
	"fmt"
	"os"

	app "go.hasen.dev/shirei/cocoabackend"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

// optional image to exercise the image-drawing path; set via SHIREI_IMAGE.
var imagePath = os.Getenv("SHIREI_IMAGE")

func main() {
	// `example --png out.png` renders one frame offscreen and exits (headless
	// verification); otherwise it opens a live window. RenderToPNG is core
	// shirei's (via the dot-import) — software rendering lives in core, so
	// PNG dumping isn't backend-specific.
	if len(os.Args) >= 3 && os.Args[1] == "--png" {
		if err := RenderToPNG(os.Args[2], 640, 480, frameFn); err != nil {
			fmt.Println("render to png failed:", err)
		}
		return
	}
	app.SetupWindow("shirei — cocoa backend", 640, 480)
	app.Run(frameFn)
}

func frameFn() {
	Container(Attrs(Viewport, Pad(20), Gap(16), Background(220, 30, 96, 1)), func() {
		// row 1: solid+shadow, gradient, bordered, rounded, and an image
		Container(Attrs(Row, Gap(16), CrossAlign(AlignMiddle)), func() {
			Element(Attrs(FixSize(130, 90), Corners(8), Background(0, 70, 60, 1), BoxShadow(8)))
			Element(Attrs(FixSize(130, 90), Corners(8), Background(120, 75, 55, 1), Grad(0, 0, 25, 0)))
			Element(Attrs(FixSize(110, 90), Corners(8), Background(210, 60, 92, 1), BorderWidth(2), BorderColor(210, 40, 40, 1)))
			Element(Attrs(FixSize(80, 80), Corners(40), Background(280, 70, 60, 1)))
			if imagePath != "" {
				Image(imagePath, Vec2{80, 80})
			}
		})

		// row 2: gradient sweep + a transparent overlay over a clipped box
		Container(Attrs(Row, Gap(16)), func() {
			Element(Attrs(FixSize(200, 120), Corners(12), Background(30, 85, 55, 1), Grad(50, 0, -25, 0)))

			// clipping: an oversized child clipped to a rounded parent
			Container(Attrs(FixSize(200, 120), Corners(12), Clip, Background(0, 0, 100, 1)), func() {
				Element(Attrs(Float(-20, -20), FixSize(160, 160), Background(340, 80, 60, 1)))
				Element(Attrs(Float(80, 40), FixSize(160, 160), Corners(20), Trans(0.35), Background(200, 80, 55, 1)))
			})
		})

		// row 3: text — Latin, weights/sizes, and Japanese (the IME target script)
		Container(Attrs(Gap(6), Pad(4)), func() {
			Label("The quick brown fox jumps over the lazy dog.", TextColor(0, 0, 15, 1))
			Label("Bold heading at size 22", FontSize(22), FontWeight(WeightBold), TextColor(210, 70, 40, 1))
			Label("日本語の入力 — こんにちは世界", FontSize(20), TextColor(0, 0, 20, 1))
			Label("mixed 日本語 and English 123", FontSize(16), TextColor(140, 60, 35, 1))
		})

		// row 4: interactive widgets (live input test)
		Container(Attrs(Row, Gap(12), CrossAlign(AlignMiddle), Pad(4)), func() {
			Label("Type:")
			TextInput(&inputText)

			if PressableButton("Click me") {
				clickCount++
			}
			Label(fmt.Sprintf("clicks: %d", clickCount))
		})
	})
}

var inputText = "edit me"
var clickCount int

// PressableButton is a tiny inline button (the widgets package button API
// varies; this keeps the example self-contained).
func PressableButton(label string) bool {
	clicked := false
	Container(Attrs(Pad2(6, 12), Corners(4), Background(210, 50, 55, 1), Grad(0, 0, 8, 0)), func() {
		if PressAction() {
			clicked = true
		}
		if IsHovered() {
			ModAttrs(Background(210, 55, 48, 1))
		}
		Label(label, TextColor(0, 0, 100, 1))
	})
	return clicked
}
