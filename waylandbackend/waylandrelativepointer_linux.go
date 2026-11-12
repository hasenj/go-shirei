//go:build linux

package waylandbackend

import (
	"go.hasen.dev/shirei"
	"go.hasen.dev/shirei/internal/dragtrace"
	"go.hasen.dev/shirei/internal/wayland/wl"
	"math"
)

var relativeManager *relativePointerManager
var relativePointerObject *relativePointer

type relativePointerManager struct{ wl.BaseProxy }

func (*relativePointerManager) Dispatch(*wl.Event) {}

type relativePointer struct{ wl.BaseProxy }

func (*relativePointer) Dispatch(ev *wl.Event) {
	if ev.Opcode != 0 {
		return
	}
	hi, lo := ev.Uint32(), ev.Uint32()
	delta := shirei.Vec2{ev.Float32(), ev.Float32()}
	unaccelerated := shirei.Vec2{ev.Float32(), ev.Float32()}
	w := pointerWindow
	if w == nil || w.closed || w.hidden {
		return
	}
	w.pointerTravel = shirei.Vec2Add(w.pointerTravel, delta)
	w.pointerHasRelative = true
	tracePointer(w, "wl.pointer.relative", "protocol_us=%d delta=%v unaccelerated=%v", uint64(hi)<<32|uint64(lo), delta, unaccelerated)
	w.dirty = true
}

func bindRelativePointerManager(name uint32) {
	m := &relativePointerManager{}
	registry.Context().Register(m)
	if err := registry.Bind(name, "zwp_relative_pointer_manager_v1", 1, m); err != nil {
		m.Unregister()
		return
	}
	relativeManager = m
	dragtrace.Logf("wl.relative.bind", "manager=%d", m.Id())
	ensureRelativePointer()
}

func ensureRelativePointer() {
	if relativeManager == nil || pointer == nil || relativePointerObject != nil {
		return
	}
	p := &relativePointer{}
	relativeManager.Context().Register(p)
	if err := relativeManager.Context().SendRequest(relativeManager, 1, p, pointer); err != nil {
		p.Unregister()
		return
	}
	relativePointerObject = p
	dragtrace.Logf("wl.relative.ready", "object=%d pointer=%d", p.Id(), pointer.Id())
}

// PointerTravel returns pointer displacement since the primary press in logical
// points, independently of surface movement. Read on the UI thread. Relative
// devices use relative-pointer events. Absolute devices use surface coordinates
// and ordered position-commit markers for explicitly positioned layer windows.
func (c Context) PointerTravel() (travel shirei.Vec2, available bool) {
	w := c.window
	source := "unavailable"
	if dragtrace.Enabled() {
		defer func() {
			tracePointer(w, "wl.pointer.sample", "source=%s value=%v available=%t", source, travel, available)
		}()
	}
	if w == nil || w.closed || w.hidden || pointerWindow != w {
		return shirei.Vec2{}, false
	}
	if w.pointerHasRelative {
		source = "relative"
		return w.pointerTravel, true
	}
	if w.pointerOriginKnown && w.pointerAbsoluteKnown {
		source = "absolute"
		return shirei.Vec2Sub(w.pointerAbsolute, w.pointerPress), true
	}
	source = "waiting_for_motion"
	return shirei.Vec2{}, relativePointerObject != nil
}

func recordAbsolutePointer(w *windowState, x, y float32) {
	if !w.pointerOriginKnown {
		return
	}
	w.pointerAbsolute = shirei.Vec2Add(w.pointerOrigin, shirei.Vec2{x, y})
	w.pointerAbsoluteKnown = true
}

type pointerOriginMarker struct {
	window  *windowState
	surface *wl.Surface
	origin  shirei.Vec2
	known   bool
}

func (m *pointerOriginMarker) HandleCallbackDone(wl.CallbackDoneEvent) {
	w := m.window
	tracePointer(w, "wl.origin.done", "marker=%p next=%v next_known=%t matches_surface=%t", m, m.origin, m.known, w.surface == m.surface)
	if w.closed || w.surface != m.surface {
		return
	}
	w.pointerOrigin, w.pointerOriginKnown = m.origin, m.known
	tracePointer(w, "wl.origin.applied", "marker=%p", m)
}

func commitPositionedSurface(w *windowState) error {
	tracePointer(w, "wl.commit.positioned", "presented=%t", w.havePresented)
	known := w.position != nil
	origin := shirei.Vec2{}
	if known {
		x, y := layerCoordinates(w)
		if o := outputFor(w.output); o != nil {
			x += float64(o.x)
			y += float64(o.y)
		}
		origin = shirei.Vec2{float32(math.Round(x)), float32(math.Round(y))}
	}
	if w.havePresented && disp != nil && (known != w.pointerOriginKnown || origin != w.pointerOrigin) {
		marker := &pointerOriginMarker{window: w, surface: w.surface, origin: origin, known: known}
		tracePointer(w, "wl.origin.send", "marker=%p next=%v next_known=%t", marker, origin, known)
		return disp.SyncBeforeCommit(w.surface, marker)
	}
	w.pointerOrigin, w.pointerOriginKnown = origin, known
	tracePointer(w, "wl.origin.direct", "")
	return w.surface.Commit()
}
