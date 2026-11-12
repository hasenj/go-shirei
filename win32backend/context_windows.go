//go:build windows

package win32backend

import (
	"unsafe"

	"go.hasen.dev/shirei"
)

// Context is the BackendContext for the Win32 backend.
// Each window exposes its context through Host.EscapeHatchBackendContext.
type Context struct{ window *windowState }

// Platform implements shirei.BackendContext.
func (Context) Platform() string { return "windows" }

// HWND returns the live window handle as an opaque pointer, or nil if the
// host has not created a window yet or the window is closed. Cast to syscall.Handle / windows.HWND.
func (c Context) HWND() unsafe.Pointer {
	if c.window == nil || c.window.closed || c.window.hwnd == 0 {
		return nil
	}
	return unsafe.Pointer(c.window.hwnd)
}

var _ shirei.BackendContext = Context{}
