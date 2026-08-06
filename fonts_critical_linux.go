//go:build linux && !android

package shirei

// criticalFontPaths returns likely Linux UI font files for first paint.
// Distros differ; missing paths are skipped. No directory walk.
// Bold/medium weights of preferred families so FontWeight(WeightBold) UI
// text does not fall through to a different family before the background
// scan finishes.
func criticalFontPaths() []string {
	return []string{
		"/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf",
		"/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf",
		"/usr/share/fonts/truetype/dejavu/DejaVuSans-Oblique.ttf",
		"/usr/share/fonts/truetype/dejavu/DejaVuSans-BoldOblique.ttf",
		"/usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf",
		"/usr/share/fonts/truetype/dejavu/DejaVuSansMono-Bold.ttf",
		"/usr/share/fonts/truetype/liberation/LiberationSans-Regular.ttf",
		"/usr/share/fonts/truetype/liberation/LiberationSans-Bold.ttf",
		"/usr/share/fonts/truetype/liberation/LiberationSans-Italic.ttf",
		"/usr/share/fonts/truetype/liberation/LiberationSans-BoldItalic.ttf",
		"/usr/share/fonts/truetype/liberation/LiberationMono-Regular.ttf",
		"/usr/share/fonts/truetype/liberation/LiberationMono-Bold.ttf",
		"/usr/share/fonts/truetype/noto/NotoSans-Regular.ttf",
		"/usr/share/fonts/truetype/noto/NotoSans-Medium.ttf",
		"/usr/share/fonts/truetype/noto/NotoSans-SemiBold.ttf",
		"/usr/share/fonts/truetype/noto/NotoSans-Bold.ttf",
		"/usr/share/fonts/truetype/noto/NotoSans-Italic.ttf",
		"/usr/share/fonts/truetype/noto/NotoSans-BoldItalic.ttf",
		"/usr/share/fonts/truetype/noto/NotoSansMono-Regular.ttf",
		"/usr/share/fonts/truetype/noto/NotoSansMono-Bold.ttf",
		"/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc",
		"/usr/share/fonts/opentype/noto/NotoSansCJK-Bold.ttc",
		"/usr/share/fonts/truetype/noto/NotoSansCJK-Regular.ttc",
		"/usr/share/fonts/truetype/noto/NotoSansCJK-Bold.ttc",
	}
}
