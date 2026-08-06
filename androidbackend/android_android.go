//go:build android

// Package androidbackend is a direct-Android (NativeActivity) backend for
// shirei. The vendored NDK native_app_glue owns activity lifecycle plumbing;
// all rasterization is done by shirei's core software renderer straight into
// the ANativeWindow's locked CPU buffer (RGBA via Host.PixelOrder).
//
// Input: multi-touch fills InputState.Touches (+ FrameInput began/ended).
// Independently, the primary finger is synthesized into mouse + wheel
// (browser-like scroll-vs-tap) with optional fling — same synthesizer as
// iosbackend (kept in sync; extraction into a shared package is a follow-up
// once both shapes are proven on device).
//
// Control flow: the glue spawns an app thread and calls android_main (shim.c),
// which calls the Go main() via the mobilerun-generated export. main reaches
// app.Run → Run, which never returns: it polls the looper (lifecycle + touch
// callbacks fire from inside the poll) and produces/presents frames.
//
// Soft keyboard / IME: ShireiActivity (a small Java NativeActivity subclass
// that mobilerun compiles into the APK) forwards InputConnection traffic —
// committed text, deletes, composition — through jni.c into the pending-input
// queue here; WantsKeyboard edges call back up to show/hide the IME. Basic
// composition maps onto core's Composition/CompositionSel. Not yet done:
// clipboard, candidate-window positioning, selection/caret sync with the IME.
package androidbackend

/*
#cgo LDFLAGS: -landroid -llog

#include <stdlib.h>
#include "android_native_app_glue.h"
#include "shim.h"
*/
import "C"

import (
	"bufio"
	"math"
	"os"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"
	"unsafe"

	"go.hasen.dev/shirei"
)

// glyphCacheBudget caps total cached glyph-bitmap bytes (enables the shared
// core glyph cache). Matches cocoabackend / iosbackend.
const glyphCacheBudget = 16 << 20

// Touch phases from shim.c (keep in sync).
const (
	touchPhaseBegin  = 0
	touchPhaseMove   = 1
	touchPhaseEnd    = 2
	touchPhaseCancel = 3
)

// idlePollMillis is the looper wait while no frame is wanted. Background
// goroutines request frames via an atomic the loop can only poll (no wake
// hook), so idle wakeups stay this coarse.
const idlePollMillis = 50

// ---------------------------------------------------------------------------
// Touch synthesizer — copied from iosbackend (keep in sync). Tunables and
// state machine are identical; only the event source differs (AMotionEvent
// via shim.c instead of UITouch via ios.m).
// ---------------------------------------------------------------------------

// TouchSlop is the movement threshold in logical points past which a
// single-finger gesture becomes scrolling instead of a tap.
var TouchSlop float32 = 10

// ClickHoldDuration is how long a synthesized tap holds MousePrimary between
// MouseClick and MouseRelease so PressAction / button press chrome can paint.
var ClickHoldDuration = 100 * time.Millisecond

// Fling tunables (logical points / second). See iosbackend for the knobs'
// meaning; values match until Android-specific tuning proves necessary.
var (
	FlingMinSpeed   float32 = 70
	FlingStopSpeed  float32 = 18
	FlingMaxSpeed   float32 = 6000
	FlingFriction   float32 = 2.0
	FlingBoost      float32 = 1.25
	FlingVelAlpha   float32 = 0.45
	FlingStaleAfter         = 80 * time.Millisecond
)

// Single-finger synthesizer states (browser-like scroll-vs-tap + fling).
const (
	touchIdle    = iota
	touchPending // down; hover only — not yet scroll or click
	touchScroll  // past TouchSlop; emit FrameInput.Scroll, never click
	touchFling   // post-lift momentum: decaying FrameInput.Scroll
)

// offscreen parks the pointer after lift so hover does not stick on the last
// touched control.
var offscreen = shirei.Vec2{-1000, -1000}

var (
	frameFn shirei.FrameFn

	softRenderer shirei.SoftRenderer

	// present skip: hash of last presented surfaces + its content rect
	lastPresentedHash                      uint64
	havePresented                          bool
	presentL, presentT, presentW, presentH int

	// pendingPark: after a gesture ends, move MousePoint offscreen.
	pendingPark bool

	// ---- raw touch table (pointer id+1 → slot) ----
	touchOSKey    [shirei.MaxTouches]uintptr // 0 = free
	nextTouchId   uint32                     // never 0
	pendingBegan  [shirei.MaxTouches]uint32
	pendingBeganN int
	pendingEnded  [shirei.MaxTouches]uint32
	pendingEndedN int

	// ---- single-finger mouse synthesizer (alongside raw Touches) ----
	touchState     int
	touchOrigin    shirei.Vec2
	touchLast      shirei.Vec2
	touchLastT     time.Time
	touchVel       shirei.Vec2
	flingVel       shirei.Vec2
	flingLastT     time.Time
	synthOSKey     uintptr // primary finger; 0 = synthesizer idle
	clickHeld      bool
	clickReleaseAt time.Time
)

