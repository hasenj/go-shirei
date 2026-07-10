//go:build linux

package app

import (
	"image"
	"os"

	"go.hasen.dev/shirei"
	"go.hasen.dev/shirei/internal/iconimg"
	"go.hasen.dev/shirei/waylandbackend"
	"go.hasen.dev/shirei/x11backend"
)

// On Linux the shell is selected at runtime: Wayland when a compositor is present
// ($WAYLAND_DISPLAY set), otherwise X11. The choice is made once so SetupWindow
// and Run always use the same backend.
var useWayland = os.Getenv("WAYLAND_DISPLAY") != ""

// SetupWindow records the window's title and initial size in points. Call it
// before Run.
func SetupWindow(title string, width, height int) {
	if useWayland {
		waylandbackend.SetupWindow(title, width, height)
	} else {
		x11backend.SetupWindow(title, width, height)
	}
}

// SetupIcon records the path of the image (PNG etc.) used as the app's icon —
// shown wherever the platform shows one (macOS: Dock; Windows: title bar and
// taskbar; X11: wherever the WM displays _NET_WM_ICON; Wayland: via
// xdg-toplevel-icon-v1 where the compositor ships it, otherwise a .desktop
// file matched by app_id). Optional; call it before Run.
func SetupIcon(imagePath string) {
	if useWayland {
		waylandbackend.SetupIcon(imagePath)
	} else {
		x11backend.SetupIcon(imagePath)
	}
}

// SetupIconImage is SetupIcon from an in-memory image instead of a file. It
// takes precedence over SetupIcon. Optional; call it before Run.
func SetupIconImage(img image.Image) {
	if useWayland {
		waylandbackend.SetupIconImage(img)
	} else {
		x11backend.SetupIconImage(img)
	}
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
	if useWayland {
		waylandbackend.Run(frameFn)
	} else {
		x11backend.Run(frameFn)
	}
}
