//go:build linux || (darwin && x11darwin)

package x11backend

import (
	"runtime"
	"time"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/shm"
	"github.com/jezek/xgb/xproto"

	"go.hasen.dev/shirei"
)

// glyphCacheBudget caps total cached glyph-bitmap bytes (enables the shared core
// glyph cache). Matches the other backends.
const glyphCacheBudget = 16 << 20

var (
	winTitle string
	winW     int
	winH     int
	frameFn  shirei.FrameFn

	X      *xgb.Conn
	screen *xproto.ScreenInfo
	win    xproto.Window
	gc     xproto.Gcontext
	depth  byte
	maxReq int // maximum request length in bytes

	wmProtocols xproto.Atom
	wmDelete    xproto.Atom

	softRenderer shirei.SoftRenderer
	presentBuf   []byte // BGRA device-pixel buffer the renderer writes into
	curW, curH   int
	windowScale  float32 = 1 // device px per logical point (from Xft.dpi)

	wantsFrame bool // last frame asked to be re-run (animation/async work)
	quit       bool
)

// SetupWindow records the window parameters. The window is created in Run.
func SetupWindow(title string, width, height int) {
	winTitle = title
	winW = width
	winH = height
}

// Run connects to the X server, opens a window, and runs the event loop. It must
// be called from the program's main goroutine and does not return until the
// window is closed.
func Run(fn shirei.FrameFn) {
	runtime.LockOSThread()
	frameFn = fn

	shirei.GlyphCacheBudgetBytes = glyphCacheBudget

	conn, err := xgb.NewConn()
	if err != nil {
		panic("x11backend: cannot connect to X server: " + err.Error())
	}
	X = conn
	defer X.Close()

	setup := xproto.Setup(X)
	screen = setup.DefaultScreen(X)
	depth = screen.RootDepth
	maxReq = int(setup.MaximumRequestLength) * 4

	loadKeymap()
	windowScale = detectScale()
	perfLog("[x11] Xft.dpi scale: %.2f", windowScale)
	createWindow()
	initClipboard()
	imeInit()
	defer imeClose()
	useShm = initShm()
	perfLog("[x11] MIT-SHM extension: %v", useShm)
	defer releaseShm()
	eventLoop()
}

func createWindow() {
	var err error
	win, err = xproto.NewWindowId(X)
	if err != nil {
		panic("x11backend: NewWindowId: " + err.Error())
	}

	// X window geometry is in device pixels; the caller's size is logical points,
	// so scale it up under HiDPI. curW/curH track the device size from here on.
	curW = int(float32(winW)*windowScale + 0.5)
	curH = int(float32(winH)*windowScale + 0.5)

	mask := uint32(xproto.CwBackPixel | xproto.CwEventMask)
	values := []uint32{
		screen.BlackPixel,
		uint32(xproto.EventMaskExposure |
			xproto.EventMaskKeyPress | xproto.EventMaskKeyRelease |
			xproto.EventMaskButtonPress | xproto.EventMaskButtonRelease |
			xproto.EventMaskPointerMotion |
			xproto.EventMaskStructureNotify |
			xproto.EventMaskFocusChange),
	}
	xproto.CreateWindow(X, depth, win, screen.Root,
		0, 0, uint16(curW), uint16(curH), 0,
		xproto.WindowClassInputOutput, screen.RootVisual, mask, values)

	// Window title. WM_NAME is the legacy Latin-1 property; it mangles non-ASCII
	// (e.g. "•"), so we also set _NET_WM_NAME as UTF-8, which is what modern EWMH
	// window managers actually display.
	xproto.ChangeProperty(X, xproto.PropModeReplace, win,
		xproto.AtomWmName, xproto.AtomString, 8,
		uint32(len(winTitle)), []byte(winTitle))
	if netName, utf8 := internAtom("_NET_WM_NAME"), internAtom("UTF8_STRING"); netName != 0 && utf8 != 0 {
		xproto.ChangeProperty(X, xproto.PropModeReplace, win,
			netName, utf8, 8, uint32(len(winTitle)), []byte(winTitle))
	}

	setIconProperty()

	// Ask the window manager to send us a ClientMessage on close instead of
	// killing the connection.
	wmProtocols = internAtom("WM_PROTOCOLS")
	wmDelete = internAtom("WM_DELETE_WINDOW")
	delBytes := []byte{
		byte(wmDelete), byte(wmDelete >> 8), byte(wmDelete >> 16), byte(wmDelete >> 24),
	}
	xproto.ChangeProperty(X, xproto.PropModeReplace, win,
		wmProtocols, xproto.AtomAtom, 32, 1, delBytes)

	gc, err = xproto.NewGcontextId(X)
	if err != nil {
		panic("x11backend: NewGcontextId: " + err.Error())
	}
	xproto.CreateGC(X, gc, xproto.Drawable(win), 0, nil)

	xproto.MapWindow(X, win)
	X.Sync()
}

