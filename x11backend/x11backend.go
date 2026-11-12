//go:build linux || (darwin && x11darwin)

package x11backend

import (
	"fmt"
	"runtime"

	"time"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/shm"
	"github.com/jezek/xgb/xproto"

	g "go.hasen.dev/generic"
	"go.hasen.dev/shirei"
)

// glyphCacheBudget caps total cached glyph-bitmap bytes (enables the shared core
// glyph cache). Matches the other backends.
const glyphCacheBudget = 16 << 20

var (
	X                     *xgb.Conn
	screen                *xproto.ScreenInfo
	maxReq                int
	wmProtocols, wmDelete xproto.Atom
)

// SetupWindow records the window parameters. The window is created in Run.
func SetupWindow(title string, width, height int) {
	currentWindow.winTitle = title
	currentWindow.winW = width
	currentWindow.winH = height
}

// SetupQuiet maps the window without asking the WM to take input focus
// (_NET_WM_USER_TIME = 0). Call before Run.
func SetupQuiet() {
	currentWindow.winQuiet = true
}

// Run connects to the X server, opens a window, and runs the event loop. It
// must be called from the program's main goroutine and does not return:
// window close calls generic.ExitWithCleanup so AddExitCleanup handlers run.
func Run(fn shirei.FrameFn) {
	runtime.LockOSThread()
	currentWindow.frameFn = fn
	uiThreadID.Store(threadID())
	mainWindow.unsubscribe = shirei.RegisterWindowUI(mainWindow.ui)

	shirei.GetHost().GlyphCacheBudgetBytes = glyphCacheBudget
	shirei.GetHost().EscapeHatchBackendContext = Context{window: mainWindow}

	conn, err := xgb.NewConn()
	if err != nil {
		panic("x11backend: cannot connect to X server: " + err.Error())
	}
	X = conn
	defer X.Close()

	setup := xproto.Setup(X)
	screen = setup.DefaultScreen(X)
	currentWindow.depth = screen.RootDepth
	maxReq = int(setup.MaximumRequestLength) * 4
	argbVisual = detectARGBVisual()

	loadKeymap()
	currentWindow.windowScale = detectScale()
	shirei.GetHost().WindowScale = currentWindow.windowScale
	perfLog("[x11] Xft.dpi scale: %.2f", currentWindow.windowScale)
	if err := createWindow(); err != nil {
		panic(err)
	}
	windowsByID[currentWindow.win] = mainWindow
	initClipboard()
	imeInit()
	defer imeClose()
	useShm = initShm()
	perfLog("[x11] MIT-SHM extension: %v", useShm)
	defer releaseShm()
	eventLoop()
	// os.Exit skips the defers above; close in the same LIFO order.
	releaseShm()
	imeClose()
	X.Close()
	g.ExitWithCleanup(0)
}

