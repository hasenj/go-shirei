// Package window is a Shirei extension that manages window geometry and
// placement (positioning, centering, and enforcing minimum window dimensions)
// on desktop platforms (macOS, Windows, Linux Wayland/X11) via
// Host.EscapeHatchBackendContext on a best-effort basis.
package window

import "go.hasen.dev/shirei"

type placementMode int

const (
	placeNone placementMode = iota
	placeCenter
	placeAt
)

type configuration struct {
	lastCtx                    shirei.BackendContext
	lastAppliedW, lastAppliedH float32
	hasPendingMinSize          bool
	pendingMinW, pendingMinH   float32
	hasPendingSize             bool
	pendingSizeW, pendingSizeH float32
	pendingPlacement           placementMode
	pendingX, pendingY         int
}

type configurationKey struct{}

// getPlatformPosition is set by each desktop backend. It returns the current
// window's top-left corner in the same coordinates as Position.
var getPlatformPosition func(ctx shirei.BackendContext) (shirei.Vec2, bool)

// getPlatformHovered is set by desktop backends to report whether the pointer
// is inside the current window's content.
var getPlatformHovered func(ctx shirei.BackendContext) bool

func config() *configuration {
	return shirei.LocalState[configuration](configurationKey{})
}

func init() {
	shirei.RegisterFrameStart(applyPending)
}

// SetMinSize enforces a minimum content width and height (in logical points)
// on the window. Best-effort across desktop backends.
//
// Safe to call during initialization (after app.SetupWindow) or dynamically
// at runtime inside frameFn or event handlers.
func SetMinSize(minWidth, minHeight float32) {
	if minWidth < 0 {
		minWidth = 0
	}
	if minHeight < 0 {
		minHeight = 0
	}

	c := config()
	ctx := shirei.GetHost().EscapeHatchBackendContext
	if ctx != nil && ctx == c.lastCtx && minWidth == c.lastAppliedW && minHeight == c.lastAppliedH {
		return
	}
	c.hasPendingMinSize = true
	c.pendingMinW, c.pendingMinH = minWidth, minHeight
	applyPending()
}

// SetSize sets the window content size (logical points). Best-effort across
// desktop backends. Wayland ordinary windows defer requests while tiled,
// maximized, fullscreen, or interactively resizing; mobile backends ignore them.
// Values below the current
// minimum (SetMinSize) are clamped by the platform.
//
// Call before app.Run, or on the UI thread inside frame functions and event handlers.
func SetSize(width, height float32) {
	if width < 0 {
		width = 0
	}
	if height < 0 {
		height = 0
	}

	c := config()
	c.hasPendingSize = true
	c.pendingSizeW, c.pendingSizeH = width, height
	applyPending()
}

// Center requests that the window be centered on the primary display.
// Best-effort across desktop platforms. Wayland centers layer windows on
// their current output; ordinary toplevels and mobile backends ignore it.
//
// Call before app.Run, or on the UI thread inside frame functions and event handlers.
func Center() {
	c := config()
	c.pendingPlacement = placeCenter
	applyPending()
}

// Position requests that the top-left corner of the window be placed at
// (x, y) in screen coordinates (logical points, origin at the top-left
// of the primary display). Wayland layer windows use logical desktop
// coordinates and select the containing or nearest output; ordinary toplevels ignore it.
// Best-effort across desktop platforms.
//
// Call before app.Run, or on the UI thread inside frame functions and event handlers.
func Position(x, y int) {
	c := config()
	c.pendingPlacement = placeAt
	c.pendingX, c.pendingY = x, y
	applyPending()
}

// Hovered reports whether the pointer is inside the current window's content.
// It is a best-effort geometric check on X11, macOS, and Windows. Wayland
// reports pointer focus on the window's surface, which is independent of
// keyboard focus, so non-activating windows report hover too. Mobile backends
// return false. Call on the UI thread.
func Hovered() bool {
	ctx := shirei.GetHost().EscapeHatchBackendContext
	if ctx == nil || getPlatformHovered == nil {
		return false
	}
	return getPlatformHovered(ctx)
}

// CurrentPosition returns the top-left corner of the current window in the
// screen coordinates used by Position. It reports false when the platform
// cannot report a position. The result is for the window whose frame is
// currently being built, so call it on the UI thread inside a frame function.
func CurrentPosition() (shirei.Vec2, bool) {
	ctx := shirei.GetHost().EscapeHatchBackendContext
	if ctx == nil || getPlatformPosition == nil {
		return shirei.Vec2{}, false
	}
	return getPlatformPosition(ctx)
}

// Activate brings the application and its main window to the front. It is
// best-effort; macOS and Windows use their native activation APIs. Wayland
// maps a hidden main window and requests focus through xdg-activation. macOS
// and Windows queue main-window activation on the UI thread, including calls
// from popups or background goroutines. The window manager controls whether
// an application can take foreground focus.
func Activate() {
	activatePlatformApp()
}

// applyPending runs on the owning UI thread, once its native context exists.
func applyPending() {
	ctx := shirei.GetHost().EscapeHatchBackendContext
	if ctx == nil {
		return
	}
	c := config()
	if c.hasPendingMinSize {
		c.hasPendingMinSize = false
		c.lastCtx, c.lastAppliedW, c.lastAppliedH = ctx, c.pendingMinW, c.pendingMinH
		setPlatformMinSize(ctx, c.pendingMinW, c.pendingMinH)
	}
	if c.hasPendingSize {
		c.hasPendingSize = false
		setPlatformSize(ctx, c.pendingSizeW, c.pendingSizeH)
	}
	placement := c.pendingPlacement
	c.pendingPlacement = placeNone
	switch placement {
	case placeCenter:
		setPlatformCenter(ctx)
	case placeAt:
		setPlatformPosition(ctx, c.pendingX, c.pendingY)
	}
}

var (
	setPlatformMinSize  = func(ctx shirei.BackendContext, minW, minH float32) {}
	setPlatformSize     = func(ctx shirei.BackendContext, w, h float32) {}
	setPlatformCenter   = func(ctx shirei.BackendContext) {}
	setPlatformPosition = func(ctx shirei.BackendContext, x, y int) {}
)
