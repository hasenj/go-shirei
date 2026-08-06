//go:build ios

package shirei

// criticalFontPaths returns likely iOS system font files for first paint.
func criticalFontPaths() []string {
	return []string{
		"/System/Library/Fonts/Core/Helvetica.ttc",
		"/System/Library/Fonts/Core/HelveticaNeue.ttc",
		"/System/Library/Fonts/CoreUI/SFUI.ttf",
		"/System/Library/Fonts/Cache/SFUI.ttf",
		"/System/Library/Fonts/Core/Times.ttc",
		"/System/Library/Fonts/Core/Courier.ttc",
		"/System/Library/Fonts/LanguageSupport/PingFang.ttc",
		"/System/Library/Fonts/Core/Hiragino Sans GB.ttc",
	}
}
