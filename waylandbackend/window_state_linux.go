//go:build linux

package waylandbackend

import (
	"go.hasen.dev/shirei"
	"go.hasen.dev/shirei/internal/atspi"
	"go.hasen.dev/shirei/internal/wayland/layershell"
	"go.hasen.dev/shirei/internal/wayland/wl"
	zxdg "go.hasen.dev/shirei/internal/wayland/xdg"
)

type windowState struct {
	ui                        *shirei.UI
	handler                   *handler
	unsubscribe               func()
	owner                     *windowState
	popupID                   shirei.PopupWindow
	onClose                   func()
	ready, closing, closed    bool
	hidden                    bool
	surfaceNeedsCreate        bool
	positionFromOwner         bool
	topLevel, floating, focus bool
	activateAfterPresent      bool

	pointerTravel                                shirei.Vec2
	pointerHasRelative                           bool
	pointerPrimaryDown, deferredLayerMove        bool
	pointerOrigin, pointerAbsolute, pointerPress shirei.Vec2
	pointerOriginKnown, pointerAbsoluteKnown     bool
	position                                     *shirei.Vec2
	exactPosition                                bool
	winTitle                                     string
	winW, winH                                   int
	frameFn                                      shirei.FrameFn
	accessBridge                                 *atspi.Bridge
	surface                                      *wl.Surface
	layerSurface                                 *layershell.Surface
	surfaceStateDirty                            bool
	minW, minH                                   int
	output                                       *wl.Output
	enteredOutputs                               map[*wl.Output]bool
	outputMigration                              bool
	preferredBufferScale                         int32
	fractionalScale                              *surfaceFractionalScale
	viewport                                     *surfaceViewport
	xdgSurface                                   *zxdg.Surface
	xdgToplevel                                  *zxdg.Toplevel
	decoration                                   *toplevelDecoration
	icon                                         *toplevelIcon
	iconBuffer                                   wlBuffer
	logicalW, logicalH                           int
	pendingW, pendingH                           int
	toplevelStates, pendingToplevelStates        uint32
	pendingToplevelConfigure, clientSizePending  bool
	minimumSizePending                           bool
	curW, curH                                   int
	windowScale                                  float32
	ackSerial                                    uint32
	hasAck                                       bool
	buffers                                      [2]wlBuffer
	frameCb                                      *wl.Callback
	waitConfigure, wantsFrame, dirty             bool
	softRenderer                                 shirei.SoftRenderer
	havePresented                                bool
	lastPresentedHash                            uint64
	presentW, presentH                           int
	gpuOK                                        bool
	gpuSlots                                     [gpuPoolSize]gpuSlot
	gpuModifiers                                 []uint64
	csdEnabled, undecorated                      bool
	pendingDecorationMode                        uint32

	textInputOnSurface, textInputEnabled               bool
	pendingText                                        string
	pendingPreedit                                     string
	pendingPreeditBegin, pendingPreeditEnd             int32
	pendingCommit                                      string
	gotPreedit, gotCommit                              bool
	lastCursorX, lastCursorY, lastCursorW, lastCursorH int32
	haveLastCursor                                     bool
	pendingPaste                                       string
	hasPendingPaste                                    bool
}

var mainWindow = newWindowState(shirei.ActiveUI())
var currentWindow = mainWindow
var windowsBySurface = map[*wl.Surface]*windowState{}
var callbackDepth, framesInProgress int
var pointerWindow, keyboardWindow, imeWindow *windowState

func newWindowState(ui *shirei.UI) *windowState {
	w := &windowState{ui: ui, windowScale: 1, csdEnabled: true, dirty: true}
	w.handler = &handler{window: w}
	return w
}

// enterWindow serializes window input and rendering with shared UI resources.
// Nested roundtrips can dispatch another window's native callbacks under this lock.
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

func liveWindows() []*windowState {
	windows := []*windowState{mainWindow}
	for _, w := range popupWindows {
		windows = append(windows, w)
	}
	return windows
}

// DMA-BUF create returns server-allocated IDs without a client-ID deletion
// acknowledgement. Drop their listeners on destroy; the display dispatcher
// consumes any release events already in flight.
func destroyWindowBuffer(buffer *wl.Buffer) {
	_ = buffer.Destroy()
	if uint32(buffer.Id()) >= 0xff000000 {
		buffer.Unregister()
	}
}
