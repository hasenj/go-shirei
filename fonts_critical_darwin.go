//go:build darwin && !ios

package shirei

import (
	"os"
	"path/filepath"
)

// criticalFontPaths returns likely macOS UI font files for first paint.
// Missing paths are skipped by UseFontFiles. No directory walk.
//
// Include bold/medium weights of preferred UI families: defaultText falls
// back family-by-family at the *requested* weight, so Noto Sans Regular
// alone is not enough for FontWeight(WeightBold) labels — those would
// otherwise land on Arial Bold until the background scan finishes.
func criticalFontPaths() []string {
	paths := []string{
		"/System/Library/Fonts/Helvetica.ttc",
		"/System/Library/Fonts/SFNS.ttf",
		"/System/Library/Fonts/SFNSMono.ttf",
		"/System/Library/Fonts/Menlo.ttc",
		"/System/Library/Fonts/Supplemental/Arial.ttf",
		"/System/Library/Fonts/Supplemental/Arial Bold.ttf",
		"/System/Library/Fonts/Supplemental/Arial Italic.ttf",
		"/System/Library/Fonts/Supplemental/Arial Bold Italic.ttf",
		"/System/Library/Fonts/Supplemental/Times New Roman.ttf",
		"/System/Library/Fonts/Supplemental/Times New Roman Bold.ttf",
		"/Library/Fonts/Arial.ttf",
		"/Library/Fonts/Arial Bold.ttf",
		// Weight-400 CJK coverage (Hiragino W3 is often weight 300 and won't
		// match DefaultFontAspect until a Regular face is registered).
		"/System/Library/Fonts/STHeiti Medium.ttc",
		"/System/Library/Fonts/Supplemental/AppleGothic.ttf",
		"/System/Library/Fonts/AppleSDGothicNeo.ttc",
		"/System/Library/Fonts/ヒラギノ角ゴシック W3.ttc",
		"/System/Library/Fonts/Hiragino Sans GB.ttc",
	}

	// Developer machines often install Noto under ~/Library/Fonts or
	// /Library/Fonts. Prefer UI weights (regular/medium/semibold/bold +
	// italics + mono) so first paint matches post-scan fallbacks.
	var notoRoots []string
	if home, err := os.UserHomeDir(); err == nil {
		notoRoots = append(notoRoots, filepath.Join(home, "Library/Fonts"))
	}
	notoRoots = append(notoRoots, "/Library/Fonts")

	notoFiles := []string{
		"NotoSans-Regular.ttf",
		"NotoSans-Medium.ttf",
		"NotoSans-SemiBold.ttf",
		"NotoSans-Bold.ttf",
		"NotoSans-Italic.ttf",
		"NotoSans-BoldItalic.ttf",
		"NotoSansMono-Regular.ttf",
		"NotoSansMono-Bold.ttf",
		// JP / CJK packages (optional; skipped if absent).
		"NotoSansJP-Regular.otf",
		"NotoSansJP-Regular.ttf",
		"NotoSansJP-Bold.otf",
		"NotoSansJP-Bold.ttf",
		"NotoSansCJKjp-Regular.otf",
		"NotoSansCJKjp-Bold.otf",
	}
	for _, root := range notoRoots {
		for _, name := range notoFiles {
			paths = append(paths, filepath.Join(root, name))
		}
	}
	return paths
}
