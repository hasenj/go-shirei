//go:build darwin && !ios

package cocoabackend

/*
#include "cocoa.h"
*/
import "C"

import (
	"unsafe"

	"go.hasen.dev/shirei"
)

// Context is the BackendContext for the macOS (AppKit) backend.
// The backend sets Host.EscapeHatchBackendContext to this value at Run.
type Context struct{}

// Platform implements shirei.BackendContext.
func (Context) Platform() string { return "darwin" }

// NSWindow returns the live NSWindow as an opaque pointer, or nil if the host
// has not created a window yet. Cast with cgo / ObjC (__bridge NSWindow *).
func (Context) NSWindow() unsafe.Pointer {
	return unsafe.Pointer(C.cocoa_nsWindow())
}

var _ shirei.BackendContext = Context{}
