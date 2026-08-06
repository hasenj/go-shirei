package main

// custom-toggles is a proof of concept for ProcessToggleEvents (ProcessButtonEvents
// + flip *bool — same interaction model as CheckBox). Three demo-only skins:
// Material (colored knob), green iOS-like track (close to default), and a creative
// "checkmark in the knob" variety. Default ToggleSwitch sits above for comparison.

import (
	"os"
	"fmt"

	"go.hasen.dev/shirei/app"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

func main() {
	if len(os.Args) >= 3 && os.Args[1] == "--png" {
		if err := RenderToPNG(os.Args[2], 640, 700, root); err != nil {
			fmt.Println("render to png failed:", err)
			os.Exit(1)
		}
		return
	}
	app.SetupWindow("Custom Toggles", 640, 700)
	app.Run(root)
}

var (
	defaultA, defaultB = true, false

	matWifi = true
	matBT   = false
	matAir  = true

	greenA, greenB = true, false

	checkA, checkB = true, false

	labelA, labelB = true, false

	actionLog = "flip a switch…"
)

func root() {
	ModAttrs(Viewport, Background(220, 12, 96, 1), Pad(28), Gap(22))
	ScrollOnInput()

	Label("Custom toggles via ProcessToggleEvents", FontWeight(WeightBold), FontSize(18))
	Label("Same interaction as CheckBox (click flips a bool). Skins are demo-only.",
		FontSize(13), TextColor(0, 0, 40, 1))

	// --- default ----------------------------------------------------------------
	section("Default ToggleSwitch")
	Container(Attrs(Row, CrossMid, Gap(16)), func() {
		ToggleSwitch(&defaultA)
		ToggleSwitch(&defaultB)
		ToggleSwitchExt(&defaultA, ToggleSwitchAttrs{Accent: AccentMeadow})
		Label(fmt.Sprintf("A=%v  B=%v", defaultA, defaultB), FontSize(12), TextColor(0, 0, 45, 1))
	})

	// --- Material (image 1) ---------------------------------------------------
	section("Material (colored knob)")
	Label("On: light-tint track + solid accent knob. Off: gray track + white knob. Soft knob shadow.",
		FontSize(12), TextColor(0, 0, 45, 1))
	Container(Attrs(Gap(14)), func() {
		row("Wi‑Fi", func() { MaterialToggle(&matWifi, materialPurple) })
		row("Bluetooth", func() { MaterialToggle(&matBT, materialPurple) })
		row("Airplane", func() { MaterialToggle(&matAir, materialTeal) })
	})

	// --- Green iOS-like (image 2) ---------------------------------------------
	section("Green track (close to default iOS)")
	Label("On: green track + white knob. Off: pale gray track + white knob. Default is the same idea with Accent.",
		FontSize(12), TextColor(0, 0, 45, 1))
	Container(Attrs(Row, CrossMid, Gap(20)), func() {
		GreenToggle(&greenA)
		GreenToggle(&greenB)
	})

	// --- Checkmark knob (image 3) ---------------------------------------------
	section("Checkmark-in-knob")
	Label("On: accent track + white knob with a check. Off: gray track + solid dark knob (no mark).",
		FontSize(12), TextColor(0, 0, 45, 1))
	Container(Attrs(Row, CrossMid, Gap(20)), func() {
		CheckToggle(&checkA, checkPurple)
		CheckToggle(&checkB, checkPurple)
	})

	// --- Overhang + ON/OFF labels ---------------------------------------------
	section("Overhanging knob + ON/OFF labels")
	Label("Knob larger than the track; track shows ON (left) or OFF (right) under the free side.",
		FontSize(12), TextColor(0, 0, 45, 1))
	Container(Attrs(Row, CrossMid, Gap(24)), func() {
		LabelToggle(&labelA)
		LabelToggle(&labelB)
	})

	// --- log ------------------------------------------------------------------
	Container(Attrs(Pad2(10, 14), Background(0, 0, 100, 1), Corners(4),
		BorderWidth(1), BorderColor(0, 0, 0, 0.08)), func() {
		Label("Last action", FontSize(11), TextColor(0, 0, 45, 1))
		Label(actionLog, FontSize(14), FontWeight(WeightBold))
	})
	ScrollBars()
}

func section(title string) {
	Label(title, FontWeight(WeightBold), FontSize(14), TextColor(0, 0, 30, 1))
}

func row(label string, body func()) {
	Container(Attrs(Row, CrossMid, Gap(12)), func() {
		Label(label, FontSize(14), TextColor(0, 0, 25, 1))
		Element(Attrs(Grow(1)))
		body()
	})
}

func note(s string) { actionLog = s }

// ---------------------------------------------------------------------------
// Shared geometry helper: stadium track + floated knob (stable key for layout
// anim of the knob). ProcessToggleEvents on the track container.
// ---------------------------------------------------------------------------

func softKnobShadow(a *AttrSet) {
	a.Shadow.Alpha = 0.18
	a.Shadow.Blur = 4
	a.Shadow.Offset[1] = 1
}

// ---------------------------------------------------------------------------
// MaterialToggle — colored knob on a tinted track when on (ref image 1).
// ---------------------------------------------------------------------------

var (
	materialPurple = Vec4{270, 55, 52, 1}
	materialTeal   = Vec4{174, 55, 42, 1}
)

// MaterialToggle is a Material-style switch. Demo only.
func MaterialToggle(on *bool, accent Vec4) {
	const (
		h    float32 = 28
		w    float32 = 48
		knob float32 = 22
		// knob overhangs the track slightly when at either end
		margin float32 = (h - knob) / 2
	)

	Container(Attrs(FixSize(w, h), Corners(h/2)), func() {
		st := ProcessToggleEvents(on, false)
		if st.Clicked {
			note(fmt.Sprintf("Material → %v", *on))
		}

		// Track: light accent wash when on, gray when off.
		track := Vec4{0, 0, 78, 1}
		if *on {
			track = Vec4{accent[0], accent[1] * 0.45, 78, 1}
		}
		if st.Hovered {
			if *on {
				track[2] += 3
			} else {
				track[2] -= 3
			}
		}
		ModAttrs(BackgroundVec(track))

		// Knob: solid accent when on, white when off.
		knobBG := Vec4{0, 0, 100, 1}
		if *on {
			knobBG = accent
		}
		knobX := margin
		if *on {
			knobX = w - margin - knob
		}
		knobY := (h - knob) / 2

		ContainerWithKey("mat-knob", Attrs(
			Float(knobX, knobY),
			FixSize(knob, knob),
			Corners(knob/2),
			BackgroundVec(knobBG),
			softKnobShadow,
			ClickThrough,
		), func() {})
	})
}

// ---------------------------------------------------------------------------
// GreenToggle — green track + white knob (ref image 2; close to default).
// ---------------------------------------------------------------------------

var greenOn = Vec4{134, 55, 48, 1} // ~#4CAF50-ish

// GreenToggle is a green iOS-like switch. Demo only.
func GreenToggle(on *bool) {
	const (
		h      float32 = 30
		w      float32 = 52
		pad    float32 = 2
		knob   float32 = h - pad*2
	)

	Container(Attrs(FixSize(w, h), Corners(h/2)), func() {
		st := ProcessToggleEvents(on, false)
		if st.Clicked {
			note(fmt.Sprintf("Green → %v", *on))
		}

		track := Vec4{0, 0, 90, 1}
		if *on {
			track = greenOn
		}
		if st.Hovered {
			if *on {
				track[2] += 4
			} else {
				track[2] -= 3
			}
		}
		ModAttrs(BackgroundVec(track))

		knobX := pad
		if *on {
			knobX = w - pad - knob
		}
		ContainerWithKey("green-knob", Attrs(
			Float(knobX, pad),
			FixSize(knob, knob),
			Corners(knob/2),
			Background(0, 0, 100, 1),
			softKnobShadow,
			ClickThrough,
		), func() {})
	})
}

// ---------------------------------------------------------------------------
// CheckToggle — accent track + white knob with check when on; gray track +
// solid dark knob when off (ref image 3).
// ---------------------------------------------------------------------------

var checkPurple = Vec4{270, 40, 38, 1}

// CheckToggle shows a checkmark inside the on-state knob. Demo only.
//
// Uses the same tick recipe as default CheckBox (oversize Microns glyph + Clip +
// slight top pad) and the default ToggleSwitch layout (Row + Grow spacer) so the
// icon is a normal layout child — a Float+ClickThrough knob was swallowing the
// glyph in practice.
func CheckToggle(on *bool, accent Vec4) {
	const (
		h    float32 = 28
		w    float32 = 50
		pad  float32 = 3
		knob float32 = h - pad*2
	)

	Container(Attrs(Row, FixSize(w, h), Corners(h/2), Pad(pad), CrossMid), func() {
		st := ProcessToggleEvents(on, false)
		if st.Clicked {
			note(fmt.Sprintf("Check → %v", *on))
		}

		track := Vec4{0, 0, 88, 1}
		if *on {
			track = accent
		}
		if st.Hovered {
			if *on {
				track[2] += 4
			} else {
				track[2] -= 3
			}
		}
		ModAttrs(BackgroundVec(track))

		// Push knob to the right when on (same as default ToggleSwitch).
		if *on {
			Element(Attrs(Grow(1)))
		} else {
			Nil()
		}

		knobBG := Vec4{0, 0, 45, 1} // dark gray when off
		if *on {
			knobBG = Vec4{0, 0, 100, 1}
		}
		// Optical top pad like CheckBoxExt — tick ink sits high in its em box.
		padTop := knob * 0.14
		Container(Attrs(
			FixSize(knob, knob),
			Corners(knob/2),
			BackgroundVec(knobBG),
			softKnobShadow,
			Pad4(padTop, 0, 0, 0),
			Clip,
			Center,
		), func() {
			if *on {
				// Purple tick on white knob. Size 1.5× matches default CheckBox.
				Icon(SymITick, FontSize(knob*1.5), TextColor(270, 70, 35, 1))
			}
		})
	})
}

// ---------------------------------------------------------------------------
// LabelToggle — knob larger than the track; ON / OFF text on the free side
// of the track (ref: green ON / blue-gray OFF with overhanging white knob).
// ---------------------------------------------------------------------------

// LabelToggle is an overhanging-knob switch with ON/OFF labels. Demo only.
func LabelToggle(on *bool) {
	const (
		// Knob is the visual star; track is shorter so the circle overhangs.
		knob   float32 = 28
		trackH float32 = 22
		trackW float32 = 64
		// Outer hit box tall enough for the overhanging knob + a little air.
		outerH float32 = knob + 4
		outerW float32 = trackW
	)

	green := Vec4{134, 60, 48, 1}    // ~#4CAF50
	offTrack := Vec4{210, 25, 82, 1} // cool gray-blue
	offText := Vec4{210, 15, 55, 1}

	Container(Attrs(FixSize(outerW, outerH)), func() {
		st := ProcessToggleEvents(on, false)
		if st.Clicked {
			note(fmt.Sprintf("Label → %v", *on))
		}

		trackY := (outerH - trackH) / 2
		trackBG := offTrack
		if *on {
			trackBG = green
		}
		if st.Hovered {
			if *on {
				trackBG[2] += 4
			} else {
				trackBG[2] -= 3
			}
		}

		// Track (centered vertically inside the taller hit box).
		Container(Attrs(
			Float(0, trackY),
			FixSize(trackW, trackH),
			Corners(trackH/2),
			BackgroundVec(trackBG),
			ClickThrough,
		), func() {
			// Label centered in the free half of the track (the side the
			// knob is not covering). Inset a bit from the stadium tip so
			// "OFF" doesn't hug the right curve.
			const inset float32 = 6
			freeW := trackW*0.5 - inset
			if *on {
				// Free region: left half.
				Container(Attrs(Float(inset, 0), FixSize(freeW, trackH), Row, CrossMid), func() {
					Filler(1)
					Label("ON", FontSize(11), FontWeight(WeightBold), TextColor(0, 0, 100, 1))
					Filler(1)
				})
			} else {
				// Free region: right half, starting after the left-side knob.
				Container(Attrs(Float(trackW*0.5, 0), FixSize(freeW, trackH), Row, CrossMid), func() {
					Filler(1)
					Label("OFF", FontSize(11), FontWeight(WeightBold), TextColorVec(offText))
					Filler(1)
				})
			}
		})

		// Overhanging white knob — taller than the track.
		knobX := float32(1)
		if *on {
			knobX = outerW - knob - 1
		}
		knobY := (outerH - knob) / 2
		ContainerWithKey("label-knob", Attrs(
			Float(knobX, knobY),
			FixSize(knob, knob),
			Corners(knob/2),
			Background(0, 0, 100, 1),
			softKnobShadow,
			ClickThrough,
		), func() {})
	})
}
