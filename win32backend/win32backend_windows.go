// Package win32backend is a direct-Windows backend for shirei. The Win32 API
// provides the window, message loop, and input. Rasterization is D3D11 into
// a GDI-compatible DXGI texture (BitBlt via GetDC) when GPU init succeeds,
// else the software renderer into a top-down 32bpp DIB. Presentation uses
// GDI BitBlt for opaque windows, UpdateLayeredWindow for transparent windows.
// SHIREI_GPU=0 forces software.
//
// Content-hash skip: when SurfacesHash and client size match the last presented
// frame, render+present are skipped (same idea as cocoabackend/androidbackend).
//
// Instrumentation: SHIREI_PERF=1 prints fps + produce/render/present (µs);
// optional SHIREI_PERF_LOG=<path> appends the same lines.
//
// Pure Go (no cgo): Win32 entry points via syscall, cross-compiles with
// GOOS=windows. Half of the GOOS-selected go.hasen.dev/shirei/app wrapper.
package win32backend

import (
	"fmt"
	"runtime"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"

	"github.com/cli/browser"
	g "go.hasen.dev/generic"
	"go.hasen.dev/shirei"
	"go.hasen.dev/shirei/internal/qwerty"
)

// glyphCacheBudget caps total cached glyph-bitmap bytes (enables the shared core
// glyph cache). Matches cocoabackend.
const glyphCacheBudget = 16 << 20

// frameTimerID identifies the animation timer (per-window timer id).
const frameTimerID = 1

// SetupWindow records the window parameters. The window is created in Run, on
// the UI thread.
func SetupWindow(title string, width, height int) {
	currentWindow.winTitle = title
	currentWindow.winW = width
	currentWindow.winH = height
}

// SetupQuiet maps the window without making it the foreground or giving it
// keyboard focus. Call before Run.
func SetupQuiet() {
	winQuiet = true
}

// SetupTransparent preserves per-pixel alpha. Call before Run.
func SetupTransparent() { currentWindow.winTransparent = true }

// SetupUndecorated omits the native title bar and borders. Call before Run.
func SetupUndecorated() { currentWindow.winUndecorated = true }

// SetupOverlay opens a transparent, borderless window above normal windows
// on its assigned virtual desktop. Call before Run.
func SetupOverlay() {
	currentWindow.winTransparent = true
	currentWindow.winUndecorated = true
	currentWindow.winFloating = true
}

// Run opens the window and runs the Win32 message loop. It must be called
// from the program's main goroutine (the message loop and window must share
// one OS thread) and does not return: WM_QUIT ends with generic.ExitWithCleanup
// so AddExitCleanup handlers run. System fonts are initialized by shirei on
// the first frame (RunFrameFn), not here.
func Run(fn shirei.FrameFn) {
	runtime.LockOSThread()

	uiThreadID.Store(uint32(currentThreadID()))
	currentWindow.frameFn = fn
	currentWindow.unsubscribe = shirei.RegisterWindowUI(currentWindow.ui)
	leave := enterWindow(mainWindow)

	shirei.GetHost().GlyphCacheBudgetBytes = glyphCacheBudget
	shirei.GetHost().EscapeHatchBackendContext = Context{window: currentWindow}
	shirei.GetHost().TransparentBackground = currentWindow.winTransparent

	enableDPIAwareness()
	if err := createWindow(); err != nil {
		panic(err)
	}
	initAccess()
	tryInitGPU()
	mainWindow.ready = true
	popupWakeTarget.Store(uintptr(mainWindow.hwnd))
	showWindow()
	leave()
	messageLoop()
	g.ExitWithCleanup(0)
}

// enableDPIAwareness opts into per-monitor DPI scaling so GetClientRect and
// mouse coordinates are in real device pixels. Best-effort: falls back through
// older entry points and finally does nothing (Wine / pre-1607 Windows).
func enableDPIAwareness() {
	if procAvailable(procSetProcDpiCtx) {
		if r, _, _ := procSetProcDpiCtx.Call(dpiPerMonitorAwareV2); r != 0 {
			return
		}
	}
	if procAvailable(procSetProcDPIAware) {
		procSetProcDPIAware.Call()
	}
}

