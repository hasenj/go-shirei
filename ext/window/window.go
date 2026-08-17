// Package window is a Shirei extension that manages window geometry and
// placement (positioning, centering, and enforcing minimum window dimensions)
// on desktop platforms (macOS, Windows, Linux Wayland/X11) via
// Host.EscapeHatchBackendContext on a best-effort basis.
package window

import (
	"sync"
	"time"

	"go.hasen.dev/shirei"
)

type placementMode int

const (
	placeNone placementMode = iota
	placeCenter
	placeAt
)

var (
	mu sync.Mutex

	// Size cache & pending state
	lastAppliedW float32
	lastAppliedH float32
	lastCtx      shirei.BackendContext

	hasPendingMinSize bool
	pendingMinW       float32
	pendingMinH       float32

	// Placement pending state
	pendingPlacement placementMode
	pendingX         int
	pendingY         int

	waiterActive bool
)

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

	mu.Lock()
	hasPendingMinSize = true
	pendingMinW = minWidth
	pendingMinH = minHeight

	ctx := shirei.GetHost().EscapeHatchBackendContext
	if ctx == nil {
		ensureWaiterLocked()
		mu.Unlock()
		return
	}

	if ctx == lastCtx && minWidth == lastAppliedW && minHeight == lastAppliedH {
		mu.Unlock()
		return
	}
	mu.Unlock()

	applyMinSize(ctx, minWidth, minHeight)
}

// Center requests that the window be centered on the primary display.
// Best-effort across desktop platforms (ignored on Wayland and mobile).
//
// Safe to call before app.Run or dynamically during runtime.
func Center() {
	mu.Lock()
	pendingPlacement = placeCenter

	ctx := shirei.GetHost().EscapeHatchBackendContext
	if ctx == nil {
		ensureWaiterLocked()
		mu.Unlock()
		return
	}
	mu.Unlock()

	setPlatformCenter(ctx)
}

// Position requests that the top-left corner of the window be placed at
// (x, y) in screen coordinates (logical points, origin at the top-left
// of the primary display). Best-effort across desktop platforms.
//
// Safe to call before app.Run or dynamically during runtime.
func Position(x, y int) {
	mu.Lock()
	pendingPlacement = placeAt
	pendingX = x
	pendingY = y

	ctx := shirei.GetHost().EscapeHatchBackendContext
	if ctx == nil {
		ensureWaiterLocked()
		mu.Unlock()
		return
	}
	mu.Unlock()

	setPlatformPosition(ctx, x, y)
}

func ensureWaiterLocked() {
	if !waiterActive {
		waiterActive = true
		go waitForWindow()
	}
}

func waitForWindow() {
	for range 200 {
		time.Sleep(10 * time.Millisecond)

		mu.Lock()
		ctx := shirei.GetHost().EscapeHatchBackendContext
		if ctx != nil {
			waiterActive = false
			doMinSize := hasPendingMinSize
			minW, minH := pendingMinW, pendingMinH
			placement := pendingPlacement
			px, py := pendingX, pendingY
			pendingPlacement = placeNone
			mu.Unlock()

			if doMinSize {
				applyMinSize(ctx, minW, minH)
			}
			switch placement {
			case placeCenter:
				setPlatformCenter(ctx)
			case placeAt:
				setPlatformPosition(ctx, px, py)
			}
			return
		}
		mu.Unlock()
	}

	mu.Lock()
	waiterActive = false
	mu.Unlock()
}

func applyMinSize(ctx shirei.BackendContext, minWidth, minHeight float32) {
	if ctx == nil {
		return
	}
	setPlatformMinSize(ctx, minWidth, minHeight)

	mu.Lock()
	lastCtx = ctx
	lastAppliedW = minWidth
	lastAppliedH = minHeight
	mu.Unlock()
}

var (
	setPlatformMinSize  = func(ctx shirei.BackendContext, minW, minH float32) {}
	setPlatformCenter   = func(ctx shirei.BackendContext) {}
	setPlatformPosition = func(ctx shirei.BackendContext, x, y int) {}
)
