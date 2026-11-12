package win32backend

import (
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"unsafe"

	"go.hasen.dev/shirei"
	"go.hasen.dev/shirei/gpurender"
)

const popupWake = 0x8000 + 74

var uiThreadID atomic.Uint32
var popupWakeTarget atomic.Uintptr
var popupWindows = map[shirei.PopupWindow]*windowState{}
var nextPopupID shirei.PopupWindow
var pendingShows []*windowState
var mainCloseRequested bool
var mainActivationRequested atomic.Bool
var pendingClosedCallbacks []popupClosedCallback
var popupCloseQueue struct {
	sync.Mutex
	ids []shirei.PopupWindow
}

var popupMoveQueue struct {
	sync.Mutex
	positions map[shirei.PopupWindow]shirei.Vec2
}

type popupClosedCallback struct {
	owner *windowState
	fn    func()
}

func currentThreadID() uintptr { id, _, _ := procGetCurrentThreadID.Call(); return id }

// OpenPopupWindow creates an owned tool window. Showing and first-frame delivery
// wait until the current UI callback returns to the message loop.
func OpenPopupWindow(options shirei.PopupWindowOptions, root shirei.FrameFn) (shirei.PopupWindow, error) {
	if id := uiThreadID.Load(); id == 0 || uint32(currentThreadID()) != id {
		return 0, fmt.Errorf("OpenPopupWindow must run on the UI thread after Run starts")
	}
	owner := currentWindow
	if !mainWindow.ready || mainCloseRequested || owner.closed || owner.closing {
		return 0, fmt.Errorf("OpenPopupWindow requires a running owner window")
	}
	if root == nil || options.Width <= 0 || options.Height <= 0 {
		return 0, fmt.Errorf("OpenPopupWindow requires a root view and positive content dimensions")
	}
	for _, p := range []*shirei.Vec2{options.Position, options.ScreenPosition} {
		if p != nil && (math.IsNaN(float64(p[0])) || math.IsNaN(float64(p[1])) || math.IsInf(float64(p[0]), 0) || math.IsInf(float64(p[1]), 0)) {
			return 0, fmt.Errorf("OpenPopupWindow position must be finite")
		}
	}
	w := newWindowState(shirei.NewUI())
	w.owner, w.frameFn = owner, root
	w.winTitle, w.winW, w.winH = options.Title, options.Width, options.Height
	w.winTransparent, w.winUndecorated = options.Transparent, options.Undecorated
	w.winFloating, w.winFocus = options.Floating, options.Focus
	w.winTopLevel, w.winExactPosition = options.TopLevel, options.ExactScreenPosition
	w.winNonactivating = options.Nonactivating
	w.ui.Host.WindowSize = shirei.Vec2{float32(options.Width), float32(options.Height)}
	w.ui.Host.GlyphCacheBudgetBytes = glyphCacheBudget
	w.ui.Host.TransparentBackground = options.Transparent
	w.ui.Host.EscapeHatchBackendContext = Context{window: w}
	leave := enterWindow(w)
	defer leave()
	if err := createWindow(); err != nil {
		return 0, err
	}
	w.onClose = options.OnClose
	placePopup(w, options.Position, options.ScreenPosition)
	initAccess()
	nextPopupID++
	w.popupID = nextPopupID
	popupWindows[w.popupID] = w
	w.unsubscribe = shirei.RegisterWindowUI(w.ui)
	pendingShows = append(pendingShows, w)
	procPostMessageW.Call(popupWakeTarget.Load(), popupWake, 0, 0)
	return w.popupID, nil
}

// ClosePopupWindow queues a close from any goroutine. Window handles and UI
// state are only touched by the message loop.
func ClosePopupWindow(id shirei.PopupWindow) {
	target := popupWakeTarget.Load()
	if id == 0 || target == 0 {
		return
	}
	popupCloseQueue.Lock()
	popupCloseQueue.ids = append(popupCloseQueue.ids, id)
	popupCloseQueue.Unlock()
	procPostMessageW.Call(target, popupWake, 0, 0)
}

// Activate requests main-window restoration and activation on the UI thread.
// Safe from any goroutine after Run starts. Windows foreground policy can deny
// activation. A maximized window keeps its size unless it is minimized.
func Activate() {
	target := popupWakeTarget.Load()
	if target == 0 {
		return
	}
	mainActivationRequested.Store(true)
	procPostMessageW.Call(target, popupWake, 0, 0)
}

// MovePopupWindow queues a new screen position for a live popup. The position
// uses the same top-left-origin screen points as the open options.
func MovePopupWindow(id shirei.PopupWindow, position *shirei.Vec2) {
	target := popupWakeTarget.Load()
	if id == 0 || target == 0 || position == nil {
		return
	}
	popupMoveQueue.Lock()
	if popupMoveQueue.positions == nil {
		popupMoveQueue.positions = map[shirei.PopupWindow]shirei.Vec2{}
	}
	popupMoveQueue.positions[id] = *position
	popupMoveQueue.Unlock()
	procPostMessageW.Call(target, popupWake, 0, 0)
}

