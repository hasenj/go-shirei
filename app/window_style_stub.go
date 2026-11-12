//go:build (!darwin || ios) && !windows && (!linux || android)

package app

// SetupUndecorated omits the window's title bar and standard window buttons.
// The window can receive keyboard input and resize. Call before Run.
// Supported on macOS, Windows, and Wayland; other platforms ignore this option.
func SetupUndecorated() {}
