//go:build darwin && !ios

package cocoabackend

import (
	"fmt"
	"math"
	"sync"
	"sync/atomic"

	"github.com/ebitengine/purego/objc"
	"go.hasen.dev/shirei"
	"go.hasen.dev/shirei/gpurender"
)

var popupWindows = map[shirei.PopupWindow]*windowState{}
var nextPopupID shirei.PopupWindow
var pendingShows []*windowState
var mainCloseRequested bool
var popupWakeTarget atomic.Uintptr
var mainActivationRequested atomic.Bool
var popupCloseQueue struct {
	sync.Mutex
	ids []shirei.PopupWindow
}

var popupMoveQueue struct {
	sync.Mutex
	positions map[shirei.PopupWindow]shirei.Vec2
}

// OpenPopupWindow creates a hidden panel and schedules its first frame after
// the current UI callback. The calling window owns the panel.
func OpenPopupWindow(options shirei.PopupWindowOptions, root shirei.FrameFn) (shirei.PopupWindow, error) {
	if err := ensureAppkit(); err != nil {
		return 0, err
	}
	if !objc.Send[bool](objc.ID(objc.GetClass("NSThread")), sel("isMainThread")) {
		return 0, fmt.Errorf("OpenPopupWindow must run on the UI thread")
	}
	owner := currentWindow
	if !mainWindow.ready || owner.closing || owner.closed {
		return 0, fmt.Errorf("OpenPopupWindow requires a running owner window")
	}
	if root == nil || options.Width <= 0 || options.Height <= 0 {
		return 0, fmt.Errorf("OpenPopupWindow requires a root view and positive content dimensions")
	}
	if p := options.Position; p != nil && (math.IsNaN(float64(p[0])) || math.IsNaN(float64(p[1])) || math.IsInf(float64(p[0]), 0) || math.IsInf(float64(p[1]), 0)) {
		return 0, fmt.Errorf("OpenPopupWindow position must be finite")
	}
	w := newWindowState(shirei.NewUI())
	w.owner, w.frameFn, w.onClose = owner, root, options.OnClose
	w.popupFloating = options.Floating
	w.popupFocus = options.Focus
	w.popupNonactivating = options.Nonactivating
	w.popupExactPosition = options.ExactScreenPosition
	w.popupTopLevel = options.TopLevel
	w.ui.Host.WindowSize = shirei.Vec2{float32(options.Width), float32(options.Height)}
	w.ui.Host.GlyphCacheBudgetBytes = glyphCacheBudget
	w.ui.Host.TransparentBackground = options.Transparent
	w.ui.Host.EscapeHatchBackendContext = Context{window: w}
	leave := enterWindow(w)
	defer leave()
	pool := objc.ID(objc.GetClass("NSAutoreleasePool")).Send(sel("new"))
	defer pool.Send(sel("drain"))
	if !createNativeWindow(options.Title, options.Width, options.Height, options.Transparent, options.Undecorated, true) {
		return 0, fmt.Errorf("could not create popup window")
	}
	enableZerocopy()
	placePopup(w, options.Position, options.ScreenPosition)
	nextPopupID++
	w.popupID = nextPopupID
	popupWindows[w.popupID] = w
	w.unsubscribe = shirei.RegisterWindowUI(w.ui)
	pendingShows = append(pendingShows, w)
	schedulePopupUpdate()
	return w.popupID, nil
}

// ClosePopupWindow queues a close without touching window or UI state on the
// calling goroutine. AppKit processes the queue between frames.
func ClosePopupWindow(id shirei.PopupWindow) {
	target := objc.ID(popupWakeTarget.Load())
	if id == 0 || target == 0 {
		return
	}
	popupCloseQueue.Lock()
	popupCloseQueue.ids = append(popupCloseQueue.ids, id)
	popupCloseQueue.Unlock()
	target.Send(sel("performSelectorOnMainThread:withObject:waitUntilDone:"), sel("updatePopupWindows"), objc.ID(0), false)
}

// MovePopupWindow queues a new screen position for a live popup. The position
// uses the same top-left-origin screen points as the open options.
func MovePopupWindow(id shirei.PopupWindow, position *shirei.Vec2) {
	target := objc.ID(popupWakeTarget.Load())
	if id == 0 || target == 0 || position == nil {
		return
	}
	popupMoveQueue.Lock()
	if popupMoveQueue.positions == nil {
		popupMoveQueue.positions = map[shirei.PopupWindow]shirei.Vec2{}
	}
	popupMoveQueue.positions[id] = *position
	popupMoveQueue.Unlock()
	target.Send(sel("performSelectorOnMainThread:withObject:waitUntilDone:"), sel("updatePopupWindows"), objc.ID(0), false)
}

