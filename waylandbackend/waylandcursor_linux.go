//go:build linux

package waylandbackend

import (
	"os"
	"strconv"

	"go.hasen.dev/shirei/internal/wayland/cursorshape"
	wos "go.hasen.dev/shirei/internal/wayland/os"
	"go.hasen.dev/shirei/internal/wayland/wl"
	"go.hasen.dev/shirei/internal/wayland/wlcursor"
)

// Wayland leaves the cursor undefined (invisible) over our surface until the
// client sets one on wl_pointer.enter. Three tiers, best first:
//
//  1. wp_cursor_shape: the compositor draws the themed cursor itself
//     (needs newer mutter; see waylandcursorshape_linux.go).
//  2. Themed xcursor: load the user's cursor theme with the vendored
//     wlcursor/xcursor loader (arm64-safe since vendoring — the upstream
//     crash was its swizzle assembly, which is gone) and hand the
//     compositor a buffer, scaled for HiDPI via set_buffer_scale.
//  3. The drawn arrow below, when no theme can be found at all.
//
// SHIREI_WL_NO_CURSOR_SHAPE=1 skips tier 1, to exercise the themed path on
// compositors that do support cursor-shape (e.g. for VM testing on Fedora).

var (
	cursorSurface *wl.Surface
	cursorBuf     *wl.Buffer
	cursorData    []byte
	cursorHotX    int32
	cursorHotY    int32
	cursorScale   int // scale the current cursor buffer was rendered at
	cursorReady   bool
)

// cursorArrow is the pointer bitmap: '#' = light outline, '.' = dark fill,
// ' ' = transparent. A dark arrow with a light edge reads on any background and
// matches typical desktop cursors. Hotspot is the tip at the top-left (0,0).
var cursorArrow = []string{
	"#         ",
	"##        ",
	"#.#       ",
	"#..#      ",
	"#...#     ",
	"#....#    ",
	"#.....#   ",
	"#......#  ",
	"#.......# ",
	"#........#",
	"#.....####",
	"#..#..#   ",
	"#.# #..#  ",
	"##  #..#  ",
	"#    #..# ",
	"     #..# ",
	"      ##  ",
}

// buildCursor (re)creates the cursor surface + buffer rendered at integer scale
// cs. Non-fatal: on failure the cursor is left to the compositor.
func buildCursor(cs int) {
	if cs < 1 {
		cs = 1
	}
	// Release a previously-built cursor buffer.
	if cursorBuf != nil {
		cursorBuf.Destroy()
		cursorBuf = nil
	}
	if cursorData != nil {
		wos.Munmap(cursorData)
		cursorData = nil
	}
	cursorReady = false

	artH := len(cursorArrow)
	artW := 0
	for _, row := range cursorArrow {
		if len(row) > artW {
			artW = len(row)
		}
	}
	w, hgt := artW*cs, artH*cs
	stride := w * 4
	size := stride * hgt

	fd, err := wos.CreateAnonymousFile(int64(size))
	if err != nil {
		perfLog("[wl] cursor file: %v", err)
		return
	}
	defer fd.Close()

	data, err := wos.Mmap(int(fd.Fd()), 0, size, wos.ProtRead|wos.ProtWrite, wos.MapShared)
	if err != nil {
		return
	}
	// ARGB8888 premultiplied (byte order B,G,R,A). Each art cell is a cs×cs block.
	put := func(px, py int, b, g, r, a byte) {
		o := py*stride + px*4
		data[o], data[o+1], data[o+2], data[o+3] = b, g, r, a
	}
	for ay, row := range cursorArrow {
		for ax := 0; ax < len(row); ax++ {
			var b, g, r, a byte
			switch row[ax] {
			case '#':
				b, g, r, a = 235, 235, 235, 255 // light outline
			case '.':
				b, g, r, a = 20, 20, 20, 255 // dark fill
			default:
				continue
			}
			for dy := 0; dy < cs; dy++ {
				for dx := 0; dx < cs; dx++ {
					put(ax*cs+dx, ay*cs+dy, b, g, r, a)
				}
			}
		}
	}

	pool, err := shm.CreatePool(fd.Fd(), int32(size))
	if err != nil {
		wos.Munmap(data)
		return
	}
	buf, err := pool.CreateBuffer(0, int32(w), int32(hgt), int32(stride), wl.ShmFormatArgb8888)
	pool.Destroy()
	if err != nil {
		wos.Munmap(data)
		return
	}

	if cursorSurface == nil {
		surf, err := compositor.CreateSurface()
		if err != nil {
			buf.Destroy()
			wos.Munmap(data)
			return
		}
		cursorSurface = surf
	}
	if compositorVer >= 3 {
		cursorSurface.SetBufferScale(int32(cs)) // buffer is cs× the logical cursor size
	}
	cursorSurface.Attach(buf, 0, 0)
	cursorSurface.Damage(0, 0, int32(artW), int32(artH)) // surface (logical) coords
	cursorSurface.Commit()

	cursorBuf = buf
	cursorData = data
	cursorHotX, cursorHotY = 0, 0 // tip at top-left, in logical coords
	cursorScale = cs
	cursorReady = true
}

