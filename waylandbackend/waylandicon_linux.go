//go:build linux

package waylandbackend

// xdg_toplevel_icon_v1 support, hand-written (neurlang doesn't ship the
// binding), following the wp_cursor_shape pattern in waylandcursorshape.
// This hands the compositor the actual icon image recorded by SetupIcon;
// compositors without the protocol (notably GNOME) never announce the
// manager, and there the .desktop-file-matched-by-app_id route stays the
// only one.
//
// Protocol (staging xdg-toplevel-icon-v1):
//
//	xdg_toplevel_icon_manager_v1.create_icon(new_id icon)             -> opcode 1
//	xdg_toplevel_icon_manager_v1.set_icon(xdg_toplevel, icon)         -> opcode 2
//	xdg_toplevel_icon_v1.add_buffer(wl_buffer, int scale)             -> opcode 2
//
// The manager's icon_size/done events are sizing hints only; we ignore them
// and hand over one square ARGB buffer, which the compositor rescales.

import (
	"image"

	wos "go.hasen.dev/shirei/internal/wayland/os"
	"go.hasen.dev/shirei/internal/wayland/wl"

	"go.hasen.dev/shirei/internal/iconimg"
)

var winIconImg *image.NRGBA

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

// Minimal proxies: embedding wl.BaseProxy satisfies wl.Proxy; Dispatch makes
// them wl.Dispatchers so the context routes (ignored) events here.
type toplevelIconManager struct{ wl.BaseProxy }

func (*toplevelIconManager) Dispatch(*wl.Event) {} // icon_size/done: hints, ignored

type toplevelIcon struct{ wl.BaseProxy }

func (*toplevelIcon) Dispatch(*wl.Event) {}

var iconMgr *toplevelIconManager

// bindToplevelIconManager binds xdg_toplevel_icon_manager_v1 from the registry.
func bindToplevelIconManager(name, version uint32) {
	ctx := registry.Context()
	if ctx == nil {
		return
	}
	mgr := &toplevelIconManager{}
	ctx.Register(mgr)
	if err := registry.Bind(name, "xdg_toplevel_icon_manager_v1", version, mgr); err != nil {
		return
	}
	iconMgr = mgr
	perfLog("[wl] xdg_toplevel_icon_manager_v1 bound")
}

// iconMaxSide caps the icon buffer; compositors rescale to their display sizes
// anyway, and 256² premultiplied ARGB is a 256 KB one-time shm allocation.
const iconMaxSide = 256

// setToplevelIcon builds a square premultiplied-ARGB wl_shm buffer from the
// recorded icon image and assigns it to the toplevel. Best-effort: any failure
// (or no manager in the registry) leaves the compositor's default. The icon
// proxy and its buffer must stay alive while the icon is in use, so they are
// simply kept for the process lifetime — that also sidesteps the protocol's
// copy-timing rules around destroy.
func setToplevelIcon() {
	if iconMgr == nil {
		return
	}
	img := iconImage()
	if img == nil {
		if winIconPath != "" {
			perfLog("[wl] icon: cannot load %s", winIconPath)
		}
		return
	}
	img = iconimg.PadSquare(iconimg.ShrinkToFit(img, iconMaxSide))
	side := img.Bounds().Dx()
	stride := side * 4
	size := stride * side

	fd, err := wos.CreateAnonymousFile(int64(size))
	if err != nil {
		perfLog("[wl] icon file: %v", err)
		return
	}
	defer fd.Close()
	data, err := wos.Mmap(int(fd.Fd()), 0, size, wos.ProtRead|wos.ProtWrite, wos.MapShared)
	if err != nil {
		return
	}

	// ARGB8888 is premultiplied, little-endian byte order B,G,R,A.
	premul := func(c, a byte) byte { return byte((int(c)*int(a) + 127) / 255) }
	for y := 0; y < side; y++ {
		srow := img.Pix[y*img.Stride : y*img.Stride+side*4]
		drow := data[y*stride:]
		for x := 0; x < side; x++ {
			r, g, b, a := srow[x*4], srow[x*4+1], srow[x*4+2], srow[x*4+3]
			drow[x*4+0] = premul(b, a)
			drow[x*4+1] = premul(g, a)
			drow[x*4+2] = premul(r, a)
			drow[x*4+3] = a
		}
	}

	pool, err := shm.CreatePool(fd.Fd(), int32(size))
	if err != nil {
		wos.Munmap(data)
		return
	}
	buf, err := pool.CreateBuffer(0, int32(side), int32(side), int32(stride), wl.ShmFormatArgb8888)
	pool.Destroy()
	if err != nil {
		wos.Munmap(data)
		return
	}

	ctx := iconMgr.Context()
	icon := &toplevelIcon{}
	ctx.Register(icon)
	if err := ctx.SendRequest(iconMgr, 1, icon); err != nil { // create_icon
		return
	}
	ctx.SendRequest(icon, 2, buf, int32(1))        // add_buffer(buffer, scale 1)
	ctx.SendRequest(iconMgr, 2, xdgToplevel, icon) // set_icon(toplevel, icon)
	perfLog("[wl] icon: set via xdg-toplevel-icon-v1 (%dpx)", side)
}
