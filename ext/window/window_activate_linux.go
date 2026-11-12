//go:build linux

package window

import (
	"go.hasen.dev/shirei/waylandbackend"
	"os"
)

func activatePlatformApp() {
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		waylandbackend.Activate()
	}
}
