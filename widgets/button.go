package widgets

import (
	"github.com/cli/browser"
	"go.hasen.dev/generic"
	. "go.hasen.dev/shirei"
)

// ButtonDefaultSize is the default font size for a button's label and icon.
const ButtonDefaultSize = DefaultTextSize
const ButtonCtrlSize = ButtonDefaultSize * 0.8

type f32 = float32

// ButtonAttrs configures ButtonExt.
type ButtonAttrs struct {
	Ctrl      bool  // render as a compact "control" button (smaller padding and size)
	Disabled  bool  // draw greyed-out and ignore clicks
	Accent    Vec4  // zero value: use the package-level Accent
	TextSize  f32   // label and icon size; zero uses ButtonDefaultSize (or the ctrl size)
	TextStyle Style // label font style (e.g. italic)
	Icon      rune  // optional icon glyph: a Microns (Sym*) or Typicons (Typ*) rune
}

// Button renders a labeled button with an optional leading icon (pass 0 for no
// icon) and returns true on the frame it is clicked.
func Button(icon rune, label string) bool {
	return ButtonExt(label, ButtonAttrs{Icon: icon})
}

// CtrlButton renders a compact "control" button (smaller, steel accent), enabled
// or disabled by the enabled flag. It returns true when clicked while enabled.
func CtrlButton(icon rune, label string, enabled bool) bool {
	return ButtonExt(label, ButtonAttrs{Ctrl: true, Icon: icon, Disabled: !enabled, Accent: AccentNylon})
}

// ButtonExt renders a button configured by attrs and returns true on the frame
// it is clicked. Button and CtrlButton are the common shortcuts over it.
func ButtonExt(label string, attrs ButtonAttrs) bool {
	if attrs.TextSize == 0 {
		attrs.TextSize = ButtonDefaultSize
		if attrs.Ctrl {
			attrs.TextSize = ButtonCtrlSize
		}
	}
	var action = false
	var pushDownDistance f32 = 1
	if attrs.Ctrl {
		pushDownDistance = 0
	}

	var padh = attrs.TextSize * 0.8
	// vertical padding totals one line height, minus the 1px the press
	// mechanic always adds (top padding when active, elevation lip when
	// idle — see below): this is what makes a default button's height
	// match a default TextInput's (attrs.FontSize + attrs.FontSize).
	var padv = (attrs.TextSize - pushDownDistance) / 2
	var br = attrs.TextSize * 0.3

	if attrs.Ctrl {
		padv *= 0.8
		padh *= 0.8
		// br *= 0.5
	}

	accent := AccentOrFallback(attrs.Accent, ButtonAccent)
	textColor := ContrastingTextColor(accent)

	Container(Attrs(), func() {
		hue, sat, light := accent[0], accent[1], accent[2]
		var topBoost f32 = 8       // the top edge reads as a highlight, not the accent itself
		var elevationDrop f32 = 16 // how much darker the resting "lip" below is

		borderWidth := f32(1)
		borderColor := accent
		borderColor[2] = 20
		borderColor[3] = 0.2

		if attrs.Ctrl {
			topBoost = 4
			elevationDrop = 8
		}

		if attrs.Disabled {
			light = 90
			topBoost, elevationDrop = 0, 0
			textColor[2] = 40
			textColor[3] = 0.5
			borderWidth = 1.5
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

		// state management
		if !attrs.Disabled {
			action = PressAction()

			// appearance management
			if IsHovered() {
				background[2] = highlight
			}
		}

		if IsActive() {
			background[2] = presslight
			// increase padding on this outer container
			ModAttrs(func(attrs *AttrSet) {
				attrs.Padding[PAD_TOP] = pushDownDistance
			})
		} else {
			shadowPadding[PAD_BOTTOM] = pushDownDistance
		}

		// we did a bunch of computations, so make sure we are not off the rails
		ClampColorVec(&background)

		Container(Attrs(BackgroundVec(elevationColor), PadVec(shadowPadding), Corners(br+1)), func() {
			var attrs1 = Attrs(Row, Corners(br), Pad2(padv, padh), Gap(padh/2), BackgroundVec(background), GradVec(grad),
				BorderColorVec(borderColor), BorderWidth(borderWidth))
			Container(attrs1, func() {
				if attrs.Icon != 0 {
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
// text attributes style the label.
func Link(label string, url string, fns ...TextAttrsFn) {
	Container(Attrs(Row), func() {
		if IsClicked() {
			browser.OpenURL(url)
		}
		Label(label, fns...)
	})
}

// SliderAttrs configures a Slider.
type SliderAttrs struct {
	Min    f32  // value at the left end of the track
	Max    f32  // value at the right end of the track
	Step   f32  // snap increment; 0 means continuous
	Width  f32  // track width in pixels; 0 uses a default
	Accent Vec4 // zero value: use the package-level Accent
}

// Slider renders a draggable horizontal slider that reads and writes *value,
// clamped to [Min, Max]. A nonzero Step snaps the value to that increment.
func Slider(value *float32, attrs SliderAttrs) {
	if attrs.Width == 0 {
		attrs.Width = 200
	}
	accent := AccentOrFallback(attrs.Accent, DefaultAccent)
	var barHeight float32 = 4
	var r float32 = 8 // radius of circle
	var height = r * 2
	Container(Attrs(Row, CrossMid, FixWidth(attrs.Width), Focusable, FixHeight(height)), func() {
		PressAction()

		if IsActive() {
			selfRect := GetScreenRect()
			mouse := InputState.MousePoint // mouse movement along x-axis
			var x = mouse[0] - (selfRect.Origin[0] + r)
			var t = x / (selfRect.Size[0] - (r * 2))
			generic.Clamp(0, &t, 1)
			// lerp
			*value = attrs.Min + (attrs.Max-attrs.Min)*t
			if attrs.Step > 0 {
				*value = Roundf32(*value/attrs.Step) * attrs.Step
			}
		}

		// background line
		Element(Attrs(CrossMid, MinSize(attrs.Width, barHeight), BackgroundVec(accent), Corners(barHeight/2)))

		// handle area
		xOffset := (attrs.Width - (r * 2)) * (*value - attrs.Min) / (attrs.Max - attrs.Min)

		// handle (circle)
		Element(Attrs(Float(xOffset, 0), Corners(r), ClickThrough, FixSize(r*2, r*2), Background(0, 0, 100, 1), Grad(0, 0, -16, 0), BoxShadow(1), BorderWidth(1), BorderColor(0, 0, 0, 0.5)))
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
