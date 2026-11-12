//go:build linux && !android

package app

import (
	"go.hasen.dev/shirei"
	"go.hasen.dev/shirei/waylandbackend"
	"go.hasen.dev/shirei/x11backend"
)

func openPopupWindow(options PopupWindowOptions, root shirei.FrameFn) (PopupWindow, error) {
	if !useWayland {
		return x11backend.OpenPopupWindow(options, root)
	}
	return waylandbackend.OpenPopupWindow(options, root)
}

func closePopupWindow(window PopupWindow) {
	if useWayland {
		waylandbackend.ClosePopupWindow(window)
	} else {
		x11backend.ClosePopupWindow(window)
	}
}

func movePopupWindow(id PopupWindow, position *shirei.Vec2) {
	if useWayland {
		waylandbackend.MovePopupWindow(id, position)
	}
}
