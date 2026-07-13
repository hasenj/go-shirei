//go:build linux

package waylandbackend

import (
	"fmt"
	"os"
	"time"

	wos "go.hasen.dev/shirei/internal/wayland/os"
	"go.hasen.dev/shirei/internal/wayland/wl"
	"go.hasen.dev/shirei/internal/wayland/wlclient"
	zxdg "go.hasen.dev/shirei/internal/wayland/xdg"

	"go.hasen.dev/shirei"
)

// glyphCacheBudget caps total cached glyph-bitmap bytes (enables the shared core
// glyph cache). Matches the other backends.
const glyphCacheBudget = 16 << 20

var (
	winTitle    string
	winIconPath string
	winW        int
	winH        int
	frameFn     shirei.FrameFn

	disp          *wl.Display
	registry      *wl.Registry
	compositor    *wl.Compositor
	compositorVer uint32 // bound wl_compositor version (set_buffer_scale needs >=3)
	wmBase        *zxdg.WmBase
	shm           *wl.Shm
	hasXRGB       bool

	surface     *wl.Surface
	xdgSurface  *zxdg.Surface
	xdgToplevel *zxdg.Toplevel

	logicalW, logicalH int         // surface (logical) size, in points
	curW, curH         int         // device-pixel buffer size = logical * scale
	windowScale        float32 = 1 // device px per logical point (wl_output scale)

	seat          *wl.Seat
	pointer       *wl.Pointer
	pointerSerial uint32 // last pointer event serial (for cursor / CSD move+resize)

	buffers       [2]wlBuffer
	frameCb       *wl.Callback
	waitConfigure bool
	wantsFrame    bool
	dirty         bool // input/state changed; redraw when no frame callback is pending
	quit          bool

	softRenderer shirei.SoftRenderer
)

// wlBuffer is one of the double-buffered wl_shm buffers the renderer draws into.
// It owns the mmap'd shared memory and the wl_buffer that references it.
type wlBuffer struct {
	buf  *wl.Buffer
	data []byte // mmap'd BGRA pixels the X server / compositor reads
	busy bool   // attached + committed, not yet released by the compositor
	w, h int
}

// HandleBufferRelease: the compositor is done reading this buffer; reuse is safe.
func (b *wlBuffer) HandleBufferRelease(wl.BufferReleaseEvent) { b.busy = false }

// SetupWindow records the window parameters. The window is created in Run.
func SetupWindow(title string, width, height int) {
	winTitle = title
	winW, winH = width, height
}

// SetupIcon records the path of the image (PNG etc.) used as the window icon.
// Call it before Run. Applied via the staging xdg-toplevel-icon-v1 protocol
// (hand-bound in waylandicon_linux.go) on compositors that ship it (KDE,
// labwc, ...); elsewhere — notably GNOME — the only route to an icon is a
// .desktop file whose name matches the toplevel's app_id, which this backend
// sets to the executable's base name.
func SetupIcon(imagePath string) {
	winIconPath = imagePath
}

