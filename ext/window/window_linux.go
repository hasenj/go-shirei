//go:build linux || (darwin && x11darwin)

package window

import (
	"github.com/jezek/xgb/xproto"
	"go.hasen.dev/shirei"
	"go.hasen.dev/shirei/waylandbackend"
	"go.hasen.dev/shirei/x11backend"
)

func init() {
	setPlatformMinSize = func(ctx shirei.BackendContext, minW, minH float32) {
		switch c := ctx.(type) {
		case waylandbackend.Context:
			toplevel := c.XdgToplevel()
			if toplevel == nil {
				return
			}
			_ = toplevel.SetMinSize(int32(minW), int32(minH))

		case x11backend.Context:
			if !c.Connected() {
				return
			}
			conn := c.Conn()
			win := c.Window()
			if conn == nil || win == 0 {
				return
			}

			scale := shirei.GetHost().WindowScale
			if scale <= 0 {
				scale = 1
			}
			devMinW := uint32(minW*scale + 0.5)
			devMinH := uint32(minH*scale + 0.5)

			// WM_SIZE_HINTS structure (18 x 32-bit uints):
			// index 0: flags (PMinSize = 1 << 4 = 16)
			// index 5: min_width
			// index 6: min_height
			const pMinSize = 1 << 4
			data := make([]byte, 18*4)
			data[0] = byte(pMinSize)
			data[1] = byte(pMinSize >> 8)
			data[2] = byte(pMinSize >> 16)
			data[3] = byte(pMinSize >> 24)

			data[5*4+0] = byte(devMinW)
			data[5*4+1] = byte(devMinW >> 8)
			data[5*4+2] = byte(devMinW >> 16)
			data[5*4+3] = byte(devMinW >> 24)

			data[6*4+0] = byte(devMinH)
			data[6*4+1] = byte(devMinH >> 8)
			data[6*4+2] = byte(devMinH >> 16)
			data[6*4+3] = byte(devMinH >> 24)

			_ = xproto.ChangePropertyChecked(conn, xproto.PropModeReplace, win,
				xproto.AtomWmNormalHints, xproto.AtomWmSizeHints, 32, 18, data).Check()
		}
	}

	setPlatformCenter = func(ctx shirei.BackendContext) {
		c, ok := ctx.(x11backend.Context)
		if !ok || !c.Connected() {
			return
		}
		conn := c.Conn()
		win := c.Window()
		if conn == nil || win == 0 {
			return
		}

		setup := xproto.Setup(conn)
		if setup == nil {
			return
		}
		screen := setup.DefaultScreen(conn)
		if screen == nil {
			return
		}

		geom, err := xproto.GetGeometry(conn, xproto.Drawable(win)).Reply()
		if err != nil || geom == nil {
			return
		}

		curW := int(geom.Width)
		curH := int(geom.Height)
		posX := uint32((int(screen.WidthInPixels) - curW) / 2)
		posY := uint32((int(screen.HeightInPixels) - curH) / 2)

		_ = xproto.ConfigureWindowChecked(conn, win,
			xproto.ConfigWindowX|xproto.ConfigWindowY,
			[]uint32{posX, posY}).Check()
	}

	setPlatformPosition = func(ctx shirei.BackendContext, x, y int) {
		c, ok := ctx.(x11backend.Context)
		if !ok || !c.Connected() {
			return
		}
		conn := c.Conn()
		win := c.Window()
		if conn == nil || win == 0 {
			return
		}

		scale := shirei.GetHost().WindowScale
		if scale <= 0 {
			scale = 1
		}
		devX := uint32(float32(x)*scale + 0.5)
		devY := uint32(float32(y)*scale + 0.5)

		_ = xproto.ConfigureWindowChecked(conn, win,
			xproto.ConfigWindowX|xproto.ConfigWindowY,
			[]uint32{devX, devY}).Check()
	}
}