func schedulePopupUpdate() {
	gDelegate.Send(sel("performSelector:withObject:afterDelay:"), sel("updatePopupWindows"), objc.ID(0), float64(0))
}

// Activate queues restoration and activation of the main window. Safe from
// popup callbacks and background goroutines; AppKit handles it between frames.
func Activate() {
	target := objc.ID(popupWakeTarget.Load())
	if target == 0 {
		return
	}
	mainActivationRequested.Store(true)
	target.Send(sel("performSelectorOnMainThread:withObject:waitUntilDone:"), sel("updatePopupWindows"), objc.ID(0), false)
}

func appWindowShouldClose(_ objc.ID, _ objc.SEL, window objc.ID) bool {
	w := windowsByObject[window]
	if w == nil {
		return true
	}
	if w == mainWindow {
		if winKeepAlive {
			w.gWindow.Send(sel("orderOut:"), objc.ID(0))
			return false
		}
		mainCloseRequested = true
		schedulePopupUpdate()
	} else {
		ClosePopupWindow(w.popupID)
	}
	return false
}

func appUpdatePopupWindows(objc.ID, objc.SEL) {
	// Native calls can synchronously dispatch callbacks while a root is being
	// built. Destruction and first-frame delivery wait for its outer scope.
	if callbackDepth > 0 {
		schedulePopupUpdate()
		return
	}
	leave := enterWindow(mainWindow)
	defer leave()
	pool := objc.ID(objc.GetClass("NSAutoreleasePool")).Send(sel("new"))
	defer pool.Send(sel("drain"))
	popupCloseQueue.Lock()
	ids := popupCloseQueue.ids
	popupCloseQueue.ids = nil
	popupCloseQueue.Unlock()

	var callbacks []popupClosedCallback
	var focusOwner *windowState
	if mainCloseRequested {
		mainWindow.closing = true
		for _, w := range popupWindows {
			markPopupClosing(w)
		}
	}
	for _, id := range ids {
		if w := popupWindows[id]; w != nil {
			markPopupClosing(w)
		}
	}
	for _, w := range popupWindows {
		if w.closing && objc.Send[bool](w.gWindow, sel("isKeyWindow")) {
			focusOwner = w.owner
			for focusOwner != nil && (focusOwner.closing || focusOwner.closed) {
				focusOwner = focusOwner.owner
			}
		}
	}
	if gpuOK && (len(ids) > 0 || mainCloseRequested) {
		gpurender.WaitIdle()
	}
	for _, w := range popupWindows {
		if w.closing {
			disposePopup(w, &callbacks)
		}
	}

	popupMoveQueue.Lock()
	moves := popupMoveQueue.positions
	popupMoveQueue.positions = nil
	popupMoveQueue.Unlock()
	for id, position := range moves {
		if w := popupWindows[id]; w != nil && !w.closing && !w.closed {
			restore := enterWindow(w)
			placePopup(w, nil, &position)
			restore()
		}
	}
	if focusOwner != nil && objc.Send[bool](nsApp(), sel("isActive")) {
		focusOwner.gWindow.Send(sel("makeKeyAndOrderFront:"), objc.ID(0))
	}
	for _, callback := range callbacks {
		owner := callback.owner
		if owner.closed {
			owner = mainWindow
		}
		restore := enterWindow(owner)
		callback.fn()
		restore()
	}
	if mainCloseRequested {
		// A close callback can request main-window shutdown during this pass.
		if len(popupWindows) > 0 {
			schedulePopupUpdate()
			return
		}
		exitWithCleanup()
		return
	}
	shows := pendingShows
	pendingShows = nil
	for _, w := range shows {
		if w.closed {
			continue
		}
		restore := enterWindow(w)
		w.ready = true
		if w.popupFloating {
			// A floating popup is a top-level panel; child-window ordering
			// would pull it back to the hidden owner's normal level.
			w.gWindow.Send(sel("setHidesOnDeactivate:"), false)
			w.gWindow.Send(sel("setLevel:"), 3)
		} else if w.popupTopLevel {
			// A normal top-level window can go behind other apps, but should
			// not disappear merely because this background app is inactive.
			w.gWindow.Send(sel("setHidesOnDeactivate:"), false)
			w.gWindow.Send(sel("setLevel:"), 0)
		} else {
			w.owner.gWindow.Send(sel("addChildWindow:ordered:"), w.gWindow, int(1)) // NSWindowAbove
		}
		startWindowClock()
		if w.popupNonactivating {
			// Show without activating the app and without making the panel key.
			w.gWindow.Send(sel("orderFrontRegardless"))
		} else if w.popupTopLevel && !w.popupFocus {
			// Normal top-level utility window: bring it forward without
			// activating the app or pinning it above other windows.
			w.gWindow.Send(sel("orderFrontRegardless"))
		} else if winQuiet && !w.popupFocus {
			w.gWindow.Send(sel("orderFront:"), objc.ID(0))
		} else {
			if w.popupFocus {
				nsApp().Send(sel("activateIgnoringOtherApps:"), true)
			}
			w.gWindow.Send(sel("makeKeyAndOrderFront:"), objc.ID(0))
		}
		restore()
	}
	if mainActivationRequested.Swap(false) && mainWindow.ready && !mainWindow.closed && !mainWindow.closing {
		w := mainWindow.gWindow
		if objc.Send[bool](w, sel("isMiniaturized")) {
			w.Send(sel("deminiaturize:"), objc.ID(0))
		}
		nsApp().Send(sel("activateIgnoringOtherApps:"), true)
		w.Send(sel("makeKeyAndOrderFront:"), objc.ID(0))
		w.Send(sel("orderFrontRegardless"))
	}
	shirei.RequestNextFrame()
}

