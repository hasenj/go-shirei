//go:build js

package app

import (
	"image"

	"go.hasen.dev/shirei"
	"go.hasen.dev/shirei/jsbackend"
)

// SetupWindow records the document title and preferred CSS-pixel content size.
// Call it before Run. On a top-level page the floating shell grows by the
// titlebar so the app body keeps that size; iframe embeds stay exact-fit.
// Pass 0,0 to fill the viewport instead.
func SetupWindow(title string, width, height int) {
	jsbackend.SetupWindow(title, width, height)
}

// CenterWindow is a no-op on the web (the page owns placement).
func CenterWindow() {
	jsbackend.CenterWindow()
}

// PositionWindow is a no-op on the web.
func PositionWindow(x, y int) {
	jsbackend.PositionWindow(x, y)
}

// SetupIcon is a no-op on the web for the first cut (use a favicon in HTML).
func SetupIcon(imagePath string) {
	jsbackend.SetupIcon(imagePath)
}

// SetupIconImage is a no-op on the web for the first cut.
func SetupIconImage(img image.Image) {
	jsbackend.SetupIconImage(img)
}

// SetupIconBytes is a no-op on the web for the first cut.
func SetupIconBytes(data []byte) {
	jsbackend.SetupIconBytes(data)
}

// Run attaches to the page canvas and drives frames via requestAnimationFrame.
// It never returns; the browser owns process lifetime.
func Run(frameFn shirei.FrameFn) {
	jsbackend.Run(frameFn)
}
