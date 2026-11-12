//go:build darwin && !ios

package gpurender

import "github.com/ebitengine/purego"

// objcMsgSend is the C function pointer for objc_msgSend, filled in bindMetal.
var objcMsgSend uintptr

// objcCall runs native methods on the system stack. AppKit and Metal may use
// more stack space than a Go goroutine has available. Extra arguments are ignored
// by methods with fewer parameters.
func objcCall(id, sel, a0, a1, a2, a3, a4 uintptr) uintptr {
	r, _, _ := purego.SyscallN(objcMsgSend, id, sel, a0, a1, a2, a3, a4)
	return r
}
