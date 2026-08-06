//go:build linux && !android

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

// SetupWindow records the window's title and initial content size in points.
// Call it before Run. On Wayland with CSD the surface is taller by the
// titlebar so the app body keeps that size (X11 uses server decorations).
func SetupWindow(title string, width, height int) {
	if useWayland {
		waylandbackend.SetupWindow(title, width, height)
	} else {
		x11backend.SetupWindow(title, width, height)
	}
}

// CenterWindow requests that the window open centered on the screen. Best-effort:
// honored on macOS (also the default), Windows, and X11; ignored on Wayland and
// mobile. Call after SetupWindow and before Run. Mutually exclusive with
// PositionWindow; the last call wins.
func CenterWindow() {
	if useWayland {
		waylandbackend.CenterWindow()
	} else {
		x11backend.CenterWindow()
	}
}

// PositionWindow requests that the window open with its top-left corner at
// (x, y) in screen points (origin at the top-left of the primary display).
// Best-effort: honored on macOS, Windows, and X11; ignored on Wayland and
// mobile. Call after SetupWindow and before Run. Mutually exclusive with
// CenterWindow; the last call wins.
func PositionWindow(x, y int) {
	if useWayland {
		waylandbackend.PositionWindow(x, y)
	} else {
		x11backend.PositionWindow(x, y)
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
