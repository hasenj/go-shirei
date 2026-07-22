//go:build linux || (darwin && x11darwin)

package x11backend

import (
	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"

	"go.hasen.dev/shirei"
)

// Context is the BackendContext for the X11 backend.
// The backend sets Host.EscapeHatchBackendContext to this value at Run.
//
// Escape hatch: Conn + Window are the live X connection and top-level
// window XID — the desktop counterpart of NSWindow / HWND (and of
// Wayland's xdg_toplevel + connection, without a separate surface role).
// The backend still owns the event loop and present path; extensions must
// not destroy the window or take over PutImage / configure handling.
type Context struct{}

// Platform implements shirei.BackendContext.
func (Context) Platform() string { return "x11" }

// Connected reports whether the X connection and window exist.
func (Context) Connected() bool { return X != nil && win != 0 }

// Conn returns the live X connection, or nil before connect / after close.
// Backend-owned; do not Close it.
func (Context) Conn() *xgb.Conn { return X }

// Window returns the top-level window XID, or 0 before createWindow.
// Backend-owned; do not DestroyWindow it.
func (Context) Window() xproto.Window { return win }

var _ shirei.BackendContext = Context{}
