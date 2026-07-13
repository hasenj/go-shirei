// Package win32backend is a direct-Windows backend for shirei. The Win32 API
// provides the window, message loop, and input; all rasterization is done by
// shirei's core software renderer, which renders each frame into a top-down
// 32-bit DIB section that GDI blits to the window (BitBlt). It mirrors
// cocoabackend (the reference shell) minus the rasterizer, and is the Windows
// half of the GOOS-selected go.hasen.dev/shirei/app wrapper.
//
// It is pure Go (no cgo): the Win32 entry points are bound lazily through the
// standard syscall package, so it cross-compiles from any OS with
// GOOS=windows. See ../notes/backends-plan.md.
package win32backend

import (
	"fmt"
	"runtime"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"

	g "go.hasen.dev/generic"
	"go.hasen.dev/shirei"
	"go.hasen.dev/shirei/internal/qwerty"
)

// glyphCacheBudget caps total cached glyph-bitmap bytes (enables the shared core
// glyph cache). Matches cocoabackend.
const glyphCacheBudget = 16 << 20

// frameTimerID identifies the animation timer (per-window timer id).
const frameTimerID = 1

var (
	winTitle string
	winW     int
	winH     int
	frameFn  shirei.FrameFn

	hwnd      syscall.Handle
	hinstance syscall.Handle

	// DIB present surface: a top-down 32bpp BGRA bitmap selected into a memory
	// DC. The renderer writes straight into dibBuf; BitBlt copies it to the window.
	memDC   syscall.Handle
	dibBM   syscall.Handle
	dibBuf  []byte
	dibW    int
	dibH    int
	dibBits unsafe.Pointer

	softRenderer shirei.SoftRenderer

	dirty      bool // a new frame must be produced+rendered before the next blit
	haveFrame  bool
	wantsFrame bool // last frame asked to be re-run (animation/async work)
	timerOn    bool

	pendingHi   uint16 // pending UTF-16 high surrogate from WM_CHAR
	pendingText string // committed text to deliver on the next frame

	wndProcCB = syscall.NewCallback(wndProc)
)

// SetupWindow records the window parameters. The window is created in Run, on
// the UI thread.
func SetupWindow(title string, width, height int) {
	winTitle = title
	winW = width
	winH = height
}

// Run opens the window and runs the Win32 message loop. It must be called
// from the program's main goroutine (the message loop and window must share
// one OS thread) and does not return until the window closes. System fonts
// are initialized by shirei on the first frame (RunFrameFn), not here.
func Run(fn shirei.FrameFn) {
	runtime.LockOSThread()

	frameFn = fn

	shirei.GlyphCacheBudgetBytes = glyphCacheBudget

	enableDPIAwareness()
	createWindow()
	messageLoop()
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

func createWindow() {
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
	if r, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		panic("win32backend: RegisterClassEx failed: " + errStr(err))
	}

	// Size the window so its *client* area is winW×winH in logical points:
	// SetupWindow sizes are points (as on macOS), and since we are DPI-aware
	// the numbers we pass here are device pixels — on a 200% display an
	// unscaled winW×winH window would come out half the intended size.
	// WM_DPICHANGED is no help at creation (it only fires on later changes),
	// so scale by the creating monitor's DPI up front; CW_USEDEFAULT places
	// us on the primary monitor, which is what dpiForCreation reports.
	dpi := dpiForCreation()
	r := win32Rect{0, 0,
		int32(uintptr(winW) * dpi / 96),
		int32(uintptr(winH) * dpi / 96)}
	if procAvailable(procAdjustWindowRectExForDpi) {
		procAdjustWindowRectExForDpi.Call(uintptr(unsafe.Pointer(&r)), wsOverlappedWindow, 0, 0, dpi)
	} else {
		procAdjustWindowRect.Call(uintptr(unsafe.Pointer(&r)), wsOverlappedWindow, 0)
	}
	wWidth := int(r.Right - r.Left)
	wHeight := int(r.Bottom - r.Top)
	if perfEnabled {
		fmt.Printf("[win32] creation dpi %d -> client %dx%d px\n", dpi, uintptr(winW)*dpi/96, uintptr(winH)*dpi/96)
	}

	title, _ := syscall.UTF16PtrFromString(winTitle)
	h, _, err := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(title)),
		wsOverlappedWindow|wsVisible,
		cwUseDefault, cwUseDefault,
		uintptr(wWidth), uintptr(wHeight),
		0, 0, uintptr(hinstance), 0,
	)
	if h == 0 {
		panic("win32backend: CreateWindowEx failed: " + errStr(err))
	}
	hwnd = syscall.Handle(h)

	procShowWindow.Call(uintptr(hwnd), swShow)
	procUpdateWindow.Call(uintptr(hwnd))

	// Pull the new window to the front and give it keyboard focus. When launched
	// from a terminal another app owns the foreground, so ShowWindow alone leaves
	// us unfocused; SetForegroundWindow asks the window manager to activate us
	// (under CrossOver this maps to winemac.drv bringing the app forward).
	procBringWindowToTop.Call(uintptr(hwnd))
	procSetForegroundWindow.Call(uintptr(hwnd))
	procSetFocus.Call(uintptr(hwnd))

	applyWindowIcon()
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
	}
}

