//go:build linux

package waylandbackend

import (
	"fmt"
	"math"
	"sync"

	"go.hasen.dev/shirei"
	"go.hasen.dev/shirei/internal/dragtrace"
	"go.hasen.dev/shirei/internal/wayland/layershell"
	"go.hasen.dev/shirei/internal/wayland/wlclient"
)

var popupMoveQueue struct {
	sync.Mutex
	positions map[shirei.PopupWindow]shirei.Vec2
	windows   map[*windowState]*shirei.Vec2
}

func validPosition(p *shirei.Vec2) bool {
	if p == nil {
		return true
	}
	for _, v := range p {
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) || float64(v) < math.MinInt32/2 || float64(v) > math.MaxInt32/2 {
			return false
		}
	}
	return true
}

// MovePopupWindow queues screen-coordinate placement for a layer popup. The
// window selects the containing or nearest output. Ordinary toplevels ignore placement.
// Safe from any goroutine. Nil and invalid positions are ignored.
func MovePopupWindow(id shirei.PopupWindow, position *shirei.Vec2) {
	if id == 0 || position == nil || !validPosition(position) || uiThreadID.Load() == 0 {
		return
	}
	popupMoveQueue.Lock()
	if popupMoveQueue.positions == nil {
		popupMoveQueue.positions = make(map[shirei.PopupWindow]shirei.Vec2)
	}
	dragtrace.Logf("wl.position.popup.queue", "popup=%d requested=%v", id, *position)
	popupMoveQueue.positions[id] = *position
	popupMoveQueue.Unlock()
}

// Position queues logical desktop coordinates for a layer window.
func (c Context) Position(x, y int) {
	p := shirei.Vec2{float32(x), float32(y)}
	if !validPosition(&p) {
		return
	}
	c.queuePlacement(&p)
}

// Center queues compositor-centered placement for a layer window.
func (c Context) Center() { c.queuePlacement(nil) }

func (c Context) queuePlacement(p *shirei.Vec2) {
	if c.window == nil {
		return
	}
	popupMoveQueue.Lock()
	if popupMoveQueue.windows == nil {
		popupMoveQueue.windows = make(map[*windowState]*shirei.Vec2)
	}
	dragtrace.Logf("wl.position.queue", "window=%p requested=%v", c.window, p)
	popupMoveQueue.windows[c.window] = p
	popupMoveQueue.Unlock()
}
func applyPlacementRequests() {
	popupMoveQueue.Lock()
	positions, windows := popupMoveQueue.positions, popupMoveQueue.windows
	popupMoveQueue.positions, popupMoveQueue.windows = nil, nil
	popupMoveQueue.Unlock()
	if windows == nil {
		windows = make(map[*windowState]*shirei.Vec2)
	}
	for id, p := range positions {
		if w := popupWindows[id]; w != nil {
			windows[w] = &p
		}
	}
	for _, w := range liveWindows() {
		if w.outputMigration && !w.closed && !w.closing && len(outputs) > 0 {
			windows[w] = w.position
		}
		if w.deferredLayerMove && !w.pointerPrimaryDown && !w.closed && !w.closing {
			if _, requested := windows[w]; !requested {
				windows[w] = w.position
			}
		}
	}
	for w, p := range windows {
		if w.closed || w.closing || w.layerSurface == nil {
			continue
		}
		tracePointer(w, "wl.position.apply", "next=%v", p)
		w.position = p
		w.positionFromOwner = false
		oldOutput := w.output
		chooseLayerOutput(w)
		w.deferredLayerMove = false
		// Replacing a surface cannot transfer its implicit pointer grab. Keep
		// the current role through a drag, then migrate on primary release.
		if oldOutput != w.output && w.pointerPrimaryDown && !w.outputMigration {
			w.output = oldOutput
			w.deferredLayerMove = true
		}
		if oldOutput != w.output || w.outputMigration {
			w.outputMigration = false
			if err := migrateLayerWindow(w); err != nil {
				panic(fmt.Errorf("waylandbackend: migrate layer: %w", err))
			}
		}
		w.surfaceStateDirty, w.dirty = true, true
	}
}

