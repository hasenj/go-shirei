//go:build linux && !android

package main

import (
	"fmt"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"

	. "go.hasen.dev/shirei"
	"go.hasen.dev/shirei/ext/window"
	"go.hasen.dev/shirei/internal/wayland/wl"
	"go.hasen.dev/shirei/waylandbackend"
	"go.hasen.dev/shirei/x11backend"
)

const platformSupported = true

const nativeChecksDescription = "all four popup styles, independent input and composition, menus and toasts, distinct live native windows"

func preferTabs() {}
func mainSizeOK() bool {
	if *requireLayerShell && *undecoratedMain && GetHost().EscapeHatchBackendContext.(waylandbackend.Context).LayerSurface() == nil {
		return false
	}
	s := GetHost().WindowSize
	return s[0] > 0 && s[1] > 0
}
func nativeWindow(ctx BackendContext) any {
	if c, ok := ctx.(x11backend.Context); ok {
		if c.Connected() {
			return c.Window()
		}
		return nil
	}
	c := ctx.(waylandbackend.Context)
	if layer := c.LayerSurface(); layer != nil {
		return layer
	}
	if top := c.XdgToplevel(); top != nil {
		return top
	}
	return nil
}
func nativeClosed(ctx BackendContext) bool { return nativeWindow(ctx) == nil }
func closeNative(ctx BackendContext) {
	if c, ok := ctx.(x11backend.Context); ok {
		if !c.Connected() {
			return
		}
		protocols, _ := xproto.InternAtom(c.Conn(), false, 12, "WM_PROTOCOLS").Reply()
		del, _ := xproto.InternAtom(c.Conn(), false, 16, "WM_DELETE_WINDOW").Reply()
		e := xproto.ClientMessageEvent{Format: 32, Window: c.Window(), Type: protocols.Atom, Data: xproto.ClientMessageDataUnionData32New([]uint32{uint32(del.Atom), 0, 0, 0, 0})}
		xproto.SendEvent(c.Conn(), false, c.Window(), 0, string(e.Bytes()))
		c.Conn().Sync()
		return
	}
	if native := nativeWindow(ctx); native != nil {
		// Both native roles use event opcode 1 to request a close.
		native.(wl.Dispatcher).Dispatch(&wl.Event{Opcode: 1})
	}
}

func checkNative(p *panel) (bool, error) {
	top := nativeWindow(p.context)
	if top == nil || top == nativeWindow(p.ownerContext) {
		return false, fmt.Errorf("panel %d has no independent native window", p.tag)
	}
	size := GetHost().WindowSize
	if GetHost().TransparentBackground != p.transparent || size[0] <= 0 || size[1] <= 0 {
		return false, fmt.Errorf("panel %d has incorrect style or empty content", p.tag)
	}
	switch ctx := p.context.(type) {
	case x11backend.Context:
		geometry, err := xproto.GetGeometry(ctx.Conn(), xproto.Drawable(ctx.Window())).Reply()
		if err != nil {
			return false, err
		}
		if p.transparent && geometry.Depth != 32 {
			return false, fmt.Errorf("panel %d has no ARGB window", p.tag)
		}
		atom, _ := xproto.InternAtom(ctx.Conn(), false, 15, "_MOTIF_WM_HINTS").Reply()
		hints, err := xproto.GetProperty(ctx.Conn(), false, ctx.Window(), atom.Atom, atom.Atom, 0, 5).Reply()
		if err != nil {
			return false, err
		}
		undecorated := len(hints.Value) >= 12 && xgb.Get32(hints.Value)&2 != 0 && xgb.Get32(hints.Value[8:]) == 0
		if undecorated != p.undecorated {
			return false, fmt.Errorf("panel %d has incorrect decorations", p.tag)
		}
		owner, err := xproto.GetProperty(ctx.Conn(), false, ctx.Window(), xproto.AtomWmTransientFor, xproto.AtomWindow, 0, 1).Reply()
		if err != nil {
			return false, err
		}
		if len(owner.Value) != 4 || xproto.Window(xgb.Get32(owner.Value)) != p.ownerContext.(x11backend.Context).Window() {
			return false, fmt.Errorf("panel %d has incorrect native owner", p.tag)
		}
	case waylandbackend.Context:
		if layer := ctx.LayerSurface(); layer != nil {
			if !p.undecorated || ctx.XdgToplevel() != nil {
				return false, fmt.Errorf("panel %d has the wrong shell role", p.tag)
			}
			want := Vec2{float32(320 + p.tag%3*20), float32(240 + p.tag%2*10)}
			if GetHost().WindowSize != want {
				return false, nil
			}
		} else if p.undecorated && *requireLayerShell {
			return false, fmt.Errorf("panel %d does not use layer-shell", p.tag)
		}

	}

	for _, q := range panels {
		if q != p && q.context != nil && !nativeClosed(q.context) && nativeWindow(q.context) == top {
			return false, fmt.Errorf("panel %d shares another panel's native window", p.tag)
		}
	}
	// Inspect the previous frame's scene; GPU pixel readback is verified by
	// gpurender's GLES tests. This harness presents every panel to the compositor.
	for _, s := range LastFrameSurfaces() {
		if s.Rect.Size == (Vec2{28, 28}) && s.Color1 == (Vec4{float32(p.tag%3) * 120, 100, 50, 1}) {
			return true, nil
		}
	}
	return false, fmt.Errorf("panel %d scene has another window's marker", p.tag)
}

// Frame input simulates composition independently in each presented UI world.
// Protocol focus and text routing are exercised by the backend's event tests.
func markNative(_ BackendContext, text string) { GetInputState().Composition = text }
func finishNativeComposition(_ BackendContext) {
	GetFrameInput().Text += GetInputState().Composition
	GetInputState().Composition = ""
}
func resizeNative(_ BackendContext, tag int) {
	window.SetMinSize(float32(280+tag%3*10), float32(180+tag%2*10))
	window.SetSize(float32(320+tag%3*20), float32(240+tag%2*10))
}