// ---- soft keyboard / IME (fed by ShireiActivity via jni.c) ----
//
// The Java natives fire on the UI thread while the frame loop runs on the
// glue's app thread, so this pending state is mutex-guarded (unlike iOS,
// where callbacks and frames share the main thread). ALooper_wake in jni.c
// makes queued input take effect immediately instead of at the idle poll.
var (
	pendingMu             sync.Mutex
	pendingText           string
	pendingDeleteCount    int // each unit → one KeyDeleteBackward frame
	pendingKey            shirei.KeyCode
	pendingKeyMod         shirei.Modifiers // delivered with pendingKey (accessory bar)
	pendingComposition    string
	pendingCompositionSet bool // composition changed since last flush
	pendingClearFocus     bool

	// clearModsAfterFrame: the accessory bar has no physical modifier keys;
	// a Ctrl it sets for one delivered key is dropped after that frame
	// (iosbackend's clearAccessoryMods). App-thread only.
	clearModsAfterFrame bool

	// keyboardShown mirrors the last state pushed to Java; WantsKeyboard is
	// re-asserted every frame, this dedups the JNI calls to edges only.
	keyboardShown bool

	// appliedOrientation is the last PreferredOrientation sent to Java.
	// -1 forces the first frame to apply even when the Host still has Any.
	appliedOrientation shirei.PreferredOrientation = -1
)

// Android keycodes handled natively (values from android.view.KeyEvent).
const (
	akeycodeDpadUp      = 19
	akeycodeDpadDown    = 20
	akeycodeDpadLeft    = 21
	akeycodeDpadRight   = 22
	akeycodeEnter       = 66
	akeycodeDel         = 67
	akeycodeNumpadEnter = 160
)

// SetupWindow records the window title and preferred size. On Android the
// surface is always full-screen; both are ignored, kept for API parity.
func SetupWindow(title string, width, height int) {
	_ = title
	_ = width
	_ = height
}

// CenterWindow is a no-op on Android (full-screen). Kept for API parity.
func CenterWindow() {}

// PositionWindow is a no-op on Android (full-screen). Kept for API parity.
func PositionWindow(x, y int) { _, _ = x, y }

// Run enters the frame loop and never returns. Must be called on the glue's
// app thread (android_main → shirei_android_main → main → app.Run → here).
// mobileComfortScale fattens default control chrome for finger targets
// (keep in sync with iosbackend).
const mobileComfortScale float32 = 1.25

func Run(fn shirei.FrameFn) {
	runtime.LockOSThread()
	frameFn = wrapFrame(fn)
	shirei.GetHost().GlyphCacheBudgetBytes = glyphCacheBudget
	shirei.GetHost().EscapeHatchBackendContext = Context{}
	shirei.GetHost().WindowFocused = true
	shirei.GetHost().ComfortScale = mobileComfortScale
	// ANativeWindow is WINDOW_FORMAT_RGBA_8888 — render in that order so we
	// never need a per-frame R/B swizzle (which also blocked dirty-rect reuse).
	shirei.GetHost().PixelOrder = shirei.PixelOrderRGBA
	// Lock orientation before the first frame (apps often set Host preference
	// in main before Run) so we do not paint portrait then flip.
	if want := shirei.GetHost().PreferredOrientation; want != appliedOrientation {
		appliedOrientation = want
		C.shirei_android_set_orientation(C.int(want))
	}

	for {
		timeout := C.int(idlePollMillis)
		if wantFrame() {
			timeout = 0
		}
		flags := C.shirei_android_poll(timeout)
		if flags&C.SHIREI_POLL_DESTROY != 0 {
			// Back button / activity teardown. NativeActivity may recreate the
			// activity in the same process, which would re-enter main() — exit
			// instead so every launch starts clean.
			os.Exit(0)
		}
		if flags&C.SHIREI_POLL_HAS_WINDOW == 0 {
			havePresented = false
			continue
		}
		if wantFrame() || !havePresented {
			produceAndPresent()
		}
	}
}

// wantFrame mirrors iosbackend's shireiFrameRequested: any pending synth
// activity keeps frames coming (click hold, fling coast, park).
func wantFrame() bool {
	return shirei.FrameRequested() || pendingPark || clickHeld ||
		touchState != touchIdle || pendingIMEWaiting()
}

func pendingIMEWaiting() bool {
	pendingMu.Lock()
	defer pendingMu.Unlock()
	return pendingText != "" || pendingDeleteCount > 0 || pendingKey != 0 ||
		pendingCompositionSet || pendingClearFocus
}

