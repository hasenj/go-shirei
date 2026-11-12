package app

import "go.hasen.dev/shirei"

type PopupWindow = shirei.PopupWindow
type PopupWindowOptions = shirei.PopupWindowOptions

// OpenPopupWindow opens a keyboard-capable auxiliary window owned by the
// calling window. Call once from a UI interaction, rather than on every frame.
// The root's first frame runs after this call returns. Focus loss does not close
// the popup. Closing its owner closes it. Supported on macOS, Windows, Wayland,
// and X11; other backends return an error. Wayland placement and activation follow compositor policy.
// On Windows, TopLevel creates an unowned normal-level utility window.
// Calls must originate on the UI thread after Run starts.
func OpenPopupWindow(options PopupWindowOptions, root shirei.FrameFn) (PopupWindow, error) {
	return openPopupWindow(options, root)
}

// ClosePopupWindow requests a close at the next safe event-loop boundary.
// Safe from any goroutine and harmless for zero, unknown, or closed handles.
func ClosePopupWindow(window PopupWindow) { closePopupWindow(window) }

// MovePopupWindow requests a new screen position for a popup, using the same
// top-left-origin screen points as PopupWindowOptions.ScreenPosition. Safe from
// any goroutine on macOS, Windows, and Wayland. Wayland moves layer windows
// between outputs, deferring migration during a primary-button drag until release.
// Ordinary Wayland toplevels and other backends ignore it.
func MovePopupWindow(window PopupWindow, position *shirei.Vec2) { movePopupWindow(window, position) }
