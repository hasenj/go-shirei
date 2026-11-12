//go:build linux

package waylandbackend

import (
	"go.hasen.dev/shirei/internal/wayland/layershell"
	"go.hasen.dev/shirei/internal/wayland/wl"
)

var layerShell *layershell.Manager

func bindLayerShell(name, version uint32) {
	var err error
	layerShell, err = layershell.Bind(registry, name, version)
	if err != nil {
		perfLog("[wl] layer-shell unavailable: %v", err)
	}
}

func createLayerWindow() error {
	w := currentWindow
	chooseLayerOutput(w)
	var err error
	w.layerSurface, err = layerShell.GetSurface(w.surface, w.output, appID(), w.handler)
	if err != nil {
		return err
	}
	return configureLayerWindow(w)
}

func configureLayerWindow(w *windowState) error {
	w.winW, w.winH = max(1, w.winW), max(1, w.winH)
	w.logicalW, w.logicalH = w.winW, layerHeight(w)
	recomputeDeviceSize()
	if err := w.layerSurface.SetSize(uint32(w.winW), uint32(layerHeight(w))); err != nil {
		return err
	}
	if err := applyLayerPlacement(w); err != nil {
		return err
	}
	if err := w.layerSurface.SetKeyboardInteractivity(layershell.KeyboardOnDemand); err != nil {
		return err
	}
	perfLog("[wl] layer window %q requested %dx%d", w.winTitle, w.winW, w.winH)
	return nil
}

func (h *handler) HandleLayerConfigure(ev layershell.ConfigureEvent) {
	w := h.window
	if w == nil || w.closed {
		return
	}
	leave := enterWindow(w)
	defer leave()
	tracePointer(w, "wl.layer.configure", "serial=%d size=%dx%d", ev.Serial, ev.Width, ev.Height)
	width, height := w.winW, layerHeight(w)
	if ev.Width > 0 {
		width = int(ev.Width)
	}
	if ev.Height > 0 {
		height = int(ev.Height)
	}
	w.logicalW, w.logicalH = width, height
	recomputeDeviceSize()
	w.ackSerial, w.hasAck = ev.Serial, true
	w.waitConfigure = false
	w.dirty = true
}

func (h *handler) HandleLayerClosed() {
	if w := h.window; w != nil && !w.closed {
		if w.outputMigration {
			return
		}
		// Output teardown can send closed before registry.global_remove. Wait
		// for an ordered display marker before treating this as a window close.
		w.waitConfigure = true
		if disp != nil {
			if cb, err := disp.Sync(); err == nil {
				cb.AddDoneHandler(&layerCloseConfirmation{handler: h, surface: w.surface})
				return
			}
		}
		w.surfaceNeedsCreate = true
		requestWindowClose(w)
	}
}

type layerCloseConfirmation struct {
	handler *handler
	surface *wl.Surface
}

func (c *layerCloseConfirmation) HandleCallbackDone(wl.CallbackDoneEvent) {
	if w := c.handler.window; w != nil && !w.closed && w.surface == c.surface && !w.outputMigration {
		w.surfaceNeedsCreate = true
		requestWindowClose(w)
	}
}

func (h *handler) HandleSurfaceEnter(ev wl.SurfaceEnterEvent) {
	if w := h.window; w != nil && !w.closed {
		w.output = ev.Output
		if w.enteredOutputs == nil {
			w.enteredOutputs = make(map[*wl.Output]bool)
		}
		w.enteredOutputs[ev.Output] = true
		updateWindowScale(w)
	}
}
func (h *handler) HandleSurfaceLeave(ev wl.SurfaceLeaveEvent) {
	if w := h.window; w != nil && !w.closed {
		delete(w.enteredOutputs, ev.Output)
		if w.layerSurface == nil && w.output == ev.Output {
			w.output = nil
		}
		updateWindowScale(w)
	}
}
