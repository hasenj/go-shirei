package widgets

import . "go.hasen.dev/shirei"

// SliderStyle colors the filled track, remaining track, and handle independently.
// Track is the filled portion up to the handle; Remainder is the unfilled portion.
type SliderStyle struct {
	Track, Handle, HandleGradient, HandleBorder Vec4
	Remainder                                   Vec4
}

type ProgressStyle struct{ Fill, Track Vec4 }

// TextInputStyle supplies field, text, selection, caret, and inset paint.
// Focus is a separate renderer argument; zero colors are literal.
type TextInputStyle struct {
	Background, Border, HoveredBorder          Vec4
	Text, Placeholder, Caret, Selection, Inset Vec4
}

// MenuStyle supplies the popup surface, separators, and item states.
type MenuStyle struct {
	Surface                            SurfaceColors
	Normal, Hovered, Pressed, Disabled SurfaceColors
	Separator                          Vec4
}

type ScrollBarStyle struct{ Track, Normal, Hovered, Pressed Vec4 }

type TableStyle struct {
	ScrollBar                  ScrollBarStyle
	Body, Header               SurfaceColors
	Hovered, Sorted, Separator Vec4
}

// ListStyle supplies file and path list surfaces and semantic text colors.
type ListStyle struct {
	Surface, Hovered, Selected           SurfaceColors
	Muted, Disabled, Folder, File, Error Vec4
}

type ToastStyle struct {
	Background, Title, Body, Accent, Track, DismissHovered Vec4
}

type LogStyle struct {
	ScrollBar                                                 ScrollBarStyle
	Hovered, CopyBackground, CopyHovered, CopyText, Selection Vec4
}

type ImageWipeStyle struct {
	Background, DividerShadow, Divider, DividerBorder Vec4
	Handle, HandleBorder, Grip, Left, Right           Vec4
	LeftText, RightText                               Vec4
}

