package main

// custom-checkboxes is a proof of concept for ProcessButtonEvents on toggle
// controls: two demo-only skins (Material Design-ish, and classic Windows XP)
// that never enter the widgets package. Default CheckBox sits above for
// comparison.

import (
	"os"
	"fmt"

	"go.hasen.dev/shirei/app"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

func main() {
	if len(os.Args) >= 3 && os.Args[1] == "--png" {
		if err := RenderToPNG(os.Args[2], 680, 560, root); err != nil {
			fmt.Println("render to png failed:", err)
			os.Exit(1)
		}
		return
	}
	app.SetupWindow("Custom Checkboxes", 680, 560)
	app.Run(root)
}

var (
	defaultA, defaultB bool = true, false

	matNotify  = true
	matWifi    = false
	matSync    = true
	matDisable bool
	matTarget  = true

	xpOptions = true
	xpSounds  = false
	xpIcons   = true
	xpDisable bool
	xpTarget  = true

	actionLog = "toggle a checkbox…"
)

func root() {
	ModAttrs(Viewport, Background(220, 12, 96, 1), Pad(28), Gap(22))
	ScrollOnInput()

	Label("Custom checkboxes via ProcessButtonEvents", FontWeight(WeightBold), FontSize(18))
	Label("Demo-only skins — not in the widget catalogue. Same click model as default CheckBox.",
		FontSize(13), TextColor(0, 0, 40, 1))

	// --- default ----------------------------------------------------------------
	section("Default widgets (for comparison)")
	Container(Attrs(Gap(8)), func() {
		CheckBox(&defaultA, "Enable notifications")
		CheckBox(&defaultB, "Play sound")
		Container(Attrs(Row, Gap(16), CrossMid), func() {
			CheckBoxExt(&defaultA, "", CheckBoxAttrs{Accent: AccentMeadow, Size: 20})
			CheckBoxExt(&defaultB, "", CheckBoxAttrs{Accent: AccentSunshine, Size: 20})
			Label("(sized + accent variants)", FontSize(12), TextColor(0, 0, 45, 1))
		})
	})

	// --- Material -------------------------------------------------------------
	section("Material Design inspired")
	Container(Attrs(Gap(10)), func() {
		MaterialCheck(&matNotify, "Show notifications", materialPrimary, false)
		MaterialCheck(&matWifi, "Connect to Wi‑Fi automatically", materialPrimary, false)
		MaterialCheck(&matSync, "Sync in the background", materialTeal, false)
		MaterialCheck(&matNotify, "Disabled sample", materialPrimary, true)
	})
	Container(Attrs(Row, CrossMid, Gap(12)), func() {
		// default checkbox to drive disabled — keeps the demo self-contained
		CheckBox(&matDisable, "Disable the next Material box")
		MaterialCheck(&matTarget, "Toggle target", materialPrimary, matDisable)
	})

	// --- XP -------------------------------------------------------------------
	// Luna chrome approx. from https://botoxparty.github.io/XP.css/ — surface
	// #ece9d8, checkbox is a sunken field (not a raised button).
	section("Windows XP (Luna / XP.css inspired)")
	Container(Attrs(Gap(8), Pad(14), Background(51, 33, 89, 1), // #ece9d8
		BorderWidth(1), BorderColor(50, 10, 70, 1)), func() {
		XPCheck(&xpOptions, "Show all options", false)
		XPCheck(&xpSounds, "Play Windows startup sound", false)
		XPCheck(&xpIcons, "Hide icons when desktop is locked", false)
		XPCheck(&xpOptions, "Disabled sample", true)
	})
	Container(Attrs(Row, CrossMid, Gap(12)), func() {
		CheckBox(&xpDisable, "Disable the next XP box")
		XPCheck(&xpTarget, "Toggle target", xpDisable)
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

func note(s string) {
	actionLog = s
}

// ---------------------------------------------------------------------------
// Material checkbox — rounded square, 2px border; fills with accent when on,
// white tick. Hover darkens the border (off) or lightens the fill (on).
// ---------------------------------------------------------------------------

var (
	materialPrimary = Vec4{211, 72, 48, 1} // Material blue-ish
	materialTeal    = Vec4{174, 60, 40, 1}
)

// MaterialCheck is a filled Material-style checkbox. Lives in this demo only.
func MaterialCheck(on *bool, label string, accent Vec4, disabled bool) {
	const size float32 = 18
	const radius float32 = 2

	Container(Attrs(Row, Gap(10), CrossMid), func() {
		st := ProcessButtonEvents(disabled)
		if st.Clicked {
			*on = !*on
			note(fmt.Sprintf("Material %q → %v", label, *on))
		}

		// Box face
		border := Vec4{0, 0, 55, 1}
		bg := Vec4{0, 0, 100, 1}
		var grad Vec4
		tick := Vec4{0, 0, 100, 1}

		if disabled {
			border = Vec4{0, 0, 75, 1}
			bg = Vec4{0, 0, 94, 1}
			if *on {
				bg = Vec4{0, 0, 78, 1}
				border = bg
				tick = Vec4{0, 0, 96, 1}
			}
		} else if *on {
			bg = accent
			border = accent
			if st.Hovered {
				bg = Vec4{accent[0], accent[1], accent[2] + 6, 1}
				border = bg
			}
			if st.Active {
				bg = Vec4{accent[0], accent[1], accent[2] - 6, 1}
				border = bg
			}
			ClampColorVec(&bg)
			border = bg
		} else {
			if st.Hovered {
				border = accent
				bg = Vec4{accent[0], accent[1] * 0.25, 97, 1}
			}
			if st.Active {
				bg = Vec4{accent[0], accent[1] * 0.35, 94, 1}
			}
		}

		Container(Attrs(FixSize(size, size), Corners(radius), Center, Clip,
			BackgroundVec(bg), GradVec(grad),
			BorderWidth(2), BorderColorVec(border)), func() {
			if *on {
				// Slightly oversized tick so ink fills the rounded box.
				Icon(SymITick, FontSize(size*1.15), TextColorVec(tick))
			}
		})

		text := Vec4{0, 0, 15, 1}
		if disabled {
			text = Vec4{0, 0, 55, 1}
		}
		if label != "" {
			Label(label, FontSize(14), TextColorVec(text))
		}
	})
}

// ---------------------------------------------------------------------------
// XP checkbox — always a sunken field, never a raised push-button.
//
// Faithful to XP.css Luna (https://botoxparty.github.io/XP.css/):
//   idle:     13×13, blue border #1d5281, face gradient #dcdcd7 → #fff
//   hover:    warm gold inset rim (#fedf9c / #f8b636)
//   active:   darker face gradient #b0b0a7 → #e3e1d2
//   checked:  green pixel-ish tick (#22a122), not black
//   disabled: muted border #cac8bb, white face, gray tick
//
// Unlike buttons (raised → sunken on press), the box stays inset in every
// state; only the fill and rim react to hover/press.
// ---------------------------------------------------------------------------

// XPCheck is a Windows XP Luna-style checkbox. Lives in this demo only.
func XPCheck(on *bool, label string, disabled bool) {
	// Outer size matches XP.css (13px content + 1px border each side ≈ 15).
	const face float32 = 13

	// Palette (HSLA approximations of the XP.css hex values).
	borderBlue := Vec4{207, 63, 31, 1}  // #1d5281
	borderMute := Vec4{50, 10, 76, 1}   // #cac8bb
	faceIdle := Vec4{60, 8, 86, 1}      // #dcdcd7
	faceIdleGrad := Vec4{0, 0, 14, 0}   // → white toward bottom-right-ish
	faceActive := Vec4{50, 5, 68, 1}    // #b0b0a7
	faceActiveGrad := Vec4{0, 0, 16, 0} // → #e3e1d2
	goldLo := Vec4{38, 93, 59, 1}       // #f8b636 (inner shadow)
	goldHi := Vec4{43, 95, 80, 1}       // #fedf9c (inner highlight)
	tickGreen := Vec4{120, 68, 38, 1}   // #22a122
	tickMute := Vec4{50, 10, 76, 1}
	text := Vec4{0, 0, 13, 1} // #222

	Container(Attrs(Row, Gap(6), CrossMid), func() {
		st := ProcessButtonEvents(disabled)
		if st.Clicked {
			*on = !*on
			note(fmt.Sprintf("XP %q → %v", label, *on))
		}

		border := borderBlue
		bg := faceIdle
		grad := faceIdleGrad
		tick := tickGreen
		if disabled {
			border = borderMute
			bg = Vec4{0, 0, 100, 1}
			grad = Vec4{}
			tick = tickMute
			text = Vec4{0, 0, 55, 1}
		} else if st.Active {
			// Pressed field: still sunken, just a duller fill (XP.css active).
			bg = faceActive
			grad = faceActiveGrad
		}

		// Outer blue (or muted) frame — the "hole" in the dialog surface.
		// Always the same pad stack so size is fixed: on hover the inner 1px
		// rim turns gold (XP.css inset gold shadows); otherwise the rim matches
		// the face and disappears into it.
		rimLo, rimHi := bg, bg
		if !disabled && st.Hovered && !st.Active {
			rimLo, rimHi = goldLo, goldHi
		}
		Container(Attrs(BackgroundVec(border), Pad(1)), func() {
			// Pad4 = top, right, bottom, left. Darker gold bottom+right, lighter
			// top+left — reads as a warm sunken glow, not a raised bevel.
			Container(Attrs(BackgroundVec(rimLo), Pad4(0, 1, 1, 0)), func() {
				Container(Attrs(BackgroundVec(rimHi), Pad4(1, 0, 0, 1)), func() {
					xpCheckFace(face-2, bg, grad, *on, tick)
				})
			})
		})

		if label != "" {
			Label(label, FontSize(11), TextColorVec(text))
		}
	})
}

func xpCheckFace(size float32, bg, grad Vec4, on bool, tick Vec4) {
	Container(Attrs(FixSize(size, size), BackgroundVec(bg), GradVec(grad), Center, Clip), func() {
		if on {
			// Oversized Microns tick (ink sits inside its em box). Do not use
			// FontWeight(WeightBold): Microns has no bold face, so shaping
			// yields zero glyphs and the check disappears.
			Icon(SymITick, FontSize(size*1.55), TextColorVec(tick))
		}
	})
}
