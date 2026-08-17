//go:build android && cgo

package darkmode

/*
#cgo LDFLAGS: -landroid
#include <android/configuration.h>

static int shirei_ext_darkmode_android_check(void) {
    AConfiguration* config = AConfiguration_new();
    if (!config) {
        return 0;
    }
    int night = AConfiguration_getUiModeNight(config);
    AConfiguration_delete(config);
    return (night == ACONFIGURATION_UI_MODE_NIGHT_YES) ? 1 : 0;
}
*/
import "C"

func initPlatform() {
	setDarkMode(C.shirei_ext_darkmode_android_check() == 1)
}
