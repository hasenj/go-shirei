//go:build linux

package waylandbackend

import (
	"go.hasen.dev/shirei/internal/wayland/wl"
	"go.hasen.dev/shirei/internal/wayland/wlclient"
)

// HiDPI via integer buffer scale. Wayland scaling is per-output: each wl_output
// advertises an integer scale (1, 2, ...). We render the buffer at that scale and
// call wl_surface.set_buffer_scale, so the window stays crisp; under fractional
// desktop scaling the compositor reports the next integer up and downscales — the
// same result as the X11 backend's Xft.dpi=192 -> 2.0 path. We use the max scale
// across outputs, which is exact for the common single-monitor case.
//
// wl_output.scale carries no output reference, so each output gets its own
// listener object tracking its scale; output.done (the end of the atomic property
// batch) recomputes and applies the max.

type outputState struct {
	scale int32
}

var outputs []*outputState

func bindOutput(name, version uint32) {
	o := wlclient.RegistryBindOutputInterface(registry, name, version)
	os := &outputState{scale: 1}
	outputs = append(outputs, os)
	wlclient.OutputAddListener(o, os)
}

func (o *outputState) HandleOutputScale(ev wl.OutputScaleEvent) {
	if ev.Factor >= 1 {
		o.scale = ev.Factor
	}
}

func (o *outputState) HandleOutputDone(wl.OutputDoneEvent)         { applyScale(maxOutputScale()) }
func (o *outputState) HandleOutputGeometry(wl.OutputGeometryEvent) {}
func (o *outputState) HandleOutputMode(wl.OutputModeEvent)         {}

func maxOutputScale() int32 {
	m := int32(1)
	for _, o := range outputs {
		if o.scale > m {
			m = o.scale
		}
	}
	return m
}

// applyScale adopts a new integer buffer scale: resize the device buffer and tell
// the compositor. Safe to call before the surface exists (during connect): the
// scale is then applied to the surface in createWindow.
func applyScale(s int32) {
	if s < 1 {
		s = 1
	}
	if compositorVer < 3 {
		s = 1 // can't tell the compositor a buffer scale; stay unscaled
	}
	if float32(s) == windowScale {
		return
	}
	windowScale = float32(s)
	recomputeDeviceSize()
	if surface != nil && compositorVer >= 3 {
		surface.SetBufferScale(s)
		dirty = true // re-render at the new device size + scale
	}
}

// recomputeDeviceSize derives the device-pixel buffer size from the logical size
// and the current scale.
func recomputeDeviceSize() {
	s := windowScale
	if s < 1 {
		s = 1
	}
	curW = int(float32(logicalW)*s + 0.5)
	curH = int(float32(logicalH)*s + 0.5)
}
