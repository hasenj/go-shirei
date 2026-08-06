package widgets

import (
	. "go.hasen.dev/shirei"
)

// ButtonDefaultSize is the default font size for a button's label and icon.
const ButtonDefaultSize = DefaultTextSize
const ButtonCtrlSize = ButtonDefaultSize * 0.8

type f32 = float32

// ButtonState is one frame's interaction snapshot for the current container.
// Call ProcessButtonEvents inside a Container body; it binds to that container.
//
// The default Button / CtrlButton widgets are thin combinations of this analysis
// plus ButtonExt chrome. Custom buttons should call ProcessButtonEvents and
// paint whatever they want from the returned state — no skin interface, no
// inverted view callback.
type ButtonState struct {
	Hovered  bool
	Active   bool // pointer captured on mouse-down, held until release
	Clicked  bool // completed click this frame (release while still hovered)
	Disabled bool
	// Local is the pointer position relative to the container's screen
	// top-left. Meaningful while Hovered or Active; otherwise whatever the
	// pointer last reported relative to this box.
	Local Vec2
}

// ProcessButtonEvents analyzes pointer interaction with the current container
// and returns a snapshot. When disabled, no press capture runs and Clicked is
// always false. Does not take keyboard focus.
//
// Interaction:
//   - Touch: Active while a latched contact is down; Clicked on lift if the
//     contact was still over this container on the last active frame
//   - Mouse: PressAction (down while hovered → Active; release while hovered
//     → Clicked); ignored while MouseFromTouch so a delayed synthetic
//     mouse-up cannot re-engage after a finger lifts
//
// Typical custom button:
//
//	Container(Attrs(...), func() {
//	    st := ProcessButtonEvents(false)
//	    if st.Hovered { ModAttrs(...) }
//	    if st.Active  { ModAttrs(...) }
//	    Label("Go")
//	    clicked = st.Clicked
//	})
func ProcessButtonEvents(disabled bool) ButtonState {
	var st ButtonState
	st.Disabled = disabled
	st.Hovered = IsHovered()
	origin := GetScreenRect().Origin
	st.Local = Vec2Sub(GetInputState().MousePoint, origin)
	if disabled {
		// Still report Hovered/Local so skins can dim or show a forbid
		// cue; never capture the pointer or report a click.
		return st
	}

	// Latched contact while a finger presses this control (0 = none).
	// over is whether that contact still hit us as of the last frame it
	// was active — used on lift so Clicked matches press-release-while-over.
	type touchPress struct {
		id   uint32
		over bool
	}
	tp := Use[touchPress]("btn-touch")

	if tp.id != 0 {
		if ti, ok := TouchById(tp.id); ok {
			st.Local = Vec2Sub(ti.Pos, origin)
			st.Active = true
			over := false
			for _, id := range TouchingIds(nil) {
				if id == tp.id {
					over = true
					break
				}
			}
			tp.over = over
		} else {
			// Contact ended this frame.
			if tp.over {
				st.Clicked = true
				RequestNextFrame()
			}
			tp.id = 0
			tp.over = false
		}
	}
	if tp.id == 0 && IsTouched() {
		ids := TouchingIds(nil)
		if len(ids) > 0 {
			if ti, ok := TouchById(ids[0]); ok {
				tp.id = ids[0]
				tp.over = true
				st.Local = Vec2Sub(ti.Pos, origin)
				st.Active = true
			}
		}
	}

	// Mouse path only when not driven by a finger (and not already on a touch).
	if tp.id == 0 && !GetInputState().MouseFromTouch {
		if PressAction() {
			st.Clicked = true
		}
		if IsActive() {
			st.Active = true
			st.Local = Vec2Sub(GetInputState().MousePoint, origin)
		}
	}

	return st
}