func updatePopupWindows() {
	if callbackDepth != 0 {
		return
	}
	leave := enterWindow(mainWindow)
	defer leave()
	for {
		popupCloseQueue.Lock()
		ids := popupCloseQueue.ids
		popupCloseQueue.ids = nil
		popupCloseQueue.Unlock()
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
		var focusOwner *windowState
		focus, _, _ := procGetFocus.Call()
		for _, w := range popupWindows {
			if w.closing && uintptr(w.hwnd) == focus {
				focusOwner = w.owner
				for focusOwner != nil && (focusOwner.closing || focusOwner.closed) {
					focusOwner = focusOwner.owner
				}
			}
		}
		for _, w := range popupWindows {
			if w.closing {
				destroyPopup(w)
			}
		}
		if focusOwner != nil {
			procSetFocus.Call(uintptr(focusOwner.hwnd))
		}
		callbacks := pendingClosedCallbacks
		pendingClosedCallbacks = nil
		for _, callback := range callbacks {
			owner := callback.owner
			if owner.closed {
				owner = mainWindow
			}
			restore := enterWindow(owner)
			callback.fn()
			restore()
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
		// A close callback may create and immediately close another popup.
		popupCloseQueue.Lock()
		more := len(popupCloseQueue.ids) > 0
		popupCloseQueue.Unlock()
		if !more && !(mainCloseRequested && len(popupWindows) > 0) {
			break
		}
	}
	if mainCloseRequested {
		popupWakeTarget.Store(0)
		if !mainWindow.closed {
			procDestroyWindow.Call(uintptr(mainWindow.hwnd))
		}
		releaseGPU()
		procPostQuitMessage.Call(0)
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
		showWindow()
		startTimer()
		noteInput()
		restore()
	}
	if mainActivationRequested.Swap(false) && mainWindow.ready && !mainWindow.closed && !mainWindow.closing {
		hwnd := uintptr(mainWindow.hwnd)
		show := uintptr(swShow)
		if minimized, _, _ := procIsIconic.Call(hwnd); minimized != 0 {
			show = swRestore
		}
		procShowWindow.Call(hwnd, show)
		procBringWindowToTop.Call(hwnd)
		procSetForegroundWindow.Call(hwnd)
		procSetFocus.Call(hwnd)
		noteInput()
	}
}

func markPopupClosing(w *windowState) {
	if w.closing || w.closed {
		return
	}
	w.closing = true
	for _, child := range popupWindows {
		if child.owner == w {
			markPopupClosing(child)
		}
	}
}

func destroyPopup(w *windowState) {
	if w.closed {
		return
	}
	for _, child := range popupWindows {
		if child.owner == w {
			destroyPopup(child)
		}
	}
	procDestroyWindow.Call(uintptr(w.hwnd))
}

// WM_DESTROY releases resources while the window still has a native identity.
// Close callbacks run after all nested native callbacks finish.
func disposeWindowResources(w *windowState) {
	if w.closed {
		return
	}
	w.closed, w.ready = true, false
	stopTimer()
	closeAccess()
	gpurender.Forget(unsafe.Pointer(w.hwnd))
	releaseDIB()
	releaseLayeredDIB()
	if w.memDC != 0 {
		procDeleteDC.Call(uintptr(w.memDC))
		w.memDC = 0
	}
	if w.unsubscribe != nil {
		w.unsubscribe()
		w.unsubscribe = nil
	}
	delete(popupWindows, w.popupID)
	if w.onClose != nil {
		pendingClosedCallbacks = append(pendingClosedCallbacks, popupClosedCallback{w.owner, w.onClose})
		w.onClose = nil
	}
	if w == mainWindow {
		mainCloseRequested = true
	}
}

func placePopup(w *windowState, position, screenPosition *shirei.Vec2) {
	var owner, frame win32Rect
	procGetWindowRect.Call(uintptr(w.owner.hwnd), uintptr(unsafe.Pointer(&owner)))
	procGetWindowRect.Call(uintptr(w.hwnd), uintptr(unsafe.Pointer(&frame)))
	width, height := frame.Right-frame.Left, frame.Bottom-frame.Top
	x, y := owner.Left+(owner.Right-owner.Left-width)/2, owner.Top+(owner.Bottom-owner.Top-height)/2
	scale := dpiScale()
	switch {
	case screenPosition != nil:
		x, y = int32(screenPosition[0]*scale), int32(screenPosition[1]*scale)
	case position != nil:
		var origin win32Point
		procClientToScreen.Call(uintptr(w.owner.hwnd), uintptr(unsafe.Pointer(&origin)))
		x, y = origin.X+int32(position[0]*scale), origin.Y+int32(position[1]*scale)
	}
	exact := screenPosition != nil && w.winExactPosition
	if !exact {
		var monitor uintptr
		if screenPosition != nil {
			pointRect := win32Rect{Left: x, Top: y, Right: x + 1, Bottom: y + 1}
			monitor, _, _ = procMonitorFromRect.Call(uintptr(unsafe.Pointer(&pointRect)), 2) // MONITOR_DEFAULTTONEAREST
		} else {
			monitor, _, _ = procMonitorFromWindow.Call(uintptr(w.owner.hwnd), 2)
		}
		info := struct {
			Size          uint32
			Monitor, Work win32Rect
			Flags         uint32
		}{}
		info.Size = uint32(unsafe.Sizeof(info))
		if r, _, _ := procGetMonitorInfoW.Call(monitor, uintptr(unsafe.Pointer(&info))); r != 0 {
			x = max(info.Work.Left, min(x, info.Work.Right-width))
			y = max(info.Work.Top, min(y, info.Work.Bottom-height))
		}
	}
	procSetWindowPos.Call(uintptr(w.hwnd), 0, uintptr(x), uintptr(y), 0, 0, swpNozorder|swpNoactivate|1)
}