// flushPendingIME moves queued keyboard/IME input into this frame's
// FrameInput/InputState, one delete per frame (iOS pacing). While a
// composition is live, deletes belong to the IME, not the app.
func flushPendingIME() {
	pendingMu.Lock()
	if pendingCompositionSet {
		st := shirei.GetInputState()
		st.Composition = pendingComposition
		n := len([]rune(pendingComposition))
		st.CompositionSel = [2]int{n, n}
		pendingCompositionSet = false
	}
	if pendingText != "" {
		shirei.GetFrameInput().Text += pendingText
		pendingText = ""
	}
	if pendingKey != 0 && shirei.GetFrameInput().Key == shirei.KeyCodeNone {
		shirei.GetFrameInput().Key = pendingKey
		if pendingKeyMod != 0 {
			shirei.GetInputState().Modifiers = pendingKeyMod
			clearModsAfterFrame = true
		}
		pendingKey = 0
		pendingKeyMod = 0
	} else if pendingDeleteCount > 0 && shirei.GetFrameInput().Key == shirei.KeyCodeNone &&
		shirei.GetInputState().Composition == "" {
		shirei.GetFrameInput().Key = shirei.KeyDeleteBackward
		pendingDeleteCount--
	}
	clearFocus := pendingClearFocus
	pendingClearFocus = false
	pendingMu.Unlock()
	if clearFocus {
		// IME "Done": drop text focus so WantsKeyboard clears and the
		// keyboard sync below hides it (same as iOS accessoryDone).
		shirei.ClearFocus()
	}
}

// contentOrigin is the content rect's top-left in device pixels, for touch
// conversion (updated per frame; app-thread only).
var contentOriginX, contentOriginY int

func produceAndPresent() {
	scale := float32(C.shirei_android_density()) / 160
	if scale <= 0 {
		scale = 2 // sane phone default when density is unreported
	}
	devW := int(C.shirei_android_window_width())
	devH := int(C.shirei_android_window_height())
	if devW <= 0 || devH <= 0 {
		return
	}

	// The scene lives in the window's CONTENT RECT, not the full surface: the
	// surface never resizes — status/nav bars and (with adjustResize) the
	// soft keyboard only shrink this rect. WindowSize follows it; rendering
	// offsets into the surface accordingly.
	var cl, ct, cr, cb C.int
	C.shirei_android_content_rect(&cl, &ct, &cr, &cb)
	conL, conT, conR, conB := int(cl), int(ct), int(cr), int(cb)
	if conL < 0 {
		conL = 0
	}
	if conT < 0 {
		conT = 0
	}
	if conR > devW || conR <= conL {
		conR = devW
	}
	if conB > devH || conB <= conT {
		conB = devH
	}
	conW, conH := conR-conL, conB-conT
	if conW <= 0 || conH <= 0 {
		conL, conT, conW, conH = 0, 0, devW, devH
	}
	contentOriginX, contentOriginY = conL, conT

	shirei.GetHost().WindowSize = shirei.Vec2{float32(conW) / scale, float32(conH) / scale}
	shirei.GetHost().WindowScale = scale
	// Physical keyboard (config / InputDevice); soft IME does not count.
	// May flip when a Bluetooth keyboard attaches or detaches.
	prevHW := shirei.GetHost().HardwareKeyboard
	shirei.GetHost().HardwareKeyboard = C.shirei_android_hardware_keyboard() != 0
	if shirei.GetHost().HardwareKeyboard != prevHW {
		shirei.RequestNextFrame()
	}

	// Synthetic tap release (ClickHoldDuration after MouseClick).
	flushClickReleaseIfDue()
	// Touch began/ended edges for this frame (InputState.Touches already live).
	flushPendingTouchEdges()
	// Momentum: add decaying Scroll before the app frame.
	flushFling()
	// Queued keyboard/IME input (text, deletes, composition, editor keys).
	flushPendingIME()

	out := shirei.RunFrameFn(frameFn)

	if clearModsAfterFrame {
		shirei.GetInputState().Modifiers = 0
		clearModsAfterFrame = false
	}

	// Soft keyboard visibility tracks the frame's WantsKeyboard; edges only.
	if want := shirei.GetHost().WantsKeyboard; want != keyboardShown {
		keyboardShown = want
		if want {
			C.shirei_android_set_keyboard(1)
		} else {
			C.shirei_android_set_keyboard(0)
		}
	}

	// Preferred orientation: sticky Host field → Activity.setRequestedOrientation.
	if want := shirei.GetHost().PreferredOrientation; want != appliedOrientation {
		appliedOrientation = want
		C.shirei_android_set_orientation(C.int(want))
	}

	if out.Copy != "" {
		setClipboard(out.Copy)
	}
	if out.Paste {
		if s := getClipboard(); s != "" && textInputOK(s) {
			pendingMu.Lock()
			pendingText += s
			pendingMu.Unlock()
			shirei.RequestNextFrame()
		}
	}
	if out.OpenURL != "" {
		openURL(out.OpenURL)
	}

	// After a gesture ends (scroll lift, tap release, cancel), park so hover
	// does not stick. Do not park while a synthetic click is still held.
	if pendingPark && !clickHeld && shirei.GetFrameInput().Mouse != shirei.MouseClick {
		shirei.GetInputState().MousePoint = offscreen
		shirei.GetInputState().MouseButton = 0
		pendingPark = false
	}

	if havePresented && out.SurfacesHash == lastPresentedHash &&
		conL == presentL && conT == presentT && conW == presentW && conH == presentH {
		return
	}

	var bits unsafe.Pointer
	var w, h, stridePx C.int
	if C.shirei_android_lock_window(&bits, &w, &h, &stridePx) == 0 {
		return
	}
	strideBytes := int(stridePx) * 4
	bufW, bufH := int(w), int(h)
	dst := unsafe.Slice((*byte)(bits), strideBytes*bufH)
	// Re-clamp against the locked buffer (its dims are authoritative and can
	// disagree transiently with the pre-frame window dims during rotation).
	rl, rt, rw, rh := conL, conT, conW, conH
	if rl+rw > bufW {
		rw = bufW - rl
	}
	if rt+rh > bufH {
		rh = bufH - rt
	}
	if rl >= bufW || rt >= bufH || rw <= 0 || rh <= 0 {
		rl, rt, rw, rh = 0, 0, bufW, bufH
	}
	// The bands outside the content rect (under status/nav bars, and the
	// keyboard area on its hide frame) are cleared white every present —
	// the BufferQueue rotates buffers, so stale pixels would flicker.
	clearBandsWhite(dst, strideBytes, bufW, bufH, rl, rt, rw, rh)
	sub := dst[rt*strideBytes+rl*4:]
	// SoftRenderer writes RGBA (Host.PixelOrder) straight into the locked window.
	softRenderer.RenderInto(sub, strideBytes, rw, rh, scale, out.Surfaces)
	C.shirei_android_unlock_post()

	lastPresentedHash = out.SurfacesHash
	presentL, presentT, presentW, presentH, havePresented = conL, conT, conW, conH, true
}

