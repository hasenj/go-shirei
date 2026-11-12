//go:build darwin && !ios && !x11darwin

package app

import (
	"go.hasen.dev/shirei"
	"go.hasen.dev/shirei/cocoabackend"
)

func openPopupWindow(options PopupWindowOptions, root shirei.FrameFn) (PopupWindow, error) {
	return cocoabackend.OpenPopupWindow(options, root)
}

func closePopupWindow(window PopupWindow) { cocoabackend.ClosePopupWindow(window) }

func movePopupWindow(window PopupWindow, position *shirei.Vec2) {
	cocoabackend.MovePopupWindow(window, position)
}
