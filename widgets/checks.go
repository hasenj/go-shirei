package widgets

// checkboxes and radios
//
import (
	. "go.hasen.dev/shirei"
)

// CheckBoxAttrs configures CheckBoxExt.
type CheckBoxAttrs struct {
	Accent Vec4 // zero value: use the package-level Accent
	Size   f32  // box side length; zero value: 18
}

// CheckBox is an independent on/off toggle: pressing flips *target.
func CheckBox(target *bool, label string) {
	CheckBoxExt(target, label, CheckBoxAttrs{})
}

// CheckBoxExt renders a checkbox with a per-instance accent and size, flipping
// *target when pressed. See CheckBox for the plain form.
func CheckBoxExt(target *bool, label string, attrs CheckBoxAttrs) {
	if attrs.Size == 0 {
		attrs.Size = 12
	}
	accent := AccentOrFallback(attrs.Accent, DefaultAccent)
	corners := attrs.Size * 0.28
	padTop := attrs.Size * 0.14

	Container(Attrs(Row, Gap(6), CrossMid), func() {
		if PressAction() {
			*target = !*target
		}
		hovered := IsHovered()

		boxBG := Vec4{0, 0, 100, 1}
		grad := Vec4{0, 0, -12, 0}
		if hovered {
			boxBG = Vec4{accent[0], accent[1] * 0.3, 96, 1}
		}
		if *target {
			grad[2] = 12
			boxBG = accent
			if hovered {
				boxBG[2] += 5
			}
		}

		// FixSize, not MinSize: the oversized tick glyph's own layout box
		// (see below) is bigger than the box and must not grow it — Clip
		// then hides the glyph's overflow instead of letting it expand.
		Container(Attrs(FixSize(attrs.Size, attrs.Size), Pad4(padTop, 0, 0, 0), Corners(corners), BackgroundVec(boxBG), GradVec(grad), BorderColor(accent[0], accent[1], accent[2], accent[3]), BorderWidth(1.5), Clip, Center), func() {
			if *target {
				// SymITick's ink sits well inside its own em box, so it needs
				// to be sized well past the box to read as a bold checkmark.
				Icon(SymITick, FontSize(attrs.Size*1.5), TextColor(0, 0, 100, 1))
			}
		})

		if label != "" {
			Label(label, FontSize(12))
		}
	})
}

// OptionButtonAttrs configures OptionButtonExt.
type OptionButtonAttrs struct {
	Accent Vec4 // zero value: use the package-level Accent
	Size   f32  // circle diameter; zero value: 18
}

// OptionButton is a radio button: pressing sets *target to this button's
// value. Several sharing one target are mutually exclusive.
func OptionButton[T comparable](target *T, label string, value T) {
	OptionButtonExt(target, label, value, OptionButtonAttrs{})
}

// OptionButtonExt is OptionButton with a per-instance accent/size, the same
// pattern as CheckBoxExt — selected fills with the accent color and shows a
// white dot; unselected is an accent-outlined ring, same as an unchecked box.
func OptionButtonExt[T comparable](target *T, label string, value T, attrs OptionButtonAttrs) {
	if attrs.Size == 0 {
		attrs.Size = 18
	}
	accent := AccentOrFallback(attrs.Accent, DefaultAccent)
	grad := Vec4{0, 0, -12, 0}

	Container(Attrs(Row, Gap(6), CrossMid), func() {
		if PressAction() {
			*target = value
		}
		hovered := IsHovered()
		selected := *target == value

		ringBG := Vec4{0, 0, 100, 1}
		if hovered {
			ringBG = Vec4{accent[0], accent[1] * 0.3, 96, 1}
		}
		if selected {
			ringBG = accent
			grad[2] = 12
			if hovered {
				ringBG[2] += 5
			}
		}

		Container(Attrs(FixSize(attrs.Size, attrs.Size), Corners(attrs.Size/2), BackgroundVec(ringBG), GradVec(grad), BorderColor(accent[0], accent[1], accent[2], accent[3]), BorderWidth(1.5), Center), func() {
			if selected {
				dot := attrs.Size * 0.4
				Element(Attrs(FixSize(dot, dot), Corners(dot/2), Background(accent[0], 10, 98, 1)))
			}
		})

		if label != "" {
			Label(label, FontSize(12))
		}
	})
}

// ToggleSwitchAttrs configures ToggleSwitchExt.
type ToggleSwitchAttrs struct {
	Accent Vec4 // zero value: use the package-level Accent
	Height f32  // track height; zero value: 24
}

// ToggleSwitch is an iOS-style on/off switch: accent-filled track with a
// white knob when on, pale gray track when off.
func ToggleSwitch(on *bool) {
	ToggleSwitchExt(on, ToggleSwitchAttrs{})
}

// ToggleSwitchExt renders a toggle switch with a per-instance accent and height,
// flipping *on when clicked.
func ToggleSwitchExt(on *bool, attrs ToggleSwitchAttrs) {
	if attrs.Height == 0 {
		attrs.Height = 24
	}
	accent := AccentOrFallback(attrs.Accent, DefaultAccent)
	width := attrs.Height * 1.8
	margin := attrs.Height * 0.1
	knobSize := attrs.Height - margin*2

	Container(Attrs(Row, FixSize(width, attrs.Height), Corners(attrs.Height/2), Pad(margin), CrossAlign(AlignMiddle)), func() {
		if IsClicked() {
			*on = !*on
		}
		hovered := IsHovered()

		trackBG := Vec4{0, 0, 88, 1}
		trackBorder := Vec4{0, 0, 75, 1}
		var grad Vec4
		if hovered {
			trackBG[2] -= 3
		}
		if *on {
			trackBG = accent
			trackBorder = accent // same as fill: reads as no border, like a filled checkbox
			grad = Vec4{0, 0, -8, 0}
			if hovered {
				trackBG[2] += 4
			}
		}
		ModAttrs(BackgroundVec(trackBG), GradVec(grad), BorderColor(trackBorder[0], trackBorder[1], trackBorder[2], trackBorder[3]), BorderWidth(1))

		if *on {
			// spacer to push the knob to the right
			Element(Attrs(Grow(1)))
		} else {
			Nil()
		}

		// the knob
		Container(Attrs(FixSize(knobSize, knobSize), Corners(knobSize/2), Background(0, 0, 100, 1), Grad(0, 0, -6, 0), BoxShadow(3)), func() {})
	})
}
