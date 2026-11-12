//go:build linux || (darwin && x11darwin)

package x11backend

import (
	"bytes"
	"os"
	"testing"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
	"go.hasen.dev/shirei"
)

// Exercise software rendering and both native image transports against a real
// server. Pixmaps allow exact readback without depending on window occlusion.
func TestX11AlphaPresentation(t *testing.T) {
	if os.Getenv("DISPLAY") == "" {
		t.Skip("requires an X server")
	}
	conn, err := xgb.NewConn()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	oldX, oldScreen, oldMaxReq, oldShm := X, screen, maxReq, useShm
	X, screen = conn, xproto.Setup(conn).DefaultScreen(conn)
	defer func() { X, screen, maxReq, useShm = oldX, oldScreen, oldMaxReq, oldShm }()
	if detectARGBVisual() == 0 {
		t.Skip("requires an ARGB visual")
	}
	hasShm := initShm()
	for _, transport := range []string{"socket", "shm"} {
		t.Run(transport, func(t *testing.T) {
			if transport == "shm" && !hasShm {
				t.Skip("requires MIT-SHM")
			}
			useShm = transport == "shm"
			// Small request budget forces the socket path to split the image into bands.
			maxReq = 128
			a := &windowState{ui: shirei.NewUI(), depth: 32}
			b := &windowState{ui: shirei.NewUI(), depth: 32}
			defer func() {
				for _, w := range []*windowState{a, b} {
					leave := enterWindow(w)
					releaseShm()
					leave()
				}
			}()
			// Switching windows and resizing exercises independent buffers and SHM teardown.
			for i, w := range []*windowState{a, b, a, b} {
				func() {
					leave := enterWindow(w)
					defer leave()
					w.ui.Host.TransparentBackground = true
					w.curW, w.curH = 12, 4+i
					pixmap, err := xproto.NewPixmapId(conn)
					if err != nil {
						t.Fatal(err)
					}
					if err := xproto.CreatePixmapChecked(conn, 32, pixmap, xproto.Drawable(screen.Root), uint16(w.curW), uint16(w.curH)).Check(); err != nil {
						t.Fatal(err)
					}
					defer xproto.FreePixmap(conn, pixmap)
					w.win = xproto.Window(pixmap)
					w.gc, err = xproto.NewGcontextId(conn)
					if err != nil {
						t.Fatal(err)
					}
					if err := xproto.CreateGCChecked(conn, w.gc, xproto.Drawable(pixmap), 0, nil).Check(); err != nil {
						t.Fatal(err)
					}
					defer xproto.FreeGC(conn, w.gc)
					ensureBuf()
					if useShm && !w.presentViaShm {
						t.Fatal("MIT-SHM buffer attachment failed")
					}
					surfaces := []shirei.Surface{
						{Rect: shirei.Rect{Size: shirei.Vec2{4, float32(w.curH)}}, Color1: shirei.Vec4{0, 100, 50, 1}, Color2: shirei.Vec4{0, 100, 50, 1}},
						{Rect: shirei.Rect{Origin: shirei.Vec2{4, 0}, Size: shirei.Vec2{4, float32(w.curH)}}, Color1: shirei.Vec4{120, 100, 50, .5}, Color2: shirei.Vec4{120, 100, 50, .5}},
					}
					w.softRenderer.RenderInto(w.presentBuf, w.curW*4, w.curW, w.curH, 1, surfaces, nil)
					present()
					pixels, err := xproto.GetImage(conn, xproto.ImageFormatZPixmap, xproto.Drawable(pixmap), 0, 0, uint16(w.curW), uint16(w.curH), 0xffffffff).Reply()
					if err != nil {
						t.Fatal(err)
					}
					if !bytes.Equal(pixels.Data, w.presentBuf) {
						t.Fatal("X server pixels differ from the rendered BGRA buffer")
					}
					for _, x := range []int{1, 5, 9} {
						p := pixels.Data[(w.curW+x)*4:][:4]
						switch x {
						case 1:
							if p[0] != 0 || p[1] != 0 || p[2] != 255 || p[3] != 255 {
								t.Fatalf("opaque pixel = %v", p)
							}
						case 5:
							if p[0] != 0 || p[2] != 0 || p[1] != p[3] || p[3] < 127 || p[3] > 128 {
								t.Fatalf("premultiplied translucent pixel = %v", p)
							}
						case 9:
							if xgb.Get32(p) != 0 {
								t.Fatalf("transparent pixel = %v", p)
							}
						}
					}
				}()
			}
		})
	}
}
