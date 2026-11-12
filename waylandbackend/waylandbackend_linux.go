//go:build linux

package waylandbackend

import (
	"fmt"
	"golang.org/x/sys/unix"
	"os"
	"runtime"
	"time"

	wos "go.hasen.dev/shirei/internal/wayland/os"
	"go.hasen.dev/shirei/internal/wayland/wl"
	"go.hasen.dev/shirei/internal/wayland/wlclient"
	zxdg "go.hasen.dev/shirei/internal/wayland/xdg"

	g "go.hasen.dev/generic"
	"go.hasen.dev/shirei"
	"go.hasen.dev/shirei/gpurender"
	"go.hasen.dev/shirei/internal/atspi"
)

// glyphCacheBudget caps total cached glyph-bitmap bytes (enables the shared core
// glyph cache). Matches the other backends.
const glyphCacheBudget = 16 << 20

var (
	winIconPath string

	disp          *wl.Display
	registry      *wl.Registry
	compositor    *wl.Compositor
	compositorVer uint32 // bound wl_compositor version (set_buffer_scale needs >=3)
	wmBase        *zxdg.WmBase
	shm           *wl.Shm
	hasXRGB       bool
	hasARGB       bool

	seat          *wl.Seat
	pointer       *wl.Pointer
	pointerSerial uint32 // last pointer event serial (for cursor / CSD move+resize)
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

// SetupWindow records the window title and preferred content size in points
// (same contract as macOS/Win32). When CSD is enabled, createWindow grows the
// surface by titlebarHeight so the titlebar does not consume the body.
func SetupWindow(title string, width, height int) {
	currentWindow.winTitle = title
	currentWindow.winW, currentWindow.winH = width, height
}

// SetupTransparent preserves per-pixel alpha in the window's content.
// Call before Run. Transparent pixels remain in the window's input region.
func SetupTransparent() {
	shirei.GetHost().TransparentBackground = true
}

// SetupQuiet suppresses launch-token activation. Mapping and focus policy
// remain under compositor control. Call before Run.
func SetupQuiet() { quiet = true }

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
// loop. It must be called from the program's main goroutine and does not return:
// toplevel close calls generic.ExitWithCleanup so AddExitCleanup handlers run.
// Everything (input + frame production) happens on this one goroutine: Wayland
// delivers events synchronously inside DisplayDispatch.
func Run(fn shirei.FrameFn) {
	runtime.LockOSThread()
	uiThreadID.Store(int64(unix.Gettid()))
	leave := enterWindow(mainWindow)
	currentWindow.frameFn = wrapFrame(fn)

	shirei.GetHost().GlyphCacheBudgetBytes = glyphCacheBudget
	shirei.GetHost().EscapeHatchBackendContext = Context{window: currentWindow}
	shirei.GetHost().WindowFocused = false

	consumeLaunchActivationToken()
	connect()
	// No icon protocol in the registry (GNOME): bridge the icon over a hidden
	// .desktop file matched by app_id, written before the window maps so the
	// shell can pick it up (see waylanddesktop_linux.go).
	if iconMgr == nil {
		ensureDesktopEntry(appID())
	}
	if err := createWindow(); err != nil {
		panic(err)
	}
	tryInitGPU()
	currentWindow.accessBridge = atspi.Start(currentWindow.winTitle, shirei.RequestNextFrame)
	g.AddExitCleanup(func() {
		for _, w := range liveWindows() {
			if !w.closed {
				disposeWindow(w)
			}
		}
	})
	mainWindow.ready = true
	mainWindow.unsubscribe = shirei.RegisterWindowUI(mainWindow.ui)
	leave()

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
	exitCode := 0
	wlDebug("wl backend build: 2026-07-13-idle-frame-wake (timeout dispatch)")
	for {
		err := wlclient.DisplayDispatchTimeout(disp, framePoll)
		if err != nil && err != wl.ErrContextRunTimeout && err != wl.ErrContextRunProxyNil {
			// Always to stderr: exiting the GUI loop is fatal for the app, and
			// after a protocol error this is the only trace of what happened.
			fmt.Fprintf(os.Stderr, "waylandbackend: display dispatch failed: %v\n", err)
			exitCode = 1
			break
		}
		updatePopupWindows()
		pumpKeyboardRepeat(time.Now())
		for _, w := range liveWindows() {
			if !w.ready || w.closed || w.closing {
				continue
			}
			restore := enterWindow(w)
			if shirei.FrameRequested() {
				w.dirty = true
			}
			if w.dirty && (w.hidden || (w.frameCb == nil && !w.waitConfigure)) {
				drawFrame()
			}
			restore()
			updatePopupWindows()
		}
	}
	g.ExitWithCleanup(exitCode)
}

var h = &handler{}

// handler routes shared seat events by focus and window events by their owner.
type handler struct{ window *windowState }

func connect() {
	d, err := wlclient.DisplayConnect(nil) // honors $WAYLAND_DISPLAY
	if err != nil {
		panic("waylandbackend: cannot connect to Wayland: " + err.Error())
	}
	disp = d
	disp.AddErrorHandler(h)
	disp.AddDeleteIdHandler(h)

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
	if shirei.GetHost().TransparentBackground && !hasARGB {
		panic("waylandbackend: wl_shm ARGB8888 format unavailable")
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
		if compositorVer > 6 {
			compositorVer = 6
		}
		compositor = wlclient.RegistryBindCompositorInterface(registry, ev.Name, compositorVer)
	case "wl_shm":
		shm = wlclient.RegistryBindShmInterface(registry, ev.Name, 1)
		wlclient.ShmAddListener(shm, h)
	case "xdg_wm_base":
		// Version 2 supplies tiled states, needed to defer client size requests.
		wmBase = wlclient.RegistryBindWmBaseInterface(registry, ev.Name, min(ev.Version, 2))
		zxdg.WmBaseAddListener(wmBase, h)
	case "zxdg_output_manager_v1":
		bindOutputManager(ev.Name, ev.Version)
	case "wp_fractional_scale_manager_v1", "wp_viewporter":
		if !g.EnvFalsy("SHIREI_FRACTIONAL_SCALE") {
			bindScaleManager(ev.Name, ev.Interface)
		}
	case "xdg_activation_v1":
		bindActivation(ev.Name)
	case "zwlr_layer_shell_v1":
		if !g.EnvFalsy("SHIREI_LAYER_SHELL") {
			bindLayerShell(ev.Name, ev.Version)
		}
	case "zxdg_decoration_manager_v1":
		bindDecorationManager(ev.Name)
	case "wl_seat":
		seat = wlclient.RegistryBindSeatInterface(registry, ev.Name, min(ev.Version, 5))
		wlclient.SeatAddListener(seat, h)
	case "wl_output":
		v := ev.Version // need v2 for the scale event; clamp to what's advertised
		if v > 3 {
			v = 3
		}
		bindOutput(ev.Name, v)
	case "wp_cursor_shape_manager_v1":
		bindCursorShapeManager(ev.Name, 1) // real themed cursor (preferred over our arrow)
	case "xdg_toplevel_icon_manager_v1":
		bindToplevelIconManager(ev.Name, 1) // hand the compositor a real icon image
	case "zwp_relative_pointer_manager_v1":
		bindRelativePointerManager(ev.Name)
	case "wl_data_device_manager":
		v := ev.Version
		if v > 3 {
			v = 3
		}
		bindDataDeviceManager(ev.Name, v)
	case "zwp_text_input_manager_v3":
		bindTextInputManager(ev.Name, ev.Version) // IME via text-input-v3
	case "zwp_linux_dmabuf_v1":
		bindLinuxDmabuf(ev.Name, ev.Version)
	}
}

func (*handler) HandleRegistryGlobalRemove(ev wl.RegistryGlobalRemoveEvent) {
	for i, o := range outputs {
		if o.name != ev.Name {
			continue
		}
		outputs = append(outputs[:i], outputs[i+1:]...)
		if o.logical != nil {
			_ = o.logical.Context().SendRequest(o.logical, 0)
		}
		if o.version >= 3 {
			_ = o.proxy.Release()
		}
		for _, w := range liveWindows() {
			delete(w.enteredOutputs, o.proxy)
			if w.output == o.proxy {
				w.output = nil
				w.outputMigration = w.layerSurface != nil
			}
			updateWindowScale(w)
		}
		break
	}
}

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

// HandleDisplayDeleteId releases proxy listeners after the compositor confirms
// destruction, including late events for closing window surfaces and buffers.
func (*handler) HandleDisplayDeleteId(ev wl.DisplayDeleteIdEvent) {
	disp.Context().Unregister(wl.ProxyId(ev.Id))
}

func (*handler) HandleShmFormat(ev wl.ShmFormatEvent) {
	if ev.Format == wl.ShmFormatArgb8888 {
		hasARGB = true
	}
	if ev.Format == wl.ShmFormatXrgb8888 {
		hasXRGB = true
	}
}

// HandleWmBasePing: keep the compositor's liveness check happy.
func (*handler) HandleWmBasePing(ev zxdg.WmBasePingEvent) {
	wmBase.Pong(ev.Serial)
}

func createWindow() error {
	currentWindow.logicalW, currentWindow.logicalH = currentWindow.winW, currentWindow.winH
	if currentWindow.csdEnabled && currentWindow.logicalW > 0 && currentWindow.logicalH > 0 {
		currentWindow.logicalH += titlebarHeight
	}
	recomputeDeviceSize() // honors a scale already learned during connect

	var err error
	if currentWindow.surface, err = compositor.CreateSurface(); err != nil {
		return fmt.Errorf("waylandbackend: CreateSurface: %w", err)
	}
	windowsBySurface[currentWindow.surface] = currentWindow
	currentWindow.surface.AddEnterHandler(currentWindow.handler)
	currentWindow.surface.AddLeaveHandler(currentWindow.handler)
	if compositorVer >= 6 {
		currentWindow.surface.AddPreferredBufferScaleHandler(currentWindow.handler)
	}
	if err := createSurfaceScale(currentWindow); err != nil {
		return err
	}
	if layerShell != nil && (currentWindow.floating || (currentWindow.undecorated && !currentWindow.topLevel)) {
		if err := createLayerWindow(); err != nil {
			return err
		}
	} else {
		if currentWindow.xdgSurface, err = wmBase.GetSurface(currentWindow.surface); err != nil {
			return fmt.Errorf("waylandbackend: GetSurface: %w", err)
		}
		currentWindow.xdgSurface.AddListener(currentWindow.handler)
		if currentWindow.xdgToplevel, err = currentWindow.xdgSurface.GetToplevel(); err != nil {
			return fmt.Errorf("waylandbackend: GetToplevel: %w", err)
		}
		zxdg.ToplevelAddListener(currentWindow.xdgToplevel, currentWindow.handler)
		if !currentWindow.topLevel && currentWindow.owner != nil && currentWindow.owner.xdgToplevel != nil {
			if err := currentWindow.xdgToplevel.SetParent(currentWindow.owner.xdgToplevel); err != nil {
				return err
			}
		}
		if err := setupDecoration(); err != nil {
			return err
		}
		if currentWindow.minW > 0 || currentWindow.minH > 0 {
			setToplevelMinimum(currentWindow)
		}
		currentWindow.xdgToplevel.SetTitle(currentWindow.winTitle)
		// app_id is how compositors identify the app (window grouping, and the
		// .desktop file lookup that supplies the icon — see waylanddesktop_linux.go).
		currentWindow.xdgToplevel.SetAppId(appID())
		setToplevelIcon()
	}
	// Commit without a buffer to trigger the initial configure handshake; the
	// first frame is drawn once the compositor acks it.
	if err := commitWindowSurface(); err != nil {
		return err
	}
	currentWindow.waitConfigure = true
	perfLog("[wl] window created; committed, awaiting first configure")
	return nil
}

// HandleSurfaceConfigure ends the configure sequence. On a size change apply
// the staged size and ask for a frame — an interactive resize holds a pointer
// grab, so no input event sets `dirty`; the ack rides with that frame (see
// drawFrame). A state-only configure (focus flip, tiling toggle at the same
// size) needs no redraw, so it is acked on arrival instead.
func (h *handler) HandleSurfaceConfigure(ev zxdg.SurfaceConfigureEvent) {
	if h.window != nil {
		wlDebug("surface configure %q (waiting=%v hidden=%v)", h.window.winTitle, h.window.waitConfigure, h.window.hidden)
	}
	if h.window == nil || h.window.closed {
		return
	}
	leave := enterWindow(h.window)
	defer leave()

	decorationChanged := false
	if currentWindow.pendingDecorationMode != 0 {
		clientSide := currentWindow.pendingDecorationMode != decorationServerSide
		decorationChanged = currentWindow.csdEnabled != clientSide
		currentWindow.csdEnabled = clientSide
		currentWindow.pendingDecorationMode = 0
		setToplevelMinimum(currentWindow)
		wlDebug("decoration configure: client-side=%v", currentWindow.csdEnabled)
	}
	// A zero height lets the client choose. At launch, keep SetupWindow's
	// requested content height and add chrome only when the client draws it.
	if currentWindow.waitConfigure && currentWindow.pendingH == 0 {
		currentWindow.logicalH = currentWindow.winH
		if currentWindow.csdEnabled && currentWindow.winW > 0 && currentWindow.winH > 0 {
			currentWindow.logicalH += titlebarHeight
		}
		recomputeDeviceSize()
	}
	// Each zero dimension independently leaves that axis to the client.
	nextW, nextH := currentWindow.logicalW, currentWindow.logicalH
	if currentWindow.pendingW > 0 {
		nextW = currentWindow.pendingW
	}
	if currentWindow.pendingH > 0 {
		nextH = currentWindow.pendingH
	}
	resized := nextW != currentWindow.logicalW || nextH != currentWindow.logicalH
	if resized {
		currentWindow.logicalW, currentWindow.logicalH = nextW, nextH
		recomputeDeviceSize()
	}
	currentWindow.pendingW, currentWindow.pendingH = 0, 0 // consume the staged size; a bare configure must not re-apply it
	if currentWindow.pendingToplevelConfigure {
		currentWindow.toplevelStates = currentWindow.pendingToplevelStates
		currentWindow.pendingToplevelConfigure = false
	}
	discardConstrainedSize(currentWindow)
	if currentWindow.clientSizePending || currentWindow.minimumSizePending {
		currentWindow.dirty = true
	}
	if !resized && !decorationChanged && !currentWindow.waitConfigure && !currentWindow.hasAck {
		currentWindow.xdgSurface.AckConfigure(ev.Serial)
		return
	}
	// A deferred ack may already be in flight; only the newest serial needs acking.
	currentWindow.ackSerial, currentWindow.hasAck = ev.Serial, true
	currentWindow.dirty = true
	if currentWindow.waitConfigure {
		currentWindow.waitConfigure = false
		perfLog("[wl] first configure; first frame follows this dispatch")
	}
}

// HandleToplevelConfigure stages size and state (0 = client chooses): the two
// configure events can land in separate dispatch batches, so applying it here
// would let a frame commit the new size under the previous ack.
func (h *handler) HandleToplevelConfigure(ev zxdg.ToplevelConfigureEvent) {
	if h.window == nil || h.window.closed {
		return
	}
	leave := enterWindow(h.window)
	defer leave()

	// Configure sizes are in logical points; the device buffer is scale times that.
	currentWindow.pendingW, currentWindow.pendingH = int(ev.Width), int(ev.Height)
	currentWindow.pendingToplevelStates = 0
	for _, state := range ev.States {
		if state > 0 && state < 32 {
			currentWindow.pendingToplevelStates |= 1 << uint32(state)
		}
	}
	currentWindow.pendingToplevelConfigure = true
	wlDebug("toplevel configure %q: %dx%d states=%v", currentWindow.winTitle, ev.Width, ev.Height, ev.States)
}

func (h *handler) HandleToplevelClose(zxdg.ToplevelCloseEvent) {
	if h.window != nil && !h.window.closed {
		requestWindowClose(h.window)
	}
}

// HandleCallbackDone: the compositor finished presenting the last frame (this is
// vsync). Draw the next one if anything still wants to animate.
func (h *handler) HandleCallbackDone(ev wl.CallbackDoneEvent) {
	if h.window == nil || h.window.closed {
		return
	}
	leave := enterWindow(h.window)
	defer leave()

	if currentWindow.frameCb != nil {
		wlclient.CallbackDestroy(currentWindow.frameCb)
		currentWindow.frameCb = nil
	}
	wlDebug("frame callback (dirty=%v wantsFrame=%v)", currentWindow.dirty, currentWindow.wantsFrame)
	if currentWindow.wantsFrame {
		currentWindow.dirty = true
	}
}

// nextBuffer returns a free buffer sized to the current window, (re)creating it
// when missing or stale after a resize.
func nextBuffer() *wlBuffer {
	var b *wlBuffer
	switch {
	case !currentWindow.buffers[0].busy:
		b = &currentWindow.buffers[0]
	case !currentWindow.buffers[1].busy:
		b = &currentWindow.buffers[1]
	default:
		return nil // both in flight; skip this frame
	}
	if b.buf != nil && (b.w != currentWindow.curW || b.h != currentWindow.curH) {
		b.destroy()
	}
	if b.buf == nil {
		if err := b.create(currentWindow.curW, currentWindow.curH); err != nil {
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
	format := uint32(wl.ShmFormatXrgb8888)
	if shirei.GetHost().TransparentBackground {
		format = wl.ShmFormatArgb8888
	}
	buf, err := pool.CreateBuffer(0, int32(w), int32(h), int32(stride), format)
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
		destroyWindowBuffer(b.buf)
		b.buf = nil
	}
	if b.data != nil {
		wos.Munmap(b.data)
		b.data = nil
	}
	b.busy = false
}

// drawFrame produces one shirei frame, rasterizes it (GLES dmabuf or shm), and
// presents it; it also arms the next frame callback when animation is wanted.
func drawFrame() {
	if framesInProgress != 0 || !currentWindow.ready || currentWindow.closed || currentWindow.closing {
		currentWindow.dirty = true
		return
	}
	framesInProgress++
	defer func() { framesInProgress-- }()
	var slot *gpuSlot
	var shm *wlBuffer
	if currentWindow.hidden {
		drawHiddenFrame()
		return
	}
	applyToplevelSize(currentWindow)
	if currentWindow.gpuOK {
		slot = nextGPUBuffer()
	}
	if slot == nil {
		shm = nextBuffer()
		if shm == nil {
			wlDebug("drawFrame SKIPPED: both buffers busy (mods=%04b)", shirei.GetInputState().Modifiers)
			currentWindow.dirty = true
			return
		}
	}
	wlDebug("drawFrame render %q (mods=%04b)", currentWindow.winTitle, shirei.GetInputState().Modifiers)
	currentWindow.dirty = false

	scale := currentWindow.windowScale
	if scale <= 0 {
		scale = 1
	}
	shirei.GetHost().WindowScale = scale
	// Full surface size so the root can host the titlebar; wrapFrame narrows
	// to content during the app build (restores for settle). Content size is
	// published again after RunFrameFn below.
	shirei.GetHost().WindowSize = shirei.Vec2{float32(currentWindow.logicalW), float32(currentWindow.logicalH)}

	// Deliver committed text (IME commits + typed chars + paste) before
	// frameFn consumes input. FrameInput is reset at the end of RunFrameFn.
	injectPendingPaste()
	flushPendingText()
	if shirei.GetFrameInput().AccessAction.Kind == 0 {
		shirei.GetFrameInput().AccessAction = currentWindow.accessBridge.NextAction()
	}

	out := shirei.RunFrameFnLocked(currentWindow.frameFn)
	currentWindow.accessBridge.Publish(out.Access, out.AccessChanged, shirei.Vec2{float32(currentWindow.logicalW), float32(currentWindow.logicalH)}, shirei.GetHost().WindowFocused)
	if currentWindow.accessBridge.HasActions() {
		out.NextFrameRequested = true
	}

	if currentWindow.csdEnabled {
		shirei.GetHost().WindowSize[1] = float32(currentWindow.logicalH - titlebarHeight)
	}

	// Refresh IME candidate anchor with the just-published CompositionPos /
	// CaretPos (same cadence as Win32's post-frame ImmSetCandidateWindow).
	commitTextInputState()

	if out.Copy != "" {
		setClipboard(out.Copy)
	}
	if out.Paste {
		requestPaste()
	}
	if out.OpenURL != "" {
		openURL(out.OpenURL)
	}

	if currentWindow.havePresented && out.SurfacesHash == currentWindow.lastPresentedHash && currentWindow.curW == currentWindow.presentW && currentWindow.curH == currentWindow.presentH && !currentWindow.hasAck {
		shirei.EmitFrameMetrics()
		currentWindow.wantsFrame = out.NextFrameRequested
		if currentWindow.wantsFrame || currentWindow.surfaceStateDirty {
			if currentWindow.frameCb == nil {
				if cb, err := currentWindow.surface.Frame(); err == nil {
					currentWindow.frameCb = cb
					wlclient.CallbackAddListener(currentWindow.frameCb, currentWindow.handler)
				}
			}
			commitWindowSurface()
		}
		return
	}

	var presentBuf *wl.Buffer
	painted := false
	if slot != nil {
		err := gpurender.Render(slot.target.Handle(), slot.w, slot.h, scale, out.Surfaces, out.GlyphRuns, out.GlyphsAdded, out.GlyphsEvicted, false)
		if err != nil {
			fmt.Fprintf(os.Stderr, "gpurender: %v; software this frame\n", err)
			slot = nil
			if shm == nil {
				shm = nextBuffer()
			}
		} else {
			presentBuf = slot.buf
			slot.busy = true
			painted = true
		}
	}
	if !painted {
		if shm == nil {
			wlDebug("drawFrame SKIPPED: software fallback, no shm buffer")
			currentWindow.dirty = true
			return
		}
		currentWindow.softRenderer.RenderInto(shm.data, currentWindow.curW*4, currentWindow.curW, currentWindow.curH, scale, out.Surfaces, out.GlyphRuns)
		presentBuf = shm.buf
		shm.busy = true
	}
	t := &shirei.ActiveUI().FrameTimings
	t.Painted = true
	t.PaintEnd = time.Now()
	shirei.EmitFrameMetrics()

	// Ack before the attach: niri keys its resize animation off the acked serial,
	// and skips it for a new size committed under an older ack.
	if currentWindow.hasAck {
		if currentWindow.layerSurface != nil {
			currentWindow.layerSurface.AckConfigure(currentWindow.ackSerial)
		} else {
			currentWindow.xdgSurface.AckConfigure(currentWindow.ackSerial)
		}
		currentWindow.hasAck = false
	}
	currentWindow.surface.Attach(presentBuf, 0, 0)
	currentWindow.surface.Damage(0, 0, int32(currentWindow.logicalW), int32(currentWindow.logicalH)) // damage is in surface (logical) coords

	currentWindow.wantsFrame = out.NextFrameRequested
	if currentWindow.wantsFrame && currentWindow.frameCb == nil {
		if cb, err := currentWindow.surface.Frame(); err == nil {
			currentWindow.frameCb = cb
			wlclient.CallbackAddListener(currentWindow.frameCb, currentWindow.handler)
		}
	}
	commitWindowSurface()
	if currentWindow.activateAfterPresent {
		requestActivation(currentWindow)
	}
	currentWindow.lastPresentedHash = out.SurfacesHash
	currentWindow.presentW, currentWindow.presentH = currentWindow.curW, currentWindow.curH
	currentWindow.havePresented = true
}
