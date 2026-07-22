//go:build android

package androidbackend

/*
#include "shim.h"
*/
import "C"

import (
	"unsafe"

	"go.hasen.dev/shirei"
)

// Context is the BackendContext for the Android backend.
// The backend sets Host.EscapeHatchBackendContext to this value at Run.
type Context struct{}

// Platform implements shirei.BackendContext.
func (Context) Platform() string { return "android" }

// Activity returns the live ShireiActivity / NativeActivity jobject
// (glue-owned; do not DeleteLocalRef). nil if unavailable.
// Prefer the generic host helpers (StartActivityForResult, RequestPermissions)
// over ad-hoc JNI when possible.
func (Context) Activity() unsafe.Pointer {
	return unsafe.Pointer(C.shirei_android_activity())
}

var _ shirei.BackendContext = Context{}