func createWindow() error {
	var err error
	created := false
	success := false
	defer func() {
		if success {
			return
		}
		if created {
			xproto.DestroyWindow(X, currentWindow.win)
		}
		if currentWindow.colormap != 0 {
			xproto.FreeColormap(X, currentWindow.colormap)
		}
	}()
	currentWindow.win, err = xproto.NewWindowId(X)
	if err != nil {
		return fmt.Errorf("x11backend: NewWindowId: %w", err)
	}

	// X window geometry is in device pixels; the caller's size is logical points,
	// so scale it up under HiDPI. curW/curH track the device size from here on.
	currentWindow.curW = int(float32(currentWindow.winW)*currentWindow.windowScale + 0.5)
	currentWindow.curH = int(float32(currentWindow.winH)*currentWindow.windowScale + 0.5)

	if currentWindow.curW <= 0 || currentWindow.curH <= 0 || currentWindow.curW > 65535 || currentWindow.curH > 65535 {
		return fmt.Errorf("X11 content dimensions must fit in 65535 device pixels")
	}

	posX, posY := int16(0), int16(0)
	visual := screen.RootVisual
	currentWindow.depth = screen.RootDepth
	if shirei.GetHost().TransparentBackground {
		visual = argbVisual
		if visual == 0 {
			return fmt.Errorf("x11backend: no 32-bit ARGB visual")
		}
		currentWindow.depth = 32
	}
	currentWindow.colormap, err = xproto.NewColormapId(X)
	if err != nil {
		return err
	}
	if err := xproto.CreateColormapChecked(X, xproto.ColormapAllocNone, currentWindow.colormap, screen.Root, visual).Check(); err != nil {
		return err
	}

	mask := uint32(xproto.CwBackPixel | xproto.CwBorderPixel | xproto.CwEventMask | xproto.CwColormap)
	values := []uint32{
		0,
		0,
		uint32(xproto.EventMaskExposure |
			xproto.EventMaskKeyPress | xproto.EventMaskKeyRelease |
			xproto.EventMaskButtonPress | xproto.EventMaskButtonRelease |
			xproto.EventMaskPointerMotion |
			xproto.EventMaskStructureNotify |
			xproto.EventMaskFocusChange),
		uint32(currentWindow.colormap),
	}
	err = xproto.CreateWindowChecked(X, currentWindow.depth, currentWindow.win, screen.Root,
		posX, posY, uint16(currentWindow.curW), uint16(currentWindow.curH), 0,
		xproto.WindowClassInputOutput, visual, mask, values).Check()
	if err != nil {
		return err
	}

	created = true

	// Window title. WM_NAME is the legacy Latin-1 property; it mangles non-ASCII
	// (e.g. "•"), so we also set _NET_WM_NAME as UTF-8, which is what modern EWMH
	// window managers actually display.
	xproto.ChangeProperty(X, xproto.PropModeReplace, currentWindow.win,
		xproto.AtomWmName, xproto.AtomString, 8,
		uint32(len(currentWindow.winTitle)), []byte(currentWindow.winTitle))
	if netName, utf8 := internAtom("_NET_WM_NAME"), internAtom("UTF8_STRING"); netName != 0 && utf8 != 0 {
		xproto.ChangeProperty(X, xproto.PropModeReplace, currentWindow.win,
			netName, utf8, 8, uint32(len(currentWindow.winTitle)), []byte(currentWindow.winTitle))
	}

	setIconProperty()

	// Ask the window manager to send us a ClientMessage on close instead of
	// killing the connection.
	wmProtocols = internAtom("WM_PROTOCOLS")
	wmDelete = internAtom("WM_DELETE_WINDOW")
	delBytes := []byte{
		byte(wmDelete), byte(wmDelete >> 8), byte(wmDelete >> 16), byte(wmDelete >> 24),
	}
	xproto.ChangeProperty(X, xproto.PropModeReplace, currentWindow.win,
		wmProtocols, xproto.AtomAtom, 32, 1, delBytes)

	currentWindow.gc, err = xproto.NewGcontextId(X)
	if err != nil {
		return fmt.Errorf("x11backend: NewGcontextId: %w", err)
	}
	if err := xproto.CreateGCChecked(X, currentWindow.gc, xproto.Drawable(currentWindow.win), 0, nil).Check(); err != nil {
		return err
	}

	if currentWindow.winQuiet {
		if a := internAtom("_NET_WM_USER_TIME"); a != 0 {
			xproto.ChangeProperty(X, xproto.PropModeReplace, currentWindow.win,
				a, xproto.AtomCardinal, 32, 1, []byte{0, 0, 0, 0})
		}
	}

	if currentWindow.undecorated {
		data := make([]byte, 20)
		xgb.Put32(data, 2)
		xproto.ChangeProperty(X, xproto.PropModeReplace, currentWindow.win, internAtom("_MOTIF_WM_HINTS"), internAtom("_MOTIF_WM_HINTS"), 32, 5, data)
	}
	success = true
	if currentWindow != mainWindow {
		return nil
	}
	xproto.MapWindow(X, currentWindow.win)
	X.Sync()
	return nil
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

	for {
		// Block until an event arrives or the animation tick fires.
		select {
		case ev, ok := <-evCh:
			if !ok {
				return
			}
			dispatchEvent(ev)
		case <-ticker.C:
			// Poll per-window animation, background requests, and queued closes.
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
				dispatchEvent(ev)
			default:
				drained = true
			}
		}

		updatePopupWindows()
		drainIMESignals()
		for _, w := range liveWindows() {
			leave := enterWindow(w)
			if w.dirty || currentWindow.wantsFrame || shirei.FrameRequested() {
				frame()
				w.dirty = false
			}
			leave()
		}
	}
}

