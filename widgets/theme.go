package widgets

import (
	. "go.hasen.dev/shirei"
)

// Presets for Accent: plain HSLA, the same Vec4 every color attribute
// already takes. Assign one to Accent to retheme every button and input
// that doesn't set its own — or drop in any custom HSLA value, globally
// or on a single widget's Accent field.
var (
	AccentLightSteel = Vec4{214, 20, 90, 1}
	AccentNylon      = Vec4{220, 80, 95, 0.5}

	AccentBlue      = Vec4{204, 70, 48, 1}
	AccentSlateBlue = Vec4{210, 20, 50, 1}
	AccentMeadow    = Vec4{125, 45, 40, 1}
	AccentSunshine  = Vec4{42, 80, 60, 1}
	AccentPlastic   = Vec4{190, 20, 80, 0.9}
)

// Accent is the package-wide default accent color, used by every widget that
// doesn't set its own (Button, TextInput, CheckBox, ToggleSwitch, ...). Assign
// one of the Accent* presets, or any HSLA Vec4, to retheme them all at once.
var DefaultAccent = AccentBlue

var ButtonAccent = AccentLightSteel

// DefaultBackground is a light surface color for floating elements (menus,
// popup panels): a clean off-white with a faint cool cast — bright enough to
// read as a raised surface, but not stark white and not a flat "Windows 98"
// gray.
var DefaultBackground = Vec4{220, 16, 98, 1}

// AccentOrFallback returns a default fallback when the accent is the zero value
func AccentOrFallback(accent Vec4, fallback Vec4) Vec4 {
	if accent == (Vec4{}) {
		return fallback
	}
	return accent
}
