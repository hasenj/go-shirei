//go:build linux

package waylandbackend

import (
	zxdg "go.hasen.dev/shirei/internal/wayland/xdg"

	"go.hasen.dev/shirei"
)

// Context is the BackendContext for the Wayland backend.
// The backend sets Host.EscapeHatchBackendContext to this value at Run.
//
// Escape hatch: XdgToplevel is the shell-level main window (title, app_id,
// maximize/fullscreen/close) — roughly the desktop counterpart of NSWindow
// or the window host role of an Activity. It is not a UIKit-style
// “present system UI from here” object; most Linux system features use
// D-Bus portals or other services. The backend still owns configure,
// commit, and present of the main surface — extensions must not destroy
// the toplevel or take over its frame loop.
type Context struct{}

// Platform implements shirei.BackendContext.
func (Context) Platform() string { return "wayland" }

// XdgToplevel returns the live xdg_toplevel for the main window, or nil
// before createWindow / after teardown. Backend-owned; do not Destroy it.
func (Context) XdgToplevel() *zxdg.Toplevel {
	return xdgToplevel
}

var _ shirei.BackendContext = Context{}
