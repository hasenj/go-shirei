package main

// custom-sliders demos ProcessSlider: default chrome vs demo-only skins
// (Apple-style continuous, Material bar+blade, Windows XP handle).
// Interaction stays in widgets.ProcessSlider; paint is local.
//
//	go run ./demos/custom-sliders

import (
	"os"
	"fmt"

	"go.hasen.dev/shirei/app"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

func main() {
	if len(os.Args) >= 3 && os.Args[1] == "--png" {
		if err := RenderToPNG(os.Args[2], 720, 640, root); err != nil {
			fmt.Println("render to png failed:", err)
			os.Exit(1)
		}
		return
	}
	app.SetupWindow("Custom Sliders", 720, 640)
	app.Run(root)
}

var (
	defaultV    float32 = 0.45
	appleDisp float32 = 0.72
	appleSnd  float32 = 0.35
	matCall   float32 = 0.65
	matMedia  float32 = 0.28
	xpV       float32 = 0.4
)

func root() {
	ModAttrs(Viewport, Background(220, 10, 96, 1), Pad(28), Gap(20))
	ScrollOnInput()

	Label("Custom sliders via ProcessSlider", FontWeight(WeightBold), FontSize(18))
	Label("One process helper (value + drag/wheel); track and handle paint are yours.",
		FontSize(13), TextColor(0, 0, 40, 1))

	section("Default", "Slider() — accent track, white circle handle")
	Container(Attrs(Row, CrossMid, Gap(12)), func() {
		Slider(&defaultV, SliderAttrs{Min: 0, Max: 1, Width: 240})
		Label(fmt.Sprintf("%.2f", defaultV), FontSize(13), TextColor(0, 0, 35, 1))
	})

	section("Apple-style", "Filled capsule + round knob; optional leading icon")
	appleCard()

	section("Material", "Thick track, thin vertical handle, fill to value")
	materialCard()

	section("Windows XP", "Thin trough + chunky handle (no tick marks)")
	Container(Attrs(Row, CrossMid, Gap(12)), func() {
		xpSlider(&xpV, 260)
		Label(fmt.Sprintf("%.2f", xpV), FontSize(13), TextColor(0, 0, 35, 1))
	})

	ScrollBars()
}

func section(title, sub string) {
	Container(Attrs(Gap(4)), func() {
		Label(title, FontWeight(WeightBold), FontSize(14), TextColor(0, 0, 20, 1))
		Label(sub, FontSize(12), TextColor(0, 0, 45, 1))
	})
}

// ---------------------------------------------------------------------------
// Apple-style: continuous fill + circle (settings-row chrome optional)
// ---------------------------------------------------------------------------

func appleCard() {
	Container(Attrs(Gap(10), Pad(14), Corners(12),
		Background(0, 0, 18, 1),
		BorderWidth(1), BorderColor(0, 0, 100, 0.08),
	), func() {
		appleRow(TypAdjustBrightness, "Display", &appleDisp)
		appleRow(SymAudio, "Sound", &appleSnd)
	})
}

func appleRow(icon IconGlyph, label string, value *float32) {
	Label(label, FontSize(13), FontWeight(WeightSemibold), TextColor(0, 0, 92, 1))
	Container(Attrs(Expand, Gap(6)), func() {
		Container(Attrs(Row, CrossMid, Gap(10)), func() {
			appleSlider(value, 280)
		})
		Container(Attrs(Float(0, 0), FixSize(24, 24), Center), func() {
			Icon(icon, FontSize(18), TextColor(0, 0, 20, 1))
		})
	})
}

func appleSlider(value *float32, width float32) {
	// Track height = knob diameter.
	// (1) Full gray capsule = the track background
	// (2) White fill on top, flat edge under the knob center (hidden by circle)
	// (3) Knob — gaps show gray track, not the card behind.
	const (
		trackH float32 = 24
		boxH   float32 = trackH
		capR           = trackH / 2
	)
	Container(Attrs(FixWidth(width), FixHeight(boxH), Corners(capR), Background(0, 0, 40, 1), Focusable), func() {
		st := ProcessSlider(value, SliderConfig{
			Min: 0, Max: 1, Width: width, HandleInset: capR,
		})
		// White fill to knob center; square at the seam (under the circle).
		fillTo := st.HandleX + capR*2
		if fillTo > width {
			fillTo = width
		}
		// white fill
		Element(Attrs(
			Float(0, 0),
			FixSize(fillTo, trackH),
			Corners(capR),
			Background(0, 0, 100, 1),
			ClickThrough,
		))

		// handle
		Element(Attrs(
			Float(st.HandleX, 0),
			FixSize(capR*2, capR*2),
			Corners(capR),
			Background(0, 0, 100, 1),
			BoxShadow(2),
			ClickThrough,
		))
	})
}

// ---------------------------------------------------------------------------
// Material: thick bar + blade handle
// ---------------------------------------------------------------------------

func materialCard() {
	Container(Attrs(Gap(16), Pad(16), Corners(8),
		Background(270, 30, 97, 1),
	), func() {
		materialRow(TypPhone, "Call volume", &matCall)
		materialRow(TypVolumeUp, "Media volume", &matMedia)
	})
}

func materialRow(icon IconGlyph, label string, value *float32) {
	Container(Attrs(Expand, Gap(8)), func() {
		Container(Attrs(Row, CrossMid, Gap(10)), func() {
			Icon(icon, FontSize(18), TextColor(0, 0, 25, 1))
			Label(label, FontSize(14), FontWeight(WeightSemibold), TextColor(0, 0, 20, 1))
		})
		materialSlider(value, 320)
	})
}

func materialSlider(value *float32, width float32) {
	const (
		trackH float32 = 16
		bladeW float32 = 4
		bladeH float32 = 28
		boxH   float32 = 32
		inset          = bladeW / 2
		fillH  float32 = 270 // hue
		capR           = trackH / 2
	)
	Container(Attrs(FixWidth(width), FixHeight(boxH), Focusable), func() {
		st := ProcessSlider(value, SliderConfig{
			Min: 0, Max: 1, Width: width, HandleInset: inset,
		})
		trackY := (boxH - trackH) / 2
		// Empty remainder (right of value): square on the handle side, round on the outer end.
		// Corners4 = top-left, top-right, bottom-right, bottom-left.
		emptyX := st.HandleX + inset
		emptyW := width - emptyX
		if emptyW > 0 {
			Element(Attrs(
				Float(emptyX, trackY),
				FixSize(emptyW, trackH),
				Corners4(0, capR, capR, 0),
				Background(270, 40, 90, 1),
				ClickThrough,
			))
		}
		// Filled left portion: round outer end, square at the handle.
		fillW := st.HandleX + inset
		if fillW > 0 {
			Element(Attrs(
				Float(0, trackY),
				FixSize(fillW, trackH),
				Corners4(capR, 0, 0, capR),
				Background(fillH, 45, 42, 1),
				ClickThrough,
			))
		}
		// Blade handle centered on value.
		bladeX := st.HandleX + inset - bladeW/2
		bladeY := (boxH - bladeH) / 2
		Element(Attrs(
			Float(bladeX, bladeY),
			FixSize(bladeW, bladeH),
			Corners(2),
			Background(fillH, 50, 35, 1),
			ClickThrough,
		))
	})
}

// ---------------------------------------------------------------------------
// Windows XP — thin track, rectangular handle (no ticks)
// ---------------------------------------------------------------------------

func xpSlider(value *float32, width float32) {
	const (
		// Slightly larger knob than the first pass for readable 3D.
		handW float32 = 12
		handH float32 = 24
		boxH  float32 = handH
		inset         = handW / 2
		capH  float32 = 3 // green plastic top/bottom
	)
	Container(Attrs(FixWidth(width), FixHeight(boxH), Center, Focusable), func() {
		st := ProcessSlider(value, SliderConfig{
			Min: 0, Max: 1, Width: width, HandleInset: inset,
		})
		// Sunken trough: dark line on top, light line on bottom (XP channel).
		const troughH float32 = 6
		Container(Attrs(FixSize(width, troughH), Background(40, 20, 90, 1), Corners(troughH/2), Clip), func() {
			// Outer dark edge (top + sides feel recessed).
			Element(Attrs(
				Float(0, 0),
				FixSize(width, troughH*0.3),
				Background(0, 0, 52, 1),
				ClickThrough,
			))

			// Inner light floor of the groove (1px up from bottom of trough).
			Element(Attrs(
				Float(0, troughH*0.7),
				FixSize(width, troughH*0.3),
				Background(0, 0, 100, 1),
				ClickThrough,
			))

		})
		// Handle sits on the trough.
		hx := st.HandleX + inset - handW/2
		hy := (boxH - handH) / 2
		Container(Attrs(Float(hx, hy), FixSize(handW, handH), ClickThrough), func() {
			xpHandle(handW, handH, capH)
		})
	})
}

// xpHandle paints a Luna-style vertical slider knob: green top/bottom caps
// and a beveled white mid (light top-left, dark bottom-right).
func xpHandle(w, h, capH float32) {
	green := Vec4{120, 55, 42, 1}
	greenHi := Vec4{120, 40, 58, 1}
	greenLo := Vec4{120, 65, 30, 1}
	// Outer dark outline.
	Container(Attrs(FixSize(w, h), Background(0, 0, 32, 1), Pad(1), Corners(4), Clip), func() {
		innerW, innerH := w-2, h-2
		Container(Attrs(FixSize(innerW, innerH)), func() {
			// Top green cap.
			Container(Attrs(FixSize(innerW, capH), BackgroundVec(greenLo)), func() {
				Element(Attrs(FixSize(innerW, 1), BackgroundVec(greenHi)))
				Element(Attrs(FixSize(innerW, capH-1), BackgroundVec(green)))
			})
			// Mid face: light strip left, dark strip right, raised face.
			faceH := innerH - capH*2
			if faceH < 6 {
				faceH = 6
			}
			// hi := Vec4{0, 0, 100, 1} // light (left / top)
			// lo := Vec4{0, 0, 48, 1}  // dark (right / bottom)
			face := Vec4{70, 20, 96, 1}
			// Dark base (shows as bottom + right).
			// Container(Attrs(FixSize(innerW, faceH), BackgroundVec(lo), Pad4(0, 1, 1, 0)), func() {
			// Light top + left.
			// Container(Attrs(FixSize(innerW-1, faceH-1), BackgroundVec(hi), Pad4(1, 0, 0, 1)), func() {
			// Face + vertical ridges (light left bar / dark right bar feel).
			Container(Attrs(FixSize(innerW, faceH), BackgroundVec(face), Row), func() {
				Element(Attrs(FixWidth(w*0.2), Expand, Background(0, 0, 100, 0.9)))
				Element(Attrs(Grow(1), Expand, BackgroundVec(face)))
				Element(Attrs(FixWidth(w*0.2), Expand, Background(0, 0, 10, 0.2)))
			})
			// })
			// })
			// Bottom green cap.
			Container(Attrs(FixSize(innerW, capH), BackgroundVec(green)), func() {
				Element(Attrs(FixSize(innerW, capH-1), BackgroundVec(green)))
				Element(Attrs(FixSize(innerW, 1), BackgroundVec(greenLo)))
			})
		})
	})
}
