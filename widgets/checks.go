package widgets

// checkboxes, radios, and toggle switches.
//
// Interaction: ProcessToggleEvents (a thin ProcessButtonEvents + flip *bool)
// on the outer row/track for CheckBox and ToggleSwitch. OptionButton uses
// ProcessButtonEvents and assigns a discrete value. Default chrome is separate.

import (
	. "go.hasen.dev/shirei"
)

// CheckBoxAttrs configures CheckBoxExt.
type CheckBoxAttrs struct {
	Accent Vec4 // zero value: use CurrentColorScheme.CheckBox
	Size   f32  // box side length; zero value: 12
}

// CheckBox is an independent on/off toggle: a completed click flips *target.
func CheckBox(target *bool, label string) {
	CheckBoxExt(target, label, CheckBoxAttrs{})
}

// CheckBoxExt renders a checkbox with a per-instance accent and size, flipping
// *target on click. See CheckBox for the plain form.
func CheckBoxExt(target *bool, label string, attrs CheckBoxAttrs) {
	style := CurrentColorScheme.CheckBox
	if attrs.Accent != (Vec4{}) {
		style = SelectionStyleWithAccent(style, attrs.Accent)
	}
	CheckBoxStyled(target, label, attrs, style, CurrentColorScheme.FocusRing)
}

// CheckBoxStyled renders a checkbox using explicit face, indicator, and focus
// colors. Colors are literal, including transparent zeros; Accent is ignored.
// The label inherits the surrounding text style. Size and interaction match
// CheckBoxExt. This renderer does not consult the active color scheme.
func CheckBoxStyled(target *bool, label string, attrs CheckBoxAttrs, style SelectionStyle, focusRing Vec4) {
	if attrs.Size == 0 {
		attrs.Size = 12
	}
	attrs.Size = comfort(attrs.Size)
	corners := attrs.Size * 0.28
	padTop := attrs.Size * 0.14
	gap := comfort(3)
	labelSize := comfort(12)

	Container(Attrs(Row, Gap(gap), CrossMid), func() {
		st := ProcessToggleEvents(target, false)
		NextAccessRole("checkbox")
		NextAccessChecked(*target)
		AssignAccess()

		stateStyle := style.Unselected
		if *target {
			stateStyle = style.Selected
		}
		paint := stateStyle.Normal
		if st.Active {
			paint = stateStyle.Pressed
		} else if st.Hovered {
			paint = stateStyle.Hovered
		}

		// FixSize, not MinSize: the oversized tick glyph's own layout box
		// (see below) is bigger than the box and must not grow it — Clip
		// then hides the glyph's overflow instead of letting it expand.
		Container(Attrs(Pad(3)), func() {
			Container(Attrs(FixSize(attrs.Size, attrs.Size), NoClip), func() {
				Container(Attrs(FixSize(attrs.Size, attrs.Size), Pad4(padTop, 0, 0, 0), Corners(corners), BackgroundVec(paint.Background), GradVec(paint.Gradient), BorderColorVec(paint.Border), BorderWidth(1), Clip, Center), func() {
					if *target {
						Icon(SymITick, FontSize(attrs.Size*1.5), TextColorVec(paint.Indicator))
					}
				})
				if st.FocusVisible {
					widgetFocusOutline(Vec2{attrs.Size, attrs.Size}, corners, focusRing)
				}
			})
		})

		if label != "" {
			Label(label, FontSize(labelSize))
		}
	})
}

// OptionButtonAttrs configures OptionButtonExt.
type OptionButtonAttrs struct {
	Accent Vec4 // zero value: use the active scheme
	Size   f32  // circle diameter; zero value: 18
}

var currentOptionGroup any

type optionGroupRun[T comparable] struct {
	target *T
}

// OptionGroup is a mutually exclusive set of OptionButton calls bound to
// *target. The group container is the access parent (role radiogroup).
//
//	NextAccessName("mood")
//	OptionGroup(&mood, func() {
//	    NextAccessName("great")
//	    OptionButton("Great", "great")
//	})
func OptionGroup[T comparable](target *T, body func()) {
	prev := currentOptionGroup
	currentOptionGroup = &optionGroupRun[T]{target: target}
	defer func() { currentOptionGroup = prev }()
	Container(Attrs(), func() {
		NextAccessRole("radiogroup")
		AssignAccess()
		if body != nil {
			body()
		}
	})
}

func optionGroupTarget[T comparable]() *T {
	run, ok := currentOptionGroup.(*optionGroupRun[T])
	if !ok || run == nil || run.target == nil {
		panic("widgets: OptionButton must be called from OptionGroup")
	}
	return run.target
}

// OptionButton is a radio button inside OptionGroup: a completed click sets
// the group's target to this button's value.
func OptionButton[T comparable](label string, value T) {
	OptionButtonExt(optionGroupTarget[T](), label, value, OptionButtonAttrs{})
}

