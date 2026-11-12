package app

// SetupOverlay configures the window as a transparent, borderless overlay that
// floats above normal windows. It can receive mouse and keyboard events;
// on Windows, fully transparent pixels pass mouse input to windows underneath.
// Give it the display's size with SetupWindow before calling Run.
//
// Always-on-top placement is implemented on macOS, Windows, and Wayland with
// layer-shell v4. On macOS the window joins all spaces; on Windows it stays on
// its assigned virtual desktop. Wayland anchors the surface at logical desktop
// origin (0, 0) on one output, including reserved desktop areas. Other platforms
// and Wayland compositors without layer-shell get a borderless transparent window.
func SetupOverlay() {
	SetupTransparent()
	SetupUndecorated()
	setupOverlayBackend()
}
