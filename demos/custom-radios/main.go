package main

// custom-radios is a proof of concept for ProcessButtonEvents on mutually
// exclusive options: Material-style radios and XP.css Luna radios, demo-only.
// Default OptionButton sits above for comparison.

import (
	"os"
	"fmt"

	"go.hasen.dev/shirei/app"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

func main() {
	if len(os.Args) >= 3 && os.Args[1] == "--png" {
		if err := RenderToPNG(os.Args[2], 640, 560, root); err != nil {
			fmt.Println("render to png failed:", err)
			os.Exit(1)
		}
		return
	}
	app.SetupWindow("Custom Radios", 640, 560)
	app.Run(root)
}

var (
	defaultMood = "ok"

	matSize   = "m"
	matTheme  = "light"
	matLocked bool

	xpDrive   = "c"
	xpLocked  bool
	actionLog = "pick an option…"
)

func root() {
	ModAttrs(Viewport, Background(220, 12, 96, 1), Pad(28), Gap(22))
	ScrollOnInput()

	Label("Custom radios via ProcessButtonEvents", FontWeight(WeightBold), FontSize(18))
	Label("Demo-only skins — not in the widget catalogue. Same click model as default OptionButton.",
		FontSize(13), TextColor(0, 0, 40, 1))

	// --- default ----------------------------------------------------------------
	section("Default widgets (for comparison)")
	Container(Attrs(Gap(6)), func() {
		OptionButton(&defaultMood, "Great", "great")
		OptionButton(&defaultMood, "Okay", "ok")
		OptionButton(&defaultMood, "Rough", "rough")
	})

	// --- Material -------------------------------------------------------------
	section("Material Design inspired")
	Container(Attrs(Gap(10)), func() {
		MaterialRadio(&matSize, "Small", "s", materialPrimary, false)
		MaterialRadio(&matSize, "Medium", "m", materialPrimary, false)
		MaterialRadio(&matSize, "Large", "l", materialPrimary, false)
		MaterialRadio(&matSize, "Disabled sample", "x", materialPrimary, true)
	})
	Container(Attrs(Row, CrossMid, Gap(12)), func() {
		CheckBox(&matLocked, "Disable the next Material radios")
		Container(Attrs(Gap(8)), func() {
			MaterialRadio(&matTheme, "Light", "light", materialTeal, matLocked)
			MaterialRadio(&matTheme, "Dark", "dark", materialTeal, matLocked)
		})
	})

	// --- XP -------------------------------------------------------------------
	section("Windows XP (Luna / XP.css inspired)")
	Container(Attrs(Gap(8), Pad(14), Background(51, 33, 89, 1),
		BorderWidth(1), BorderColor(50, 10, 70, 1)), func() {
		XPRadio(&xpDrive, "Local Disk (C:)", "c", false)
		XPRadio(&xpDrive, "DVD Drive (D:)", "d", false)
		XPRadio(&xpDrive, "Network Drive (Z:)", "z", false)
		XPRadio(&xpDrive, "Disabled sample", "x", true)
	})
	Container(Attrs(Row, CrossMid, Gap(12)), func() {
		CheckBox(&xpLocked, "Disable the next XP radios")
		Container(Attrs(Gap(6)), func() {
			XPRadio(&xpDrive, "Floppy (A:)", "a", xpLocked)
			XPRadio(&xpDrive, "CD-ROM (E:)", "e", xpLocked)
		})
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
// Material radio — outlined circle; accent ring + solid center when on.
// ---------------------------------------------------------------------------

var (
	materialPrimary = Vec4{211, 72, 48, 1}
	materialTeal    = Vec4{174, 60, 40, 1}
)

// MaterialRadio is a Material-style radio. Demo only.
func MaterialRadio[T comparable](target *T, label string, value T, accent Vec4, disabled bool) {
	const size float32 = 20
	const ring float32 = 2

	Container(Attrs(Row, Gap(10), CrossMid), func() {
		st := ProcessButtonEvents(disabled)
		if st.Clicked {
			*target = value
			note(fmt.Sprintf("Material %q → %v", label, value))
		}
		selected := *target == value

		border := Vec4{0, 0, 55, 1}
		bg := Vec4{0, 0, 100, 1}
		dot := accent
		text := Vec4{0, 0, 15, 1}

		if disabled {
			border = Vec4{0, 0, 75, 1}
			bg = Vec4{0, 0, 96, 1}
			dot = Vec4{0, 0, 75, 1}
			text = Vec4{0, 0, 55, 1}
		} else if selected {
			border = accent
			if st.Hovered {
				border = Vec4{accent[0], accent[1], accent[2] + 6, 1}
			}
			if st.Active {
				border = Vec4{accent[0], accent[1], accent[2] - 6, 1}
			}
			ClampColorVec(&border)
		} else {
			if st.Hovered {
				border = accent
				bg = Vec4{accent[0], accent[1] * 0.2, 97, 1}
			}
			if st.Active {
				bg = Vec4{accent[0], accent[1] * 0.3, 94, 1}
			}
		}

		Container(Attrs(FixSize(size, size), Corners(size/2), Center,
			BackgroundVec(bg), BorderWidth(ring), BorderColorVec(border)), func() {
			if selected {
				d := size * 0.45
				Element(Attrs(FixSize(d, d), Corners(d/2), BackgroundVec(dot)))
			}
		})

		if label != "" {
			Label(label, FontSize(14), TextColorVec(text))
		}
	})
}

// ---------------------------------------------------------------------------
// XP radio — always a sunken circle field (never a raised button).
// XP.css Luna: blue border, beige→white face, gold hover, green center dot.
// ---------------------------------------------------------------------------

// XPRadio is a Windows XP Luna-style radio. Demo only.
func XPRadio[T comparable](target *T, label string, value T, disabled bool) {
	const face float32 = 13

	borderBlue := Vec4{207, 63, 31, 1} // #1d5281
	borderMute := Vec4{50, 10, 76, 1}  // #cac8bb
	faceIdle := Vec4{60, 8, 86, 1}
	faceIdleGrad := Vec4{0, 0, 14, 0}
	faceActive := Vec4{50, 5, 68, 1}
	faceActiveGrad := Vec4{0, 0, 16, 0}
	goldLo := Vec4{38, 93, 59, 1}
	goldHi := Vec4{43, 95, 80, 1}
	dotGreen := Vec4{120, 68, 38, 1} // ~#22a122
	dotMute := Vec4{50, 10, 76, 1}
	text := Vec4{0, 0, 13, 1}

	Container(Attrs(Row, Gap(6), CrossMid), func() {
		st := ProcessButtonEvents(disabled)
		if st.Clicked {
			*target = value
			note(fmt.Sprintf("XP %q → %v", label, value))
		}
		selected := *target == value

		border := borderBlue
		bg := faceIdle
		grad := faceIdleGrad
		dot := dotGreen
		if disabled {
			border = borderMute
			bg = Vec4{0, 0, 100, 1}
			grad = Vec4{}
			dot = dotMute
			text = Vec4{0, 0, 55, 1}
		} else if st.Active {
			bg = faceActive
			grad = faceActiveGrad
		}

		rimLo, rimHi := bg, bg
		if !disabled && st.Hovered && !st.Active {
			rimLo, rimHi = goldLo, goldHi
		}

		// Circular well: outer blue ring, optional gold inset on hover.
		r := face / 2
		Container(Attrs(BackgroundVec(border), Pad(1), Corners(r+1)), func() {
			Container(Attrs(BackgroundVec(rimLo), Pad4(0, 1, 1, 0), Corners(r)), func() {
				Container(Attrs(BackgroundVec(rimHi), Pad4(1, 0, 0, 1), Corners(r-1)), func() {
					inner := face - 2
					Container(Attrs(FixSize(inner, inner), Corners(inner/2),
						BackgroundVec(bg), GradVec(grad), Center), func() {
						if selected {
							// XP.css uses a small green pixel blob (~5px).
							d := float32(5)
							Element(Attrs(FixSize(d, d), Corners(d/2), BackgroundVec(dot)))
						}
					})
				})
			})
		})

		if label != "" {
			Label(label, FontSize(11), TextColorVec(text))
		}
	})
}
