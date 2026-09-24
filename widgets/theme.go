package widgets

import (
	. "go.hasen.dev/shirei"
)

// Accent presets are HSLA colors for per-widget overrides and scheme data.
var (
	AccentLightSteel = Vec4{214, 20, 90, 1}
	AccentNylon      = Vec4{220, 80, 95, 0.5}

	AccentBlue      = Vec4{204, 70, 48, 1}
	AccentSlateBlue = Vec4{210, 20, 50, 1}
	AccentMeadow    = Vec4{125, 45, 40, 1}
	AccentSunshine  = Vec4{42, 80, 60, 1}
	AccentRed       = Vec4{0, 70, 48, 1}
	AccentPlastic   = Vec4{190, 20, 80, 0.9}
)

// DefaultAccent is an accent preset for explicit overrides.
// Deprecated: set widget colors through CurrentColorScheme.
var DefaultAccent = AccentBlue

// ButtonPaint holds the HSLA paint for one interaction state. Gradient is an
// HSLA delta from Background at the top of the face to its bottom. Elevation
// paints the lip beneath the face. Zero colors are valid, including transparency.
type ButtonPaint struct {
	Background Vec4
	Gradient   Vec4
	Text       Vec4
	Border     Vec4
	Elevation  Vec4
}

// ButtonStyle supplies a complete set of interaction-state paints.
// The renderer takes a separate focus color; themed callers supply ColorScheme.FocusRing.
type ButtonStyle struct {
	Normal   ButtonPaint
	Hovered  ButtonPaint
	Pressed  ButtonPaint
	Disabled ButtonPaint
}

// ButtonStyles maps semantic roles to paint independently of button geometry.
type ButtonStyles struct {
	Default     ButtonStyle
	Primary     ButtonStyle
	Destructive ButtonStyle
}

// SelectionPaint holds HSLA colors for a selection control's face and indicator.
// Gradient is a background delta. Indicator colors the checkmark or other mark;
// the label inherits its container's text style. Zero colors are transparent.
type SelectionPaint struct {
	Background        Vec4
	Gradient          Vec4
	Border            Vec4
	Indicator         Vec4
	IndicatorGradient Vec4
	// Shadow supplies segmented-cell depth; dimensions use design units.
	Shadow Shadow
}

// SelectionStateStyle supplies interaction paint for one selection state.
type SelectionStateStyle struct {
	Normal  SelectionPaint
	Hovered SelectionPaint
	Pressed SelectionPaint
}

// SelectionStyle separates the bound selection value from interaction state.
// Focus is supplied separately to the renderer.
type SelectionStyle struct {
	Unselected SelectionStateStyle
	Selected   SelectionStateStyle
}

// SurfaceRole describes the purpose of an application container.
type SurfaceRole uint8

const (
	SurfaceCanvas SurfaceRole = iota
	SurfacePanel
	SurfaceToolbar
)

// SurfaceColors pairs a surface's HSLA background with its text and border.
// Zero colors are valid transparent values.
type SurfaceColors struct {
	Background Vec4
	Text       Vec4
	Border     Vec4
}

type SurfaceStyles struct {
	Canvas  SurfaceColors
	Panel   SurfaceColors
	Toolbar SurfaceColors
}

// ColorScheme contains application surfaces, widget styles, and the focus color.
// Start from a preset and edit complete values; zero fields do not inherit.
type ColorScheme struct {
	Surfaces  SurfaceStyles
	Buttons   ButtonStyles
	Radio     SelectionStyle
	Switch    SelectionStyle
	Segmented SelectionStyle
	Slider    SliderStyle
	Progress  ProgressStyle
	TextInput TextInputStyle
	Menu      MenuStyle
	ScrollBar ScrollBarStyle
	Table     TableStyle
	List      ListStyle
	Toast     ToastStyle
	Log       LogStyle
	ImageWipe ImageWipeStyle
	Overlay   SurfaceColors
	Scrim     Vec4
	CheckBox  SelectionStyle
	FocusRing Vec4
}

// CurrentColorScheme is the active application-wide scheme. Assign on the UI
// thread before building widgets. RequestNextFrame after changes made outside
// a frame callback. Widgets read the scheme on every build, including popups.
var CurrentColorScheme = preferredLightColorScheme

var (
	preferredLightColorScheme = LightColorScheme()
	preferredDarkColorScheme  = DarkColorScheme()
	darkMode                  bool
)

// SetLightColorScheme stores the preferred light scheme, initially LightColorScheme().
// If light mode is active, it also applies the scheme and requests a frame when
// the effective colors change. Call on the UI thread or under the frame lock.
func SetLightColorScheme(scheme ColorScheme) {
	preferredLightColorScheme = scheme
	if !darkMode {
		SetDarkMode(false)
	}
}

