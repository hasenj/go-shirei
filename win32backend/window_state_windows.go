package win32backend

import (
	"syscall"
	"unsafe"

	"go.hasen.dev/shirei"
)

type windowState struct {
	ui                     *shirei.UI
	unsubscribe            func()
	owner                  *windowState
	popupID                shirei.PopupWindow
	onClose                func()
	ready, closing, closed bool
	access                 *windowAccess

	winTitle         string
	winW             int
	winH             int
	winTransparent   bool
	winUndecorated   bool
	winFloating      bool
	winFocus         bool
	winTopLevel      bool
	winNonactivating bool
	winExactPosition bool
	frameFn          shirei.FrameFn

	hwnd syscall.Handle

	// DIB present surface: top-down 32bpp BGRA selected into a memory DC.
	// SoftRenderer writes dibBuf; presentDC sends it to the window.
	memDC    syscall.Handle
	dibBM    syscall.Handle
	dibOldBM syscall.Handle
	dibBuf   []byte
	dibW     int
	dibH     int
	dibBits  unsafe.Pointer

	softRenderer shirei.SoftRenderer

	dirty      bool // a new frame must be produced before the next present
	haveFrame  bool
	wantsFrame bool // last frame asked to be re-run (animation/async work)
	timerOn    bool

	// Content-hash present skip (mirrors cocoabackend / androidbackend).
	lastPresentedHash uint64
	havePresented     bool
	presentW          int
	presentH          int

	pendingHi   uint16 // pending UTF-16 high surrogate from WM_CHAR
	pendingText string // committed text to deliver on the next frame

	layeredDC, layeredBM, layeredOldBM uintptr
	layeredPixels                      []byte
	layeredW, layeredH                 int
	presentingLayered                  bool
}

var mainWindow = newWindowState(shirei.ActiveUI())
var currentWindow = mainWindow
var windowsByHandle = map[uintptr]*windowState{}
var creatingWindow *windowState
var callbackDepth, framesInProgress int
var winQuiet bool
var winBackground bool
var hinstance syscall.Handle
var wndProcCB = syscall.NewCallback(wndProc)

func newWindowState(ui *shirei.UI) *windowState {
	return &windowState{ui: ui, dirty: true, access: newWindowAccess()}
}

// updateWindowPosition records the window frame's top-left corner in screen
// logical points, with the origin at the top-left of the primary display (the
// same space as PopupWindowOptions.ScreenPosition). Call on the UI thread with
// the window's frame lock taken.
func updateWindowPosition(w *windowState) {
	if w == nil || w.closed || w.hwnd == 0 {
		return
	}
	var bounds win32Rect
	if r, _, _ := procGetWindowRect.Call(uintptr(w.hwnd), uintptr(unsafe.Pointer(&bounds))); r == 0 {
		return
	}
	scale := dpiScale()
	if scale <= 0 {
		scale = 1
	}
	w.ui.Host.WindowPosition = shirei.Vec2{
		float32(bounds.Left) / scale,
		float32(bounds.Top) / scale,
	}
	w.ui.Host.WindowPositionKnown = true
}

// Native callbacks can reenter while Windows processes another window's event.
// Only the outermost callback takes the frame lock.
func enterWindow(w *windowState) func() {
	previous := currentWindow
	var restore func()
	if callbackDepth == 0 {
		restore = shirei.EnterUI(w.ui)
	} else {
		restore = shirei.EnterUILocked(w.ui)
	}
	currentWindow = w
	callbackDepth++
	return func() { callbackDepth--; currentWindow = previous; restore() }
}
