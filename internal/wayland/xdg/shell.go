// Package xdg implements the stable XDG Window Manager Base protocol
package xdg

//go:generate ../../../../../bin/go-wayland-scanner -pkg xdg -i xdg-shell.xml -o xdg-shell.xml.go

import "go.hasen.dev/shirei/internal/wayland/wl"

type WlSurface = wl.Surface
type BaseProxy = wl.BaseProxy
type Event = wl.Event
type Context = wl.Context
type Proxy = wl.Proxy

func (s *Surface) AddListener(h SurfaceConfigureHandler) {
	s.AddConfigureHandler(h)
}

type Seat = wl.Seat
type Output = wl.Output

func NewShell(ctx *Context) *WmBase {
	ret := new(WmBase)
	ret.privateWmBasePings = make(map[WmBasePingHandler]struct{})
	ctx.Register(ret)
	return ret
}

func WmBaseAddListener(s *WmBase, h WmBasePingHandler) {
	s.AddPingHandler(h)
}

type ToplevelListener interface {
	ToplevelConfigureHandler
	ToplevelCloseHandler
}

func ToplevelAddListener(tl *Toplevel, h ToplevelListener) {
	tl.AddConfigureHandler(h)
	tl.AddCloseHandler(h)
}
