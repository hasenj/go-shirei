package widgets

import (
	"fmt"
	"os"
	"slices"

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
	Hovered      bool
	Active       bool // pointer captured on mouse-down, held until release
	Clicked      bool // completed click this frame (pointer or Space/Enter released while still engaged)
	Disabled     bool
	HasFocus     bool // this container holds keyboard focus
	FocusVisible bool // paint the keyboard focus indicator
	// Local is the pointer position relative to the container's screen
	// top-left. Meaningful while Hovered or Active; otherwise whatever the
	// pointer last reported relative to this box.
	Local Vec2
}

// ProcessButtonEvents analyzes pointer and keyboard interaction with the
// current container and returns a snapshot. Call it before any child is
// added (it may ModAttrs). When disabled, no press capture runs, Clicked
// is always false, and the container is kept out of the tab ring.
//
// Interaction:
//   - Focus: the container is Focusable; a pointer press on it takes
//     keyboard focus. Space or Enter while focused: Active while held,
//     Clicked on release (cancelled if focus is lost before release).
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
//	    if st.FocusVisible { ModAttrs(...) }
//	    Label("Go")
//	    clicked = st.Clicked
//	})
func ProcessButtonEvents(disabled bool) ButtonState {
	var st ButtonState
	st.Disabled = disabled
	action, requested := ProcessAccessAction(AccessPress|AccessFocus, disabled)
	st.Clicked = requested && action.Kind == AccessPress
	st.Hovered = IsHovered()
	origin := GetScreenRect().Origin
	st.Local = Vec2Sub(GetInputState().MousePoint, origin)
	if disabled {
		// Still report Hovered/Local so skins can dim or show a forbid
		// cue; never capture the pointer or report a click. Drop out of
		// the tab ring even if the caller passed Attrs(Focusable).
		ModAttrs(func(a *AttrSet) { a.Focusable = false })
		st.HasFocus = HasFocus()
		st.FocusVisible = HasVisibleFocus()
		return st
	}

	ModAttrs(Focusable)
	FocusOnClick()
	st.HasFocus = HasFocus()
	st.FocusVisible = HasVisibleFocus()

	type keyPress struct {
		key   KeyCode // 0 = none
		epoch int64
	}
	kp := Use[keyPress]("btn-key")
	if !st.HasFocus {
		kp.key = 0
	} else if kp.key == 0 {
		switch GetFrameInput().Key {
		case KeySpace, KeyEnter:
			ShowFocusIndicator()
			st.FocusVisible = true
			kp.key = GetFrameInput().Key
			kp.epoch = InputEpoch()
			st.Active = true
			RequestNextFrame()
		}
	} else {
		held := kp.epoch == InputEpoch() ||
			GetFrameInput().Key == kp.key ||
			slices.Contains(GetInputState().DownKeys, kp.key)
		if held {
			st.Active = true
		} else {
			st.Clicked = true
			kp.key = 0
			RequestNextFrame()
		}
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

// ButtonLook controls button geometry. Regular vs compact geometry differs only by these numbers
// (see DefaultButtonLook / DefaultCtrlButtonLook).
type ButtonLook struct {
	// TextSize is used when ButtonAttrs.TextSize is zero.
	TextSize f32
	// PushDown is the resting elevation lip (bottom) / press inset (top).
	// Regular uses 0.5; flat ctrl uses 0.
	PushDown f32
	// PadScale multiplies horizontal and vertical padding (1 regular, 0.8 ctrl).
	// Zero is treated as 1.
	PadScale f32
}

// DefaultButtonLook returns the regular elevated button chrome.
func DefaultButtonLook() ButtonLook {
	return ButtonLook{
		TextSize: ButtonDefaultSize,
		PushDown: 0.5,
		PadScale: 1,
	}
}

// DefaultCtrlButtonLook returns the compact flat "control" button chrome.
func DefaultCtrlButtonLook() ButtonLook {
	return ButtonLook{
		TextSize: ButtonCtrlSize,
		PushDown: 0,
		PadScale: 0.8,
	}
}

// ButtonType identifies a button's semantic role, independently of its size.
type ButtonType uint8

const (
	ButtonDefault ButtonType = iota
	ButtonPrimary
	ButtonDestructive
)

// ButtonAttrs configures content and theming through NextButtonAttrs or ButtonExt. Look (push,
// elevation, pad scale) is a separate ButtonLook argument — not a flag here.
type ButtonAttrs struct {
	Disabled  bool       // draw greyed-out and ignore clicks
	Type      ButtonType // zero value: ordinary button
	Accent    Vec4       // zero value: use the theme color for Type
	TextSize  f32        // label and icon size; zero uses the look's TextSize
	TextStyle Style      // label font style (e.g. italic)
	Icon      IconGlyph  // optional leading icon (Sym*, Typ*, or custom); zero = none
}

var nextButtonAttrs ButtonAttrs

func init() {
	RegisterFrameCleanup(func() {
		if nextButtonAttrs != (ButtonAttrs{}) {
			fmt.Fprintln(os.Stderr, "shirei/widgets: leftover NextButton properties without a button")
			nextButtonAttrs = ButtonAttrs{}
		}
	})
}

// NextButtonAttrs replaces all pending button properties. The next Button,
// CtrlButton, or MenuButton family call consumes them and resets the defaults.
// Set properties inside any conditional that controls whether the button is built.
// Explicit Ext configurations replace pending properties in full.
func NextButtonAttrs(attrs ButtonAttrs) { nextButtonAttrs = attrs }

// NextButtonType sets the semantic role of the next button.
func NextButtonType(kind ButtonType) { nextButtonAttrs.Type = kind }

// NextButtonDisabled sets both interaction and accessibility disabled state.
func NextButtonDisabled(disabled bool) { nextButtonAttrs.Disabled = disabled }

// NextButtonAccent overrides the next button's role color. Zero uses the role.
func NextButtonAccent(accent Vec4) { nextButtonAttrs.Accent = accent }

// NextButtonTextSize sets the next button's text size. Zero uses its look's size.
func NextButtonTextSize(size float32) { nextButtonAttrs.TextSize = size }

// NextButtonTextStyle sets the next button's label font style.
func NextButtonTextStyle(style Style) { nextButtonAttrs.TextStyle = style }

func takeButtonAttrs() ButtonAttrs {
	attrs := nextButtonAttrs
	nextButtonAttrs = ButtonAttrs{}
	return attrs
}

// Button renders a labeled button with an optional leading icon (pass NoIcon
// for none) and returns true on the frame it is clicked. It consumes NextButton
// properties; the icon argument supplies the content independently of them.
func Button(icon IconGlyph, label string) bool {
	attrs := takeButtonAttrs()
	attrs.Icon = icon
	return buttonExt(label, attrs, DefaultButtonLook())
}

// ButtonWithAccent consumes NextButton properties, with accent taking precedence.
func ButtonWithAccent(icon IconGlyph, label string, accent Vec4) bool {
	attrs := takeButtonAttrs()
	attrs.Icon, attrs.Accent = icon, accent
	return buttonExt(label, attrs, DefaultButtonLook())
}

// CtrlButton renders a compact flat control button (smaller padding, no push
// lip). Its role colors come from CurrentColorScheme, like a regular Button.
// It consumes NextButton properties.
// Either enabled=false or NextButtonDisabled(true) disables the button.
func CtrlButton(icon IconGlyph, label string, enabled bool) bool {
	attrs := takeButtonAttrs()
	attrs.Icon = icon
	attrs.Disabled = attrs.Disabled || !enabled
	return buttonExt(label, attrs, DefaultCtrlButtonLook())
}

// CtrlButtonWithAccent is CtrlButton with an explicit accent color.
func CtrlButtonWithAccent(icon IconGlyph, label string, accent Vec4, enabled bool) bool {
	attrs := takeButtonAttrs()
	attrs.Icon, attrs.Accent = icon, accent
	attrs.Disabled = attrs.Disabled || !enabled
	return buttonExt(label, attrs, DefaultCtrlButtonLook())
}

// ButtonExt renders the elevated-accent button face: ProcessButtonEvents plus
// the continuous look axes. Button and CtrlButton are the common defaults over
// this; pass DefaultButtonLook or DefaultCtrlButtonLook, or a custom ButtonLook.
// attrs is the complete configuration: it replaces and clears pending NextButton
// properties, including fields whose explicit value is zero.
func ButtonExt(label string, attrs ButtonAttrs, look ButtonLook) bool {
	nextButtonAttrs = ButtonAttrs{}
	return buttonExt(label, attrs, look)
}

func buttonExt(label string, attrs ButtonAttrs, look ButtonLook) bool {
	return ButtonStyled(label, attrs, look, resolveButtonStyle(attrs), CurrentColorScheme.FocusRing)
}

func resolveButtonStyle(attrs ButtonAttrs) ButtonStyle {
	style := CurrentColorScheme.Buttons.Default
	switch attrs.Type {
	case ButtonPrimary:
		style = CurrentColorScheme.Buttons.Primary
	case ButtonDestructive:
		style = CurrentColorScheme.Buttons.Destructive
	}
	if attrs.Accent != (Vec4{}) {
		style = ButtonStyleWithAccent(style, attrs.Accent)
	}
	return style
}

// ButtonStyled renders standard button geometry and interaction using only the
// supplied style and focus color. All colors, including zero/transparent values,
// are literal. Type and Accent are scheme-resolution hints and are ignored here.
// attrs replaces and clears pending NextButton properties in full.
// Use DefaultCtrlButtonLook for a compact button.
func ButtonStyled(label string, attrs ButtonAttrs, look ButtonLook, style ButtonStyle, focusRing Vec4) bool {
	nextButtonAttrs = ButtonAttrs{}
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

	var action bool
	Container(Attrs(), func() {
		st := ProcessButtonEvents(attrs.Disabled)
		NextAccessRole("button")
		NextAccessDisabled(attrs.Disabled)
		AssignAccess()
		action = st.Clicked

		paint := style.Normal
		switch {
		case st.Disabled:
			paint = style.Disabled
		case st.Active:
			paint = style.Pressed
		case st.Hovered:
			paint = style.Hovered
		}
		borderWidth := f32(1)
		if st.FocusVisible && !st.Disabled {
			borderWidth = 0
		}
		shadowPadding := Vec4{0}

		if st.Active {
			ModAttrs(func(a *AttrSet) {
				a.Padding[PAD_TOP] = pushDown
			})
		} else {
			shadowPadding[PAD_BOTTOM] = pushDown
		}

		Container(Attrs(BackgroundVec(paint.Elevation), PadVec(shadowPadding), Corners(br+1)), func() {
			var face = Attrs(Row, CrossMid, Corners(br), Pad2(padv, padh), Gap(padh/2), BackgroundVec(paint.Background), GradVec(paint.Gradient),
				BorderColorVec(paint.Border), BorderWidth(borderWidth))
			Container(face, func() {
				if attrs.Icon.Rune != 0 {
					Icon(attrs.Icon, FontSize(attrs.TextSize), TextColorVec(paint.Text))
				}
				if label != "" {
					Label(label, FontSize(attrs.TextSize), FontStyle(attrs.TextStyle), TextColorVec(paint.Text))
				}
				if st.FocusVisible && !st.Disabled {
					widgetFocusBorder(GetResolvedSize(), br, focusRing)
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
