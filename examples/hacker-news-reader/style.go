package main

import (
	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

type readerPalette struct {
	page, header, headerText, headerBorder Vec4
	card, featured, tint, border           Vec4
	text, muted, accent, thread            Vec4
}

func readerColors() readerPalette {
	if CurrentColorScheme.Surfaces.Canvas.Background[2] < 50 {
		return readerPalette{
			page:         Vec4{220, 16, 11, 1},
			header:       Vec4{22, 85, 32, 1},
			headerText:   Vec4{0, 0, 100, 1},
			headerBorder: Vec4{0, 0, 100, .65},
			card:         Vec4{220, 14, 17, 1},
			featured:     Vec4{25, 22, 23, 1},
			tint:         Vec4{25, 20, 27, 1},
			border:       Vec4{220, 12, 31, 1},
			text:         Vec4{220, 15, 93, 1},
			muted:        Vec4{220, 10, 68, 1},
			accent:       Vec4{25, 95, 67, 1},
			thread:       Vec4{25, 82, 52, 1},
		}
	}
	return readerPalette{
		page:         Vec4{35, 46, 97, 1},
		header:       Vec4{22, 90, 48, 1},
		headerText:   Vec4{0, 0, 100, 1},
		headerBorder: Vec4{0, 0, 100, .7},
		card:         Vec4{0, 0, 100, 1},
		featured:     Vec4{29, 100, 95, 1},
		tint:         Vec4{28, 55, 96, 1},
		border:       Vec4{30, 32, 89, 1},
		text:         Vec4{220, 24, 16, 1},
		muted:        Vec4{220, 9, 43, 1},
		accent:       Vec4{22, 95, 39, 1},
		thread:       Vec4{22, 86, 55, 1},
	}
}

func readerButtonStyle(background, border, text Vec4) ButtonStyle {
	normal := ButtonPaint{Background: background, Border: border, Text: text, Elevation: background}
	hovered, pressed, disabled := normal, normal, normal
	hovered.Background[2] += 5
	pressed.Background[2] -= 5
	ClampColorVec(&hovered.Background)
	ClampColorVec(&pressed.Background)
	disabled.Text[3] *= .55
	return ButtonStyle{Normal: normal, Hovered: hovered, Pressed: pressed, Disabled: disabled}
}

func readerHeaderButton(icon IconGlyph, label string, disabled bool, colors readerPalette) bool {
	style := readerButtonStyle(colors.header, colors.headerBorder, colors.headerText)
	return ButtonStyled(label, ButtonAttrs{Icon: icon, Disabled: disabled, TextSize: 15},
		ButtonLook{TextSize: 15, PadScale: .9}, style, colors.headerText)
}