// clearBandsWhite fills every surface pixel outside the content rect with
// opaque white (matching the renderer's canvas), row-bands first, then the
// left/right column bands of the content rows.
func clearBandsWhite(p []byte, stride, w, h, cl, ct, cw, ch int) {
	fill := func(seg []byte) {
		for i := range seg {
			seg[i] = 0xff
		}
	}
	if ct > 0 {
		fill(p[:ct*stride])
	}
	if cb := ct + ch; cb < h {
		fill(p[cb*stride : h*stride])
	}
	cr := cl + cw
	for y := ct; y < ct+ch; y++ {
		row := p[y*stride:]
		if cl > 0 {
			fill(row[:cl*4])
		}
		if cr < w {
			fill(row[cr*4 : w*4])
		}
	}
}

//export shireiAndroidCmd
func shireiAndroidCmd(cmd C.int32_t) {
	switch cmd {
	case C.APP_CMD_INIT_WINDOW:
		havePresented = false
		// Window recreation drops IME state; re-sync on the next frame.
		keyboardShown = false
		shirei.RequestNextFrame()
	case C.APP_CMD_TERM_WINDOW:
		havePresented = false
	case C.APP_CMD_WINDOW_RESIZED, C.APP_CMD_CONFIG_CHANGED,
		C.APP_CMD_CONTENT_RECT_CHANGED:
		shirei.RequestNextFrame()
	case C.APP_CMD_GAINED_FOCUS:
		shirei.GetHost().WindowFocused = true
		shirei.RequestNextFrame()
	case C.APP_CMD_LOST_FOCUS:
		shirei.GetHost().WindowFocused = false
		shirei.RequestNextFrame()
	}
}

//export shireiAndroidTouch
func shireiAndroidTouch(x, y C.double, phase C.int, osKey C.uintptr_t) {
	// Coordinates arrive in device pixels relative to the surface; the scene
	// lives in the content rect, so shift by its origin (last frame's — a
	// one-frame skew during rect changes, same class of lag as iOS layout).
	scale := float32(C.shirei_android_density()) / 160
	if scale <= 0 {
		scale = 2
	}
	pt := shirei.Vec2{
		(float32(x) - float32(contentOriginX)) / scale,
		(float32(y) - float32(contentOriginY)) / scale,
	}
	key := uintptr(osKey)
	if key == 0 {
		return
	}
	ph := int(phase)

	switch ph {
	case touchPhaseBegin:
		slot := findFreeTouchSlot()
		if slot < 0 {
			return // drop extra contacts beyond MaxTouches
		}
		id := allocTouchId()
		touchOSKey[slot] = key
		shirei.GetInputState().Touches[slot] = shirei.TouchInfo{
			Active: true,
			Id:     id,
			Pos:    pt,
		}
		noteTouchBegan(id)

		if synthOSKey == 0 && !clickHeld &&
			(touchState == touchIdle || touchState == touchFling) {
			synthOSKey = key
			synthFromPrimary(pt, touchPhaseBegin)
		}
		shirei.RequestNextFrame()

	case touchPhaseMove:
		slot := findTouchSlotByOS(key)
		if slot < 0 {
			return
		}
		shirei.GetInputState().Touches[slot].Pos = pt
		if key == synthOSKey {
			synthFromPrimary(pt, touchPhaseMove)
		}
		shirei.RequestNextFrame()

	case touchPhaseEnd, touchPhaseCancel:
		slot := findTouchSlotByOS(key)
		if slot < 0 {
			return
		}
		id := shirei.GetInputState().Touches[slot].Id
		shirei.GetInputState().Touches[slot] = shirei.TouchInfo{}
		touchOSKey[slot] = 0
		noteTouchEnded(id)

		if key == synthOSKey {
			if ph == touchPhaseCancel {
				synthFromPrimary(pt, touchPhaseCancel)
			} else {
				synthFromPrimary(pt, touchPhaseEnd)
			}
		}
		shirei.RequestNextFrame()
	}
}

