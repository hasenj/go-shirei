//go:build linux || (darwin && x11darwin)

package x11backend

import (
	"github.com/jezek/xgb/shm"
	"github.com/jezek/xgb/xproto"
	"go.hasen.dev/shirei"
)

type windowState struct {
	ui                    *shirei.UI
	owner                 *windowState
	popupID               shirei.PopupWindow
	onClose               func()
	unsubscribe           func()
	dirty, closed, mapped bool

	winTitle                   string
	winW, winH                 int
	winQuiet, undecorated      bool
	frameFn                    shirei.FrameFn
	win                        xproto.Window
	gc                         xproto.Gcontext
	colormap                   xproto.Colormap
	depth                      byte
	curW, curH                 int
	windowScale                float32
	wantsFrame                 bool
	softRenderer               shirei.SoftRenderer
	presentBuf                 []byte
	shmAttached, presentViaShm bool
	shmSeg                     shm.Seg
	shmID                      int
	shmData                    []byte

	ime                       imeState
	pendingPaste, pendingText string
	hasPendingPaste           bool
}

var mainWindow = &windowState{ui: shirei.ActiveUI(), windowScale: 1, dirty: true}
var currentWindow = mainWindow
var focusedWindow *windowState
var windowsByID = map[xproto.Window]*windowState{}
var callbackDepth int

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
	result := []*windowState{mainWindow}
	for _, w := range popupWindows {
		if !w.closed {
			result = append(result, w)
		}
	}
	return result
}