// -----------------------------------------------------------------------------
//  Window procedure
// -----------------------------------------------------------------------------

func wndProc(hWnd, msg, wparam, lparam uintptr) uintptr {
	switch uint32(msg) {
	case wmPaint:
		onPaint()
		return 0

	case wmErasebkgnd:
		return 1 // we paint every pixel; skip the background erase (no flicker)

	case wmSize:
		dirty = true
		invalidate()
		return 0

	case wmDpichanged:
		// lParam points at the suggested new window rect for the new DPI.
		nr := (*win32Rect)(unsafe.Pointer(lparam))
		procSetWindowPos.Call(uintptr(hwnd), 0,
			uintptr(nr.Left), uintptr(nr.Top),
			uintptr(nr.Right-nr.Left), uintptr(nr.Bottom-nr.Top),
			swpNozorder|swpNoactivate)
		dirty = true
		invalidate()
		return 0
	case wmKillfocus:
		clearComposition()
		noteInput()
		return 0

	case wmMousemove:
		onMouse(lparam, 0, 0)
		noteInput()
		return 0

	case wmLbuttondown:
		commitImeBeforeInterruption()
		procSetCapture.Call(uintptr(hwnd))
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
		if wantsFrame || shirei.FrameRequested() {
			dirty = true
			invalidate()
		}
		return 0

	case wmDestroy:
		procPostQuitMessage.Call(0)
		return 0
	}

	r, _, _ := procDefWindowProcW.Call(hWnd, msg, wparam, lparam)
	return r
}

// invalidate schedules a WM_PAINT for the whole client area (no background erase).
func invalidate() {
	procInvalidateRect.Call(uintptr(hwnd), 0, 0)
}

// noteInput marks that input changed so the next paint produces a fresh frame,
// and schedules that paint.
func noteInput() {
	dirty = true
	invalidate()
}

// -----------------------------------------------------------------------------
//  Frame production + present
// -----------------------------------------------------------------------------

func onPaint() {
	var ps paintStruct
	hdc, _, _ := procBeginPaint.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&ps)))
	defer procEndPaint.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&ps)))

	cw, ch := clientSize()
	if cw <= 0 || ch <= 0 {
		return
	}
	if !ensureDIB(cw, ch) {
		return
	}

	if dirty || !haveFrame {
		produceAndRender(cw, ch)
		dirty = false
	}

	// Blit the rendered DIB to the window (GDI copies, so no tearing concern).
	procBitBlt.Call(hdc, 0, 0, uintptr(cw), uintptr(ch),
		uintptr(memDC), 0, 0, srccopy)
}