// ProcessToggleEvents is ProcessButtonEvents plus flipping *on on a completed
// click. CheckBox and ToggleSwitch share this interaction model — they differ
// only in chrome. OptionButton / SegmentedControl are different (they assign a
// discrete value, not a bool flip).
//
//	Container(Attrs(...), func() {
//	    st := ProcessToggleEvents(&enabled, false)
//	    // paint from st and *enabled (or st after flip: *enabled is already updated)
//	})
func ProcessToggleEvents(on *bool, disabled bool) ButtonState {
	st := ProcessButtonEvents(disabled)
	if st.Clicked {
		*on = !*on
	}
	return st
}

// ButtonLook is the continuous chrome axes for the default elevated-accent
// button face. No mode flags: primary vs ctrl differ only by these numbers
// (see DefaultButtonLook / DefaultCtrlButtonLook).
type ButtonLook struct {
	// TextSize is used when ButtonAttrs.TextSize is zero.
	TextSize f32
	// PushDown is the resting elevation lip (bottom) / press inset (top).
	// Primary uses 1; flat ctrl uses 0.
	PushDown f32
	// TopBoost lightens the face relative to the accent light channel.
	TopBoost f32
	// ElevationDrop darkens the lip under the resting face.
	ElevationDrop f32
	// PadScale multiplies horizontal and vertical padding (1 primary, 0.8 ctrl).
	// Zero is treated as 1.
	PadScale f32
}

// DefaultButtonLook returns the primary elevated button chrome.
func DefaultButtonLook() ButtonLook {
	return ButtonLook{
		TextSize:      ButtonDefaultSize,
		PushDown:      1,
		TopBoost:      8,
		ElevationDrop: 16,
		PadScale:      1,
	}
}

// DefaultCtrlButtonLook returns the compact flat "control" button chrome.
func DefaultCtrlButtonLook() ButtonLook {
	return ButtonLook{
		TextSize:      ButtonCtrlSize,
		PushDown:      0,
		TopBoost:      4,
		ElevationDrop: 8,
		PadScale:      0.8,
	}
}

// ButtonAttrs configures content and theming for ButtonExt. Look (push,
// elevation, pad scale) is a separate ButtonLook argument — not a flag here.
type ButtonAttrs struct {
	Disabled  bool      // draw greyed-out and ignore clicks
	Accent    Vec4      // zero value: use the package-level ButtonAccent
	TextSize  f32       // label and icon size; zero uses the look's TextSize
	TextStyle Style     // label font style (e.g. italic)
	Icon      IconGlyph // optional leading icon (Sym*, Typ*, or custom); zero = none
}

// Button renders a labeled primary button with an optional leading icon (pass
// NoIcon for none) and returns true on the frame it is clicked.
func Button(icon IconGlyph, label string) bool {
	return ButtonExt(label, ButtonAttrs{Icon: icon}, DefaultButtonLook())
}

// ButtonWithAccent is Button with an explicit accent color.
func ButtonWithAccent(icon IconGlyph, label string, accent Vec4) bool {
	return ButtonExt(label, ButtonAttrs{Icon: icon, Accent: accent}, DefaultButtonLook())
}

// CtrlButton renders a compact flat control button (smaller padding, no push
// lip, nylon accent by default), enabled or disabled by the enabled flag.
func CtrlButton(icon IconGlyph, label string, enabled bool) bool {
	return ButtonExt(label, ButtonAttrs{
		Icon:     icon,
		Disabled: !enabled,
		Accent:   AccentNylon,
	}, DefaultCtrlButtonLook())
}

// CtrlButtonWithAccent is CtrlButton with an explicit accent color.
func CtrlButtonWithAccent(icon IconGlyph, label string, accent Vec4, enabled bool) bool {
	return ButtonExt(label, ButtonAttrs{
		Icon:     icon,
		Disabled: !enabled,
		Accent:   accent,
	}, DefaultCtrlButtonLook())
}

