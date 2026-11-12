//go:build linux

package waylandbackend

import (
	"go.hasen.dev/shirei"
	"go.hasen.dev/shirei/internal/wayland/wlclient"
)

var background bool
var mainHideRequested bool

// SetupBackground keeps the event loop and main UI running after its window
// closes. Activate maps the main window again. Call before Run.
func SetupBackground() { background = true }

func hideMainWindow() {
	w := mainWindow
	if w.closed || w.surface == nil || (w.hidden && !w.surfaceNeedsCreate) {
		return
	}
	w.hidden = true
	if w.pointerPrimaryDown {
		w.pointerPrimaryDown = false
		shirei.GetFrameInput().Mouse = shirei.MouseRelease
	}
	if keyboardWindow == w {
		keyboardWindow = nil
	}
	if pointerWindow == w {
		pointerWindow = nil
	}
	if activationSource == w {
		activationSource = nil
	}
	if repeatKey.window == w {
		repeatKey.window = nil
	}
	if imeWindow == w {
		imeWindow = nil
		if textInput != nil {
			_ = textInput.Disable()
			_ = textInput.Commit()
			textInputSerial++
		}
	}
	w.textInputEnabled, w.textInputOnSurface = false, false
	w.haveLastCursor = false
	resetPendingIME()
	w.ui.Host.Input.DownKeys = nil
	w.ui.Host.Input.Modifiers = 0
	w.ui.Host.Input.Composition = ""
	w.ui.Host.Input.CompositionSel = [2]int{}
	w.ui.Host.Input.MousePoint = shirei.Vec2{-1, -1}
	wlDebug("hide main %q", w.winTitle)
	if w.frameCb != nil {
		wlclient.CallbackDestroy(w.frameCb)
		w.frameCb = nil
	}
	if w.xdgToplevel != nil {
		w.surfaceNeedsCreate = true
		if w.decoration != nil {
			w.decoration.window = &windowState{closed: true}
			_ = w.decoration.Context().SendRequest(w.decoration, 0)
			w.decoration = nil
		}
		if w.icon != nil {
			_ = w.icon.Context().SendRequest(w.icon, 0)
			w.icon = nil
		}
		w.iconBuffer.destroy()
		_ = w.xdgToplevel.Destroy()
		_ = w.xdgSurface.Destroy()
		w.xdgToplevel, w.xdgSurface = nil, nil
	}
	if w.surfaceNeedsCreate {
		destroySurfaceScale(w)
		// A fresh role supplies the initial configure handshake when restoring.
		if w.layerSurface != nil {
			_ = w.layerSurface.Destroy()
		}
		_ = w.surface.Destroy()
		delete(windowsBySurface, w.surface)
		w.handler.window = nil
		w.handler = &handler{window: w}
		w.layerSurface, w.surface = nil, nil
	} else {
		_ = w.surface.Attach(nil, 0, 0)
		_ = w.surface.Commit()
	}
	w.havePresented = false
	w.ui.Host.WindowFocused = false
	w.dirty = true
}

// A hidden owner still processes application work and queued UI requests. It
// does not attach buffers or ask the compositor for presentation callbacks.
func drawHiddenFrame() {
	w := currentWindow
	w.dirty = false
	shirei.GetHost().WindowSize = shirei.Vec2{float32(w.logicalW), float32(w.logicalH)}
	flushPendingText()
	out := shirei.RunFrameFnLocked(w.frameFn)
	if w.csdEnabled {
		shirei.GetHost().WindowSize[1] = float32(w.logicalH - titlebarHeight)
	}
	w.wantsFrame = out.NextFrameRequested
	w.dirty = w.wantsFrame
}