func completeColorScheme(s ColorScheme) ColorScheme {
	p, canvas := s.Surfaces.Panel, s.Surfaces.Canvas
	accent := s.CheckBox.Selected.Normal.Background
	mark := s.CheckBox.Selected.Normal.Indicator
	muted := p.Text
	muted[3] *= 0.6
	hover := canvas.Background
	pressed := canvas.Border
	s.Radio = s.CheckBox
	s.Switch = s.CheckBox
	for _, state := range []*SelectionStateStyle{&s.Switch.Unselected, &s.Switch.Selected} {
		for _, paint := range []*SelectionPaint{&state.Normal, &state.Hovered, &state.Pressed} {
			paint.Indicator = p.Background
			paint.IndicatorGradient = Vec4{0, 0, -6, 0}
		}
	}
	s.Switch.Unselected.Normal.Background = canvas.Border
	s.Switch.Unselected.Hovered.Background = pressed
	s.Switch.Unselected.Pressed.Background = p.Border
	tray := canvas.Background
	tray[2] -= 8
	ClampColorVec(&tray)
	face := p.Background
	if canvas.Background[2] < 50 {
		tray = canvas.Background
		face = s.Buttons.Default.Normal.Background
	}
	emptySegment := SelectionPaint{Background: tray, Border: p.Border, Indicator: p.Text}
	selectedSegment := SelectionPaint{Background: face, Border: p.Border, Indicator: p.Text,
		Shadow: Shadow{Offset: Vec2{0, .5}, Blur: 1, Alpha: .16}}
	s.Segmented = SelectionStyle{
		Unselected: SelectionStateStyle{Normal: emptySegment, Hovered: emptySegment, Pressed: emptySegment},
		Selected:   SelectionStateStyle{Normal: selectedSegment, Hovered: selectedSegment, Pressed: selectedSegment},
	}
	for _, state := range []*SelectionStateStyle{&s.Segmented.Unselected, &s.Segmented.Selected} {
		delta := float32(-3)
		if canvas.Background[2] < 50 {
			delta = 3
		}
		state.Hovered.Background[2] += delta
		state.Pressed.Background[2] += delta * 2
		ClampColorVec(&state.Hovered.Background)
		ClampColorVec(&state.Pressed.Background)
	}
	// Checkboxes reserve the accent for a selected value. Radio and switch
	// paints keep their own selection language.
	boxBorder := p.Text
	boxBorder[3] = .45
	for _, paint := range []*SelectionPaint{&s.CheckBox.Unselected.Normal, &s.CheckBox.Unselected.Hovered, &s.CheckBox.Unselected.Pressed} {
		paint.Border = boxBorder
		paint.Gradient = Vec4{}
	}
	for _, paint := range []*SelectionPaint{&s.CheckBox.Selected.Normal, &s.CheckBox.Selected.Hovered, &s.CheckBox.Selected.Pressed} {
		paint.Gradient = Vec4{}
	}
	s.Slider = SliderStyle{Track: accent, Remainder: canvas.Border, Handle: p.Background, HandleBorder: boxBorder}

	s.Progress = ProgressStyle{accent, canvas.Border}
	selection := accent
	selection[3] = 0.35
	s.TextInput = TextInputStyle{p.Background, p.Border, canvas.Border, p.Text, muted, p.Text, selection, Vec4{0, 0, 0, 0.04}}
	s.Menu = MenuStyle{
		Surface: p, Normal: SurfaceColors{Text: p.Text},
		Hovered:  SurfaceColors{Background: accent, Text: mark},
		Pressed:  SurfaceColors{Background: s.CheckBox.Selected.Pressed.Background, Text: mark},
		Disabled: SurfaceColors{Text: muted}, Separator: p.Border,
	}
	thumb := p.Text
	thumb[3] = 0.4
	thumbHover := thumb
	thumbHover[3] = 0.58
	thumbPress := thumb
	thumbPress[3] = 0.72
	s.ScrollBar = ScrollBarStyle{Normal: thumb, Hovered: thumbHover, Pressed: thumbPress}
	tableHover := canvas.Background
	tableHover[2] -= 4
	s.Table = TableStyle{ScrollBar: s.ScrollBar, Body: p, Header: SurfaceColors{Background: canvas.Background, Text: p.Text}, Hovered: tableHover, Sorted: pressed, Separator: p.Border}
	s.List = ListStyle{Surface: p, Hovered: SurfaceColors{Background: hover, Text: p.Text}, Selected: SurfaceColors{Background: accent, Text: mark}, Muted: muted, Disabled: muted, Folder: Vec4{40, 55, 42, 1}, File: Vec4{200, 40, 40, 1}, Error: s.Buttons.Destructive.Normal.Background}
	s.Toast = ToastStyle{Background: s.Surfaces.Toolbar.Background, Title: s.Surfaces.Toolbar.Text, Body: s.Surfaces.Toolbar.Text, Accent: accent, Track: Vec4{0, 0, 100, 0.12}, DismissHovered: Vec4{0, 0, 100, 0.15}}
	s.Log = LogStyle{ScrollBar: s.ScrollBar, Hovered: hover, CopyBackground: canvas.Background, CopyHovered: pressed, CopyText: p.Text, Selection: selection}
	s.ImageWipe = ImageWipeStyle{Background: canvas.Background, DividerShadow: Vec4{0, 0, 0, 0.06}, Divider: p.Background, DividerBorder: p.Border, Handle: p.Background, HandleBorder: p.Border, Grip: p.Text, Left: ImageWipeLeftAccent, Right: ImageWipeRightAccent, LeftText: Vec4{0, 0, 100, 1}, RightText: Vec4{0, 0, 100, 1}}
	s.Overlay = s.Surfaces.Toolbar
	s.Scrim = Vec4{220, 25, 12, 0.45}
	return s
}

// ModalStyleForScheme resolves core modal paint from an explicit widget scheme.
func ModalStyleForScheme(s ColorScheme) ModalStyle {
	return ModalStyle{Background: s.Surfaces.Panel.Background, Text: s.Surfaces.Panel.Text, Scrim: s.Scrim}
}

func init() { DefaultModalStyle = func() ModalStyle { return ModalStyleForScheme(CurrentColorScheme) } }
