//go:build linux || (darwin && x11darwin)

package x11backend

import (
	"fmt"
	"math"
	"sync"
	"sync/atomic"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
	g "go.hasen.dev/generic"
	"go.hasen.dev/shirei"
)

var uiThreadID atomic.Int64
var popupWindows = map[shirei.PopupWindow]*windowState{}
var nextPopupID shirei.PopupWindow
var mainCloseRequested bool
var closeQueue struct {
	sync.Mutex
	ids []shirei.PopupWindow
}

// SetupTransparent preserves per-pixel alpha. Requires an X11 compositing
// manager for desktop transparency. Call before Run.
func SetupTransparent() { mainWindow.ui.Host.TransparentBackground = true }

// SetupUndecorated requests a window without title bar or buttons. Call before Run.
func SetupUndecorated() { mainWindow.undecorated = true }

// OpenPopupWindow opens an owned auxiliary window with independent UI state.
func OpenPopupWindow(options shirei.PopupWindowOptions, root shirei.FrameFn) (shirei.PopupWindow, error) {
	if uiThreadID.Load() == 0 || uiThreadID.Load() != threadID() {
		return 0, fmt.Errorf("OpenPopupWindow must run on the UI thread after Run starts")
	}
	owner := currentWindow
	if owner.closed || mainCloseRequested {
		return 0, fmt.Errorf("OpenPopupWindow requires a running owner window")
	}
	if root == nil || options.Width <= 0 || options.Height <= 0 {
		return 0, fmt.Errorf("OpenPopupWindow requires a root view and positive content dimensions")
	}
	if p := options.Position; p != nil && (math.IsNaN(float64(p[0])) || math.IsNaN(float64(p[1])) || math.IsInf(float64(p[0]), 0) || math.IsInf(float64(p[1]), 0)) {
		return 0, fmt.Errorf("OpenPopupWindow position must be finite")
	}
	if options.Transparent && argbVisual == 0 {
		return 0, fmt.Errorf("X11 ARGB visual unavailable")
	}
	w := &windowState{ui: shirei.NewUI(), owner: owner, frameFn: root, winTitle: options.Title, winW: options.Width, winH: options.Height, windowScale: owner.windowScale, undecorated: options.Undecorated, dirty: true, onClose: options.OnClose}
	w.ui.Host.WindowSize = shirei.Vec2{float32(options.Width), float32(options.Height)}
	w.ui.Host.WindowScale = w.windowScale
	w.ui.Host.GlyphCacheBudgetBytes = glyphCacheBudget
	w.ui.Host.TransparentBackground = options.Transparent
	w.ui.Host.EscapeHatchBackendContext = Context{window: w}
	leave := enterWindow(w)
	err := createWindow()
	leave()
	if err != nil {
		return 0, err
	}
	leave = enterWindow(w)
	imeInit()
	leave()
	nextPopupID++
	w.popupID = nextPopupID
	popupWindows[w.popupID] = w
	windowsByID[w.win] = w
	w.unsubscribe = shirei.RegisterWindowUI(w.ui)
	data := make([]byte, 4)
	xgb.Put32(data, uint32(owner.win))
	xproto.ChangeProperty(X, xproto.PropModeReplace, w.win, xproto.AtomWmTransientFor, xproto.AtomWindow, 32, 1, data)
	placePopupWindow(w, options.Position)
	return w.popupID, nil
}

// ClosePopupWindow queues disposal on the UI thread.
func ClosePopupWindow(id shirei.PopupWindow) {
	if id == 0 || uiThreadID.Load() == 0 {
		return
	}
	closeQueue.Lock()
	closeQueue.ids = append(closeQueue.ids, id)
	closeQueue.Unlock()
}

func disposePopup(w *windowState, callbacks *[]func()) {
	if w.closed {
		return
	}
	w.closed = true
	for _, child := range popupWindows {
		if child.owner == w {
			disposePopup(child, callbacks)
		}
	}
	if focusedWindow == w {
		leave := enterWindow(w)
		imeFocusOut()
		leave()
		focusedWindow = nil
	}
	delete(windowsByID, w.win)
	delete(popupWindows, w.popupID)
	leave := enterWindow(w)
	releaseShm()
	imeClose()
	leave()
	xproto.FreeGC(X, w.gc)
	xproto.DestroyWindow(X, w.win)
	xproto.FreeColormap(X, w.colormap)
	w.unsubscribe()
	if fn := w.onClose; fn != nil {
		owner := w.owner
		*callbacks = append(*callbacks, func() {
			if owner.closed {
				owner = mainWindow
			}
			leave := enterWindow(owner)
			defer leave()
			fn()
			owner.ui.RequestFrame()
		})
	}
	*w = windowState{closed: true}
}

func updatePopupWindows() {
	for {
		closeQueue.Lock()
		ids := closeQueue.ids
		closeQueue.ids = nil
		closeQueue.Unlock()
		var callbacks []func()
		if mainCloseRequested {
			for _, w := range popupWindows {
				disposePopup(w, &callbacks)
			}
		}
		for _, id := range ids {
			if w := popupWindows[id]; w != nil {
				disposePopup(w, &callbacks)
			}
		}
		for _, cb := range callbacks {
			cb()
		}
		closeQueue.Lock()
		more := len(closeQueue.ids) > 0
		closeQueue.Unlock()
		if !more && !(mainCloseRequested && len(popupWindows) > 0) {
			break
		}
	}
	if mainCloseRequested {
		uiThreadID.Store(0)
		releaseShm()
		imeClose()
		X.Close()
		g.ExitWithCleanup(0)
	}
	for _, w := range popupWindows {
		if !w.mapped {
			xproto.MapWindow(X, w.win)
			w.mapped = true
		}
	}
	X.Sync()
}

func dispatchEvent(ev xgb.Event) {
	var id xproto.Window
	switch e := ev.(type) {
	case xproto.ExposeEvent:
		id = e.Window
	case xproto.ConfigureNotifyEvent:
		id = e.Window
	case xproto.DestroyNotifyEvent:
		id = e.Window
	case xproto.ClientMessageEvent:
		id = e.Window
	case xproto.MotionNotifyEvent:
		id = e.Event
	case xproto.ButtonPressEvent:
		id = e.Event
	case xproto.ButtonReleaseEvent:
		id = e.Event
	case xproto.KeyPressEvent:
		id = e.Event
	case xproto.KeyReleaseEvent:
		id = e.Event
	case xproto.FocusInEvent:
		id = e.Event
	case xproto.FocusOutEvent:
		id = e.Event
	case xproto.SelectionRequestEvent:
		id = e.Owner
	case xproto.SelectionNotifyEvent:
		id = e.Requestor
	case xproto.SelectionClearEvent:
		id = e.Owner
	}
	w := windowsByID[id]
	if w == nil {
		return
	}
	leave := enterWindow(w)
	defer leave()
	w.dirty = handleEvent(ev) || w.dirty
}
