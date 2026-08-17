//go:build darwin && !ios && !x11darwin

package window

/*
#cgo CFLAGS: -fobjc-arc
#cgo LDFLAGS: -framework Cocoa
#include "window_darwin.h"
*/
import "C"

import (
	"unsafe"

	"go.hasen.dev/shirei"
)

type darwinNSWindowContext interface {
	shirei.BackendContext
	NSWindow() unsafe.Pointer
}

func init() {
	setPlatformMinSize = func(ctx shirei.BackendContext, minW, minH float32) {
		c, ok := ctx.(darwinNSWindowContext)
		if !ok {
			return
		}
		ptr := c.NSWindow()
		if ptr == nil {
			return
		}
		C.window_setNSWindowMinSize(ptr, C.double(minW), C.double(minH))
	}

	setPlatformCenter = func(ctx shirei.BackendContext) {
		c, ok := ctx.(darwinNSWindowContext)
		if !ok {
			return
		}
		ptr := c.NSWindow()
		if ptr == nil {
			return
		}
		C.window_centerNSWindow(ptr)
	}

	setPlatformPosition = func(ctx shirei.BackendContext, x, y int) {
		c, ok := ctx.(darwinNSWindowContext)
		if !ok {
			return
		}
		ptr := c.NSWindow()
		if ptr == nil {
			return
		}
		C.window_positionNSWindow(ptr, C.int(x), C.int(y))
	}
}
