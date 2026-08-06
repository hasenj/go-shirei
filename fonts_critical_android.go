//go:build android

package shirei

// criticalFontPaths returns likely Android system font files for first paint.
func criticalFontPaths() []string {
	return []string{
		"/system/fonts/Roboto-Regular.ttf",
		"/system/fonts/Roboto-Bold.ttf",
		"/system/fonts/Roboto-Italic.ttf",
		"/system/fonts/DroidSansMono.ttf",
		"/system/fonts/NotoSansCJK-Regular.ttc",
		"/system/fonts/NotoSerifCJK-Regular.ttc",
		"/system/fonts/NotoColorEmoji.ttf",
		"/system/fonts/RobotoStatic-Regular.ttf",
	}
}