// produceAndRender runs one shirei frame and rasterizes it into the DIB buffer.
func produceAndRender(cw, ch int) {
	scale := dpiScale()
	shirei.WindowScale = scale
	shirei.WindowSize = shirei.Vec2{float32(cw) / scale, float32(ch) / scale}

	flushPendingText()

	t0 := time.Now()
	out := shirei.RunFrameFn(frameFn)
	perfRecordProduce(time.Since(t0))
	updateImeCandidateWindow()

	if out.Copy != "" {
		setClipboard(out.Copy)
	}
	if out.Paste {
		appendPendingText(getClipboard())
	}

	t1 := time.Now()
	softRenderer.RenderInto(dibBuf, dibW*4, cw, ch, scale, out.Surfaces)
	perfRecordPaint(time.Since(t1))

	haveFrame = true
	wantsFrame = out.NextFrameRequested
	// Keep the timer running even when this frame settled: a later
	// RequestNextFrame from a background goroutine must be able to wake
	// the message loop (same role as cocoa's always-ticking CADisplayLink).
	// wmTimer only invalidates when wantsFrame || FrameRequested, so idle
	// ticks are cheap.
	startTimer()
}

// ensureDIB (re)creates the DIB present surface when the client size changes.
// Returns false if creation failed.
func ensureDIB(w, h int) bool {
	if dibBM != 0 && dibW == w && dibH == h {
		return true
	}
	releaseDIB()

	if memDC == 0 {
		dc, _, _ := procCreateCompatibleDC.Call(0)
		memDC = syscall.Handle(dc)
		if memDC == 0 {
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
		uintptr(memDC),
		uintptr(unsafe.Pointer(&bmi)),
		dibRGBColors,
		uintptr(unsafe.Pointer(&dibBits)),
		0, 0,
	)
	if bm == 0 || dibBits == nil {
		return false
	}
	dibBM = syscall.Handle(bm)
	procSelectObject.Call(uintptr(memDC), bm)

	dibW, dibH = w, h
	// 32bpp rows are always dword-aligned, so stride == w*4.
	dibBuf = unsafe.Slice((*byte)(dibBits), w*h*4)
	return true
}

func releaseDIB() {
	if dibBM != 0 {
		procDeleteObject.Call(uintptr(dibBM))
		dibBM = 0
	}
	dibBuf = nil
	dibBits = nil
	dibW, dibH = 0, 0
}

func clientSize() (int, int) {
	var r win32Rect
	procGetClientRect.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&r)))
	return int(r.Right - r.Left), int(r.Bottom - r.Top)
}

