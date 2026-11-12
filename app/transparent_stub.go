//go:build (!darwin || ios) && !windows && (!linux || android)

package app

// SetupTransparent lets unpainted pixels reveal the windows behind this window.
// App backgrounds and controls retain their own alpha. Call before Run.
// Supported on macOS, Windows, and Wayland; other platforms ignore this option.
func SetupTransparent() {}