func markPopupClosing(w *windowState) {
	if w.closing {
		return
	}
	w.closing = true
	for _, child := range popupWindows {
		if child.owner == w {
			markPopupClosing(child)
		}
	}
}

type popupClosedCallback struct {
	owner *windowState
	fn    func()
}

func disposePopup(w *windowState, callbacks *[]popupClosedCallback) {
	if w.closed {
		return
	}
	for _, child := range popupWindows {
		if child.owner == w {
			disposePopup(child, callbacks)
		}
	}
	restore := enterWindow(w)
	w.closed = true
	w.ready = false
	if w.clock != 0 {
		w.clock.Send(sel("invalidate"))
		w.clock.Send(sel("release"))
	}
	objc.ID(objc.GetClass("NSObject")).Send(sel("cancelPreviousPerformRequestsWithTarget:"), w.gView)
	updateAccess(nil)
	w.gContentLayer.Send(sel("setContents:"), objc.ID(0))
	for _, surface := range w.surfacePool {
		if gpuOK {
			gpurender.Forget(surface.ref)
		}
		delete(surfaceWindows, surface.ref)
		ioSurfaceRelease(surface.ref)
	}
	if !w.popupFloating {
		w.owner.gWindow.Send(sel("removeChildWindow:"), w.gWindow)
	}
	w.gWindow.Send(sel("setDelegate:"), objc.ID(0))
	w.gWindow.Send(sel("close"))
	delete(windowsByObject, w.gView)
	delete(windowsByObject, w.gWindow)
	w.gWindow.Send(sel("setContentView:"), objc.ID(0))
	w.gView.Send(sel("release"))
	w.gWindow.Send(sel("release"))
	w.unsubscribe()
	delete(popupWindows, w.popupID)
	if w.onClose != nil {
		*callbacks = append(*callbacks, popupClosedCallback{w.owner, w.onClose})
	}
	restore()
	// Native contexts can outlive their windows. Keep only the closed marker.
	*w = windowState{closed: true}
}

func placePopup(w *windowState, position, screenPosition *shirei.Vec2) {
	owner := objc.Send[nsRect](w.owner.gWindow, sel("frame"))
	frame := objc.Send[nsRect](w.gWindow, sel("frame"))
	var screen objc.ID
	x := owner.Origin.X + (owner.Size.Width-frame.Size.Width)/2
	y := owner.Origin.Y + (owner.Size.Height+frame.Size.Height)/2
	switch {
	case screenPosition != nil:
		screen = objc.ID(objc.GetClass("NSScreen")).Send(sel("mainScreen"))
		if screen != 0 {
			display := objc.Send[nsRect](screen, sel("frame"))
			x = float64(screenPosition[0])
			y = display.Origin.Y + display.Size.Height - float64(screenPosition[1])
		}
	case position != nil:
		content := objc.Send[nsRect](w.owner.gWindow, sel("contentRectForFrameRect:"), owner)
		x = content.Origin.X + float64(position[0])
		y = content.Origin.Y + content.Size.Height - float64(position[1])
	}
	if screen == 0 {
		screen = w.owner.gWindow.Send(sel("screen"))
	}
	if screen == 0 {
		screen = objc.ID(objc.GetClass("NSScreen")).Send(sel("mainScreen"))
	}
	if screen != 0 && !w.popupExactPosition {
		visible := objc.Send[nsRect](screen, sel("visibleFrame"))
		x = math.Max(visible.Origin.X, math.Min(x, visible.Origin.X+visible.Size.Width-frame.Size.Width))
		y = math.Min(visible.Origin.Y+visible.Size.Height, math.Max(y, visible.Origin.Y+frame.Size.Height))
	}
	w.gWindow.Send(sel("setFrameTopLeftPoint:"), nsPoint{X: x, Y: y})
}
