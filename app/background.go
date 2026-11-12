package app

// SetupBackground keeps the process running after its last visible window
// closes. Use it for menu-bar apps, global-shortcut helpers, and other
// background processes that own no persistent visible window. Call before Run.
//
// The default is to terminate when the last window closes. Call this only to
// opt out for a background helper. Supported on macOS, Windows, and Wayland; other
// platforms ignore it. Windows support is best-effort and has not been verified
// on physical Windows hardware.
func SetupBackground() {
	setupBackgroundBackend()
}
