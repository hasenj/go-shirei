package main

// custom-buttons is a proof of concept for ProcessButtonEvents: demo-only
// skins (flat Material/Bootstrap, classic Windows 98 raised bevel, and
// Windows XP Luna from XP.css) that never enter the widgets package.

import (
	"fmt"

	"go.hasen.dev/shirei/app"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

func main() {
	app.SetupWindow("Custom Buttons", 720, 520)
	app.Run(root)
}

var clickLog = "click a button…"
var flatDisabled bool
var win98Disabled bool
var xpDisabled bool

func root() {
	ModAttrs(Viewport, Background(220, 12, 96, 1), Pad(28), Gap(22))
	ScrollOnInput()

	Label("Custom buttons via ProcessButtonEvents", FontWeight(WeightBold), FontSize(18))
	Label("Demo-only skins — not in the widget catalogue. Same click model as default Button.",
		FontSize(13), TextColor(0, 0, 40, 1))

	// --- default, for comparison ------------------------------------------------
	section("Default widgets (for comparison)")
	Container(Attrs(Row, Wrap, CrossMid, Gap(10)), func() {
		if Button(NoIcon, "Primary") {
			note("default Primary")
		}
		if CtrlButton(NoIcon, "Ctrl", true) {
			note("default Ctrl")
		}
		if ButtonExt("Meadow", ButtonAttrs{Accent: AccentMeadow}) {
			note("default Meadow")
		}
		ButtonExt("Disabled", ButtonAttrs{Disabled: true})
	})

	// --- flat -----------------------------------------------------------------
	section("Flat (Material / Bootstrap inspired)")
	Container(Attrs(Row, Wrap, CrossMid, Gap(10)), func() {
		if FlatButton("Primary", flatPrimary, false) {
			note("flat Primary")
		}
		if FlatButton("Success", flatSuccess, false) {
			note("flat Success")
		}
		if FlatButton("Danger", flatDanger, false) {
			note("flat Danger")
		}
		if FlatOutlineButton("Outline", flatPrimary, false) {
			note("flat Outline")
		}
		if FlatButton("Disabled", flatPrimary, true) {
			note("flat Disabled") // never fires
		}
	})
	Container(Attrs(Row, CrossMid, Gap(10)), func() {
		CheckBox(&flatDisabled, "Disable the next flat button")
		if FlatButton("Toggle target", flatPrimary, flatDisabled) {
			note("flat toggle target")
		}
	})

	// --- Win98 ----------------------------------------------------------------
	section("Windows 98 classic (raised 3D bevel)")
	Container(Attrs(Row, Wrap, CrossMid, Gap(10), Pad(12),
		Background(0, 0, 75, 1)), func() {
		if Win98Button("OK", false) {
			note("98 OK")
		}
		if Win98Button("Cancel", false) {
			note("98 Cancel")
		}
		if Win98Button("Apply", false) {
			note("98 Apply")
		}
		if Win98Button("Disabled", true) {
			note("98 Disabled")
		}
	})
	Container(Attrs(Row, CrossMid, Gap(10)), func() {
		CheckBox(&win98Disabled, "Disable the next 98 button")
		if Win98Button("Toggle target", win98Disabled) {
			note("98 toggle target")
		}
	})

	// --- XP Luna --------------------------------------------------------------
	// https://botoxparty.github.io/XP.css/ — themes/XP/_buttons.scss
	section("Windows XP Luna (XP.css)")
	Label("Blue border, white→beige vertical gradient, gold inset hover, pressed gradient invert. Soft 3px corners.",
		FontSize(12), TextColor(0, 0, 45, 1))
	Container(Attrs(Row, Wrap, CrossMid, Gap(10), Pad(14),
		Background(51, 33, 89, 1)), func() { // #ece9d8 surface
		if XPButton("OK", false) {
			note("XP OK")
		}
		if XPButton("Cancel", false) {
			note("XP Cancel")
		}
		if XPButton("Apply", false) {
			note("XP Apply")
		}
		if XPButton("Disabled", true) {
			note("XP Disabled")
		}
	})
	Container(Attrs(Row, CrossMid, Gap(10)), func() {
		CheckBox(&xpDisabled, "Disable the next XP button")
		if XPButton("Toggle target", xpDisabled) {
			note("XP toggle target")
		}
	})

	// --- log ------------------------------------------------------------------
	Container(Attrs(Pad2(10, 14), Background(0, 0, 100, 1), Corners(4),
		BorderWidth(1), BorderColor(0, 0, 0, 0.08)), func() {
		Label("Last action", FontSize(11), TextColor(0, 0, 45, 1))
		Label(clickLog, FontSize(14), FontWeight(WeightBold))
	})
	ScrollBars()
}

func section(title string) {
	Container(Attrs(Gap(8)), func() {
		Label(title, FontWeight(WeightBold), FontSize(14), TextColor(0, 0, 30, 1))
	})
}

func note(s string) {
	clickLog = fmt.Sprintf("clicked: %s", s)
}

// ---------------------------------------------------------------------------
// Flat button — solid fill, no bevel, soft radius. Hover darkens; press
// darkens further. Bootstrap/Material primary, not a default AccentButton look.
// ---------------------------------------------------------------------------

var (
	flatPrimary = Vec4{211, 72, 48, 1} // bootstrap-ish blue
	flatSuccess = Vec4{134, 55, 40, 1}
	flatDanger  = Vec4{354, 70, 48, 1}
)

// FlatButton is a filled flat button. Lives in this demo only.
func FlatButton(label string, accent Vec4, disabled bool) bool {
	var clicked bool
	face := accent
	text := ContrastingTextColor(accent)

	Container(Attrs(Row, CrossMid, Corners(4), Pad2(8, 16)), func() {
		st := ProcessButtonEvents(disabled)
		clicked = st.Clicked

		if disabled {
			face = Vec4{0, 0, 78, 1}
			text = Vec4{0, 0, 55, 1}
		} else if st.Active {
			face = Vec4{accent[0], accent[1], accent[2] - 12, 1}
		} else if st.Hovered {
			face = Vec4{accent[0], accent[1], accent[2] - 6, 1}
		}
		ClampColorVec(&face)

		ModAttrs(BackgroundVec(face))
		// Material-ish resting shadow; lifts slightly more on hover.
		if !disabled {
			blur := float32(2)
			if st.Hovered && !st.Active {
				blur = 6
			}
			if !st.Active {
				ModAttrs(BoxShadow(blur))
			}
		}

		Label(label, FontSize(13), FontWeight(WeightMedium), TextColorVec(text))
	})
	return clicked
}

// FlatOutlineButton is a bordered flat button that fills on hover.
func FlatOutlineButton(label string, accent Vec4, disabled bool) bool {
	var clicked bool
	bg := Vec4{0, 0, 100, 1}
	text := accent
	border := accent

	Container(Attrs(Row, CrossMid, Corners(4), Pad2(7, 15),
		BorderWidth(1.5), BorderColorVec(border)), func() {
		st := ProcessButtonEvents(disabled)
		clicked = st.Clicked

		if disabled {
			bg = Vec4{0, 0, 96, 1}
			text = Vec4{0, 0, 60, 1}
			border = Vec4{0, 0, 75, 1}
		} else if st.Active {
			bg = Vec4{accent[0], accent[1], accent[2] - 8, 1}
			text = ContrastingTextColor(bg)
		} else if st.Hovered {
			bg = accent
			text = ContrastingTextColor(accent)
		}

		ModAttrs(BackgroundVec(bg), BorderColorVec(border))
		Label(label, FontSize(13), FontWeight(WeightMedium), TextColorVec(text))
	})
	return clicked
}

// ---------------------------------------------------------------------------
// Win98Button — classic raised 3D bevel (98.css / Win32 push button).
// Light top-left, dark bottom-right; press inverts the bevel and nudges label.
// ---------------------------------------------------------------------------

// Win98Button is a Windows 98–style raised button. Demo only.
func Win98Button(label string, disabled bool) bool {
	var clicked bool

	// Classic silver face (approximate #D4D0C8 / system button face).
	face := Vec4{40, 8, 82, 1}
	hi := Vec4{0, 0, 100, 1} // top-left highlight
	lo := Vec4{0, 0, 45, 1}  // bottom-right shadow
	outer := Vec4{0, 0, 25, 1}
	text := Vec4{0, 0, 10, 1}

	// Outermost blackish frame (classic 3D push button outline).
	Container(Attrs(BackgroundVec(outer), Pad(1)), func() {
		st := ProcessButtonEvents(disabled)
		clicked = st.Clicked

		if disabled {
			face = Vec4{0, 0, 88, 1}
			hi = Vec4{0, 0, 94, 1}
			lo = Vec4{0, 0, 70, 1}
			text = Vec4{0, 0, 55, 1}
		} else if st.Active {
			// Inset: swap highlight/shadow; slightly darker face.
			hi, lo = lo, hi
			face = Vec4{40, 8, 78, 1}
		} else if st.Hovered {
			face = Vec4{40, 10, 86, 1}
		}

		// Raised (or inset when Active): dark pad on bottom+right, light on top+left.
		// Pad4 is top, right, bottom, left. Outer size is fixed; press only
		// redistributes face padding so the label shifts down-right 1px.
		Container(Attrs(BackgroundVec(lo), Pad4(0, 1, 1, 0)), func() {
			Container(Attrs(BackgroundVec(hi), Pad4(1, 0, 0, 1)), func() {
				padT, padR, padB, padL := float32(5), float32(14), float32(5), float32(14)
				if st.Active && !disabled {
					padT, padR, padB, padL = 6, 13, 4, 15
				}
				Container(Attrs(Row, CrossMid, BackgroundVec(face),
					Pad4(padT, padR, padB, padL)), func() {
					Label(label, FontSize(12), TextColorVec(text))
				})
			})
		})
	})
	return clicked
}

// ---------------------------------------------------------------------------
// XPButton — Windows XP Luna command button (XP.css themes/XP/_buttons.scss).
//
//   idle:    1px #003c74 border, 3px radius, vertical gradient
//            #fff → #ecebe5 (~86%) → #d8d0c4
//   hover:   gold inset rim (#fff0cf / #fdd889 / #fbc761 / #e5a01a)
//   active:  inverted gradient (darker top, lighter bottom)
//   disabled: muted border/face, gray label
// ---------------------------------------------------------------------------

// XPButton is a Windows XP Luna–style button. Demo only.
func XPButton(label string, disabled bool) bool {
	var clicked bool

	// Palette (HSLA approximations of the XP.css hex values).
	borderBlue := Vec4{209, 100, 23, 1} // #003c74
	borderMute := Vec4{50, 10, 70, 1}
	// Idle gradient: white at top → beige at bottom (Grad is bottom delta).
	faceIdle := Vec4{0, 0, 100, 1}   // #fff
	gradIdle := Vec4{40, 12, -14, 0} // toward #d8d0c4-ish
	// Active: start grayish, lighten toward bottom.
	faceActive := Vec4{40, 6, 78, 1} // #cdcac3-ish
	gradActive := Vec4{0, 0, 14, 0}  // toward #f2f2f1
	// Gold hover/active rim (approximates multi-inset gold shadows).
	goldOuter := Vec4{38, 80, 50, 1} // #e5a01a-ish
	goldInner := Vec4{42, 95, 78, 1} // #fdd889-ish
	text := Vec4{0, 0, 13, 1}        // #222
	clear := Vec4{0, 0, 0, 0}        // transparent — no idle white band

	const (
		radius float32 = 3
		minW   float32 = 75
		rim    float32 = 3 // gold glow thickness
	)

	// Outer carries the blue frame and the face gradient (full bleed). Rim
	// pads on top use transparent bg when idle so the gradient shows through;
	// gold only when hovered or pressed.
	Container(Attrs(
		Corners(radius),
		BorderWidth(1), BorderColorVec(borderBlue),
		MinWidth(minW),
		BackgroundVec(faceIdle), GradVec(gradIdle),
	), func() {
		st := ProcessButtonEvents(disabled)
		clicked = st.Clicked

		face := faceIdle
		grad := gradIdle
		border := borderBlue
		if disabled {
			face = Vec4{50, 8, 92, 1}
			grad = Vec4{}
			border = borderMute
			text = Vec4{0, 0, 55, 1}
		} else if st.Active {
			face = faceActive
			grad = gradActive
		}
		ModAttrs(BackgroundVec(face), GradVec(grad), BorderColorVec(border))

		// Gold on hover AND active (stay orange while pressed). Idle: clear
		// so the face gradient is uninterrupted — no solid white frame.
		rimLo, rimHi := clear, clear
		if !disabled && (st.Hovered || st.Active) {
			rimLo, rimHi = goldOuter, goldInner
		}
		innerR := radius - 1
		if innerR < 0 {
			innerR = 0
		}

		Container(Attrs(Expand, BackgroundVec(rimLo), Pad4(0, rim, rim, 0), Corners(innerR)), func() {
			Container(Attrs(Expand, BackgroundVec(rimHi), Pad4(rim, 0, 0, rim), Corners(innerR)), func() {
				// Center must repaint the face: gold parents fill their whole
				// rect, so a transparent hole would still be gold.
				padT, padR, padB, padL := float32(4), float32(12), float32(4), float32(12)
				if st.Active && !disabled {
					padT, padR, padB, padL = 5, 11, 3, 13
				}
				Container(Attrs(Expand, Row, CrossMid,
					BackgroundVec(face), GradVec(grad),
					Pad4(padT, padR, padB, padL),
				), func() {
					Filler(1)
					Label(label, FontSize(11), TextColorVec(text))
					Filler(1)
				})
			})
		})
	})
	return clicked
}