// SetDarkColorScheme stores the preferred dark scheme, initially DarkColorScheme().
// If dark mode is active, it also applies the scheme and requests a frame when
// the effective colors change. Call on the UI thread or under the frame lock.
func SetDarkColorScheme(scheme ColorScheme) {
	preferredDarkColorScheme = scheme
	if darkMode {
		SetDarkMode(true)
	}
}

// SetDarkMode selects the preferred light or dark scheme immediately. Light is
// initially active. Repeating the call with unchanged colors requests no frame.
// Call before building the UI, on the UI thread or under the frame lock.
// Applications can pass darkmode.OSDarkMode() to follow the system preference.
// A direct CurrentColorScheme assignment is replaced by the selected preference.
func SetDarkMode(dark bool) {
	darkMode = dark
	scheme := preferredLightColorScheme
	if dark {
		scheme = preferredDarkColorScheme
	}
	if CurrentColorScheme != scheme {
		CurrentColorScheme = scheme
		RequestNextFrame()
	}
}

// UseSurface applies the active scheme's background, text, and border colors.
// Use it in Attrs or in ModAttrs before adding children. Text inherits through
// plain nested containers. Fonts, border width, and layout stay caller-controlled.
// The scheme is read when the setter runs, so a retained setter follows changes.
func UseSurface(role SurfaceRole) AttrsFn {
	return func(a *AttrSet) {
		colors := CurrentColorScheme.Surfaces.Canvas
		switch role {
		case SurfacePanel:
			colors = CurrentColorScheme.Surfaces.Panel
		case SurfaceToolbar:
			colors = CurrentColorScheme.Surfaces.Toolbar
		}
		a.Background = colors.Background
		a.BorderColor = colors.Border
		AmendTextStyle(TextColorVec(colors.Text))(a)
	}
}

// LightColorScheme returns a cool light scheme with blue primary actions.
func LightColorScheme() ColorScheme {
	disabled := ButtonPaint{
		Background: Vec4{214, 20, 90, 1},
		Text:       Vec4{0, 0, 40, 0.5},
		Border:     Vec4{0, 0, 75, 1},
		Elevation:  Vec4{214, 20, 90, 1},
	}
	border := Vec4{214, 20, 20, 0.2}
	return completeColorScheme(ColorScheme{
		Surfaces: SurfaceStyles{
			Canvas:  SurfaceColors{Background: Vec4{220, 16, 95, 1}, Text: Vec4{220, 20, 18, 1}, Border: Vec4{220, 16, 78, 1}},
			Panel:   SurfaceColors{Background: Vec4{220, 16, 99, 1}, Text: Vec4{220, 20, 18, 1}, Border: Vec4{220, 16, 82, 1}},
			Toolbar: SurfaceColors{Background: Vec4{214, 24, 27, 1}, Text: Vec4{214, 20, 97, 1}, Border: Vec4{214, 24, 18, 1}},
		},
		Buttons: ButtonStyles{
			Default:     lightButtonStyle(Vec4{214, 20, 98, 1}, Vec4{0, 0, 10, 1}, border, disabled),
			Primary:     lightButtonStyle(Vec4{204, 70, 40, 1}, Vec4{0, 0, 100, 1}, border, disabled),
			Destructive: lightButtonStyle(Vec4{0, 65, 42, 1}, Vec4{0, 0, 100, 1}, border, disabled),
		},
		CheckBox:  lightSelectionStyle(AccentBlue, Vec4{0, 0, 100, 1}, Vec4{0, 0, 100, 1}),
		FocusRing: Vec4{210, 85, 52, 1},
	})
}

// WarmColorScheme returns a cream light scheme with green primary actions.
func WarmColorScheme() ColorScheme {
	disabled := ButtonPaint{
		Background: Vec4{38, 20, 87, 1},
		Text:       Vec4{30, 12, 53, 1},
		Border:     Vec4{35, 18, 72, 1},
		Elevation:  Vec4{38, 20, 87, 1},
	}
	border := Vec4{30, 24, 30, 0.35}
	return completeColorScheme(ColorScheme{
		Surfaces: SurfaceStyles{
			Canvas:  SurfaceColors{Background: Vec4{38, 30, 92, 1}, Text: Vec4{25, 30, 18, 1}, Border: Vec4{35, 18, 72, 1}},
			Panel:   SurfaceColors{Background: Vec4{40, 45, 97, 1}, Text: Vec4{25, 30, 18, 1}, Border: Vec4{35, 24, 78, 1}},
			Toolbar: SurfaceColors{Background: Vec4{145, 24, 23, 1}, Text: Vec4{45, 40, 96, 1}, Border: Vec4{145, 24, 15, 1}},
		},
		Buttons: ButtonStyles{
			Default:     lightButtonStyle(Vec4{40, 45, 92, 1}, Vec4{25, 30, 18, 1}, border, disabled),
			Primary:     lightButtonStyle(Vec4{145, 42, 32, 1}, Vec4{45, 50, 98, 1}, border, disabled),
			Destructive: lightButtonStyle(Vec4{12, 60, 38, 1}, Vec4{45, 50, 98, 1}, border, disabled),
		},
		CheckBox:  lightSelectionStyle(Vec4{145, 42, 32, 1}, Vec4{45, 50, 98, 1}, Vec4{40, 45, 97, 1}),
		FocusRing: Vec4{145, 55, 35, 1},
	})
}