func chooseLayerOutput(w *windowState) {
	if w.owner != nil && w.output == nil && (w.position == nil || w.positionFromOwner) {
		w.output = w.owner.output
	}
	if w.position == nil || w.positionFromOwner {
		return
	}
	var best *outputState
	distance := math.Inf(1)
	for _, o := range outputs {
		if o.width <= 0 || o.height <= 0 {
			continue
		}
		x, y := float64(w.position[0]), float64(w.position[1])
		inside := x >= float64(o.x) && x < float64(o.x)+float64(o.width) && y >= float64(o.y) && y < float64(o.y)+float64(o.height)
		dx := x - max(float64(o.x), min(x, float64(o.x)+float64(o.width)))
		dy := y - max(float64(o.y), min(y, float64(o.y)+float64(o.height)))
		d := dx*dx + dy*dy
		if inside {
			d = -1
		}
		if d < distance || (d == distance && best != nil && (o.proxy == w.output || (best.proxy != w.output && o.name < best.name))) {
			best, distance = o, d
		}
	}
	if best != nil {
		w.output = best.proxy
	}
}

// A layer role fixes its output for its lifetime. Native objects are replaced
// while the UI, popup identifier, owner and close callback retain their lifetime.
func migrateLayerWindow(w *windowState) error {
	leave := enterWindow(w)
	defer leave()
	if w.pointerPrimaryDown {
		// Output removal can end a grab without a physical release event.
		w.pointerPrimaryDown = false
		shirei.GetFrameInput().Mouse = shirei.MouseRelease
	}
	if w.frameCb != nil {
		wlclient.CallbackDestroy(w.frameCb)
		w.frameCb = nil
	}
	w.handler.window = nil
	w.handler = &handler{window: w}
	destroySurfaceScale(w)
	if w.layerSurface != nil {
		_ = w.layerSurface.Destroy()
	}
	if w.surface != nil {
		delete(windowsBySurface, w.surface)
		_ = w.surface.Destroy()
	}
	w.layerSurface, w.surface = nil, nil
	w.havePresented, w.hasAck = false, false
	w.surfaceStateDirty = false
	w.pointerOriginKnown = false
	w.textInputOnSurface, w.textInputEnabled, w.haveLastCursor = false, false, false
	if keyboardWindow == w {
		keyboardWindow = nil
		w.ui.Host.Input.DownKeys = nil
		w.ui.Host.Input.Modifiers = 0
	}
	if imeWindow == w {
		imeWindow = nil
		if textInput != nil {
			_ = textInput.Disable()
			_ = textInput.Commit()
			textInputSerial++
		}
	}
	resetPendingIME()
	w.ui.Host.Input.Composition = ""
	w.ui.Host.Input.CompositionSel = [2]int{}
	w.ui.Host.WindowFocused = false
	if pointerWindow == w {
		pointerWindow = nil
	}
	if repeatKey.window == w {
		repeatKey.window = nil
	}
	if activationSource == w {
		activationSource = nil
	}
	w.surfaceNeedsCreate = true
	w.dirty = true
	if w.hidden {
		return nil
	}
	if err := createWindow(); err != nil {
		return err
	}
	w.surfaceNeedsCreate = false
	return nil
}

// Layer placement uses output-local top/left anchors. Positioned surfaces use
// the full output rather than the exclusive-zone work area so overlay pixels
// and logical desktop coordinates share an origin.
func applyLayerPlacement(w *windowState) error {
	if w.position == nil {
		if err := w.layerSurface.SetAnchor(0); err != nil {
			return err
		}
		if err := w.layerSurface.SetMargin(0, 0, 0, 0); err != nil {
			return err
		}
		return w.layerSurface.SetExclusiveZone(0)
	}
	x, y := layerCoordinates(w)
	if err := w.layerSurface.SetAnchor(layershell.AnchorTopLeft); err != nil {
		return err
	}
	if err := w.layerSurface.SetMargin(int32(max(math.MinInt32, min(math.MaxInt32, math.Round(y)))), 0, 0, int32(max(math.MinInt32, min(math.MaxInt32, math.Round(x))))); err != nil {
		return err
	}
	return w.layerSurface.SetExclusiveZone(-1)
}

func layerHeight(w *windowState) int {
	if w.csdEnabled {
		return w.winH + titlebarHeight
	}
	return w.winH
}

// SetupOverlay selects a borderless top-layer window anchored at the desktop
// origin. Call before Run, together with SetupWindow and SetupTransparent.
func SetupOverlay() {
	mainWindow.floating = true
	mainWindow.exactPosition = true
	mainWindow.position = &shirei.Vec2{0, 0}
}

func layerCoordinates(w *windowState) (float64, float64) {
	x, y := float64(w.position[0]), float64(w.position[1])
	if o := outputFor(w.output); o != nil {
		x, y = x-float64(o.x), y-float64(o.y)
		if !w.exactPosition && o.width > 0 && o.height > 0 {
			x = max(0, min(x, float64(max(0, int(o.width)-w.winW))))
			y = max(0, min(y, float64(max(0, int(o.height)-layerHeight(w)))))
		}
	}
	return x, y
}
