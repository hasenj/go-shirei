//go:build linux

package waylandbackend

import (
	"testing"

	"go.hasen.dev/shirei"
	"go.hasen.dev/shirei/internal/wayland/layershell"
	"go.hasen.dev/shirei/internal/wayland/textinput"
	"go.hasen.dev/shirei/internal/wayland/wl"
	zxdg "go.hasen.dev/shirei/internal/wayland/xdg"
)

func TestWindowInputAndConfigureRouting(t *testing.T) {
	oldSurfaces, oldPointer, oldKeyboard, oldIME := windowsBySurface, pointerWindow, keyboardWindow, imeWindow
	oldMask, oldTextInput := seatModifierMask, textInput
	t.Cleanup(func() {
		windowsBySurface, pointerWindow, keyboardWindow, imeWindow = oldSurfaces, oldPointer, oldKeyboard, oldIME
		seatModifierMask, textInput = oldMask, oldTextInput
	})
	textInput = nil
	a, b := newWindowState(shirei.NewUI()), newWindowState(shirei.NewUI())
	a.surface, b.surface = &wl.Surface{}, &wl.Surface{}
	a.ui.Host.WindowFocused, b.ui.Host.WindowFocused = false, false
	windowsBySurface = map[*wl.Surface]*windowState{a.surface: a, b.surface: b}
	seatModifierMask = wlModCtrl
	h.HandleKeyboardEnter(wl.KeyboardEnterEvent{Surface: a.surface})
	h.HandlePointerEnter(wl.PointerEnterEvent{Surface: b.surface, SurfaceX: 20, SurfaceY: 30})
	h.HandlePointerMotion(wl.PointerMotionEvent{SurfaceX: 40, SurfaceY: 50})
	if !a.ui.Host.WindowFocused || b.ui.Host.WindowFocused || a.ui.Host.Input.Modifiers != shirei.ModCtrl || b.ui.Host.Input.MousePoint != (shirei.Vec2{40, 50}) {
		t.Fatal("keyboard and pointer focus do not target separate windows")
	}
	h.HandleKeyboardLeave(wl.KeyboardLeaveEvent{Surface: a.surface})
	h.HandleKeyboardEnter(wl.KeyboardEnterEvent{Surface: b.surface})
	// A delayed leave from another surface must not clear the new focus target.
	h.HandleKeyboardLeave(wl.KeyboardLeaveEvent{Surface: a.surface})
	if keyboardWindow != b || a.ui.Host.WindowFocused || !b.ui.Host.WindowFocused || a.ui.Host.Input.Modifiers != 0 {
		t.Fatal("keyboard focus leaks across windows")
	}

	h.HandleTextInputEnter(textinput.EnterEvent{Surface: a.surface})
	h.HandleTextInputPreeditString(textinput.PreeditStringEvent{Text: "日本", CursorBegin: 3, CursorEnd: 6})
	h.HandleTextInputDone(textinput.DoneEvent{})
	h.HandleTextInputEnter(textinput.EnterEvent{Surface: b.surface})
	h.HandleTextInputCommitString(textinput.CommitStringEvent{Text: "panel B"})
	h.HandleTextInputDone(textinput.DoneEvent{})
	if a.ui.Host.Input.Composition != "日本" || a.ui.Host.Input.CompositionSel != ([2]int{1, 2}) || b.ui.Host.Input.Composition != "" || b.pendingText != "panel B" || a.pendingText != "" {
		t.Fatal("composition and committed text cross window boundaries")
	}
	h.HandleTextInputLeave(textinput.LeaveEvent{Surface: a.surface})
	if imeWindow != b || a.ui.Host.Input.Composition != "" {
		t.Fatal("IME leave clears another surface's focus")
	}
	leave := enterWindow(b)
	flushPendingText()
	leave()
	if b.ui.Host.FrameInput.Text != "panel B" || a.ui.Host.FrameInput.Text != "" {
		t.Fatal("pending input is flushed to the wrong frame")
	}

	a.waitConfigure, b.waitConfigure = true, true
	a.winW, a.winH, b.winW, b.winH = 320, 240, 500, 300
	a.logicalW, a.logicalH = a.winW, a.winH
	a.undecorated, a.csdEnabled = true, false
	b.windowScale = 2
	// A roundtrip can deliver B's configure while A's UI owns the frame lock.
	leave = enterWindow(a)
	b.handler.HandleToplevelConfigure(zxdg.ToplevelConfigureEvent{Width: 420, Height: 280})
	b.handler.HandleSurfaceConfigure(zxdg.SurfaceConfigureEvent{Serial: 17})
	restored := currentWindow == a && shirei.ActiveUI() == a.ui
	leave()
	if !restored {
		t.Fatal("nested callback does not restore its owner UI")
	}
	a.handler.HandleSurfaceConfigure(zxdg.SurfaceConfigureEvent{Serial: 18})
	if b.logicalW != 420 || b.logicalH != 280 || b.curW != 840 || b.ackSerial != 17 || a.logicalW != 320 || a.logicalH != 240 || a.ackSerial != 18 {
		t.Fatal("configure size or acknowledgement belongs to another window")
	}
}

