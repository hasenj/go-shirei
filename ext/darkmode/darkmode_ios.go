//go:build ios && cgo

package darkmode

/*
#cgo CFLAGS: -fobjc-arc
#cgo LDFLAGS: -framework UIKit -framework Foundation
#include "darkmode_ios.h"
*/
import "C"

//export shireiExtDarkmodeIOSUpdate
func shireiExtDarkmodeIOSUpdate(isDark C.int) {
	setDarkMode(isDark == 1)
}

func initPlatform() {
	isDark := C.shirei_ext_darkmode_ios_is_dark()
	setDarkMode(isDark == 1)
	C.shirei_ext_darkmode_ios_start_observer()
}
