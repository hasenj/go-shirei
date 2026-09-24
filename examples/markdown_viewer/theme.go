package main

import (
	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

type Theme struct {
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
}

// currentTheme maps document-specific paint to the active application scheme.
func currentTheme() Theme {
	s := CurrentColorScheme
	return Theme{
		ThematicBreak:  s.Surfaces.Panel.Border,
		QuoteBar:       s.FocusRing,
		BodyText:       s.Surfaces.Panel.Text,
		HeadingText:    s.Surfaces.Panel.Text,
		MarkerText:     s.List.Muted,
		LinkText:       s.FocusRing,
		CodeBg:         s.Surfaces.Canvas.Background,
		CodeText:       s.Surfaces.Canvas.Text,
		InlineCodeBg:   s.Table.Header.Background,
		TableHeaderBg:  s.Table.Header.Background,
		TableCellBg:    s.Table.Body.Background,
		TableBorder:    s.Table.Separator,
		TableColSep:    s.Table.Separator,
		TableHeaderSep: s.Table.Separator,
	}
}
