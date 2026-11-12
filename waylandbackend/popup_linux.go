//go:build linux

package waylandbackend

import (
	"fmt"
	"math"
	"sync"
	"sync/atomic"

	g "go.hasen.dev/generic"
	"go.hasen.dev/shirei"
	"go.hasen.dev/shirei/internal/atspi"
	"golang.org/x/sys/unix"
)

var uiThreadID atomic.Int64
var popupWindows = map[shirei.PopupWindow]*windowState{}
var nextPopupID shirei.PopupWindow
var pendingShows []*windowState
var mainCloseRequested bool
var popupCloseQueue struct {
	sync.Mutex
	ids []shirei.PopupWindow
}

// OpenPopupWindow creates an auxiliary window with independent UI and styles.
// Undecorated windows use layer-shell v4 when available; other windows use
// toplevels, with a native parent when their owner is also a toplevel.
// Mapping and first-frame delivery wait for the current UI callback to finish.
// Toplevel placement and focus follow compositor policy. Layer windows accept
// desktop screen coordinates and movement between outputs. Migration during a
// primary-button drag waits for release.
func OpenPopupWindow(options shirei.PopupWindowOptions, root shirei.FrameFn) (shirei.PopupWindow, error) {
	if id := uiThreadID.Load(); id == 0 || int64(unix.Gettid()) != id {
		return 0, fmt.Errorf("OpenPopupWindow must run on the UI thread after Run starts")
	}
	owner := currentWindow
	if !mainWindow.ready || mainCloseRequested || owner.closed || owner.closing {
		return 0, fmt.Errorf("OpenPopupWindow requires a running owner window")
	}
	if root == nil || options.Width <= 0 || options.Height <= 0 || options.Width > math.MaxInt32 || options.Height > math.MaxInt32-titlebarHeight {
		return 0, fmt.Errorf("OpenPopupWindow requires a root view and positive content dimensions")
	}
	if !validPosition(options.Position) || !validPosition(options.ScreenPosition) {
		return 0, fmt.Errorf("OpenPopupWindow position must be finite and within Wayland coordinate limits")
	}
	if options.Transparent && !hasARGB {
		return 0, fmt.Errorf("Wayland ARGB8888 buffers unavailable")
	}
	w := newWindowState(shirei.NewUI())
	w.owner, w.frameFn = owner, wrapFrame(root)
	w.topLevel, w.floating, w.focus = options.TopLevel, options.Floating, options.Focus
	w.activateAfterPresent = options.Focus
	if options.Nonactivating {
		w.focus = false
		w.activateAfterPresent = false
	}
	w.winTitle, w.winW, w.winH = options.Title, options.Width, options.Height
	w.undecorated, w.csdEnabled = options.Undecorated, !options.Undecorated
	w.exactPosition = options.ExactScreenPosition
	if options.ScreenPosition != nil {
		p := *options.ScreenPosition
		w.position = &p
	} else if options.Position != nil && owner.layerSurface != nil && owner.position != nil {
		x, y := layerCoordinates(owner)
		if o := outputFor(owner.output); o != nil {
			x += float64(o.x)
			y += float64(o.y)
		}
		p := shirei.Vec2{float32(x) + options.Position[0], float32(y) + options.Position[1]}
		w.positionFromOwner = true
		if owner.csdEnabled {
			p[1] += titlebarHeight
		}
		w.position = &p
	}
	w.windowScale = owner.windowScale
	w.ui.Host.WindowSize = shirei.Vec2{float32(options.Width), float32(options.Height)}
	w.ui.Host.GlyphCacheBudgetBytes = glyphCacheBudget
	w.ui.Host.TransparentBackground = options.Transparent
	w.ui.Host.WindowFocused = false
	w.ui.Host.EscapeHatchBackendContext = Context{window: w}
	leave := enterWindow(w)
	defer leave()
	if err := createWindow(); err != nil {
		disposeWindow(w)
		return 0, err
	}
	nextPopupID++
	w.popupID, w.onClose = nextPopupID, options.OnClose
	popupWindows[w.popupID] = w
	w.unsubscribe = shirei.RegisterWindowUI(w.ui)
	pendingShows = append(pendingShows, w)
	return w.popupID, nil
}

