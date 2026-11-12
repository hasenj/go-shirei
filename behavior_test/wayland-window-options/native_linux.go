//go:build linux && !android

package main

import (
	"fmt"
	"go.hasen.dev/shirei"
	"go.hasen.dev/shirei/internal/wayland/wl"
	"go.hasen.dev/shirei/waylandbackend"
	"os"
)

func waylandAvailable() bool { return os.Getenv("WAYLAND_DISPLAY") != "" }
func positionMain(ctx shirei.BackendContext, p shirei.Vec2) {
	ctx.(waylandbackend.Context).Position(int(p[0]), int(p[1]))
}
func closeMain(ctx shirei.BackendContext) {
	c := ctx.(waylandbackend.Context)
	if layer := c.LayerSurface(); layer != nil {
		layer.Dispatch(&wl.Event{Opcode: 1})
	} else {
		c.XdgToplevel().Dispatch(&wl.Event{Opcode: 1})
	}
}
func checkRoles(owner shirei.BackendContext, panels []shirei.BackendContext) error {
	if c := owner.(waylandbackend.Context); c.LayerSurface() == nil && c.XdgToplevel() == nil {
		return fmt.Errorf("main window has no native role")
	}
	for i, ctx := range panels {
		c := ctx.(waylandbackend.Context)
		if i == 0 {
			if c.XdgToplevel() == nil || c.LayerSurface() != nil {
				return fmt.Errorf("normal utility window uses a layer role")
			}
		} else if c.LayerSurface() == nil || c.XdgToplevel() != nil {
			return fmt.Errorf("floating window %d has no layer role", i)
		}
	}
	return nil
}
