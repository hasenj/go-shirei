//go:build darwin && !ios && !x11darwin && cgo

package darkmode

/*
#cgo CFLAGS: -fobjc-arc
#cgo LDFLAGS: -framework Cocoa
#include "darkmode_darwin.h"
*/
import "C"

//export shireiExtDarkmodeOnUpdate
func shireiExtDarkmodeOnUpdate(isDark C.int) {
	setDarkMode(isDark == 1)
}

func initPlatform() {
	isDark := C.shirei_ext_darkmode_darwin_is_dark()
	setDarkMode(isDark == 1)
	C.shirei_ext_darkmode_darwin_start_observer()
}
