//go:build windows

package app

import (
	"image"

	"go.hasen.dev/shirei"
	"go.hasen.dev/shirei/internal/iconimg"
	"go.hasen.dev/shirei/win32backend"
)

// SetupWindow records the window's title and initial size in points. Call it
// before Run.
func SetupWindow(title string, width, height int) {
	shirei.GetHost().WindowSize = shirei.Vec2{float32(width), float32(height)}
	win32backend.SetupWindow(title, width, height)
}

func setupBackgroundBackend() {
	win32backend.SetupBackground()
}

func setupOverlayBackend() {
	win32backend.SetupOverlay()
}

// SetupQuiet maps the window without making it the foreground. Call before Run.
func SetupQuiet() {
	win32backend.SetupQuiet()
}

// SetupTransparent preserves per-pixel alpha in the window's content.
// Fully transparent pixels pass mouse input to windows underneath.
// Call before Run. Supported on macOS and Windows; ignored elsewhere.
func SetupTransparent() {
	win32backend.SetupTransparent()
}

// SetupUndecorated omits the title bar and standard window buttons.
// The window can receive keyboard input and resize. Call before Run.
// Supported on macOS and Windows; ignored elsewhere.
func SetupUndecorated() {
	win32backend.SetupUndecorated()
}

// SetupIcon records the path of the image (PNG etc.) used as the app's icon —
// shown wherever the platform shows one (macOS: Dock; Windows: title bar and
// taskbar; X11: wherever the WM displays _NET_WM_ICON; Wayland: via
// xdg-toplevel-icon-v1 where the compositor ships it, otherwise a .desktop
// file matched by app_id). Optional; call it before Run.
func SetupIcon(imagePath string) {
	win32backend.SetupIcon(imagePath)
}

// SetupIconImage is SetupIcon from an in-memory image instead of a file. It
// takes precedence over SetupIcon. Optional; call it before Run.
func SetupIconImage(img image.Image) {
	win32backend.SetupIconImage(img)
}

// SetupIconBytes is SetupIcon from encoded image bytes (PNG etc.), e.g. a
// go:embed-ed asset. It takes precedence over SetupIcon; bytes that fail to
// decode leave the default icon. Optional; call it before Run.
func SetupIconBytes(data []byte) {
	if img := iconimg.DecodeBytes(data); img != nil {
		SetupIconImage(img)
	}
}

// Run opens the window and runs the native event loop, invoking frameFn once per
// frame. It must be called from the program's main goroutine and does not return:
// quit paths call generic.ExitWithCleanup so AddExitCleanup handlers run.
func Run(frameFn shirei.FrameFn) {
	win32backend.Run(frameFn)
}
