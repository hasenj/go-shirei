//go:build linux

package waylandbackend

import (
	"math"
	"sync"

	zxdg "go.hasen.dev/shirei/internal/wayland/xdg"
)

const toplevelSizeConstraints = 1<<zxdg.ToplevelStateMaximized | 1<<zxdg.ToplevelStateFullscreen |
	1<<zxdg.ToplevelStateResizing | 1<<zxdg.ToplevelStateTiledLeft | 1<<zxdg.ToplevelStateTiledRight |
	1<<zxdg.ToplevelStateTiledTop | 1<<zxdg.ToplevelStateTiledBottom

type windowSizeRequest struct {
	window        *windowState
	width, height int
	minimum       bool
}

var windowSizeQueue struct {
	sync.Mutex
	requests []windowSizeRequest
}

// SetSize queues a logical content-size request. Ordinary toplevels discard it
// while the compositor controls their size. A discarded request is not retried.
// Safe from any goroutine, including before the window's first frame.
func (c Context) SetSize(width, height float32) { c.queueSize(width, height, false) }

// SetMinSize queues minimum content dimensions. Layer windows clamp subsequent
// size requests; toplevel windows send the compositor a minimum-size hint.
// Safe from any goroutine.
func (c Context) SetMinSize(width, height float32) { c.queueSize(width, height, true) }

func (c Context) queueSize(width, height float32, minimum bool) {
	if c.window == nil || math.IsNaN(float64(width)) || math.IsNaN(float64(height)) || math.IsInf(float64(width), 0) || math.IsInf(float64(height), 0) || width < 0 || height < 0 || float64(width) > math.MaxInt32 || float64(height) > math.MaxInt32 {
		return
	}
	windowSizeQueue.Lock()
	windowSizeQueue.requests = append(windowSizeQueue.requests, windowSizeRequest{c.window, int(width), int(height), minimum})
	windowSizeQueue.Unlock()
}

func applyWindowSizeRequests() {
	windowSizeQueue.Lock()
	requests := windowSizeQueue.requests
	windowSizeQueue.requests = nil
	windowSizeQueue.Unlock()
	for _, r := range requests {
		w := r.window
		if w.closed || w.closing {
			continue
		}
		layer := w.layerSurface != nil || (layerShell != nil && (w.floating || (w.undecorated && !w.topLevel)))
		if !layer {
			if r.minimum {
				w.minW, w.minH = r.width, r.height
				setToplevelMinimum(w)
				w.minimumSizePending = true
			} else {
				w.winW, w.winH = r.width, r.height
				w.clientSizePending = true
			}
			discardConstrainedSize(w)
			w.surfaceStateDirty, w.dirty = true, true
			continue
		}
		width, height := r.width, r.height
		if r.minimum {
			w.minW, w.minH = width, height
			width, height = w.winW, w.winH
		}
		width, height = max(1, w.minW, width), max(1, w.minH, height)
		if width == w.winW && height == w.winH {
			continue
		}
		w.winW, w.winH = width, height
		w.surfaceStateDirty, w.dirty = true, true
	}
}

func setToplevelMinimum(w *windowState) {
	if w.xdgToplevel == nil {
		return
	}
	height := w.minH
	if height > 0 && w.csdEnabled {
		height += titlebarHeight
	}
	_ = w.xdgToplevel.SetMinSize(int32(w.minW), int32(height))
}

// Compositor-controlled dimensions consume one-shot size requests. Minimum
// hints remain native window properties, independent of immediate resizing.
func discardConstrainedSize(w *windowState) {
	if w.toplevelStates&toplevelSizeConstraints == 0 {
		return
	}
	if w.clientSizePending {
		wlDebug("client size ignored for %q: compositor controls size", w.winTitle)
	}
	w.clientSizePending, w.minimumSizePending = false, false
	w.winW, w.winH = w.logicalW, w.logicalH
	if w.csdEnabled {
		w.winH -= titlebarHeight
	}
}

// Configure state and size become effective together at xdg_surface.configure.
func applyToplevelSize(w *windowState) {
	if (!w.clientSizePending && !w.minimumSizePending) || w.xdgToplevel == nil || w.waitConfigure || w.pendingToplevelConfigure {
		return
	}
	if !w.clientSizePending {
		w.winW, w.winH = w.logicalW, w.logicalH
		if w.csdEnabled {
			w.winH -= titlebarHeight
		}
	}
	w.winW, w.winH = max(1, w.minW, w.winW), max(1, w.minH, w.winH)
	w.logicalW, w.logicalH = w.winW, w.winH
	if w.csdEnabled {
		w.logicalH += titlebarHeight
	}
	w.clientSizePending, w.minimumSizePending = false, false
	recomputeDeviceSize()
	wlDebug("client size %q: %dx%d", w.winTitle, w.logicalW, w.logicalH)
}

// Commit pending window properties with the frame's surface state, after any
// configure acknowledgement.
func commitWindowSurface() error {
	w := currentWindow
	if compositorVer >= 3 {
		scale := int32(w.windowScale)
		if w.viewport != nil {
			scale = 1
		}
		if err := w.surface.SetBufferScale(max(1, scale)); err != nil {
			return err
		}
	}
	if w.viewport != nil && w.logicalW > 0 && w.logicalH > 0 {
		if err := w.viewport.Context().SendRequest(w.viewport, 2, int32(w.logicalW), int32(w.logicalH)); err != nil {
			return err
		}
	}
	if w.layerSurface != nil && w.surfaceStateDirty {
		if err := w.layerSurface.SetSize(uint32(w.winW), uint32(layerHeight(w))); err != nil {
			return err
		}
	}
	if w.layerSurface != nil && w.surfaceStateDirty {
		if err := applyLayerPlacement(w); err != nil {
			return err
		}
	}
	var err error
	if w.layerSurface != nil {
		err = commitPositionedSurface(w)
	} else {
		err = w.surface.Commit()
	}
	if err == nil {
		w.surfaceStateDirty = false
	}
	return err
}