// Run connects to the Wayland compositor, opens a window, and runs the dispatch
// loop. It must be called from the program's main goroutine and does not return
// until the window is closed. Everything (input + frame production) happens on
// this one goroutine: Wayland delivers events synchronously inside DisplayDispatch.
func Run(fn shirei.FrameFn) {
	frameFn = fn

	shirei.GlyphCacheBudgetBytes = glyphCacheBudget

	connect()
	// No icon protocol in the registry (GNOME): bridge the icon over a hidden
	// .desktop file matched by app_id, written before the window maps so the
	// shell can pick it up (see waylanddesktop_linux.go).
	if iconMgr == nil {
		ensureDesktopEntry(appID())
	}
	createWindow()

	if csdEnabled {
		// Draw the client-side titlebar transparently above every app's content.
		shirei.DecorationFn = drawTitlebar
		shirei.DecorationHeight = titlebarHeight
	}

	// Pump events until the toplevel is closed. Wayland delivers a batch of
	// events per DisplayDispatch; input handlers update the shirei globals and set
	// `dirty`. While animating, the frame callback drives redraws (and consumes
	// input); when idle, a redraw here reacts to input the moment it arrives.
	//
	// Dispatch is time-bounded (~60 Hz) so a background RequestNextFrame — e.g.
	// process_monitor's sampler, LogView appends, async image decode — is
	// noticed without requiring pointer motion. Cocoa's CADisplayLink does the
	// same via shireiFrameRequested(); a pure blocking pump would sleep forever
	// once wantsFrame settles. Timeout wakes do no produce/paint work unless
	// dirty or FrameRequested. (RunTimeout cannot split events: the deadline
	// only guards the header wait.)
	//
	// (A held-modifier staleness on the Parallels VM was chased to Parallels
	// itself withholding lone modifier presses from the guest — no client-side
	// dispatch trick can help. SHIREI_WL_DEBUG prints event timing.)
	const framePoll = 16 * time.Millisecond
	wlDebug("wl backend build: 2026-07-13-idle-frame-wake (timeout dispatch)")
	for !quit {
		err := wlclient.DisplayDispatchTimeout(disp, framePoll)
		if err != nil && err != wl.ErrContextRunTimeout && err != wl.ErrContextRunProxyNil {
			// Always to stderr: exiting the GUI loop is fatal for the app, and
			// after a protocol error this is the only trace of what happened.
			fmt.Fprintf(os.Stderr, "waylandbackend: display dispatch failed: %v\n", err)
			break
		}
		// Background goroutines set the RequestNextFrame flag; pick it up here
		// the same way cocoa's tick checks shireiFrameRequested().
		if shirei.FrameRequested() {
			dirty = true
		}
		if dirty && frameCb == nil && !waitConfigure && !quit {
			drawFrame()
		}
	}
}

var h = &handler{}

// handler implements the listener callbacks for the singleton objects (registry,
// shm, xdg_wm_base, xdg_surface, xdg_toplevel, frame callback).
type handler struct{}

func connect() {
	d, err := wlclient.DisplayConnect(nil) // honors $WAYLAND_DISPLAY
	if err != nil {
		panic("waylandbackend: cannot connect to Wayland: " + err.Error())
	}
	disp = d
	disp.AddErrorHandler(h)

	reg, err := disp.GetRegistry()
	if err != nil {
		panic("waylandbackend: GetRegistry: " + err.Error())
	}
	registry = reg
	wlclient.RegistryAddListener(registry, h)

	// First roundtrip: receive the globals and bind them.
	if err := wlclient.DisplayRoundtrip(disp); err != nil {
		panic("waylandbackend: roundtrip(globals): " + err.Error())
	}
	if compositor == nil || shm == nil || wmBase == nil {
		panic("waylandbackend: compositor/shm/xdg_wm_base missing")
	}
	// Second roundtrip: receive the shm format advertisements.
	if err := wlclient.DisplayRoundtrip(disp); err != nil {
		panic("waylandbackend: roundtrip(formats): " + err.Error())
	}
	if !hasXRGB {
		panic("waylandbackend: wl_shm XRGB8888 format unavailable")
	}
	perfLog("[wl] connected; compositor+shm+xdg_wm_base bound, XRGB8888 ok")
}

func (*handler) HandleRegistryGlobal(ev wl.RegistryGlobalEvent) {
	switch ev.Interface {
	case "wl_compositor":
		// Need v3 for wl_surface.set_buffer_scale (HiDPI); bind as high as offered, capped.
		compositorVer = ev.Version
		if compositorVer > 4 {
			compositorVer = 4
		}
		compositor = wlclient.RegistryBindCompositorInterface(registry, ev.Name, compositorVer)
	case "wl_shm":
		shm = wlclient.RegistryBindShmInterface(registry, ev.Name, 1)
		wlclient.ShmAddListener(shm, h)
	case "xdg_wm_base":
		wmBase = wlclient.RegistryBindWmBaseInterface(registry, ev.Name, 1)
		zxdg.WmBaseAddListener(wmBase, h)
	case "wl_seat":
		seat = wlclient.RegistryBindSeatInterface(registry, ev.Name, 1)
		wlclient.SeatAddListener(seat, h)
	case "wl_output":
		v := ev.Version // need v2 for the scale event; clamp to what's advertised
		if v > 2 {
			v = 2
		}
		bindOutput(ev.Name, v)
	case "wp_cursor_shape_manager_v1":
		bindCursorShapeManager(ev.Name, 1) // real themed cursor (preferred over our arrow)
	case "xdg_toplevel_icon_manager_v1":
		bindToplevelIconManager(ev.Name, 1) // hand the compositor a real icon image
	case "wl_data_device_manager":
		v := ev.Version
		if v > 3 {
			v = 3
		}
		bindDataDeviceManager(ev.Name, v)
	case "zwp_text_input_manager_v3":
		bindTextInputManager(ev.Name, ev.Version) // IME via text-input-v3
	}
}

