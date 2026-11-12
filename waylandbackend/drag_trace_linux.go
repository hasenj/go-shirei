//go:build linux

package waylandbackend

import (
	"fmt"

	"go.hasen.dev/shirei/internal/dragtrace"
)

// tracePointer records native state on the Wayland event thread.
func tracePointer(w *windowState, event, format string, args ...any) {
	if !dragtrace.Enabled() {
		return
	}
	if w == nil {
		dragtrace.Logf(event, "window=nil %s", fmt.Sprintf(format, args...))
		return
	}
	var surface uint32
	if w.surface != nil {
		surface = uint32(w.surface.Id())
	}
	dragtrace.Logf(event, "window=%p surface=%d popup=%d closed=%t hidden=%t focused_pointer=%t origin=%v origin_known=%t absolute=%v absolute_known=%t press=%v relative=%v has_relative=%t relative_bound=%t requested=%v logical=%dx%d scale=%g dirty=%t surface_dirty=%t wait_configure=%t has_ack=%t %s",
		w, surface, w.popupID, w.closed, w.hidden, pointerWindow == w,
		w.pointerOrigin, w.pointerOriginKnown, w.pointerAbsolute, w.pointerAbsoluteKnown,
		w.pointerPress, w.pointerTravel, w.pointerHasRelative, relativePointerObject != nil,
		w.position, w.logicalW, w.logicalH, w.windowScale,
		w.dirty, w.surfaceStateDirty, w.waitConfigure, w.hasAck, fmt.Sprintf(format, args...))
}