func createWindow() error {
	hinstance = getModuleHandle()
	className, _ := syscall.UTF16PtrFromString("shireiWindowClass")
	cursor, _, _ := procLoadCursorW.Call(0, uintptr(idcArrow))

	wc := wndClassExW{
		WndProc:   wndProcCB,
		Instance:  hinstance,
		Cursor:    syscall.Handle(cursor),
		ClassName: className,
	}
	wc.Size = uint32(unsafe.Sizeof(wc))
	if r, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 && err != syscall.Errno(1410) {
		return fmt.Errorf("RegisterClassEx: %w", err)
	}

	// Size the window so its *client* area is winW×winH in logical points:
	// SetupWindow sizes are points (as on macOS), and since we are DPI-aware
	// the numbers we pass here are device pixels — on a 200% display an
	// unscaled winW×winH window would come out half the intended size.
	// WM_DPICHANGED is no help at creation (it only fires on later changes),
	// so scale by the creating monitor's DPI up front; CW_USEDEFAULT places
	// us on the primary monitor, which is what dpiForCreation reports.
	dpi := dpiForCreation()
	if currentWindow.owner != nil && procAvailable(procGetDpiForWindow) {
		if d, _, _ := procGetDpiForWindow.Call(uintptr(currentWindow.owner.hwnd)); d != 0 {
			dpi = d
		}
	}
	style := uintptr(wsOverlappedWindow)
	if currentWindow.winUndecorated {
		style = wsPopup | wsThickframe | wsSysmenu | wsMinimizebox | wsMaximizebox
	}
	exStyle := uintptr(0)
	var owner uintptr
	topLevel := currentWindow.winTopLevel
	if currentWindow.owner != nil && !topLevel {
		owner = uintptr(currentWindow.owner.hwnd)
		exStyle |= wsExToolwindow
		style = wsPopup | wsThickframe | wsSysmenu
		if !currentWindow.winUndecorated {
			style |= wsCaption
		}
	} else if topLevel {
		// A normal-level utility window: unowned so it can go behind other
		// apps, but a tool window so it does not add a taskbar button.
		exStyle |= wsExToolwindow
	}
	if currentWindow.winTransparent {
		exStyle |= wsExLayered
	}
	if currentWindow.winNonactivating {
		exStyle |= wsExNoActivate
	}
	r := win32Rect{0, 0,
		int32(uintptr(currentWindow.winW) * dpi / 96),
		int32(uintptr(currentWindow.winH) * dpi / 96)}
	if !currentWindow.winUndecorated {
		if procAvailable(procAdjustWindowRectExForDpi) {
			procAdjustWindowRectExForDpi.Call(uintptr(unsafe.Pointer(&r)), style, 0, exStyle, dpi)
		} else {
			procAdjustWindowRectEx.Call(uintptr(unsafe.Pointer(&r)), style, 0, exStyle)
		}
	}
	wWidth := int(r.Right - r.Left)
	wHeight := int(r.Bottom - r.Top)
	if shirei.PerfEnabled() {
		fmt.Printf("[win32] creation dpi %d -> client %dx%d px\n", dpi, uintptr(currentWindow.winW)*dpi/96, uintptr(currentWindow.winH)*dpi/96)
	}

	x, y := uintptr(cwUseDefault), uintptr(cwUseDefault)
	if currentWindow.winUndecorated || owner != 0 || topLevel {
		// CW_USEDEFAULT is only defined for overlapped windows.
		sw, _, _ := procGetSystemMetrics.Call(smCXScreen)
		sh, _, _ := procGetSystemMetrics.Call(smCYScreen)
		x = uintptr(max(0, (int(sw)-wWidth)/2))
		y = uintptr(max(0, (int(sh)-wHeight)/2))
	}

	if owner != 0 {
		// Create on the owner's monitor so the initial DPI matches its content size.
		var bounds win32Rect
		procGetWindowRect.Call(owner, uintptr(unsafe.Pointer(&bounds)))
		x, y = uintptr(bounds.Left), uintptr(bounds.Top)
	}

	title, err := syscall.UTF16PtrFromString(currentWindow.winTitle)
	if err != nil {
		return fmt.Errorf("window title: %w", err)
	}
	previous := creatingWindow
	creatingWindow = currentWindow
	defer func() { creatingWindow = previous }()
	h, _, err := procCreateWindowExW.Call(
		exStyle,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(title)),
		style,
		x, y,
		uintptr(wWidth), uintptr(wHeight),
		owner, 0, uintptr(hinstance), 0,
	)
	if h == 0 {
		return fmt.Errorf("CreateWindowEx: %w", err)
	}
	currentWindow.hwnd = syscall.Handle(h)
	return nil
}

// SetupBackground keeps the process running after the main window closes.
// Call before Run.
func SetupBackground() {
	winBackground = true
}

func showWindow() {
	if currentWindow.winTransparent && !currentWindow.haveFrame {
		// A layered window needs a bitmap before it becomes visible. Produce it
		// explicitly so quiet startup does not depend on activation or exposure.
		onPaint()
	}
	activate := !currentWindow.winNonactivating && (currentWindow.winFocus || (!winQuiet && !currentWindow.winTopLevel))
	if !activate {
		procShowWindow.Call(uintptr(currentWindow.hwnd), swShowNoActivate)
		procUpdateWindow.Call(uintptr(currentWindow.hwnd))
		if currentWindow.winTopLevel {
			// Bring a normal-level utility window forward without taking focus.
			procSetWindowPos.Call(uintptr(currentWindow.hwnd), 0, 0, 0, 0, 0,
				swpNoMove|swpNoSize|swpNoactivate)
		}
	} else {
		procShowWindow.Call(uintptr(currentWindow.hwnd), swShow)
		procUpdateWindow.Call(uintptr(currentWindow.hwnd))
		// Pull the new window to the front and give it keyboard focus. When launched
		// from a terminal another app owns the foreground, so ShowWindow alone leaves
		// us unfocused; SetForegroundWindow asks the window manager to activate us
		// (under CrossOver this maps to winemac.drv bringing the app forward).
		procBringWindowToTop.Call(uintptr(currentWindow.hwnd))
		procSetForegroundWindow.Call(uintptr(currentWindow.hwnd))
		procSetFocus.Call(uintptr(currentWindow.hwnd))
	}

	if currentWindow.winFloating {
		// Keep a floating window above normal windows without activating it.
		procSetWindowPos.Call(uintptr(currentWindow.hwnd), hwndTopMost, 0, 0, 0, 0,
			swpNoMove|swpNoSize|swpNoactivate)
	}

	if currentWindow == mainWindow {
		applyWindowIcon()
	}
}