func (*handler) HandleRegistryGlobalRemove(wl.RegistryGlobalRemoveEvent) {}

// HandleDisplayError surfaces wl_display.error, the compositor's fatal
// protocol complaint. Always to stderr: without this handler the message (the
// only diagnostic Wayland gives) would vanish and the app would just exit its
// loop silently.
func (*handler) HandleDisplayError(ev wl.DisplayErrorEvent) {
	var id uint32
	if ev.ObjectId != nil {
		id = uint32(ev.ObjectId.Id())
	}
	fmt.Fprintf(os.Stderr, "waylandbackend: protocol error: object %d, code %d: %s\n",
		id, ev.Code, ev.Message)
}

func (*handler) HandleShmFormat(ev wl.ShmFormatEvent) {
	if ev.Format == wl.ShmFormatXrgb8888 {
		hasXRGB = true
	}
}

// HandleWmBasePing: keep the compositor's liveness check happy.
func (*handler) HandleWmBasePing(ev zxdg.WmBasePingEvent) {
	wmBase.Pong(ev.Serial)
}

func createWindow() {
	logicalW, logicalH = winW, winH
	recomputeDeviceSize() // honors a scale already learned during connect

	var err error
	if surface, err = compositor.CreateSurface(); err != nil {
		panic("waylandbackend: CreateSurface: " + err.Error())
	}
	if xdgSurface, err = wmBase.GetSurface(surface); err != nil {
		panic("waylandbackend: GetSurface: " + err.Error())
	}
	xdgSurface.AddListener(h)
	if xdgToplevel, err = xdgSurface.GetToplevel(); err != nil {
		panic("waylandbackend: GetToplevel: " + err.Error())
	}
	zxdg.ToplevelAddListener(xdgToplevel, h)
	xdgToplevel.SetTitle(winTitle)
	// app_id is how compositors identify the app (window grouping, and the
	// .desktop file lookup that supplies the icon — see waylanddesktop_linux.go).
	xdgToplevel.SetAppId(appID())
	setToplevelIcon()
	if compositorVer >= 3 { // set_buffer_scale exists from wl_compositor v3
		surface.SetBufferScale(int32(windowScale)) // apply any scale learned during connect
	}

	// Commit without a buffer to trigger the initial configure handshake; the
	// first frame is drawn once the compositor acks it.
	surface.Commit()
	waitConfigure = true
	perfLog("[wl] window created; committed, awaiting first configure")
}

// HandleSurfaceConfigure: ack the configure and, on the first one, kick off
// rendering.
func (*handler) HandleSurfaceConfigure(ev zxdg.SurfaceConfigureEvent) {
	xdgSurface.AckConfigure(ev.Serial)
	if waitConfigure {
		waitConfigure = false
		perfLog("[wl] first configure acked; drawing first frame")
		drawFrame()
	}
}

// HandleToplevelConfigure carries the compositor's requested size (0 = client
// chooses). On a real size change we adopt it; buffers are recreated lazily.
func (*handler) HandleToplevelConfigure(ev zxdg.ToplevelConfigureEvent) {
	// Configure sizes are in logical points; the device buffer is scale times that.
	if ev.Width > 0 && ev.Height > 0 && (int(ev.Width) != logicalW || int(ev.Height) != logicalH) {
		logicalW, logicalH = int(ev.Width), int(ev.Height)
		recomputeDeviceSize()
	}
}

func (*handler) HandleToplevelClose(zxdg.ToplevelCloseEvent) { quit = true }

// HandleCallbackDone: the compositor finished presenting the last frame (this is
// vsync). Draw the next one if anything still wants to animate.
func (*handler) HandleCallbackDone(ev wl.CallbackDoneEvent) {
	if frameCb != nil {
		wlclient.CallbackDestroy(frameCb)
		frameCb = nil
	}
	wlDebug("frame callback (dirty=%v wantsFrame=%v)", dirty, wantsFrame)
	if wantsFrame && !quit {
		drawFrame()
	}
}

