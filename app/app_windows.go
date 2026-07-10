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
	win32backend.SetupWindow(title, width, height)
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
// frame. It must be called from the program's main goroutine and does not return
// until the app exits.
func Run(frameFn shirei.FrameFn) {
	win32backend.Run(frameFn)
}
