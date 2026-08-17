//go:build ios

package app

import (
	"image"

	"go.hasen.dev/shirei"
	"go.hasen.dev/shirei/internal/iconimg"
	"go.hasen.dev/shirei/iosbackend"
)

// SetupWindow records the window's title and preferred size. On iOS the view is
// full-screen (safe-area); size is accepted for API parity with desktop.
func SetupWindow(title string, width, height int) {
	iosbackend.SetupWindow(title, width, height)
}

// SetupIcon is a no-op on iOS for the spike (bundle icon comes from the host
// app's Assets/Info.plist).
func SetupIcon(imagePath string) {}

// SetupIconImage is a no-op on iOS for the spike.
func SetupIconImage(img image.Image) { _ = img }

// SetupIconBytes is a no-op on iOS for the spike.
func SetupIconBytes(data []byte) {
	_ = iconimg.DecodeBytes(data)
}

// Run attaches the UIKit content view and returns; CADisplayLink drives frames
// afterward. Must be called on the main goroutine after UIApplicationMain has
// started (ios-run.sh / ioshost arrange this via shirei_ios_run).
func Run(frameFn shirei.FrameFn) {
	iosbackend.Run(frameFn)
}