var cursorShapeDisabled = os.Getenv("SHIREI_WL_NO_CURSOR_SHAPE") != ""

// themed-cursor cache, per integer scale. The Theme is retained because the
// image buffers live in its shm pool (and os.File would close the fd when
// collected); tried marks scales that failed so we don't retry every enter.
var (
	themedTheme = map[int]*wlcursor.Theme{}
	themedImage = map[int]*wlcursor.ImageBuffer{}
	themedTried = map[int]bool{}
)

// themedCursorImage loads (once per scale) the user's themed arrow via the
// vendored xcursor loader: XCURSOR_THEME (or "default", which resolves
// through index.theme Inherits chains), XCURSOR_SIZE (or 24) times the
// output scale.
func themedCursorImage(cs int) *wlcursor.ImageBuffer {
	if themedTried[cs] {
		return themedImage[cs]
	}
	themedTried[cs] = true

	name := os.Getenv("XCURSOR_THEME")
	if name == "" {
		name = "default"
	}
	base := 24
	if v, err := strconv.Atoi(os.Getenv("XCURSOR_SIZE")); err == nil && v > 0 {
		base = v
	}
	size := uint32(base * cs)

	theme, err := wlcursor.LoadThemeFromName(name, size, shm)
	if err != nil {
		wlDebug("themed cursor: theme pool failed: %v", err)
		return nil
	}
	cur, err := theme.GetCursor(wlcursor.LeftPtr)
	if err != nil {
		cur, err = theme.GetCursor("default")
	}
	if err != nil {
		wlDebug("themed cursor: no arrow in theme %q: %v", name, err)
		theme.Destroy()
		return nil
	}
	img := cur.GetCursorImage(0)
	if img == nil {
		theme.Destroy()
		return nil
	}
	themedTheme[cs] = theme
	themedImage[cs] = img
	wlDebug("themed cursor: %q size=%d -> %dx%d hotspot=(%d,%d)",
		name, size, img.GetWidth(), img.GetHeight(), img.GetHotspotX(), img.GetHotspotY())
	return img
}

// attachThemedCursor points the cursor surface at the themed buffer and
// installs it for this enter serial. Buffer pixels are physical; the
// hotspot and damage are surface-local (logical), hence the /cs.
func attachThemedCursor(serial uint32, img *wlcursor.ImageBuffer, cs int) bool {
	if cursorSurface == nil {
		surf, err := compositor.CreateSurface()
		if err != nil {
			return false
		}
		cursorSurface = surf
	}
	if compositorVer >= 3 {
		cursorSurface.SetBufferScale(int32(cs))
	}
	cursorSurface.Attach(img.GetBuffer(), 0, 0)
	cursorSurface.Damage(0, 0, int32(img.GetWidth()/cs)+1, int32(img.GetHeight()/cs)+1)
	cursorSurface.Commit()
	pointer.SetCursor(serial, cursorSurface,
		int32(img.GetHotspotX()/cs), int32(img.GetHotspotY()/cs))
	return true
}

// applyCursor sets the cursor for this enter serial: compositor-drawn shape,
// else themed xcursor, else the drawn arrow (built lazily, rebuilt on scale
// changes).
func applyCursor(serial uint32) {
	if pointer == nil {
		return
	}
	if !cursorShapeDisabled {
		ensureCursorShapeDevice()
		if cursorShapeDev != nil {
			cursorShapeDev.SetShape(serial, cursorshape.ShapeDefault)
			return
		}
	}
	cs := int(windowScale)
	if cs < 1 {
		cs = 1
	}
	if img := themedCursorImage(cs); img != nil && attachThemedCursor(serial, img, cs) {
		return
	}
	if !cursorReady || cursorScale != cs {
		buildCursor(cs)
	}
	if cursorReady {
		pointer.SetCursor(serial, cursorSurface, cursorHotX, cursorHotY)
	}
}
