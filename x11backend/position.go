//go:build linux || (darwin && x11darwin)

package x11backend

import (
	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xinerama"
	"github.com/jezek/xgb/xproto"
	"go.hasen.dev/shirei"
)

// placePopupWindow constrains placement to the owner's monitor and the desktop
// work area, which excludes reserved space such as panels and taskbars.
func placePopupWindow(w *windowState, position *shirei.Vec2) {
	owner := w.owner
	origin, err := xproto.TranslateCoordinates(X, owner.win, screen.Root, 0, 0).Reply()
	if err != nil {
		return
	}
	ox, oy := int(origin.DstX), int(origin.DstY)
	left, top, right, bottom := 0, 0, int(screen.WidthInPixels), int(screen.HeightInPixels)
	if xinerama.Init(X) == nil {
		if monitors, err := xinerama.QueryScreens(X).Reply(); err == nil {
			cx, cy := ox+owner.curW/2, oy+owner.curH/2
			for _, m := range monitors.ScreenInfo {
				x, y := int(m.XOrg), int(m.YOrg)
				if cx >= x && cy >= y && cx < x+int(m.Width) && cy < y+int(m.Height) {
					left, top, right, bottom = x, y, x+int(m.Width), y+int(m.Height)
					break
				}
			}
		}
	}
	desktop := uint32(0)
	if p, err := xproto.GetProperty(X, false, screen.Root, internAtom("_NET_CURRENT_DESKTOP"), xproto.AtomCardinal, 0, 1).Reply(); err == nil && len(p.Value) == 4 {
		desktop = xgb.Get32(p.Value)
	}
	if p, err := xproto.GetProperty(X, false, screen.Root, internAtom("_NET_WORKAREA"), xproto.AtomCardinal, desktop*4, 4).Reply(); err == nil && len(p.Value) == 16 {
		x, y := int(int32(xgb.Get32(p.Value))), int(int32(xgb.Get32(p.Value[4:])))
		r, b := x+int(xgb.Get32(p.Value[8:])), y+int(xgb.Get32(p.Value[12:]))
		if min(right, r) > max(left, x) && min(bottom, b) > max(top, y) {
			left, top, right, bottom = max(left, x), max(top, y), min(right, r), min(bottom, b)
		}
	}
	x, y := ox+(owner.curW-w.curW)/2, oy+(owner.curH-w.curH)/2
	if position != nil {
		x, y = ox+int(position[0]*w.windowScale), oy+int(position[1]*w.windowScale)
	}
	x, y = max(left, min(x, right-w.curW)), max(top, min(y, bottom-w.curH))
	xproto.ConfigureWindow(X, w.win, xproto.ConfigWindowX|xproto.ConfigWindowY, []uint32{uint32(x), uint32(y)})
	hints := make([]byte, 18*4)
	xgb.Put32(hints, 1|4) // USPosition | PPosition
	xgb.Put32(hints[4:], uint32(x))
	xgb.Put32(hints[8:], uint32(y))
	xproto.ChangeProperty(X, xproto.PropModeReplace, w.win, xproto.AtomWmNormalHints, xproto.AtomWmSizeHints, 32, 18, hints)
}
