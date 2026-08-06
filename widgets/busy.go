package widgets

import (
	"time"

	. "go.hasen.dev/shirei"
)

// Busy helpers draw lightweight activity indicators. Each call keeps the
// frame loop alive with RequestNextFrame while the indicator is painted so
// the phase advances even when nothing else is dirty.

// busyPhase returns a cycling index in [0, n) based on wall time.
func busyPhase(n int, period time.Duration) int {
	if n <= 0 {
		return 0
	}
	if period <= 0 {
		period = 200 * time.Millisecond
	}
	return int(time.Now().UnixMilli()/int64(period/time.Millisecond)) % n
}

// BusyDots paints a fixed-width sliding ellipsis: the dots walk through a
// 5-cell mono field so layout width stays stable.
//
//	"..   " → "...  " → " ... " → "  ..." → " . .." (then wraps)
//
// Optional TextStyleFns style the glyph (font size, color).
func BusyDots(style ...TextStyleFn) {
	RequestNextFrame()
	const cells = 5
	frames := []string{
		"..   ",
		"...  ",
		" ... ",
		"  ...",
		"   ..",
		".   .",
	}
	_ = cells
	s := frames[busyPhase(len(frames), 180*time.Millisecond)]
	fns := append([]TextStyleFn{
		Fonts(Monospace...),
		FontSize(12),
		TextColor(0, 0, 40, 1),
	}, style...)
	Label(s, fns...)
}

// BusyBraille paints a classic braille spinner (⠋⠙⠹…). Mono, fixed width.
func BusyBraille(style ...TextStyleFn) {
	RequestNextFrame()
	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	s := frames[busyPhase(len(frames), 90*time.Millisecond)]
	fns := append([]TextStyleFn{
		Fonts(Monospace...),
		FontSize(12),
		TextColor(0, 0, 40, 1),
	}, style...)
	Label(s, fns...)
}

// BusySlash paints a rotating ASCII spinner: | / - \.
func BusySlash(style ...TextStyleFn) {
	RequestNextFrame()
	frames := []string{"|", "/", "-", "\\"}
	s := frames[busyPhase(len(frames), 120*time.Millisecond)]
	fns := append([]TextStyleFn{
		Fonts(Monospace...),
		FontSize(12),
		TextColor(0, 0, 40, 1),
	}, style...)
	Label(s, fns...)
}

// BusyPulse paints a three-dot pulse that grows and shrinks: . → .. → ... → ..
func BusyPulse(style ...TextStyleFn) {
	RequestNextFrame()
	frames := []string{".  ", ".. ", "...", ".. ", ".  ", "   "}
	s := frames[busyPhase(len(frames), 200*time.Millisecond)]
	fns := append([]TextStyleFn{
		Fonts(Monospace...),
		FontSize(12),
		TextColor(0, 0, 40, 1),
	}, style...)
	Label(s, fns...)
}