// ---- keyboard accessory bar -------------------------------------------
//
// iOS gets a native inputAccessoryView above the keyboard; Android has no
// such concept for a NativeActivity, so the bar is drawn by shirei itself:
// while the soft keyboard is up, the app's frame is wrapped in a column of
// [app content (grow)][46pt bar], and the bar's buttons feed the same
// pending-key path as iOS accessory actions (arrows plain; Select All /
// Copy / Paste as Ctrl+A/C/V — widgets' edit shortcuts use Ctrl off-darwin).
//
// Note: Host.WindowSize still reports the full window including the bar
// strip; the app's content area is sized by layout, not by WindowSize.

// accessoryBarHeight is the bar's height in logical points (finger-sized).
const accessoryBarHeight = 46

// wrapFrame installs the accessory bar below the app while the keyboard is
// up. The app-content container is ALWAYS present, keyboard or not: focus
// lives on identity-tree nodes and identity is path-scoped, so wrapping the
// app only-sometimes reparents every app container the moment the keyboard
// appears — the focused TextInput's node goes stale, focus drops,
// WantsKeyboard clears, and the keyboard dismisses itself in an oscillation.
// Only the bar (a trailing sibling) is conditional.
//
// Clicks landing in the bar are hidden from the app during its build:
// FocusOnClick treats any outside click as a blur, which would drop the
// TextInput's focus (dismissing the keyboard) before the bar button could
// act. The event is restored for the bar's own buttons afterwards.
func wrapFrame(appFn shirei.FrameFn) shirei.FrameFn {
	return func() {
		fi := shirei.GetFrameInput()
		full := shirei.GetHost().WindowSize
		var inBar bool
		var savedMouse shirei.MouseAction
		// The interception geometry keys off keyboardShown — the bar that was
		// actually visible last frame, which is what the finger aimed at.
		if keyboardShown {
			barTop := full[1] - accessoryBarHeight
			inBar = shirei.GetInputState().MousePoint[1] >= barTop
			savedMouse = fi.Mouse
			if inBar {
				fi.Mouse = 0
			}
			// Apps and popups see (and clamp against) the area above the
			// bar, not the full window — same bookkeeping as waylandbackend's
			// titlebar wrapper. Restored after the build for the next pass.
			shirei.GetHost().WindowSize[1] = barTop
		}
		// Viewport, not just Grow+Clip: without ExtrinsicSize the container
		// sizes to the app's content (which can dwarf the window) and the
		// bar gets pushed below the visible area.
		shirei.ContainerWithKey("app-content", shirei.Attrs(shirei.Viewport), func() {
			appFn()
			shirei.PopupsHost() // content-scoped: popups clamp above the bar
		})
		// Bar visibility is the frame's own WantsKeyboard — asserted by the
		// focused TextInput during the app build above. keyboardShown would
		// lag (it updates post-frame and resets on window recreation), which
		// left the bar undrawn with no follow-up frame requested.
		if shirei.GetHost().WantsKeyboard {
			if inBar {
				fi.Mouse = savedMouse
			}
			accessoryBar()
			if inBar {
				fi.Mouse = 0
			}
		}
		shirei.GetHost().WindowSize = full
	}
}

func accessoryBar() {
	shirei.Container(shirei.Attrs(shirei.Row, shirei.Expand, shirei.CrossAlign(shirei.AlignMiddle),
		shirei.MinHeight(accessoryBarHeight), shirei.MaxHeight(accessoryBarHeight),
		shirei.Background(0, 0, 94, 1), shirei.BorderWidth(1), shirei.BorderColor(0, 0, 82, 1),
		shirei.Pad2(5, 8), shirei.Gap(8)), func() {
		if barButton("←") {
			queueAccessoryKey(shirei.KeyLeft, 0)
		}
		if barButton("→") {
			queueAccessoryKey(shirei.KeyRight, 0)
		}
		shirei.Container(shirei.Attrs(shirei.Grow(1)), func() {})
		if barButton("All") {
			queueAccessoryKey(shirei.KeyA, shirei.ModCtrl)
		}
		if barButton("Copy") {
			queueAccessoryKey(shirei.KeyC, shirei.ModCtrl)
		}
		if barButton("Paste") {
			queueAccessoryKey(shirei.KeyV, shirei.ModCtrl)
		}
		shirei.Container(shirei.Attrs(shirei.Grow(1)), func() {})
		if barButton("Done") {
			shirei.ClearFocus()
			shirei.RequestNextFrame()
		}
	})
}

