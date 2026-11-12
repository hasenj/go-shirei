//go:build linux

package waylandbackend

import (
	"go.hasen.dev/shirei/internal/wayland/wl"
	"go.hasen.dev/shirei/internal/wayland/wlclient"
)

// Output properties supply the integer fallback for surfaces on that output.
type outputState struct {
	scale               int32
	proxy               *wl.Output
	name                uint32
	version             uint32
	x, y, width, height int32
	logical             *logicalOutput
}

var outputs []*outputState

func bindOutput(name, version uint32) {
	o := wlclient.RegistryBindOutputInterface(registry, name, version)
	os := &outputState{scale: 1, proxy: o, name: name, version: version}
	outputs = append(outputs, os)
	wlclient.OutputAddListener(o, os)
	if outputManager != nil {
		bindLogicalOutput(os)
	}
}

func (o *outputState) HandleOutputScale(ev wl.OutputScaleEvent) {
	if ev.Factor >= 1 {
		o.scale = ev.Factor
	}
}

func (o *outputState) HandleOutputDone(wl.OutputDoneEvent) {
	for _, w := range liveWindows() {
		updateWindowScale(w)
	}
}
func (o *outputState) HandleOutputGeometry(wl.OutputGeometryEvent) {}
func (o *outputState) HandleOutputMode(wl.OutputModeEvent)         {}

// recomputeDeviceSize derives the device-pixel buffer size from the logical size
// and the current scale.
func recomputeDeviceSize() {
	s := currentWindow.windowScale
	if s <= 0 {
		s = 1
	}
	currentWindow.curW = int(float32(currentWindow.logicalW)*s + 0.5)
	currentWindow.curH = int(float32(currentWindow.logicalH)*s + 0.5)
}
