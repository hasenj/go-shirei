//go:build linux || (darwin && x11darwin)

// Window icon: decodes the image recorded by SetupIcon and publishes it as the
// _NET_WM_ICON property, which EWMH window managers show in the title bar,
// taskbar, and window switcher.
package x11backend

import (
	"image"

	"github.com/jezek/xgb/xproto"

	"go.hasen.dev/shirei/internal/iconimg"
)

var (
	winIconPath string
	winIconImg  *image.NRGBA
)

// SetupIcon records the path of the image (PNG etc.) used as the window icon.
// Call it before Run; empty keeps the WM's default.
func SetupIcon(imagePath string) {
	winIconPath = imagePath
}

// SetupIconImage is SetupIcon from an in-memory image (e.g. decoded from
// go:embed-ed bytes) instead of a file. It takes precedence over SetupIcon.
func SetupIconImage(img image.Image) {
	winIconImg = iconimg.FromImage(img)
}

// iconImage resolves whichever icon source was recorded (nil if none/broken).
func iconImage() *image.NRGBA {
	if winIconImg != nil {
		return winIconImg
	}
	if winIconPath != "" {
		return iconimg.LoadNRGBA(winIconPath)
	}
	return nil
}

// iconMaxSide caps the published icon so the ChangeProperty request stays well
// under the core-protocol maximum request length (typically 256 KB without
// BIG-REQUESTS): 128×128 ARGB is 64 KB.
const iconMaxSide = 128

// setIconProperty publishes _NET_WM_ICON: an array of CARDINALs — width,
// height, then width*height straight-alpha ARGB pixels in top-down rows.
// Cardinals are packed little-endian to match xgb's wire encoding.
// Best-effort: on failure the window keeps the WM's default icon.
func setIconProperty() {
	img := iconImage()
	if img == nil {
		if winIconPath != "" {
			perfLog("[x11] icon: cannot load %s", winIconPath)
		}
		return
	}
	img = iconimg.ShrinkToFit(img, iconMaxSide)
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	netIcon := internAtom("_NET_WM_ICON")
	if netIcon == 0 {
		return
	}

	data := make([]byte, 4*(2+w*h))
	put32 := func(off int, v uint32) {
		data[off+0] = byte(v)
		data[off+1] = byte(v >> 8)
		data[off+2] = byte(v >> 16)
		data[off+3] = byte(v >> 24)
	}
	put32(0, uint32(w))
	put32(4, uint32(h))
	off := 8
	for y := 0; y < h; y++ {
		row := img.Pix[y*img.Stride : y*img.Stride+w*4]
		for x := 0; x < w; x++ {
			r, g, b, a := row[x*4], row[x*4+1], row[x*4+2], row[x*4+3]
			put32(off, uint32(a)<<24|uint32(r)<<16|uint32(g)<<8|uint32(b))
			off += 4
		}
	}
	xproto.ChangeProperty(X, xproto.PropModeReplace, win,
		netIcon, xproto.AtomCardinal, 32, uint32(2+w*h), data)
}