func barButton(label string) bool {
	var pressed bool
	shirei.Container(shirei.Attrs(shirei.Pad2(8, 14), shirei.Corners(8),
		shirei.Background(0, 0, 100, 1), shirei.BorderWidth(1), shirei.BorderColor(0, 0, 84, 1)), func() {
		pressed = shirei.PressAction()
		shirei.Label(label, shirei.FontSize(14))
	})
	return pressed
}

// queueAccessoryKey delivers one key (+ optional modifier) on the next frame,
// through the same slot hardware keys use.
func queueAccessoryKey(key shirei.KeyCode, mod shirei.Modifiers) {
	pendingMu.Lock()
	pendingKey = key
	pendingKeyMod = mod
	pendingMu.Unlock()
	shirei.RequestNextFrame()
}

// ---- clipboard (JNI → ClipboardManager) ----

func setClipboard(s string) {
	cs := C.CString(s)
	C.shirei_android_set_clipboard(cs)
	C.free(unsafe.Pointer(cs))
}

func getClipboard() string {
	cs := C.shirei_android_get_clipboard()
	if cs == nil {
		return ""
	}
	s := C.GoString(cs)
	C.free(unsafe.Pointer(cs))
	return s
}

// openURL opens url in the system browser (or the scheme's handler) via
// Intent.ACTION_VIEW on ShireiActivity. Called from the frame loop when
// FrameOutputData.OpenURL is set. Errors ignored.
func openURL(url string) {
	if url == "" {
		return
	}
	cs := C.CString(url)
	C.shirei_android_open_url(cs)
	C.free(unsafe.Pointer(cs))
}

// ---- IME exports (called from jni.c on the UI thread) ----

//export shireiAndroidCommitText
func shireiAndroidCommitText(ctext *C.char) {
	s := C.GoString(ctext)
	if !textInputOK(s) {
		return
	}
	pendingMu.Lock()
	// A commit replaces any in-flight composition (the committed string
	// already contains it).
	pendingComposition = ""
	pendingCompositionSet = true
	pendingText += s
	pendingMu.Unlock()
	shirei.RequestNextFrame()
}

//export shireiAndroidDeleteBackward
func shireiAndroidDeleteBackward(count C.int) {
	if count <= 0 {
		return
	}
	pendingMu.Lock()
	pendingDeleteCount += int(count)
	pendingMu.Unlock()
	shirei.RequestNextFrame()
}

//export shireiAndroidSetComposition
func shireiAndroidSetComposition(ctext *C.char) {
	s := C.GoString(ctext)
	pendingMu.Lock()
	pendingComposition = s
	pendingCompositionSet = true
	pendingMu.Unlock()
	shirei.RequestNextFrame()
}

//export shireiAndroidFinishComposition
func shireiAndroidFinishComposition() {
	pendingMu.Lock()
	if pendingComposition != "" {
		pendingText += pendingComposition
		pendingComposition = ""
	}
	pendingCompositionSet = true
	pendingMu.Unlock()
	shirei.RequestNextFrame()
}

//export shireiAndroidEditorAction
func shireiAndroidEditorAction(action C.int) {
	const imeActionDone = 6 // EditorInfo.IME_ACTION_DONE
	pendingMu.Lock()
	if int(action) == imeActionDone {
		pendingClearFocus = true
	} else {
		// Go / Next / Search / Send all act like Return.
		pendingText += "\n"
	}
	pendingMu.Unlock()
	shirei.RequestNextFrame()
}

// shireiAndroidKeyDown handles native-queue key events (hardware keyboards,
// `adb shell input text/keyevent`). Runs on the app thread. Returns 1 when
// consumed, 0 to give the key back to the system.
//
//export shireiAndroidKeyDown
func shireiAndroidKeyDown(keyCode, unicode C.int) C.int {
	handled := true
	pendingMu.Lock()
	switch int(keyCode) {
	case akeycodeDel:
		pendingDeleteCount++
	case akeycodeEnter, akeycodeNumpadEnter:
		pendingText += "\n"
	case akeycodeDpadLeft:
		pendingKey = shirei.KeyLeft
	case akeycodeDpadRight:
		pendingKey = shirei.KeyRight
	case akeycodeDpadUp:
		pendingKey = shirei.KeyUp
	case akeycodeDpadDown:
		pendingKey = shirei.KeyDown
	default:
		// Dead keys carry a combining-accent flag (negative); skip those.
		if s := string(rune(int32(unicode))); unicode > 0 && textInputOK(s) {
			pendingText += s
		} else {
			handled = false
		}
	}
	pendingMu.Unlock()
	if !handled {
		return 0
	}
	shirei.RequestNextFrame()
	return 1
}

