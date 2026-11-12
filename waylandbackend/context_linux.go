//go:build linux

package waylandbackend

import (
	zxdg "go.hasen.dev/shirei/internal/wayland/xdg"

	"go.hasen.dev/shirei"
	"go.hasen.dev/shirei/internal/wayland/layershell"
)

// Context is the BackendContext for the Wayland backend.
// The backend sets Host.EscapeHatchBackendContext to this value at Run.
//
// Escape hatch: XdgToplevel is the shell-level window (title, app_id,
// maximize/fullscreen/close) — roughly the desktop counterpart of NSWindow
// or the window host role of an Activity. It is not a UIKit-style
// “present system UI from here” object; most Linux system features use
// D-Bus portals or other services. The backend still owns configure,
// commit, and present of each surface — extensions must not destroy
// the toplevel or take over its frame loop.
type Context struct{ window *windowState }

// ToplevelStates returns the states from the latest completed configure.
// Each bit is indexed by an xdg.ToplevelState value. Read on the UI thread.
func (c Context) ToplevelStates() uint32 {
	if c.window == nil {
		return 0
	}
	return c.window.toplevelStates
}

// Platform implements shirei.BackendContext.
func (Context) Platform() string { return "wayland" }

// PointerInside reports whether the pointer currently hovers this window's
// surface. Wayland tracks pointer focus per surface and separately from keyboard
// focus, so a window reports hover without being activated or focused, and an
// occluding window takes it away. A compositor input grab can keep the report
// true while the pointer is held outside the surface. Read on the UI thread.
func (c Context) PointerInside() bool {
	w := c.window
	return w != nil && !w.closed && !w.hidden && pointerWindow == w
}

// XdgToplevel returns the live xdg_toplevel for this window, or nil
// before creation, for layer surfaces, or after teardown. Backend-owned.
func (c Context) XdgToplevel() *zxdg.Toplevel {
	if c.window == nil || c.window.closed {
		return nil
	}
	return c.window.xdgToplevel
}

var _ shirei.BackendContext = Context{}

// LayerSurface returns this window's layer role, or nil for a toplevel or closed
// window. Backend-owned; do not destroy it or take over configure/commit handling.
func (c Context) LayerSurface() *layershell.Surface {
	if c.window == nil || c.window.closed {
		return nil
	}
	return c.window.layerSurface
}
