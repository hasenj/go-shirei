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
	shirei.GetHost().WindowSize = shirei.Vec2{float32(width), float32(height)}
	if useWayland {
		waylandbackend.SetupWindow(title, width, height)
	} else {
		x11backend.SetupWindow(title, width, height)
	}
}

// SetupTransparent preserves per-pixel alpha in the window's content.
// X11 requires a compositing manager. Call before Run.
// Transparent pixels receive input within the window's normal input region.
func SetupTransparent() {
	if useWayland {
		waylandbackend.SetupTransparent()
	} else {
		x11backend.SetupTransparent()
	}
}

// SetupUndecorated omits the window's title bar and standard buttons.
// On Wayland, uses a centered, top-layer window with on-demand keyboard focus
// when layer-shell v4 is available. Provide close and size controls in the app.
// Otherwise uses a toplevel subject to compositor policy. Call before Run.
// X11 requests undecorated windows through the window manager.
func SetupUndecorated() {
	if useWayland {
		waylandbackend.SetupUndecorated()
	} else {
		x11backend.SetupUndecorated()
	}
}

// SetupQuiet maps the window without asking the compositor/WM to take focus.
// Call before Run.
func SetupQuiet() {
	if useWayland {
		waylandbackend.SetupQuiet()
	} else {
		x11backend.SetupQuiet()
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
// frame. It must be called from the program's main goroutine and does not return:
// quit paths call generic.ExitWithCleanup so AddExitCleanup handlers run.
func Run(frameFn shirei.FrameFn) {
	if useWayland {
		waylandbackend.Run(frameFn)
	} else {
		x11backend.Run(frameFn)
	}
}

func setupBackgroundBackend() {
	if useWayland {
		waylandbackend.SetupBackground()
	}
}

func setupOverlayBackend() {
	if useWayland {
		waylandbackend.SetupOverlay()
	}
}