// textInputOK filters committed text. Allows printable runes plus \n/\t;
// drops empty, control chars, and invalid UTF-8 (same as iosbackend).
func textInputOK(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r >= 0xF700 && r <= 0xF8FF {
			return false
		}
		if r < 0x20 && r != '\n' && r != '\t' {
			return false
		}
		if r == 0x7f {
			return false
		}
	}
	return utf8.ValidString(s)
}

// ---------------------------------------------------------------------------
// Everything below is the iosbackend synthesizer verbatim (keep in sync).
// ---------------------------------------------------------------------------

func dist2(a, b shirei.Vec2) float32 {
	dx, dy := a[0]-b[0], a[1]-b[1]
	return dx*dx + dy*dy
}

func flushClickReleaseIfDue() {
	if !clickHeld || clickReleaseAt.IsZero() {
		return
	}
	if time.Now().Before(clickReleaseAt) {
		shirei.RequestNextFrame()
		return
	}
	shirei.GetInputState().MouseButton = 0
	shirei.GetFrameInput().Mouse = shirei.MouseRelease
	clickHeld = false
	clickReleaseAt = time.Time{}
	setMouseFromTouch(false)
	pendingPark = true
}

func flushPendingTouchEdges() {
	n := pendingBeganN
	if n > shirei.MaxTouches {
		n = shirei.MaxTouches
	}
	shirei.GetFrameInput().TouchesBeganCount = n
	copy(shirei.GetFrameInput().TouchesBegan[:n], pendingBegan[:n])
	pendingBeganN = 0

	n = pendingEndedN
	if n > shirei.MaxTouches {
		n = shirei.MaxTouches
	}
	shirei.GetFrameInput().TouchesEndedCount = n
	copy(shirei.GetFrameInput().TouchesEnded[:n], pendingEnded[:n])
	pendingEndedN = 0
}

func noteTouchBegan(id uint32) {
	if pendingBeganN < shirei.MaxTouches {
		pendingBegan[pendingBeganN] = id
		pendingBeganN++
	}
}

func noteTouchEnded(id uint32) {
	if pendingEndedN < shirei.MaxTouches {
		pendingEnded[pendingEndedN] = id
		pendingEndedN++
	}
}

func allocTouchId() uint32 {
	nextTouchId++
	if nextTouchId == 0 {
		nextTouchId = 1
	}
	return nextTouchId
}

func findTouchSlotByOS(osKey uintptr) int {
	for i := 0; i < shirei.MaxTouches; i++ {
		if touchOSKey[i] == osKey {
			return i
		}
	}
	return -1
}

func findFreeTouchSlot() int {
	for i := 0; i < shirei.MaxTouches; i++ {
		if !shirei.GetInputState().Touches[i].Active {
			return i
		}
	}
	return -1
}

func setMouseFromTouch(v bool) {
	shirei.GetInputState().MouseFromTouch = v
}

func vecLen(v shirei.Vec2) float32 {
	return float32(math.Hypot(float64(v[0]), float64(v[1])))
}

func clampVecLen(v shirei.Vec2, maxLen float32) shirei.Vec2 {
	n := vecLen(v)
	if n <= maxLen || n <= 0 {
		return v
	}
	s := maxLen / n
	return shirei.Vec2{v[0] * s, v[1] * s}
}

func stopFling() {
	if touchState == touchFling {
		touchState = touchIdle
	}
	flingVel = shirei.Vec2{}
	flingLastT = time.Time{}
}

func endFling() {
	stopFling()
	synthOSKey = 0
	shirei.GetInputState().MouseButton = 0
	setMouseFromTouch(false)
	pendingPark = true
}

func clearSynth() {
	stopFling()
	touchState = touchIdle
	synthOSKey = 0
	clickHeld = false
	clickReleaseAt = time.Time{}
	touchVel = shirei.Vec2{}
	shirei.GetInputState().MouseButton = 0
	setMouseFromTouch(false)
}

func flushFling() {
	if touchState != touchFling {
		return
	}
	now := time.Now()
	dt := float32(now.Sub(flingLastT).Seconds())
	if dt < 0 {
		dt = 0
	}
	if dt > 1.0/30.0 {
		dt = 1.0 / 30.0
	}
	flingLastT = now

	decay := float32(math.Exp(float64(-FlingFriction * dt)))
	flingVel[0] *= decay
	flingVel[1] *= decay

	shirei.GetFrameInput().Scroll = shirei.Vec2Add(shirei.GetFrameInput().Scroll, shirei.Vec2{
		flingVel[0] * dt,
		flingVel[1] * dt,
	})

	if vecLen(flingVel) < FlingStopSpeed {
		endFling()
		return
	}
	shirei.RequestNextFrame()
}

