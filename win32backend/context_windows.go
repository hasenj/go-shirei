//go:build windows

package win32backend

import (
	"unsafe"

	"go.hasen.dev/shirei"
)

// Context is the BackendContext for the Win32 backend.
// The backend sets Host.EscapeHatchBackendContext to this value at Run.
type Context struct{}

// Platform implements shirei.BackendContext.
func (Context) Platform() string { return "windows" }

// HWND returns the live window handle as an opaque pointer, or nil if the
// host has not created a window yet. Cast to syscall.Handle / windows.HWND.
func (Context) HWND() unsafe.Pointer {
	if hwnd == 0 {
		return nil
	}
	return unsafe.Pointer(hwnd)
}

var _ shirei.BackendContext = Context{}