func internAtom(name string) xproto.Atom {
	r, err := xproto.InternAtom(X, false, uint16(len(name)), name).Reply()
	if err != nil || r == nil {
		return 0
	}
	return r.Atom
}

// eventLoop reads X events on a background goroutine and drives frames on the
// main goroutine, so animation (a ticker) and input share one place. Producing a
// frame and presenting it always happen on the main goroutine.
func eventLoop() {
	evCh := make(chan xgb.Event, 256)
	go func() {
		defer close(evCh)
		for {
			ev, err := X.WaitForEvent()
			if ev == nil && err == nil {
				return // connection closed
			}
			if ev != nil {
				evCh <- ev
			}
		}
	}()

	ticker := time.NewTicker(16 * time.Millisecond)
	defer ticker.Stop()

	dirty := true // produce + present the first frame
	for !quit {
		// Block until an event arrives or the animation tick fires.
		select {
		case ev, ok := <-evCh:
			if !ok {
				return
			}
			if handleEvent(ev) {
				dirty = true
			}
		case <-ticker.C:
			// wantsFrame covers in-frame animation; FrameRequested covers
			// background RequestNextFrame (sampler loops, LogView appends,
			// async image decode) when the last frame settled to idle.
			// imeNeedsFrame covers IBus preedit/commit signals from the
			// D-Bus goroutine.
			if wantsFrame || shirei.FrameRequested() || imeNeedsFrame() {
				dirty = true
			}
		}

		// Coalesce: pull every queued event into this one frame so a burst of
		// motion events never backs up behind a slow present. Input is sampled,
		// not queued — only the latest mouse position/state matters per frame.
		drained := false
		for !drained {
			select {
			case ev, ok := <-evCh:
				if !ok {
					return
				}
				if handleEvent(ev) {
					dirty = true
				}
			default:
				drained = true
			}
		}

		if dirty && !quit {
			frame()
			dirty = false
		}
	}
}

// frame runs one shirei frame and presents it.
func frame() {
	if curW == 0 || curH == 0 {
		curW, curH = winW, winH // until the first ConfigureNotify
	}
	scale := windowScale
	if scale <= 0 {
		scale = 1
	}
	shirei.WindowScale = scale
	shirei.WindowSize = shirei.Vec2{float32(curW) / scale, float32(curH) / scale}

	// Deliver paste + IME commits before frameFn consumes input.
	injectPendingPaste()
	flushPendingText()

	t0 := time.Now()
	out := shirei.RunFrameFn(frameFn)
	perfRecordProduce(time.Since(t0))

	updateIMECursor()

	if out.Copy != "" {
		setClipboard(out.Copy)
	}
	if out.Paste {
		requestPaste() // async: the result arrives as a SelectionNotify, next frame
	}

	ensureBuf()
	t1 := time.Now()
	softRenderer.RenderInto(presentBuf, curW*4, curW, curH, scale, out.Surfaces)
	present()
	perfRecordPaint(time.Since(t1))

	wantsFrame = out.NextFrameRequested
}

func ensureBuf() {
	if useShm {
		if b := ensureShm(curW, curH); b != nil {
			presentBuf = b
			presentViaShm = true
			return
		}
	}
	presentViaShm = false
	n := curW * curH * 4
	if cap(presentBuf) < n {
		presentBuf = make([]byte, n)
	} else {
		presentBuf = presentBuf[:n]
	}
}

// present blits the rendered BGRA buffer to the window via PutImage, split into
// horizontal bands so each request fits under the server's maximum request size.
// The renderer's BGRA premultiplied pixels map directly onto a depth-24 ZPixmap
// (X reads B,G,R and ignores the 4th byte); the window is opaque.
func present() {
	stride := curW * 4
	if stride == 0 {
		return
	}

	// Fast path: the renderer wrote straight into the shared segment, so we send
	// only a small request that references it — no pixels over the socket.
	if presentViaShm {
		shm.PutImage(X, xproto.Drawable(win), gc,
			uint16(curW), uint16(curH), // total
			0, 0, uint16(curW), uint16(curH), // src x,y,w,h
			0, 0, // dst x,y
			depth, xproto.ImageFormatZPixmap, 0, // depth, format, no completion event
			shmSeg, 0)
		X.Sync() // server reads the shm before we render the next frame
		return
	}

	// Fallback: serialize the buffer into PutImage requests, banded to fit the
	// server's maximum request size.
	rows := (maxReq - 64) / stride
	if rows < 1 {
		rows = 1
	}
	for y := 0; y < curH; y += rows {
		h := rows
		if y+h > curH {
			h = curH - y
		}
		data := presentBuf[y*stride : (y+h)*stride]
		xproto.PutImage(X, xproto.ImageFormatZPixmap, xproto.Drawable(win), gc,
			uint16(curW), uint16(h), 0, int16(y), 0, depth, data)
	}
	X.Sync() // flush the request buffer so the frame actually shows
}
