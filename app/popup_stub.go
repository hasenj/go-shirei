//go:build (!darwin || ios || x11darwin) && !windows && (!linux || android)

package app

import (
	"fmt"
	"go.hasen.dev/shirei"
)

func openPopupWindow(PopupWindowOptions, shirei.FrameFn) (PopupWindow, error) {
	return 0, fmt.Errorf("native popup windows are not supported by this backend")
}

func closePopupWindow(PopupWindow) {}

func movePopupWindow(PopupWindow, *shirei.Vec2) {}