func messageLoop() {
	var msg win32Msg
	for {
		ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		switch int32(ret) {
		case 0, -1: // WM_QUIT or error
			return
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
		updatePopupWindows()
	}
}

// -----------------------------------------------------------------------------
//  Window procedure
// -----------------------------------------------------------------------------

func wndProc(hWnd, msg, wparam, lparam uintptr) uintptr {
	w := windowsByHandle[hWnd]
	if msg == wmNccreate && creatingWindow != nil {
		w = creatingWindow
		w.hwnd = syscall.Handle(hWnd)
		windowsByHandle[hWnd] = w
	}
	if w == nil {
		r, _, _ := procDefWindowProcW.Call(hWnd, msg, wparam, lparam)
		return r
	}
	leave := enterWindow(w)
	defer leave()
	switch uint32(msg) {
	case wmNccalcsize:
		if currentWindow.winUndecorated {
			return 0 // client fills the entire window, including the resize border
		}
	case wmNchittest:
		if currentWindow.winUndecorated {
			return undecoratedHitTest(hWnd, lparam)
		}
	case wmNcpaint:
		if currentWindow.winTransparent {
			if !currentWindow.presentingLayered {
				currentWindow.havePresented = false
				noteInput()
			}
			return 0
		}
	case wmNcactivate:
		if currentWindow.winTransparent {
			// Let Windows track activation without drawing into the window DC.
			r, _, _ := procDefWindowProcW.Call(hWnd, msg, wparam, ^uintptr(0))
			currentWindow.havePresented = false
			noteInput()
			return r
		}
	case 0x003D: // WM_GETOBJECT
		if result, handled := accessGetObject(hWnd, wparam, lparam); handled {
			return result
		}
	case accessWake: // queued accessibility action
		noteInput()
		return 0
	case 0x0003, 0x0007: // WM_MOVE, WM_SETFOCUS
		refreshAccess()
		noteInput()
		return 0

	case wmPrintclient:
		copyPresentedFrame(wparam)
		return 0

	case wmPaint:
		onPaint()
		return 0

	case wmErasebkgnd:
		return 1 // we paint every pixel; skip the background erase (no flicker)

	case wmSize:
		refreshAccess()
		// Size change invalidates the last presented content.
		currentWindow.havePresented = false
		currentWindow.dirty = true
		invalidate()
		return 0

	case wmDpichanged:
		// lParam points at the suggested new window rect for the new DPI.
		nr := (*win32Rect)(unsafe.Pointer(lparam))
		procSetWindowPos.Call(uintptr(currentWindow.hwnd), 0,
			uintptr(nr.Left), uintptr(nr.Top),
			uintptr(nr.Right-nr.Left), uintptr(nr.Bottom-nr.Top),
			swpNozorder|swpNoactivate)
		currentWindow.havePresented = false
		currentWindow.dirty = true
		invalidate()
		return 0
	case wmKillfocus:
		currentWindow.pendingHi = 0
		shirei.GetInputState().DownKeys = nil
		shirei.GetInputState().Modifiers = 0
		shirei.GetInputState().MouseButton = 0
		refreshAccess()
		clearComposition()
		noteInput()
		return 0

	case wmMousemove:
		onMouse(lparam, 0, 0)
		noteInput()
		return 0

	case wmLbuttondown:
		commitImeBeforeInterruption()
		procSetCapture.Call(uintptr(currentWindow.hwnd))
		onMouse(lparam, int(shirei.MousePrimary), int(shirei.MouseClick))
		noteInput()
		return 0
	case wmLbuttonup:
		procReleaseCapture.Call()
		onMouse(lparam, int(shirei.MousePrimary), int(shirei.MouseRelease))
		noteInput()
		return 0
	case wmRbuttondown:
		commitImeBeforeInterruption()
		onMouse(lparam, int(shirei.MouseSecondary), int(shirei.MouseClick))
		noteInput()
		return 0
	case wmRbuttonup:
		onMouse(lparam, int(shirei.MouseSecondary), int(shirei.MouseRelease))
		noteInput()
		return 0
	case wmMbuttondown:
		commitImeBeforeInterruption()
		onMouse(lparam, int(shirei.MouseTertiary), int(shirei.MouseClick))
		noteInput()
		return 0
	case wmMbuttonup:
		onMouse(lparam, int(shirei.MouseTertiary), int(shirei.MouseRelease))
		noteInput()
		return 0

	case wmMousewheel:
		onWheel(wparam, false)
		noteInput()
		return 0
	case wmMousehwheel:
		onWheel(wparam, true)
		noteInput()
		return 0

	case wmKeydown:
		onKey(wparam, lparam, true)
		noteInput()
		return 0
	case wmKeyup:
		onKey(wparam, lparam, false)
		noteInput()
		return 0
	case wmSyskeydown:
		onKey(wparam, lparam, true)
		noteInput()
		// fall through to DefWindowProc so system combos (Alt+F4, Alt+Space) work
	case wmSyskeyup:
		onKey(wparam, lparam, false)
		noteInput()
		// fall through to DefWindowProc

	case wmChar:
		onChar(uint16(wparam))
		noteInput()
		return 0
	case wmImeSetcontext:
		// Shirei renders preedit inline. Hide only the system composition
		// window; keep candidate UI bits for the system-drawn candidate list.
		lparam &^= uintptr(iscShowUICompositionWindow)
		r, _, _ := procDefWindowProcW.Call(hWnd, msg, wparam, lparam)
		return r
	case wmImeStartcomposition:
		clearComposition()
		noteInput()
		return 0
	case wmImeComposition:
		onImeComposition(lparam)
		updateImeCandidateWindow()
		noteInput()
		return 0
	case wmImeEndcomposition:
		clearComposition()
		noteInput()
		return 0
	case wmImeChar:
		// Result strings are read from GCS_RESULTSTR. Swallow the fallback
		// IME text relay so commits cannot double-insert via WM_CHAR.
		return 0
	case wmImeNotify:
		if wparam == imnOpencandidate {
			updateImeCandidateWindow()
		}
		r, _, _ := procDefWindowProcW.Call(hWnd, msg, wparam, lparam)
		return r

	case wmTimer:
		// wantsFrame covers in-frame animation; FrameRequested covers
		// background RequestNextFrame when the last frame settled to idle
		// (matches cocoa's shireiFrameRequested check on the display link).
		if currentWindow.dirty || currentWindow.wantsFrame || shirei.FrameRequested() {
			currentWindow.dirty = true
			invalidate()
		}
		return 0

	case popupWake:
		return 0
	case wmClose:
		if w == mainWindow {
			if winBackground {
				procShowWindow.Call(uintptr(w.hwnd), swHide)
				return 0
			}
			mainCloseRequested = true
		} else {
			ClosePopupWindow(w.popupID)
		}
		return 0
	case wmDestroy:
		disposeWindowResources(w)
		return 0
	case wmNcdestroy:
		delete(windowsByHandle, hWnd)
		if w != mainWindow {
			*w = windowState{closed: true}
		}

	}

	r, _, _ := procDefWindowProcW.Call(hWnd, msg, wparam, lparam)
	return r
}

// invalidate schedules a WM_PAINT for the whole client area (no background erase).
func invalidate() {
	if currentWindow.hwnd != 0 {
		procInvalidateRect.Call(uintptr(currentWindow.hwnd), 0, 0)
	}
}

// noteInput marks that input changed so the next paint produces a fresh frame,
// and schedules that paint.
func noteInput() {
	currentWindow.dirty = true
	invalidate()
}

// -----------------------------------------------------------------------------
//  Frame production + present
// -----------------------------------------------------------------------------

func onPaint() {
	if !currentWindow.ready || currentWindow.closed || framesInProgress > 0 {
		// Validate this paint and let the timer schedule a frame after the outer root.
		var ps paintStruct
		procBeginPaint.Call(uintptr(currentWindow.hwnd), uintptr(unsafe.Pointer(&ps)))
		procEndPaint.Call(uintptr(currentWindow.hwnd), uintptr(unsafe.Pointer(&ps)))
		currentWindow.dirty = true
		return
	}
	framesInProgress++
	defer func() { framesInProgress-- }()

	var ps paintStruct
	hdc, _, _ := procBeginPaint.Call(uintptr(currentWindow.hwnd), uintptr(unsafe.Pointer(&ps)))
	defer procEndPaint.Call(uintptr(currentWindow.hwnd), uintptr(unsafe.Pointer(&ps)))

	cw, ch := clientSize()
	if cw <= 0 || ch <= 0 {
		return
	}

	needProduce := currentWindow.dirty || !currentWindow.haveFrame
	currentWindow.dirty = false

	if needProduce {
		out, skipped := produceFrame(cw, ch)
		// A root can resize its window. Layered presentation must not restore the
		// dimensions captured before that callback.
		if width, height := clientSize(); width != cw || height != ch || dpiScale() != shirei.GetHost().WindowScale {
			currentWindow.haveFrame = false
			noteInput()
			return
		}
		if !skipped {
			renderAndPresent(hdc, cw, ch, out)
		}
		currentWindow.haveFrame = true
		return
	}

	// Expose / repaint without new content: present the last surface.
	if currentWindow.haveFrame {
		if gpuOK && blitLastGPU(hdc, cw, ch) {
			return
		}
		if !ensureDIB(cw, ch) {
			return
		}
		presentDC(hdc, uintptr(currentWindow.memDC), cw, ch)
	}
}

// produceFrame runs one shirei frame and returns whether paint can be skipped
// because the content hash matches what is already on screen.
func produceFrame(cw, ch int) (out shirei.FrameOutputData, skipped bool) {
	scale := dpiScale()
	shirei.GetHost().WindowScale = scale
	shirei.GetHost().WindowSize = shirei.Vec2{float32(cw) / scale, float32(ch) / scale}

	flushPendingText()
	flushAccessAction()

	out = shirei.RunFrameFnLocked(currentWindow.frameFn)
	updateAccess(out.Access, out.AccessChanged)
	updateImeCandidateWindow()

	if out.Copy != "" {
		setClipboard(out.Copy)
	}
	if out.Paste {
		appendPendingText(getClipboard())
	}
	if out.OpenURL != "" {
		openURL(out.OpenURL)
	}

	currentWindow.wantsFrame = out.NextFrameRequested
	// Keep the timer running even when this frame settled: a later
	// RequestNextFrame from a background goroutine must be able to wake
	// the message loop (same role as cocoa's always-ticking CADisplayLink).
	// wmTimer only invalidates when wantsFrame || FrameRequested, so idle
	// ticks are cheap.
	startTimer()

	if currentWindow.havePresented && out.SurfacesHash == currentWindow.lastPresentedHash && cw == currentWindow.presentW && ch == currentWindow.presentH {
		shirei.EmitFrameMetrics()
		return out, true
	}
	return out, false
}

// renderAndPresent rasterizes out.Surfaces and presents the result to the window.
func renderAndPresent(hdc uintptr, cw, ch int, out shirei.FrameOutputData) {
	scale := shirei.GetHost().WindowScale
	if scale <= 0 {
		scale = 1
	}
	if gpuOK && renderGPU(hdc, cw, ch, scale, out) {
		currentWindow.lastPresentedHash = out.SurfacesHash
		currentWindow.presentW, currentWindow.presentH = cw, ch
		currentWindow.havePresented = true
		t := &shirei.ActiveUI().FrameTimings
		t.Painted = true
		t.PaintEnd = time.Now()
		shirei.EmitFrameMetrics()
		return
	}
	if !ensureDIB(cw, ch) {
		return
	}
	currentWindow.softRenderer.RenderInto(currentWindow.dibBuf, currentWindow.dibW*4, cw, ch, scale, out.Surfaces, out.GlyphRuns)
	if !presentDC(hdc, uintptr(currentWindow.memDC), cw, ch) {
		return
	}
	currentWindow.lastPresentedHash = out.SurfacesHash
	currentWindow.presentW, currentWindow.presentH = cw, ch
	currentWindow.havePresented = true
	t := &shirei.ActiveUI().FrameTimings
	t.Painted = true
	t.PaintEnd = time.Now()
	shirei.EmitFrameMetrics()
}

// produceAndRender is used by IME interruption frames (no WM_PAINT DC).
func produceAndRender(cw, ch int) {
	out, skipped := produceFrame(cw, ch)
	if width, height := clientSize(); width != cw || height != ch {
		noteInput()
		return
	}
	if skipped {
		return
	}
	renderAndPresent(0, cw, ch, out)
}

// ensureDIB (re)creates the DIB present surface when the client size changes.
// Returns false if creation failed.
func ensureDIB(w, h int) bool {
	if currentWindow.dibBM != 0 && currentWindow.dibW == w && currentWindow.dibH == h {
		return true
	}
	releaseDIB()

	if currentWindow.memDC == 0 {
		dc, _, _ := procCreateCompatibleDC.Call(0)
		currentWindow.memDC = syscall.Handle(dc)
		if currentWindow.memDC == 0 {
			return false
		}
	}

	bmi := bitmapInfo{Header: bitmapInfoHeader{
		Width:       int32(w),
		Height:      -int32(h), // negative => top-down rows (matches the renderer)
		Planes:      1,
		BitCount:    32,
		Compression: biRGB,
	}}
	bmi.Header.Size = uint32(unsafe.Sizeof(bmi.Header))

	bm, _, _ := procCreateDIBSection.Call(
		uintptr(currentWindow.memDC),
		uintptr(unsafe.Pointer(&bmi)),
		dibRGBColors,
		uintptr(unsafe.Pointer(&currentWindow.dibBits)),
		0, 0,
	)
	if bm == 0 || currentWindow.dibBits == nil {
		return false
	}
	currentWindow.dibBM = syscall.Handle(bm)
	old, _, _ := procSelectObject.Call(uintptr(currentWindow.memDC), bm)
	currentWindow.dibOldBM = syscall.Handle(old)

	currentWindow.dibW, currentWindow.dibH = w, h
	// 32bpp rows are always dword-aligned, so stride == w*4.
	currentWindow.dibBuf = unsafe.Slice((*byte)(currentWindow.dibBits), w*h*4)
	return true
}

func releaseDIB() {
	if currentWindow.dibBM != 0 {
		procSelectObject.Call(uintptr(currentWindow.memDC), uintptr(currentWindow.dibOldBM))
		procDeleteObject.Call(uintptr(currentWindow.dibBM))
		currentWindow.dibBM = 0
	}
	currentWindow.dibBuf = nil
	currentWindow.dibBits = nil
	currentWindow.dibW, currentWindow.dibH = 0, 0
}

func clientSize() (int, int) {
	var r win32Rect
	procGetClientRect.Call(uintptr(currentWindow.hwnd), uintptr(unsafe.Pointer(&r)))
	return int(r.Right - r.Left), int(r.Bottom - r.Top)
}

// dpiScale returns device pixels per logical point. Per-window DPI when
// available, else the same system-level fallbacks as dpiForCreation — the
// two must agree, or the window would be sized for one scale and rendered
// at another.
func dpiScale() float32 {
	if procAvailable(procGetDpiForWindow) {
		if dpi, _, _ := procGetDpiForWindow.Call(uintptr(currentWindow.hwnd)); dpi != 0 {
			return float32(dpi) / 96
		}
	}
	return float32(dpiForCreation()) / 96
}

// dpiForCreation returns the DPI to size a new window with, before any window
// exists: GetDpiForSystem (Win10 1607+ / modern Wine), else the screen DC's
// LOGPIXELSX (available everywhere, reads e.g. a Wine bottle's DPI setting),
// else the classic 96.
func dpiForCreation() uintptr {
	if procAvailable(procGetDpiForSystem) {
		if dpi, _, _ := procGetDpiForSystem.Call(); dpi != 0 {
			return dpi
		}
	}
	if hdc, _, _ := procGetDC.Call(0); hdc != 0 {
		dpi, _, _ := procGetDeviceCaps.Call(hdc, logPixelsX)
		procReleaseDC.Call(0, hdc)
		if dpi != 0 {
			return dpi
		}
	}
	return 96
}

func startTimer() {
	if currentWindow.timerOn {
		return
	}
	// ~60 Hz wake; each tick re-invalidates while animation is wanted.
	procSetTimer.Call(uintptr(currentWindow.hwnd), frameTimerID, 16, 0)
	currentWindow.timerOn = true
}

func stopTimer() {
	if !currentWindow.timerOn {
		return
	}
	procKillTimer.Call(uintptr(currentWindow.hwnd), frameTimerID)
	currentWindow.timerOn = false
}

// -----------------------------------------------------------------------------
//  Input translation (Win32 messages -> shirei input globals; sample, not queue)
// -----------------------------------------------------------------------------

// onMouse records pointer position (and an optional click/release). lParam packs
// the client-area position in device pixels; shirei works in logical points.
func onMouse(lparam uintptr, button, action int) {
	scale := shirei.GetHost().WindowScale
	if scale <= 0 {
		scale = 1
	}
	x := float32(int16(lparam&0xffff)) / scale
	y := float32(int16((lparam>>16)&0xffff)) / scale

	np := shirei.Vec2{x, y}
	prev := shirei.GetInputState().MousePoint
	shirei.GetFrameInput().Motion = shirei.Vec2Add(shirei.GetFrameInput().Motion, shirei.Vec2Sub(np, prev))
	shirei.GetInputState().MousePoint = np

	updateModifiers()

	switch action {
	case int(shirei.MouseClick):
		shirei.GetInputState().MouseButton = shirei.MouseButton(button)
		shirei.GetFrameInput().Mouse = shirei.MouseClick
	case int(shirei.MouseRelease):
		shirei.GetInputState().MouseButton = shirei.MouseButton(button)
		shirei.GetFrameInput().Mouse = shirei.MouseRelease
	}
}

// onWheel maps a wheel notch (WHEEL_DELTA == 120 per detent) to a scroll amount.
// Sign is negated to match shirei's convention (as cocoabackend does); ~30 pts
// per notch approximates the cocoa feel. horizontal => x axis.
func onWheel(wparam uintptr, horizontal bool) {
	delta := float32(int16((wparam>>16)&0xffff)) / 120
	amt := -delta * 30
	if horizontal {
		shirei.GetFrameInput().Scroll = shirei.Vec2Add(shirei.GetFrameInput().Scroll, shirei.Vec2{amt, 0})
	} else {
		shirei.GetFrameInput().Scroll = shirei.Vec2Add(shirei.GetFrameInput().Scroll, shirei.Vec2{0, amt})
	}
}

func onKey(wparam, lparam uintptr, down bool) {
	updateModifiers()
	if uint32(wparam) == vkProcesskey {
		return
	}
	code := mapKey(uint32(wparam), lparam)
	if code == shirei.KeyCodeNone {
		return
	}
	if down {
		shirei.GetFrameInput().Key = code
		g.SliceAddUniq(&shirei.GetInputState().DownKeys, code)
	} else {
		g.SliceRemove(&shirei.GetInputState().DownKeys, code)
	}
}

// onChar handles a typed character (WM_CHAR delivers UTF-16 code units).
// Control characters and modified shortcuts are suppressed — those arrive via
// onKey. Surrogate pairs are reassembled across two messages.
func onChar(u uint16) {
	if u >= 0xD800 && u < 0xDC00 { // high surrogate: wait for the low half
		currentWindow.pendingHi = u
		return
	}
	var r rune
	if u >= 0xDC00 && u < 0xE000 && currentWindow.pendingHi != 0 { // low surrogate
		r = utf16.DecodeRune(rune(currentWindow.pendingHi), rune(u))
		currentWindow.pendingHi = 0
	} else {
		currentWindow.pendingHi = 0
		r = rune(u)
	}
	if r < 0x20 || r == 0x7f {
		return
	}
	if shirei.GetInputState().Modifiers&(shirei.ModCmd|shirei.ModCtrl) != 0 {
		return
	}
	appendPendingText(string(r))
}

func appendPendingText(s string) {
	currentWindow.pendingText += s
}

func flushPendingText() {
	if currentWindow.pendingText == "" {
		return
	}
	shirei.GetFrameInput().Text += currentWindow.pendingText
	currentWindow.pendingText = ""
}

func onImeComposition(lparam uintptr) {
	himc, release := imeContext()
	if himc == 0 {
		return
	}
	defer release()

	if lparam&gcsResultstr != 0 {
		appendPendingText(imeCompositionString(himc, gcsResultstr))
		clearComposition()
	}
	if lparam&gcsCompstr != 0 {
		u16 := imeCompositionUTF16(himc, gcsCompstr)
		if len(u16) == 0 {
			clearComposition()
			return
		}
		setCompositionUTF16(u16)
	}
}

func imeContext() (uintptr, func()) {
	himc, _, _ := procImmGetContext.Call(uintptr(currentWindow.hwnd))
	if himc == 0 {
		return 0, func() {}
	}
	return himc, func() {
		procImmReleaseContext.Call(uintptr(currentWindow.hwnd), himc)
	}
}

func imeCompositionString(himc uintptr, index uintptr) string {
	u16 := imeCompositionUTF16(himc, index)
	if len(u16) == 0 {
		return ""
	}
	return string(utf16.Decode(u16))
}

func imeCompositionUTF16(himc uintptr, index uintptr) []uint16 {
	n, _, _ := procImmGetCompositionString.Call(himc, index, 0, 0)
	if int32(n) <= 0 {
		return nil
	}
	buf := make([]uint16, int(n)/2)
	if len(buf) == 0 {
		return nil
	}
	got, _, _ := procImmGetCompositionString.Call(
		himc,
		index,
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(len(buf)*2),
	)
	if int32(got) <= 0 {
		return nil
	}
	return buf[:int(got)/2]
}

func clearComposition() {
	shirei.GetInputState().Composition = ""
	shirei.GetInputState().CompositionSel = [2]int{}
}

func setCompositionUTF16(u16 []uint16) {
	cursor := utf16UnitOffsetToRuneOffset(u16, len(u16))
	shirei.GetInputState().Composition = string(utf16.Decode(u16))
	shirei.GetInputState().CompositionSel = [2]int{cursor, cursor}
}

func utf16UnitOffsetToRuneOffset(u16 []uint16, offset int) int {
	if offset < 0 {
		offset = 0
	}
	if offset > len(u16) {
		offset = len(u16)
	}
	return len(utf16.Decode(u16[:offset]))
}

func updateImeCandidateWindow() {
	if shirei.GetInputState().Composition == "" {
		return
	}
	himc, release := imeContext()
	if himc == 0 {
		return
	}
	defer release()

	form := candidateForm{
		Style:      cfsCandidatepos,
		CurrentPos: candidatePoint(shirei.GetHost().CompositionPos, shirei.GetHost().WindowScale),
	}
	procImmSetCandidateWindow.Call(himc, uintptr(unsafe.Pointer(&form)))
}

func candidatePoint(pos shirei.Vec2, scale float32) win32Point {
	if scale <= 0 {
		scale = 1
	}
	return win32Point{
		X: int32(pos[0]*scale + 0.5),
		Y: int32(pos[1]*scale + 0.5),
	}
}

func commitImeBeforeInterruption() {
	if shirei.GetInputState().Composition == "" {
		return
	}
	himc, release := imeContext()
	if himc != 0 {
		procImmNotifyIME.Call(himc, niCompositionstr, cpsComplete, 0)
		release()
	}
	clearComposition()
	if currentWindow.pendingText != "" {
		produceInterruptionFrame()
	}
}

func produceInterruptionFrame() {
	if framesInProgress > 0 || !currentWindow.ready {
		noteInput()
		return
	}
	cw, ch := clientSize()
	if cw <= 0 || ch <= 0 {
		return
	}
	framesInProgress++
	defer func() { framesInProgress-- }()
	produceAndRender(cw, ch)
	currentWindow.haveFrame = true
	currentWindow.dirty = false
}

// updateModifiers reads the live modifier-key state and mirrors it into shirei's
// InputState (both the Modifiers bitfield and DownKeys, which widgets consult).
func updateModifiers() {
	var m shirei.Modifiers
	if keyDown(vkShift) {
		m |= shirei.ModShift
	}
	if keyDown(vkControl) {
		m |= shirei.ModCtrl
	}
	if keyDown(vkMenu) {
		m |= shirei.ModAlt
	}
	if keyDown(vkLwin) || keyDown(vkRwin) {
		m |= shirei.ModSuper
	}
	shirei.GetInputState().Modifiers = m

	syncModKey(m, shirei.ModShift, shirei.KeyShift)
	syncModKey(m, shirei.ModCtrl, shirei.KeyCtrl)
	syncModKey(m, shirei.ModAlt, shirei.KeyAlt)
	syncModKey(m, shirei.ModSuper, shirei.KeySuper)
}

func syncModKey(m, bit shirei.Modifiers, k shirei.KeyCode) {
	if m&bit != 0 {
		g.SliceAddUniq(&shirei.GetInputState().DownKeys, k)
	} else {
		g.SliceRemove(&shirei.GetInputState().DownKeys, k)
	}
}

func keyDown(vk int) bool {
	s, _, _ := procGetKeyState.Call(uintptr(vk))
	// GetKeyState returns a SHORT; the high-order bit means "down", i.e. the
	// 16-bit value is negative.
	return int16(s) < 0
}

// mapKey maps a keystroke to a shirei KeyCode. The writing block resolves by
// scancode (lParam bits 16-23) — positional, so KeyW is the physical key at
// the US-QWERTY W position no matter the active layout (virtual-key codes
// follow the layout: AZERTY's physical Q sends VK_A). Typed text still
// honors the layout via WM_CHAR. Special keys resolve by virtual-key code;
// extended keys (bit 24: arrows, numpad enter, right-side modifiers) are
// never writing-block keys, so they skip the scancode table.
func mapKey(vk uint32, lparam uintptr) shirei.KeyCode {
	extended := lparam&(1<<24) != 0
	if !extended {
		if code := qwerty.FromScan(uint16(lparam >> 16 & 0xFF)); code != shirei.KeyCodeNone {
			return code
		}
	}
	return mapVKey(vk)
}

// mapVKey maps a Win32 virtual-key code to a shirei KeyCode (special keys;
// the writing block is handled positionally in mapKey).
func mapVKey(vk uint32) shirei.KeyCode {
	switch vk {
	case vkLeft:
		return shirei.KeyLeft
	case vkRight:
		return shirei.KeyRight
	case vkUp:
		return shirei.KeyUp
	case vkDown:
		return shirei.KeyDown
	case vkReturn:
		return shirei.KeyEnter
	case vkEscape:
		return shirei.KeyEscape
	case vkBack:
		return shirei.KeyDeleteBackward
	case vkDelete:
		return shirei.KeyDeleteForward
	case vkHome:
		return shirei.KeyHome
	case vkEnd:
		return shirei.KeyEnd
	case vkInsert:
		return shirei.KeyInsert
	case vkPrior:
		return shirei.KeyPageUp
	case vkNext:
		return shirei.KeyPageDown
	case vkTab:
		return shirei.KeyTab
	case vkSpace:
		return shirei.KeySpace
	}
	if vk >= vkF1 && vk <= vkF12 {
		return shirei.KeyF1 + shirei.KeyCode(vk-vkF1)
	}
	// Letters (VK 'A'..'Z') and digits ('0'..'9') share ASCII codes with KeyA../Key0..
	if vk >= 'A' && vk <= 'Z' {
		return shirei.KeyCode(vk)
	}
	if vk >= '0' && vk <= '9' {
		return shirei.KeyCode(vk)
	}
	return shirei.KeyCodeNone
}

// -----------------------------------------------------------------------------
//  Clipboard
// -----------------------------------------------------------------------------

func getClipboard() string {
	if r, _, _ := procOpenClipboard.Call(0); r == 0 {
		return ""
	}
	defer procCloseClipboard.Call()

	h, _, _ := procGetClipboardData.Call(cfUnicodeText)
	if h == 0 {
		return ""
	}
	p, _, _ := procGlobalLock.Call(h)
	if p == 0 {
		return ""
	}
	defer procGlobalUnlock.Call(h)

	var u16 []uint16
	ptr := unsafe.Pointer(p)
	for {
		c := *(*uint16)(ptr)
		if c == 0 {
			break
		}
		u16 = append(u16, c)
		ptr = unsafe.Add(ptr, 2)
	}
	return string(utf16.Decode(u16))
}

// openURL opens url in the system browser (FrameOutputData.OpenURL). Errors ignored.
func openURL(url string) {
	if url == "" {
		return
	}
	_ = browser.OpenURL(url)
}

func setClipboard(s string) {
	u16, err := syscall.UTF16FromString(s) // NUL-terminated
	if err != nil {
		return
	}
	if r, _, _ := procOpenClipboard.Call(0); r == 0 {
		return
	}
	defer procCloseClipboard.Call()
	procEmptyClipboard.Call()

	n := uintptr(len(u16) * 2)
	h, _, _ := procGlobalAlloc.Call(gmemMoveable, n)
	if h == 0 {
		return
	}
	p, _, _ := procGlobalLock.Call(h)
	if p == 0 {
		return
	}
	dst := unsafe.Slice((*uint16)(unsafe.Pointer(p)), len(u16))
	copy(dst, u16)
	procGlobalUnlock.Call(h)
	// On success the system owns the handle; don't free it.
	procSetClipboardData.Call(cfUnicodeText, h)
}

// -----------------------------------------------------------------------------
//  helpers
// -----------------------------------------------------------------------------

func getModuleHandle() syscall.Handle {
	h, _, _ := procGetModuleHandleW.Call(0)
	return syscall.Handle(h)
}

func errStr(err error) string {
	if err == nil {
		return "unknown error"
	}
	return err.Error()
}