func sampleFingerVel(pt shirei.Vec2, now time.Time) {
	if touchLastT.IsZero() {
		touchLastT = now
		return
	}
	dt := float32(now.Sub(touchLastT).Seconds())
	if dt <= 0 || dt > 0.1 {
		touchLastT = now
		return
	}
	inst := shirei.Vec2{
		(pt[0] - touchLast[0]) / dt,
		(pt[1] - touchLast[1]) / dt,
	}
	a := FlingVelAlpha
	if a < 0.05 {
		a = 0.05
	}
	if a > 1 {
		a = 1
	}
	touchVel[0] = touchVel[0]*(1-a) + inst[0]*a
	touchVel[1] = touchVel[1]*(1-a) + inst[1]*a
	touchVel = clampVecLen(touchVel, FlingMaxSpeed)
	touchLastT = now
}

func tryStartFling() bool {
	now := time.Now()
	if !touchLastT.IsZero() && now.Sub(touchLastT) > FlingStaleAfter {
		return false
	}
	v := shirei.Vec2{-touchVel[0], -touchVel[1]}
	if FlingBoost > 0 {
		v[0] *= FlingBoost
		v[1] *= FlingBoost
	}
	v = clampVecLen(v, FlingMaxSpeed)
	if vecLen(v) < FlingMinSpeed {
		return false
	}
	flingVel = v
	flingLastT = now
	touchState = touchFling
	setMouseFromTouch(true)
	shirei.GetInputState().MouseButton = 0
	return true
}

func synthFromPrimary(pt shirei.Vec2, phase int) {
	switch phase {
	case touchPhaseBegin:
		stopFling()
		clickHeld = false
		clickReleaseAt = time.Time{}
		pendingPark = false
		touchState = touchPending
		touchOrigin = pt
		touchLast = pt
		touchLastT = time.Now()
		touchVel = shirei.Vec2{}
		shirei.GetInputState().MousePoint = pt
		shirei.GetInputState().MouseButton = 0
		setMouseFromTouch(true)
		shirei.RequestNextFrame()

	case touchPhaseMove:
		if clickHeld {
			return
		}
		if touchState == touchFling {
			return
		}
		if touchState == touchPending {
			shirei.GetInputState().MousePoint = pt
			if dist2(pt, touchOrigin) > TouchSlop*TouchSlop {
				touchState = touchScroll
			} else {
				touchLast = pt
				touchLastT = time.Now()
				shirei.RequestNextFrame()
				return
			}
		}
		if touchState == touchScroll {
			now := time.Now()
			sampleFingerVel(pt, now)
			d := shirei.Vec2Sub(pt, touchLast)
			shirei.GetFrameInput().Scroll = shirei.Vec2Add(shirei.GetFrameInput().Scroll,
				shirei.Vec2{-d[0], -d[1]})
			shirei.GetInputState().MouseButton = 0
			touchLast = pt
			shirei.RequestNextFrame()
		}

	case touchPhaseEnd:
		switch touchState {
		case touchPending:
			shirei.GetInputState().MousePoint = pt
			shirei.GetInputState().MouseButton = shirei.MousePrimary
			shirei.GetFrameInput().Mouse = shirei.MouseClick
			clickHeld = true
			clickReleaseAt = time.Now().Add(ClickHoldDuration)
			touchState = touchIdle
			synthOSKey = 0
			touchVel = shirei.Vec2{}
			setMouseFromTouch(true)
			shirei.RequestNextFrame()
		case touchScroll:
			synthOSKey = 0
			shirei.GetInputState().MouseButton = 0
			if tryStartFling() {
				shirei.RequestNextFrame()
			} else {
				touchState = touchIdle
				touchVel = shirei.Vec2{}
				setMouseFromTouch(false)
				pendingPark = true
				shirei.RequestNextFrame()
			}
		case touchFling:
			endFling()
			shirei.RequestNextFrame()
		default:
			synthOSKey = 0
			setMouseFromTouch(false)
			pendingPark = true
			shirei.RequestNextFrame()
		}

	case touchPhaseCancel:
		clearSynth()
		pendingPark = true
		shirei.RequestNextFrame()
	}
}

// ---------------------------------------------------------------------------
// stdout/stderr → logcat, so Go panics and prints are visible via
// `adb logcat -s shirei`. Runs at package init so even early failures land.
// ---------------------------------------------------------------------------

func init() {
	redirectLogsToLogcat()
}

func redirectLogsToLogcat() {
	r, w, err := os.Pipe()
	if err != nil {
		return
	}
	// Dup3, not Dup2: linux/arm64 has no dup2 syscall.
	syscall.Dup3(int(w.Fd()), 1, 0)
	syscall.Dup3(int(w.Fd()), 2, 0)
	os.Stdout = w
	os.Stderr = w
	go func() {
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			cs := C.CString(sc.Text())
			C.shirei_android_log(cs)
			C.free(unsafe.Pointer(cs))
		}
	}()
}