// nextBuffer returns a free buffer sized to the current window, (re)creating it
// when missing or stale after a resize.
func nextBuffer() *wlBuffer {
	var b *wlBuffer
	switch {
	case !buffers[0].busy:
		b = &buffers[0]
	case !buffers[1].busy:
		b = &buffers[1]
	default:
		return nil // both in flight; skip this frame
	}
	if b.buf != nil && (b.w != curW || b.h != curH) {
		b.destroy()
	}
	if b.buf == nil {
		if err := b.create(curW, curH); err != nil {
			perfLog("[wl] buffer create failed: %v", err)
			return nil
		}
	}
	return b
}

func (b *wlBuffer) create(w, h int) error {
	stride := w * 4
	size := stride * h
	fd, err := wos.CreateAnonymousFile(int64(size))
	if err != nil {
		return err
	}
	defer fd.Close()

	data, err := wos.Mmap(int(fd.Fd()), 0, size, wos.ProtRead|wos.ProtWrite, wos.MapShared)
	if err != nil {
		return err
	}
	pool, err := shm.CreatePool(fd.Fd(), int32(size))
	if err != nil {
		wos.Munmap(data)
		return err
	}
	buf, err := pool.CreateBuffer(0, int32(w), int32(h), int32(stride), wl.ShmFormatXrgb8888)
	if err != nil {
		pool.Destroy()
		wos.Munmap(data)
		return err
	}
	pool.Destroy() // the buffer keeps the fd alive; the pool isn't needed anymore
	wlclient.BufferAddListener(buf, b)

	b.buf, b.data, b.w, b.h = buf, data, w, h
	return nil
}

func (b *wlBuffer) destroy() {
	if b.buf != nil {
		b.buf.Destroy()
		b.buf = nil
	}
	if b.data != nil {
		wos.Munmap(b.data)
		b.data = nil
	}
	b.busy = false
}

// drawFrame produces one shirei frame, rasterizes it into a free shm buffer, and
// presents it; it also arms the next frame callback when animation is wanted.
func drawFrame() {
	b := nextBuffer()
	if b == nil {
		wlDebug("drawFrame SKIPPED: both buffers busy (mods=%04b)", shirei.InputState.Modifiers)
		dirty = true // both buffers in flight; retry when one is released
		return
	}
	wlDebug("drawFrame render (mods=%04b)", shirei.InputState.Modifiers)
	dirty = false

	scale := windowScale
	if scale <= 0 {
		scale = 1
	}
	shirei.WindowScale = scale
	// The app's content area excludes the titlebar; the core reserves that strip
	// (DecorationHeight) above it and draws drawTitlebar there.
	contentH := float32(logicalH)
	if csdEnabled {
		contentH -= titlebarHeight
	}
	shirei.WindowSize = shirei.Vec2{float32(logicalW), contentH}

	// Deliver committed text (IME commits + typed chars + paste) before
	// frameFn consumes input. FrameInput is reset at the end of RunFrameFn.
	injectPendingPaste()
	flushPendingText()

	t0 := time.Now()
	out := shirei.RunFrameFn(frameFn)
	perfRecordProduce(time.Since(t0))

	// Refresh IME candidate anchor with the just-published CompositionPos /
	// CaretPos (same cadence as Win32's post-frame ImmSetCandidateWindow).
	commitTextInputState()

	if out.Copy != "" {
		setClipboard(out.Copy)
	}
	if out.Paste {
		requestPaste()
	}

	t1 := time.Now()
	softRenderer.RenderInto(b.data, curW*4, curW, curH, scale, out.Surfaces)
	surface.Attach(b.buf, 0, 0)
	surface.Damage(0, 0, int32(logicalW), int32(logicalH)) // damage is in surface (logical) coords

	wantsFrame = out.NextFrameRequested
	if wantsFrame && frameCb == nil {
		if cb, err := surface.Frame(); err == nil {
			frameCb = cb
			wlclient.CallbackAddListener(frameCb, h)
		}
	}
	surface.Commit()
	b.busy = true
	perfRecordPaint(time.Since(t1))
}
