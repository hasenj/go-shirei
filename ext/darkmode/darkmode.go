// Package darkmode provides a lightweight, real-time query for the host
// operating system's dark mode preference across desktop, mobile, and web.
package darkmode

import (
	"sync"
	"sync/atomic"

	"go.hasen.dev/shirei"
)

var (
	isDarkMode atomic.Bool
	initOnce   sync.Once
)

// OSDarkMode reports whether the host operating system is currently in dark mode.
//
// Fast and safe to call every frame (sub-nanosecond atomic read). On first call,
// it inspects the system theme and registers an OS-level notification observer
// so that changes made by the user or scheduled by the OS update automatically
// and request a new frame.
func OSDarkMode() bool {
	initOnce.Do(initPlatform)
	return isDarkMode.Load()
}

func setDarkMode(val bool) {
	old := isDarkMode.Swap(val)
	if old != val {
		shirei.RequestNextFrame()
	}
}