// OptionButtonExt resolves radio paint with an optional accent and size override.
func OptionButtonExt[T comparable](target *T, label string, value T, attrs OptionButtonAttrs) {
	style := CurrentColorScheme.Radio
	if attrs.Accent != (Vec4{}) {
		style = SelectionStyleWithAccent(style, attrs.Accent)
	}
	OptionButtonStyled(target, label, value, attrs, style, CurrentColorScheme.FocusRing)
}

// OptionButtonStyled renders a radio with literal paint and focus colors. Accent is ignored.
func OptionButtonStyled[T comparable](target *T, label string, value T, attrs OptionButtonAttrs, style SelectionStyle, focusRing Vec4) {
	if attrs.Size == 0 {
		attrs.Size = 18
	}
	attrs.Size = comfort(attrs.Size)
	gap := comfort(6)
	labelSize := comfort(12)

	Container(Attrs(Row, Gap(gap), CrossMid), func() {
		st := ProcessButtonEvents(false)
		if st.Clicked {
			*target = value
		}
		selected := *target == value
		NextAccessRole("radio")
		NextAccessChecked(selected)
		AssignAccess()
		if st.FocusVisible {
			ModAttrs(BorderWidth(2), BorderColorVec(focusRing), Corners(3))
		}

		stateStyle := style.Unselected
		if selected {
			stateStyle = style.Selected
		}
		paint := stateStyle.Normal
		if st.Active {
			paint = stateStyle.Pressed
		} else if st.Hovered {
			paint = stateStyle.Hovered
		}

		Container(Attrs(FixSize(attrs.Size, attrs.Size), Corners(attrs.Size/2), BackgroundVec(paint.Background), GradVec(paint.Gradient), BorderColorVec(paint.Border), BorderWidth(1.5), Center), func() {
			if selected {
				dot := attrs.Size * 0.4
				Element(Attrs(FixSize(dot, dot), Corners(dot/2), BackgroundVec(paint.Indicator)))
			}
		})

		if label != "" {
			Label(label, FontSize(labelSize))
		}
	})
}

// ToggleSwitchAttrs configures ToggleSwitchExt.
type ToggleSwitchAttrs struct {
	Accent Vec4 // zero value: use the active scheme
	Height f32  // track height; zero value: 24
}

// ToggleSwitch draws an on/off track with a sliding knob using the active scheme.
func ToggleSwitch(on *bool) {
	ToggleSwitchExt(on, ToggleSwitchAttrs{})
}

// ToggleSwitchExt renders a toggle switch with a per-instance accent and height,
// flipping *on on a completed click (ProcessToggleEvents — same model as CheckBox).
func ToggleSwitchExt(on *bool, attrs ToggleSwitchAttrs) {
	style := CurrentColorScheme.Switch
	if attrs.Accent != (Vec4{}) {
		style = SelectionStyleWithAccent(style, attrs.Accent)
	}
	ToggleSwitchStyled(on, attrs, style, CurrentColorScheme.FocusRing)
}

// ToggleSwitchStyled renders a switch with literal track, knob, and focus paint. Accent is ignored.
func ToggleSwitchStyled(on *bool, attrs ToggleSwitchAttrs, style SelectionStyle, focusRing Vec4) {
	if attrs.Height == 0 {
		attrs.Height = 24
	}
	attrs.Height = comfort(attrs.Height)
	width := attrs.Height * 1.8
	margin := attrs.Height * 0.1
	knobSize := attrs.Height - margin*2

	Container(Attrs(Row, FixSize(width, attrs.Height), Corners(attrs.Height/2), Pad(margin), CrossAlign(AlignMiddle)), func() {
		st := ProcessToggleEvents(on, false)
		NextAccessRole("switch")
		NextAccessChecked(*on)
		AssignAccess()

		stateStyle := style.Unselected
		if *on {
			stateStyle = style.Selected
		}
		paint := stateStyle.Normal
		if st.Active {
			paint = stateStyle.Pressed
		} else if st.Hovered {
			paint = stateStyle.Hovered
		}
		border, width := paint.Border, float32(1)
		if st.FocusVisible {
			border, width = focusRing, 2
		}
		ModAttrs(BackgroundVec(paint.Background), GradVec(paint.Gradient), BorderColorVec(border), BorderWidth(width))

		if *on {
			// spacer to push the knob to the right
			Element(Attrs(Grow(1)))
		} else {
			Nil()
		}

		// the knob
		Container(Attrs(FixSize(knobSize, knobSize), Corners(knobSize/2), BackgroundVec(paint.Indicator), GradVec(paint.IndicatorGradient), BoxShadow(3)), func() {})
	})
}
