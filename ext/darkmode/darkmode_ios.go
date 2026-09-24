//go:build ios && cgo

package darkmode

/*
#cgo CFLAGS: -fobjc-arc
#cgo LDFLAGS: -framework UIKit -framework Foundation
#include "darkmode_ios.h"
*/
import "C"

import (
	"unsafe"

	"go.hasen.dev/shirei"
)

//export shireiExtDarkmodeIOSUpdate
func shireiExtDarkmodeIOSUpdate(isDark C.int) {
	setDarkMode(isDark == 1)
}

func initPlatform() {
	var root unsafe.Pointer
	if ctx, ok := shirei.GetHost().EscapeHatchBackendContext.(interface{ RootViewController() unsafe.Pointer }); ok {
		root = ctx.RootViewController()
	}
	isDark := C.shirei_ext_darkmode_ios_start_observer(root)
	setDarkMode(isDark == 1)
}
