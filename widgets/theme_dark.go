package widgets

import . "go.hasen.dev/shirei"

// DarkColorScheme returns a cool charcoal scheme with blue primary actions.
func DarkColorScheme() ColorScheme {
	return darkColorScheme(220, 18, Vec4{220, 18, 92, 1}, Vec4{210, 70, 42, 1}, Vec4{210, 85, 70, 1}, Vec4{0, 65, 43, 1})
}

// WarmDarkColorScheme returns warm charcoal surfaces with green primary actions.
func WarmDarkColorScheme() ColorScheme {
	return darkColorScheme(32, 14, Vec4{42, 28, 91, 1}, Vec4{145, 42, 31, 1}, Vec4{145, 50, 66, 1}, Vec4{12, 60, 42, 1})
}

func darkColorScheme(hue, saturation float32, text, accent, focus, danger Vec4) ColorScheme {
	neutral := func(light float32) Vec4 { return Vec4{hue, saturation, light, 1} }
	border := neutral(40)
	mark := Vec4{text[0], text[1], 98, 1}
	disabled := ButtonPaint{
		Background: neutral(22), Text: neutral(53), Border: neutral(30), Elevation: neutral(15),
	}
	button := func(face Vec4) ButtonStyle {
		paint := ButtonPaint{Background: face, Text: mark, Border: border,
			Elevation: Vec4{face[0], face[1], face[2] - 10, 1}}
		style := ButtonStyle{Normal: paint, Hovered: paint, Pressed: paint, Disabled: disabled}
		style.Hovered.Background[2] += 4
		style.Pressed.Background[2] -= 4
		return style
	}
	empty := SelectionPaint{Background: neutral(18), Gradient: Vec4{0, 0, -3, 0}, Border: neutral(55), Indicator: mark}
	selected := SelectionPaint{Background: accent, Gradient: Vec4{0, 0, -4, 0}, Border: focus, Indicator: mark}
	selection := SelectionStyle{
		Unselected: SelectionStateStyle{Normal: empty, Hovered: empty, Pressed: empty},
		Selected:   SelectionStateStyle{Normal: selected, Hovered: selected, Pressed: selected},
	}
	selection.Unselected.Hovered.Background = neutral(26)
	selection.Unselected.Pressed.Background = neutral(14)
	selection.Selected.Hovered.Background[2] += 4
	selection.Selected.Pressed.Background[2] -= 4

	s := completeColorScheme(ColorScheme{
		Surfaces: SurfaceStyles{
			Canvas:  SurfaceColors{Background: neutral(12), Text: text, Border: neutral(32)},
			Panel:   SurfaceColors{Background: neutral(18), Text: text, Border: border},
			Toolbar: SurfaceColors{Background: Vec4{accent[0], 18, 21, 1}, Text: text, Border: neutral(35)},
		},
		Buttons:  ButtonStyles{Default: button(neutral(29)), Primary: button(accent), Destructive: button(danger)},
		CheckBox: selection, FocusRing: focus,
	})
	// Raised handles stay distinct from both the empty and selected tracks.
	for _, state := range []*SelectionStateStyle{&s.Switch.Unselected, &s.Switch.Selected} {
		for _, paint := range []*SelectionPaint{&state.Normal, &state.Hovered, &state.Pressed} {
			paint.Indicator = neutral(91)
			paint.IndicatorGradient = Vec4{0, 0, -8, 0}
		}
	}
	s.Switch.Unselected.Hovered.Background = neutral(38)
	s.Switch.Unselected.Pressed.Background = neutral(26)
	s.Slider.Track, s.Progress.Fill = focus, focus
	s.Slider.Handle, s.Slider.HandleGradient, s.Slider.HandleBorder = neutral(90), Vec4{}, neutral(62)
	s.TextInput.Background = neutral(10)
	s.TextInput.HoveredBorder = neutral(56)
	s.TextInput.Placeholder = neutral(65)
	s.TextInput.Selection = Vec4{focus[0], focus[1], focus[2], .4}
	s.TextInput.Inset = Vec4{0, 0, 0, .2}
	s.Table.Header.Background = neutral(23)
	s.Table.Hovered, s.Table.Sorted = neutral(30), neutral(37)
	s.List.Hovered.Background = neutral(27)
	s.List.Muted, s.List.Disabled = neutral(65), neutral(50)
	s.List.Folder, s.List.File, s.List.Error = Vec4{42, 65, 68, 1}, Vec4{205, 65, 72, 1}, Vec4{danger[0], 70, 72, 1}
	s.Log.Hovered, s.Log.CopyBackground, s.Log.CopyHovered = neutral(26), neutral(32), neutral(40)
	s.Log.Selection = s.TextInput.Selection
	s.ImageWipe.Divider, s.ImageWipe.Handle = neutral(85), neutral(27)
	s.ImageWipe.DividerBorder, s.ImageWipe.HandleBorder = neutral(40), neutral(65)
	s.ImageWipe.DividerShadow = Vec4{0, 0, 0, .25}
	s.Scrim = Vec4{0, 0, 0, .65}
	return s
}
