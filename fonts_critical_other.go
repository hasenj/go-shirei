//go:build !windows && !darwin && !linux && !android && !ios

package shirei

// criticalFontPaths is empty on uncommon GOOS values; the background walk
// (when fontscan reports dirs) still fills the registry.
func criticalFontPaths() []string {
	return nil
}