// dpiScale returns device pixels per logical point. Per-window DPI when
// available, else the same system-level fallbacks as dpiForCreation — the
// two must agree, or the window would be sized for one scale and rendered
// at another.
func dpiScale() float32 {
	if procAvailable(procGetDpiForWindow) {
		if dpi, _, _ := procGetDpiForWindow.Call(uintptr(hwnd)); dpi != 0 {
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
	if timerOn {
		return
	}
	// ~60 Hz wake; each tick re-invalidates while animation is wanted.
	procSetTimer.Call(uintptr(hwnd), frameTimerID, 16, 0)
	timerOn = true
}

func stopTimer() {
	if !timerOn {
		return
	}
	procKillTimer.Call(uintptr(hwnd), frameTimerID)
	timerOn = false
}

// -----------------------------------------------------------------------------
//  Input translation (Win32 messages -> shirei input globals; sample, not queue)
// -----------------------------------------------------------------------------

// onMouse records pointer position (and an optional click/release). lParam packs
// the client-area position in device pixels; shirei works in logical points.
func onMouse(lparam uintptr, button, action int) {
	scale := shirei.WindowScale
	if scale <= 0 {
		scale = 1
	}
	x := float32(int16(lparam&0xffff)) / scale
	y := float32(int16((lparam>>16)&0xffff)) / scale

	np := shirei.Vec2{x, y}
	prev := shirei.InputState.MousePoint
	shirei.FrameInput.Motion = shirei.Vec2Add(shirei.FrameInput.Motion, shirei.Vec2Sub(np, prev))
	shirei.InputState.MousePoint = np

	updateModifiers()

	switch action {
	case int(shirei.MouseClick):
		shirei.InputState.MouseButton = shirei.MouseButton(button)
		shirei.FrameInput.Mouse = shirei.MouseClick
	case int(shirei.MouseRelease):
		shirei.InputState.MouseButton = shirei.MouseButton(button)
		shirei.FrameInput.Mouse = shirei.MouseRelease
	}
}

// onWheel maps a wheel notch (WHEEL_DELTA == 120 per detent) to a scroll amount.
// Sign is negated to match shirei's convention (as cocoabackend does); ~30 pts
// per notch approximates the cocoa feel. horizontal => x axis.
func onWheel(wparam uintptr, horizontal bool) {
	delta := float32(int16((wparam>>16)&0xffff)) / 120
	amt := -delta * 30
	if horizontal {
		shirei.FrameInput.Scroll = shirei.Vec2Add(shirei.FrameInput.Scroll, shirei.Vec2{amt, 0})
	} else {
		shirei.FrameInput.Scroll = shirei.Vec2Add(shirei.FrameInput.Scroll, shirei.Vec2{0, amt})
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
		shirei.FrameInput.Key = code
		g.SliceAddUniq(&shirei.InputState.DownKeys, code)
	} else {
		g.SliceRemove(&shirei.InputState.DownKeys, code)
	}
}

// onChar handles a typed character (WM_CHAR delivers UTF-16 code units).
// Control characters and modified shortcuts are suppressed — those arrive via
// onKey. Surrogate pairs are reassembled across two messages.
func onChar(u uint16) {
	if u >= 0xD800 && u < 0xDC00 { // high surrogate: wait for the low half
		pendingHi = u
		return
	}
	var r rune
	if u >= 0xDC00 && u < 0xE000 && pendingHi != 0 { // low surrogate
		r = utf16.DecodeRune(rune(pendingHi), rune(u))
		pendingHi = 0
	} else {
		pendingHi = 0
		r = rune(u)
	}
	if r < 0x20 || r == 0x7f {
		return
	}
	if shirei.InputState.Modifiers&(shirei.ModCmd|shirei.ModCtrl) != 0 {
		return
	}
	appendPendingText(string(r))
}

func appendPendingText(s string) {
	pendingText += s
}

func flushPendingText() {
	if pendingText == "" {
		return
	}
	shirei.FrameInput.Text += pendingText
	pendingText = ""
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
	himc, _, _ := procImmGetContext.Call(uintptr(hwnd))
	if himc == 0 {
		return 0, func() {}
	}
	return himc, func() {
		procImmReleaseContext.Call(uintptr(hwnd), himc)
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
	shirei.InputState.Composition = ""
	shirei.InputState.CompositionSel = [2]int{}
}

func setCompositionUTF16(u16 []uint16) {
	cursor := utf16UnitOffsetToRuneOffset(u16, len(u16))
	shirei.InputState.Composition = string(utf16.Decode(u16))
	shirei.InputState.CompositionSel = [2]int{cursor, cursor}
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
	if shirei.InputState.Composition == "" {
		return
	}
	himc, release := imeContext()
	if himc == 0 {
		return
	}
	defer release()

	form := candidateForm{
		Style:      cfsCandidatepos,
		CurrentPos: candidatePoint(shirei.CompositionPos, shirei.WindowScale),
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
	if shirei.InputState.Composition == "" {
		return
	}
	himc, release := imeContext()
	if himc != 0 {
		procImmNotifyIME.Call(himc, niCompositionstr, cpsComplete, 0)
		release()
	}
	clearComposition()
	if pendingText != "" {
		produceInterruptionFrame()
	}
}

func produceInterruptionFrame() {
	cw, ch := clientSize()
	if cw <= 0 || ch <= 0 {
		return
	}
	if !ensureDIB(cw, ch) {
		return
	}
	produceAndRender(cw, ch)
	dirty = false
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
	shirei.InputState.Modifiers = m

	syncModKey(m, shirei.ModShift, shirei.KeyShift)
	syncModKey(m, shirei.ModCtrl, shirei.KeyCtrl)
	syncModKey(m, shirei.ModAlt, shirei.KeyAlt)
	syncModKey(m, shirei.ModSuper, shirei.KeySuper)
}

func syncModKey(m, bit shirei.Modifiers, k shirei.KeyCode) {
	if m&bit != 0 {
		g.SliceAddUniq(&shirei.InputState.DownKeys, k)
	} else {
		g.SliceRemove(&shirei.InputState.DownKeys, k)
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
