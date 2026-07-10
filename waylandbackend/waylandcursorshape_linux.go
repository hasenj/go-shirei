//go:build linux

package waylandbackend

import (
	"go.hasen.dev/shirei/internal/wayland/cursorshape"
)

// wp_cursor_shape_v1 support: the compositor draws the real themed cursor —
// the same cursor the rest of the desktop uses, correctly sized on HiDPI. We
// ask for the "default" shape on pointer enter. When the compositor doesn't
// advertise the protocol (older versions), applyCursor falls back to the
// themed-xcursor loader, then the drawn arrow (waylandcursor_linux.go).

var (
	cursorShapeMgr *cursorshape.Manager
	cursorShapeDev *cursorshape.Device
)

// bindCursorShapeManager binds wp_cursor_shape_manager_v1 from the registry.
func bindCursorShapeManager(name, version uint32) {
	if mgr, err := cursorshape.BindManager(registry, name, version); err == nil {
		cursorShapeMgr = mgr
	}
}

// ensureCursorShapeDevice creates the per-pointer shape device once both the
// manager and the pointer exist.
func ensureCursorShapeDevice() {
	if cursorShapeDev != nil || cursorShapeMgr == nil || pointer == nil {
		return
	}
	if dev, err := cursorShapeMgr.GetPointer(pointer); err == nil {
		cursorShapeDev = dev
	}
}