// ClosePopupWindow queues a close from any goroutine. The bounded display poll
// drains requests without accessing UI or native objects on the calling thread.
func ClosePopupWindow(id shirei.PopupWindow) {
	if id == 0 || uiThreadID.Load() == 0 {
		return
	}
	popupCloseQueue.Lock()
	popupCloseQueue.ids = append(popupCloseQueue.ids, id)
	popupCloseQueue.Unlock()
}

func requestWindowClose(w *windowState) {
	if w == mainWindow {
		if background {
			mainHideRequested = true
		} else {
			mainCloseRequested = true
		}
	} else {
		ClosePopupWindow(w.popupID)
	}
}

func markPopupClosing(w *windowState) {
	if w.closed || w.closing {
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

func updatePopupWindows() {
	if callbackDepth != 0 || framesInProgress != 0 {
		return
	}
	leave := enterWindow(mainWindow)
	defer leave()
	applyWindowSizeRequests()
	applyPlacementRequests()
	applyActivationRequests()
	if mainHideRequested {
		mainHideRequested = false
		hideMainWindow()
	}
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
		var callbacks []popupClosedCallback
		for _, w := range popupWindows {
			if w.closing {
				disposePopup(w, &callbacks)
			}
		}
		for _, cb := range callbacks {
			owner := cb.owner
			if owner.closed {
				owner = mainWindow
			}
			restore := enterWindow(owner)
			cb.fn()
			owner.ui.RequestFrame()
			restore()
		}
		popupCloseQueue.Lock()
		more := len(popupCloseQueue.ids) > 0
		popupCloseQueue.Unlock()
		if !more && !(mainCloseRequested && len(popupWindows) > 0) {
			break
		}
	}
	if mainCloseRequested {
		uiThreadID.Store(0)
		g.ExitWithCleanup(0)
		return
	}
	shows := pendingShows
	pendingShows = nil
	for _, w := range shows {
		if w.closed || w.closing {
			continue
		}
		restore := enterWindow(w)
		tryInitGPU()
		w.accessBridge = atspi.Start(w.winTitle, w.ui.RequestFrame)
		w.ready = true
		restore()
	}
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
	if w.onClose != nil {
		*callbacks = append(*callbacks, popupClosedCallback{w.owner, w.onClose})
	}
	delete(popupWindows, w.popupID)
	disposeWindow(w)
}

func disposeWindow(w *windowState) {
	leave := enterWindow(w)
	defer leave()
	w.closed, w.ready = true, false
	if pointerWindow == w {
		pointerWindow = nil
	}
	if keyboardWindow == w {
		keyboardWindow = nil
	}
	if imeWindow == w {
		imeWindow = nil
		if textInput != nil {
			_ = textInput.Disable()
			_ = textInput.Commit()
			textInputSerial++
		}
	}
	// In-flight frame callbacks retain the closed marker until delete_id.
	// Their done event can precede the compositor processing surface destruction.
	destroyGPUBuffers()
	for i := range w.buffers {
		w.buffers[i].destroy()
	}
	if w.accessBridge != nil {
		w.accessBridge.Close()
	}
	if w.icon != nil {
		_ = w.icon.Context().SendRequest(w.icon, 0)
	}
	w.iconBuffer.destroy()
	if w.decoration != nil {
		_ = w.decoration.Context().SendRequest(w.decoration, 0)
	}
	if w.layerSurface != nil {
		_ = w.layerSurface.Destroy()
	}
	if w.xdgToplevel != nil {
		_ = w.xdgToplevel.Destroy()
	}
	if w.xdgSurface != nil {
		_ = w.xdgSurface.Destroy()
	}
	if w.surface != nil {
		destroySurfaceScale(w)
		delete(windowsBySurface, w.surface)
		_ = w.surface.Destroy()
	}
	if w.unsubscribe != nil {
		w.unsubscribe()
	}
	*w = windowState{closed: true}
}
