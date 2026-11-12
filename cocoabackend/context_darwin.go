//go:build darwin && !ios

package cocoabackend

import (
	"unsafe"

	"go.hasen.dev/shirei"
)

// Context is the BackendContext for the macOS (AppKit) backend.
// The backend sets Host.EscapeHatchBackendContext to this value at Run.
type Context struct{ window *windowState }

// Platform implements shirei.BackendContext.
func (Context) Platform() string { return "darwin" }

// NSWindow returns the live NSWindow as an opaque pointer, or nil if the host
// has not created a window yet or it is closed. Call on the UI thread; cast
// with objc / AppKit (NSWindow *).
func (c Context) NSWindow() unsafe.Pointer {
	if c.window == nil || c.window.closed {
		return nil
	}
	return unsafe.Pointer(uintptr(c.window.gWindow))
}

var _ shirei.BackendContext = Context{}
