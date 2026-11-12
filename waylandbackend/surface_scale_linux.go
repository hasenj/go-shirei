//go:build linux

package waylandbackend

import "go.hasen.dev/shirei/internal/wayland/wl"

// Version 1 fractional-scale and viewporter objects belong to one native surface.
// Viewport destinations keep logical geometry independent of rounded pixel sizes.
type scaleManager struct{ wl.BaseProxy }

func (*scaleManager) Dispatch(*wl.Event) {}

var fractionalScaleManager, viewportManager *scaleManager

type surfaceViewport struct{ wl.BaseProxy }

func (*surfaceViewport) Dispatch(*wl.Event) {}

type surfaceFractionalScale struct {
	wl.BaseProxy
	handler   *handler
	preferred uint32
}

func (s *surfaceFractionalScale) Dispatch(ev *wl.Event) {
	if ev.Opcode != 0 || s.handler.window == nil || s.handler.window.closed {
		return
	}
	if scale := ev.Uint32(); scale > 0 {
		s.preferred = scale
		updateWindowScale(s.handler.window)
	}
}

func bindScaleManager(name uint32, protocol string) {
	m := &scaleManager{}
	registry.Context().Register(m)
	if err := registry.Bind(name, protocol, 1, m); err != nil {
		m.Unregister()
		return
	}
	if protocol == "wp_viewporter" {
		viewportManager = m
	} else {
		fractionalScaleManager = m
	}
}

func createSurfaceScale(w *windowState) error {
	w.preferredBufferScale = 0
	w.enteredOutputs = make(map[*wl.Output]bool)
	if fractionalScaleManager != nil && viewportManager != nil {
		v := &surfaceViewport{}
		viewportManager.Context().Register(v)
		if err := viewportManager.Context().SendRequest(viewportManager, 1, v, w.surface); err != nil {
			v.Unregister()
			return err
		}
		w.viewport = v
		s := &surfaceFractionalScale{handler: w.handler}
		fractionalScaleManager.Context().Register(s)
		if err := fractionalScaleManager.Context().SendRequest(fractionalScaleManager, 1, s, w.surface); err != nil {
			s.Unregister()
			return err
		}
		w.fractionalScale = s
	}
	updateWindowScale(w)
	return nil
}

func destroySurfaceScale(w *windowState) {
	if w.fractionalScale != nil {
		_ = w.fractionalScale.Context().SendRequest(w.fractionalScale, 0)
		w.fractionalScale = nil
	}
	if w.viewport != nil {
		_ = w.viewport.Context().SendRequest(w.viewport, 0)
		w.viewport = nil
	}
	w.enteredOutputs = nil
	w.preferredBufferScale = 0
}

func (h *handler) HandleSurfacePreferredBufferScale(ev wl.SurfacePreferredBufferScaleEvent) {
	if w := h.window; w != nil && !w.closed && ev.Factor > 0 {
		w.preferredBufferScale = ev.Factor
		updateWindowScale(w)
	}
}

func updateWindowScale(w *windowState) {
	if w.closed {
		return
	}
	s := float32(1)
	if compositorVer >= 3 {
		if o := outputFor(w.output); o != nil {
			s = float32(max(1, o.scale))
		}
		for output := range w.enteredOutputs {
			if o := outputFor(output); o != nil {
				s = max(s, float32(o.scale))
			}
		}
		if w.preferredBufferScale > 0 {
			s = float32(w.preferredBufferScale)
		}
	}
	if w.fractionalScale != nil && w.viewport != nil && w.fractionalScale.preferred > 0 {
		s = float32(w.fractionalScale.preferred) / 120
	}
	if s == w.windowScale {
		return
	}
	leave := enterWindow(w)
	defer leave()
	w.windowScale = s
	recomputeDeviceSize()
	w.dirty = true
	w.surfaceStateDirty = true
	wlDebug("surface scale %q: %g logical=%dx%d pixels=%dx%d", w.winTitle, s, w.logicalW, w.logicalH, w.curW, w.curH)
}