// frame runs one shirei frame and presents it.
func frame() {
	if currentWindow.curW == 0 || currentWindow.curH == 0 {
		currentWindow.curW, currentWindow.curH = currentWindow.winW, currentWindow.winH // until the first ConfigureNotify
	}
	scale := currentWindow.windowScale
	if scale <= 0 {
		scale = 1
	}
	shirei.GetHost().WindowScale = scale
	shirei.GetHost().WindowSize = shirei.Vec2{float32(currentWindow.curW) / scale, float32(currentWindow.curH) / scale}

	// Deliver paste + IME commits before frameFn consumes input.
	injectPendingPaste()
	flushPendingText()

	out := shirei.RunFrameFnLocked(currentWindow.frameFn)

	if focusedWindow == currentWindow {
		updateIMECursor()
	}

	if out.Copy != "" {
		setClipboard(out.Copy)
	}
	if out.Paste {
		requestPaste() // async: the result arrives as a SelectionNotify, next frame
	}
	if out.OpenURL != "" {
		openURL(out.OpenURL)
	}

	ensureBuf()
	currentWindow.softRenderer.RenderInto(currentWindow.presentBuf, currentWindow.curW*4, currentWindow.curW, currentWindow.curH, scale, out.Surfaces, out.GlyphRuns)
	present()
	t := &shirei.ActiveUI().FrameTimings
	t.Painted = true
	t.PaintEnd = time.Now()
	shirei.EmitFrameMetrics()

	currentWindow.wantsFrame = out.NextFrameRequested
}

func ensureBuf() {
	if useShm {
		if b := ensureShm(currentWindow.curW, currentWindow.curH); b != nil {
			currentWindow.presentBuf = b
			currentWindow.presentViaShm = true
			return
		}
	}
	if useShm {
		currentWindow.presentBuf = nil
	}
	currentWindow.presentViaShm = false
	n := currentWindow.curW * currentWindow.curH * 4
	if cap(currentWindow.presentBuf) < n {
		currentWindow.presentBuf = make([]byte, n)
	} else {
		currentWindow.presentBuf = currentWindow.presentBuf[:n]
	}
}

// present blits the rendered BGRA buffer to the window via PutImage, split into
// horizontal bands so each request fits under the server's maximum request size.
// The renderer's BGRA premultiplied pixels map directly onto a depth-24 ZPixmap
// (X reads B,G,R and ignores the 4th byte), or depth-32 ARGB for transparency.
func present() {
	stride := currentWindow.curW * 4
	if stride == 0 {
		return
	}

	// Fast path: the renderer wrote straight into the shared segment, so we send
	// only a small request that references it — no pixels over the socket.
	if currentWindow.presentViaShm {
		shm.PutImage(X, xproto.Drawable(currentWindow.win), currentWindow.gc,
			uint16(currentWindow.curW), uint16(currentWindow.curH), // total
			0, 0, uint16(currentWindow.curW), uint16(currentWindow.curH), // src x,y,w,h
			0, 0, // dst x,y
			currentWindow.depth, xproto.ImageFormatZPixmap, 0, // depth, format, no completion event
			currentWindow.shmSeg, 0)
		X.Sync() // server reads the shm before we render the next frame
		return
	}

	// Fallback: serialize the buffer into PutImage requests, banded to fit the
	// server's maximum request size.
	rows := (maxReq - 64) / stride
	if rows < 1 {
		rows = 1
	}
	for y := 0; y < currentWindow.curH; y += rows {
		h := rows
		if y+h > currentWindow.curH {
			h = currentWindow.curH - y
		}
		data := currentWindow.presentBuf[y*stride : (y+h)*stride]
		xproto.PutImage(X, xproto.ImageFormatZPixmap, xproto.Drawable(currentWindow.win), currentWindow.gc,
			uint16(currentWindow.curW), uint16(h), 0, int16(y), 0, currentWindow.depth, data)
	}
	X.Sync() // flush the request buffer so the frame actually shows
}
