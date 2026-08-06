//go:build windows

package shirei

import (
	"os"
	"path/filepath"
)

// criticalFontPaths returns likely Windows UI font files for first paint.
// Missing paths are skipped by UseFontFiles. No directory walk.
func criticalFontPaths() []string {
	windir := os.Getenv("WINDIR")
	if windir == "" {
		windir = os.Getenv("SystemRoot")
	}
	if windir == "" {
		windir = `C:\Windows`
	}
	dir := filepath.Join(windir, "Fonts")
	// Bold/italic peers of UI families so FontWeight(WeightBold) does not
	// fall through to a different family before the background scan finishes.
	return []string{
		filepath.Join(dir, "segoeui.ttf"),
		filepath.Join(dir, "segoeuib.ttf"),
		filepath.Join(dir, "segoeuii.ttf"),
		filepath.Join(dir, "segoeuiz.ttf"), // bold italic
		filepath.Join(dir, "segoeuil.ttf"), // light
		filepath.Join(dir, "seguisb.ttf"),  // semibold
		filepath.Join(dir, "consola.ttf"),
		filepath.Join(dir, "consolab.ttf"),
		filepath.Join(dir, "arial.ttf"),
		filepath.Join(dir, "arialbd.ttf"),
		filepath.Join(dir, "ariali.ttf"),
		filepath.Join(dir, "arialbi.ttf"),
		filepath.Join(dir, "tahoma.ttf"),
		filepath.Join(dir, "tahomabd.ttf"),
		filepath.Join(dir, "times.ttf"),
		filepath.Join(dir, "timesbd.ttf"),
		// Common CJK faces when installed (optional; skipped if absent).
		filepath.Join(dir, "msyh.ttc"),
		filepath.Join(dir, "msyhbd.ttc"),
		filepath.Join(dir, "msgothic.ttc"),
		filepath.Join(dir, "YuGothR.ttc"),
		filepath.Join(dir, "YuGothB.ttc"),
		filepath.Join(dir, "malgun.ttf"),
		filepath.Join(dir, "malgunbd.ttf"),
	}
}