// ButtonExt renders the elevated-accent button face: ProcessButtonEvents plus
// the continuous look axes. Button and CtrlButton are the common defaults over
// this; pass DefaultButtonLook or DefaultCtrlButtonLook, or a custom ButtonLook.
func ButtonExt(label string, attrs ButtonAttrs, look ButtonLook) bool {
	if attrs.TextSize == 0 {
		attrs.TextSize = look.TextSize
		if attrs.TextSize == 0 {
			attrs.TextSize = ButtonDefaultSize
		}
	}
	// Design units × Host.ComfortScale (default and caller-supplied text size).
	attrs.TextSize = comfort(attrs.TextSize)
	padScale := look.PadScale
	if padScale == 0 {
		padScale = 1
	}
	pushDown := look.PushDown

	var padh = attrs.TextSize * 0.8 * padScale
	// vertical padding totals one line height, minus the push lip the press
	// mechanic always adds (top padding when active, elevation lip when
	// idle — see below): this is what makes a default button's height
	// match a default TextInput's (attrs.FontSize + attrs.FontSize).
	var padv = (attrs.TextSize - pushDown) / 2 * padScale
	var br = attrs.TextSize * 0.3

	accent := AccentOrFallback(attrs.Accent, ButtonAccent)
	textColor := ContrastingTextColor(accent)

	var action bool
	Container(Attrs(), func() {
		st := ProcessButtonEvents(attrs.Disabled)
		action = st.Clicked

		hue, sat, light := accent[0], accent[1], accent[2]
		topBoost := look.TopBoost
		elevationDrop := look.ElevationDrop

		borderWidth := f32(1)
		borderColor := accent
		borderColor[2] = 20
		borderColor[3] = 0.2

		if attrs.Disabled {
			light = 90
			topBoost, elevationDrop = 0, 0
			textColor[2] = 40
			textColor[3] = 0.5
			// Same stroke width as enabled; only color/alpha change for the mute look.
			borderColor = Vec4{0, 0, 75, 1}
		}

		top := light + topBoost
		var grad Vec4
		grad[LIGHT] = -topBoost // bottom of the gradient settles back to `light`

		highlight := top + 3
		presslight := top - 3
		if attrs.Disabled {
			highlight, presslight = top, top
		}

		background := Vec4{hue, sat, top, 1}
		elevationColor := Vec4{hue, sat, light - elevationDrop, 1}
		shadowPadding := Vec4{0}

		if st.Hovered && !attrs.Disabled {
			background[2] = highlight
		}

		if st.Active {
			background[2] = presslight
			ModAttrs(func(a *AttrSet) {
				a.Padding[PAD_TOP] = pushDown
			})
		} else {
			shadowPadding[PAD_BOTTOM] = pushDown
		}

		ClampColorVec(&background)

		Container(Attrs(BackgroundVec(elevationColor), PadVec(shadowPadding), Corners(br+1)), func() {
			var face = Attrs(Row, CrossMid, Corners(br), Pad2(padv, padh), Gap(padh/2), BackgroundVec(background), GradVec(grad),
				BorderColorVec(borderColor), BorderWidth(borderWidth))
			Container(face, func() {
				if attrs.Icon.Rune != 0 {
					Icon(attrs.Icon, FontSize(attrs.TextSize), TextColorVec(textColor))
				}
				if label != "" {
					Label(label, FontSize(attrs.TextSize), FontStyle(attrs.TextStyle), TextColorVec(textColor))
				}
			})
		})
	})
	return action
}

// Link renders text that opens url in the system browser when clicked. Extra
// text attributes style the label. Queues via OpenURL → RequestOpenURL; the
// backend opens after the frame (desktop / Safari / Android ACTION_VIEW).
func Link(label string, url string, fns ...TextStyleFn) {
	Container(Attrs(Row), func() {
		if IsClicked() {
			OpenURL(url)
		}
		Label(label, fns...)
	})
}

// Filler adds a flexible empty element with grow factor g, pushing the
// surrounding content apart — e.g. to right-align a toolbar item.
func Filler(g f32) {
	Element(Attrs(Grow(g)))
}

// Spacer adds a fixed empty element of s pixels along the current container's
// main axis: width in a row, height in a column.
func Spacer(s f32) {
	var width f32
	var height f32

	var a = GetAttrs()
	if a.Row {
		width = s
	} else {
		height = s
	}

	Element(Attrs(FixWidth(width), FixHeight(height)))
}