func lightButtonStyle(face, text, border Vec4, disabled ButtonPaint) ButtonStyle {
	paint := ButtonPaint{
		Background: face,
		Text:       text,
		Border:     border,
		Elevation:  Vec4{face[0], face[1], face[2] - 24, face[3]},
	}
	style := ButtonStyle{Normal: paint, Hovered: paint, Pressed: paint, Disabled: disabled}
	style.Hovered.Background[2] += 3
	style.Pressed.Background[2] -= 3
	ClampColorVec(&style.Hovered.Background)
	ClampColorVec(&style.Pressed.Background)
	return style
}

func lightSelectionStyle(accent, indicator, empty Vec4) SelectionStyle {
	unselected := SelectionPaint{Background: empty, Gradient: Vec4{0, 0, -6, 0}, Border: accent, Indicator: indicator}
	selected := SelectionPaint{Background: accent, Gradient: Vec4{0, 0, 6, 0}, Border: accent, Indicator: indicator}
	style := SelectionStyle{
		Unselected: SelectionStateStyle{Normal: unselected, Hovered: unselected, Pressed: unselected},
		Selected:   SelectionStateStyle{Normal: selected, Hovered: selected, Pressed: selected},
	}
	style.Unselected.Hovered.Background = Vec4{accent[0], accent[1] * 0.3, empty[2] - 4, empty[3]}
	style.Unselected.Pressed.Background = Vec4{accent[0], accent[1] * 0.3, empty[2] - 8, empty[3]}
	style.Selected.Hovered.Background[2] += 5
	style.Selected.Pressed.Background[2] -= 4
	return style
}

// SelectionStyleWithAccent tints selected fills and all borders. Selected fills
// preserve their lightness offsets and get contrasting indicator colors.
// Unselected fills and all gradients remain as supplied by the style.
func SelectionStyleWithAccent(style SelectionStyle, accent Vec4) SelectionStyle {
	normalLight := style.Selected.Normal.Background[2]
	for _, paint := range []*SelectionPaint{&style.Selected.Normal, &style.Selected.Hovered, &style.Selected.Pressed} {
		paint.Background = Vec4{accent[0], accent[1], accent[2] + paint.Background[2] - normalLight, accent[3]}
		ClampColorVec(&paint.Background)
		paint.Border = accent
		paint.Indicator = ContrastingTextColor(paint.Background)
	}
	for _, paint := range []*SelectionPaint{&style.Unselected.Normal, &style.Unselected.Hovered, &style.Unselected.Pressed} {
		paint.Border = accent
	}
	return style
}

// ButtonStyleWithAccent tints the enabled faces and elevation of style. The
// accent is the normal face color; other states retain their lightness offsets.
// Foregrounds are derived from the tinted fills. Borders, gradients, and the
// entire disabled paint stay as supplied by the scheme.
func ButtonStyleWithAccent(style ButtonStyle, accent Vec4) ButtonStyle {
	normalLight := style.Normal.Background[2]
	for _, paint := range []*ButtonPaint{&style.Normal, &style.Hovered, &style.Pressed} {
		light := accent[2] + paint.Background[2] - normalLight
		elevation := accent[2] + paint.Elevation[2] - normalLight
		paint.Background = Vec4{accent[0], accent[1], light, accent[3]}
		paint.Elevation = Vec4{accent[0], accent[1], elevation, accent[3]}
		ClampColorVec(&paint.Background)
		ClampColorVec(&paint.Elevation)
		paint.Text = ContrastingTextColor(paint.Background)
	}
	return style
}

// DefaultBackground is an off-white preset for explicit surface overrides.
// Deprecated: set surface colors through CurrentColorScheme.
var DefaultBackground = Vec4{220, 16, 98, 1}

// AccentOrFallback returns a default fallback when the accent is the zero value
func AccentOrFallback(accent Vec4, fallback Vec4) Vec4 {
	if accent == (Vec4{}) {
		return fallback
	}
	return accent
}
