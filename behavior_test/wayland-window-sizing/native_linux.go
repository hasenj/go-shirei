//go:build linux && !android

package main

import (
	"go.hasen.dev/shirei"
	xdg "go.hasen.dev/shirei/internal/wayland/xdg"
	"go.hasen.dev/shirei/waylandbackend"
)

func configuredState(ctx shirei.BackendContext) int {
	states := ctx.(waylandbackend.Context).ToplevelStates()
	if states&(1<<xdg.ToplevelStateFullscreen) != 0 {
		return 2
	}
	if states&(1<<xdg.ToplevelStateMaximized) != 0 {
		return 1
	}
	return 0
}

func sizeConstrained(ctx shirei.BackendContext) bool {
	const constrained = 1<<xdg.ToplevelStateMaximized | 1<<xdg.ToplevelStateFullscreen |
		1<<xdg.ToplevelStateResizing | 1<<xdg.ToplevelStateTiledLeft | 1<<xdg.ToplevelStateTiledRight |
		1<<xdg.ToplevelStateTiledTop | 1<<xdg.ToplevelStateTiledBottom
	return ctx.(waylandbackend.Context).ToplevelStates()&constrained != 0
}

func resizeOther(ctx shirei.BackendContext, w, h float32) { ctx.(waylandbackend.Context).SetSize(w, h) }
func constrain(ctx shirei.BackendContext, state string) {
	t := ctx.(waylandbackend.Context).XdgToplevel()
	switch state {
	case "maximize":
		t.SetMaximized()
	case "unmaximize":
		t.UnsetMaximized()
	case "fullscreen":
		t.SetFullscreen(nil)
	case "unfullscreen":
		t.UnsetFullscreen()
	}
}