func TestPointerHoverFollowsSurfaceFocus(t *testing.T) {
	oldSurfaces, oldPointer := windowsBySurface, pointerWindow
	t.Cleanup(func() { windowsBySurface, pointerWindow = oldSurfaces, oldPointer })
	pointerWindow = nil
	a, b := newWindowState(shirei.NewUI()), newWindowState(shirei.NewUI())
	a.surface, b.surface = &wl.Surface{}, &wl.Surface{}
	a.focus, b.focus = true, false // b is a non-activating window
	windowsBySurface = map[*wl.Surface]*windowState{a.surface: a, b.surface: b}
	ca, cb := Context{window: a}, Context{window: b}
	if ca.PointerInside() || cb.PointerInside() {
		t.Fatal("hover exists before the pointer enters either window")
	}

	h.HandlePointerEnter(wl.PointerEnterEvent{Surface: b.surface, SurfaceX: 12, SurfaceY: 8})
	if !cb.PointerInside() || ca.PointerInside() {
		t.Fatal("non-activating window does not report hover")
	}

	// Entering another window moves hover; a delayed leave from the previous
	// surface must not clear the new one.
	h.HandlePointerEnter(wl.PointerEnterEvent{Surface: a.surface, SurfaceX: 1, SurfaceY: 2})
	h.HandlePointerLeave(wl.PointerLeaveEvent{Surface: b.surface})
	if cb.PointerInside() || !ca.PointerInside() {
		t.Fatal("hover does not follow the entered surface")
	}

	h.HandlePointerLeave(wl.PointerLeaveEvent{Surface: a.surface})
	if ca.PointerInside() {
		t.Fatal("hover survives the pointer leaving the window")
	}

	h.HandlePointerEnter(wl.PointerEnterEvent{Surface: b.surface, SurfaceX: 12, SurfaceY: 8})
	b.hidden = true
	if cb.PointerInside() {
		t.Fatal("closed or hidden window reports hover")
	}
}

func TestClosedWindowIgnoresLateCallbacks(t *testing.T) {
	w := newWindowState(shirei.NewUI())
	listener := w.handler
	context := Context{window: w}
	*w = windowState{closed: true}
	listener.HandleToplevelConfigure(zxdg.ToplevelConfigureEvent{Width: 500, Height: 400})
	listener.HandleSurfaceConfigure(zxdg.SurfaceConfigureEvent{Serial: 5})
	listener.HandleCallbackDone(wl.CallbackDoneEvent{})
	listener.HandleToplevelClose(zxdg.ToplevelCloseEvent{})
	listener.HandleLayerConfigure(layershell.ConfigureEvent{Serial: 6, Width: 400, Height: 300})
	listener.HandleLayerClosed()
	if context.XdgToplevel() != nil || w.pendingW != 0 || w.hasAck || w.dirty {
		t.Fatal("late callbacks revive a closed window")
	}
}

func TestLayerSizeRequestsAndConfigureRouting(t *testing.T) {
	windowSizeQueue.Lock()
	previous := windowSizeQueue.requests
	windowSizeQueue.requests = nil
	windowSizeQueue.Unlock()
	t.Cleanup(func() { windowSizeQueue.Lock(); windowSizeQueue.requests = previous; windowSizeQueue.Unlock() })
	a, b := newWindowState(shirei.NewUI()), newWindowState(shirei.NewUI())
	a.layerSurface, b.layerSurface = &layershell.Surface{}, &layershell.Surface{}
	a.csdEnabled, b.csdEnabled = false, false
	a.winW, a.winH, a.logicalW, a.logicalH = 320, 240, 320, 240
	b.winW, b.winH, b.logicalW, b.logicalH = 500, 300, 500, 300
	a.waitConfigure, b.waitConfigure = true, true
	b.windowScale = 2
	ca, cb := Context{window: a}, Context{window: b}
	ca.SetMinSize(300, 180)
	done := make(chan struct{})
	go func() { ca.SetSize(200, 100); cb.SetSize(700, 500); close(done) }()
	<-done
	applyWindowSizeRequests()
	if a.winW != 300 || a.winH != 180 || b.winW != 700 || b.winH != 500 || !a.surfaceStateDirty || !b.surfaceStateDirty {
		t.Fatal("layer size requests or minimum bounds cross windows")
	}
	if a.logicalW != 320 || b.logicalW != 500 {
		t.Fatal("requested size is applied before configure")
	}
	leave := enterWindow(a)
	b.handler.HandleLayerConfigure(layershell.ConfigureEvent{Serial: 11, Width: 680, Height: 480})
	restored := currentWindow == a && shirei.ActiveUI() == a.ui
	leave()
	a.handler.HandleLayerConfigure(layershell.ConfigureEvent{Serial: 12})
	if !restored || a.logicalW != 300 || a.logicalH != 180 || b.logicalW != 680 || b.curW != 1360 || a.ackSerial != 12 || b.ackSerial != 11 || a.waitConfigure || b.waitConfigure {
		t.Fatal("layer configure/acknowledgement does not belong to its UI")
	}
	stale := Context{window: b}
	*b = windowState{closed: true}
	stale.SetSize(900, 700)
	applyWindowSizeRequests()
	if b.surfaceStateDirty || b.winW != 0 {
		t.Fatal("stale size request revives a closed layer window")
	}
}
