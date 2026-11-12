//go:build linux

package waylandbackend

import "go.hasen.dev/shirei/internal/wayland/wl"

// xdg-output v2 reports logical desktop coordinates, including fractional-scale
// and rotated output geometry. Its done event publishes each complete batch.
type logicalOutputManager struct{ wl.BaseProxy }

func (*logicalOutputManager) Dispatch(*wl.Event) {}

var outputManager *logicalOutputManager

type logicalOutput struct {
	wl.BaseProxy
	output              *outputState
	x, y, width, height int32
}

func (o *logicalOutput) Dispatch(ev *wl.Event) {
	switch ev.Opcode {
	case 0:
		o.x, o.y = ev.Int32(), ev.Int32()
	case 1:
		o.width, o.height = ev.Int32(), ev.Int32()
	case 2:
		o.output.x, o.output.y = o.x, o.y
		o.output.width, o.output.height = o.width, o.height
		for _, w := range liveWindows() {
			if w.output == o.output.proxy && w.layerSurface != nil && !w.closed {
				w.surfaceStateDirty, w.dirty = true, true
			}
		}
	}
}
func bindOutputManager(name, version uint32) {
	m := &logicalOutputManager{}
	registry.Context().Register(m)
	if err := registry.Bind(name, "zxdg_output_manager_v1", min(version, 2), m); err != nil {
		m.Unregister()
		return
	}
	outputManager = m
	for _, o := range outputs {
		bindLogicalOutput(o)
	}
}
func bindLogicalOutput(o *outputState) {
	if o.logical != nil {
		return
	}
	l := &logicalOutput{output: o}
	outputManager.Context().Register(l)
	if err := outputManager.Context().SendRequest(outputManager, 1, l, o.proxy); err != nil {
		l.Unregister()
		return
	}
	o.logical = l
}
func outputFor(proxy *wl.Output) *outputState {
	for _, o := range outputs {
		if o.proxy == proxy {
			return o
		}
	}
	return nil
}
