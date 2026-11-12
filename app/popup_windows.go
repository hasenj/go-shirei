//go:build windows

package app

import (
	"go.hasen.dev/shirei"
	"go.hasen.dev/shirei/win32backend"
)

func openPopupWindow(options PopupWindowOptions, root shirei.FrameFn) (PopupWindow, error) {
	return win32backend.OpenPopupWindow(options, root)
}

func closePopupWindow(window PopupWindow) { win32backend.ClosePopupWindow(window) }

func movePopupWindow(window PopupWindow, position *shirei.Vec2) {
	win32backend.MovePopupWindow(window, position)
}
