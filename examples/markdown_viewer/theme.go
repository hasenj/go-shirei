package main

import (
	. "go.hasen.dev/shirei"
	"go.hasen.dev/shirei/ext/darkmode"
)

type Theme struct {
	IsDark bool

	// Backgrounds
	Bg        Vec4
	BgDoc     Vec4
	ToolbarBg Vec4

	// Chrome & Lines
	ToolbarTitle  Vec4
	ToolbarSub    Vec4
	ToolbarHint   Vec4
	Rule          Vec4
	ThematicBreak Vec4
	QuoteBar      Vec4

	// Typography
	BodyText    Vec4
	HeadingText Vec4
	MarkerText  Vec4
	LinkText    Vec4

	// Code
	CodeBg       Vec4
	CodeText     Vec4
	InlineCodeBg Vec4

	// Tables
	TableHeaderBg  Vec4
	TableCellBg    Vec4
	TableBorder    Vec4
	TableColSep    Vec4
	TableHeaderSep Vec4

	// Status & Error
	EmptyTitle  Vec4
	EmptyHint   Vec4
	EmptyScan   Vec4
	ErrorTitle  Vec4
	ErrorSub    Vec4
	PickerTitle Vec4
}

var lightTheme = Theme{
	IsDark:         false,
	Bg:             Vec4{40, 8, 98, 1},
	BgDoc:          Vec4{40, 12, 97, 1},
	ToolbarBg:      Vec4{40, 10, 95, 1},
	ToolbarTitle:   Vec4{220, 25, 20, 1},
	ToolbarSub:     Vec4{0, 0, 45, 1},
	ToolbarHint:    Vec4{0, 0, 55, 1},
	Rule:           Vec4{0, 0, 0, 0.10},
	ThematicBreak:  Vec4{0, 0, 0, 0.18},
	QuoteBar:       Vec4{210, 35, 55, 0.55},
	BodyText:       Vec4{0, 0, 16, 1},
	HeadingText:    Vec4{220, 20, 14, 1},
	MarkerText:     Vec4{0, 0, 40, 1},
	LinkText:       Vec4{210, 70, 38, 1},
	CodeBg:         Vec4{220, 12, 96, 1},
	CodeText:       Vec4{0, 0, 18, 1},
	InlineCodeBg:   Vec4{220, 15, 92, 1},
	TableHeaderBg:  Vec4{220, 10, 96, 1},
	TableCellBg:    Vec4{0, 0, 100, 1},
	TableBorder:    Vec4{0, 0, 0, 0.18},
	TableColSep:    Vec4{0, 0, 0, 0.20},
	TableHeaderSep: Vec4{0, 0, 0, 0.14},
	EmptyTitle:     Vec4{220, 25, 18, 1},
	EmptyHint:      Vec4{0, 0, 40, 1},
	EmptyScan:      Vec4{0, 0, 55, 1},
	ErrorTitle:     Vec4{10, 70, 40, 1},
	ErrorSub:       Vec4{0, 0, 35, 1},
	PickerTitle:    Vec4{220, 25, 25, 1},
}

var darkTheme = Theme{
	IsDark:         true,
	Bg:             Vec4{225, 14, 12, 1},
	BgDoc:          Vec4{225, 14, 14, 1},
	ToolbarBg:      Vec4{225, 14, 18, 1},
	ToolbarTitle:   Vec4{0, 0, 95, 1},
	ToolbarSub:     Vec4{220, 10, 65, 1},
	ToolbarHint:    Vec4{220, 10, 50, 1},
	Rule:           Vec4{0, 0, 100, 0.10},
	ThematicBreak:  Vec4{0, 0, 100, 0.18},
	QuoteBar:       Vec4{210, 45, 65, 0.70},
	BodyText:       Vec4{0, 0, 88, 1},
	HeadingText:    Vec4{215, 50, 90, 1},
	MarkerText:     Vec4{220, 10, 70, 1},
	LinkText:       Vec4{210, 80, 65, 1},
	CodeBg:         Vec4{225, 14, 19, 1},
	CodeText:       Vec4{0, 0, 92, 1},
	InlineCodeBg:   Vec4{225, 15, 24, 1},
	TableHeaderBg:  Vec4{225, 14, 23, 1},
	TableCellBg:    Vec4{225, 14, 16, 1},
	TableBorder:    Vec4{0, 0, 100, 0.18},
	TableColSep:    Vec4{0, 0, 100, 0.18},
	TableHeaderSep: Vec4{0, 0, 100, 0.14},
	EmptyTitle:     Vec4{215, 60, 85, 1},
	EmptyHint:      Vec4{220, 10, 75, 1},
	EmptyScan:      Vec4{220, 10, 55, 1},
	ErrorTitle:     Vec4{10, 80, 65, 1},
	ErrorSub:       Vec4{0, 0, 75, 1},
	PickerTitle:    Vec4{215, 60, 85, 1},
}

func currentTheme() Theme {
	if darkmode.OSDarkMode() {
		return darkTheme
	}
	return lightTheme
}
