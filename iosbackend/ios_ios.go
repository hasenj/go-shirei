//go:build ios

// Package iosbackend is a direct-iOS (UIKit) backend for shirei. UIKit provides
// the window, run loop, and touch input; all rasterization is done by shirei's
// core software renderer, which rasterizes each frame into a BGRA buffer that is
// presented via CALayer.contents.
//
// Input: multi-touch fills InputState.Touches (+ FrameInput began/ended).
// Independently, the primary finger is synthesized into mouse + wheel
// (browser-like scroll-vs-tap) with optional fling (decaying Scroll after lift).
// Soft keyboard uses UITextInput; inputAccessoryView synthesizes edit keys.
// WindowSize tracks safe area minus keyboard; the content view stays full
// safe-area so present never stretches.
//
// Inversion of control: UIApplicationMain owns the process. Run attaches a
// full-screen view and returns; CADisplayLink drives frames afterward.
package iosbackend

/*
#cgo CFLAGS: -fobjc-arc
#cgo LDFLAGS: -framework UIKit -framework Foundation -framework QuartzCore -framework CoreGraphics -framework GameController
#include <stdlib.h>
#include "ios.h"
*/
import "C"

import (
	"math"
	"runtime"
	"time"
	"unicode/utf16"
	"unicode/utf8"
	"unsafe"

	"go.hasen.dev/shirei"
)

// glyphCacheBudget caps total cached glyph-bitmap bytes (enables the shared core
// glyph cache). Matches cocoabackend.
const glyphCacheBudget = 16 << 20

// Touch phases from ios.m (keep in sync).
const (
	touchPhaseBegin  = 0
	touchPhaseMove   = 1
	touchPhaseEnd    = 2
	touchPhaseCancel = 3
)

// TouchSlop is the movement threshold in logical points past which a
// single-finger gesture becomes scrolling instead of a tap. The name matches
// Android ViewConfiguration touch slop / common browser practice (~8–10pt).
var TouchSlop float32 = 10

// ClickHoldDuration is how long a synthesized tap holds MousePrimary between
// MouseClick and MouseRelease so PressAction / button press chrome can paint.
// Default 100ms — long enough to see the press, short enough to feel instant.
var ClickHoldDuration = 100 * time.Millisecond

// Fling tunables (logical points / second). Tuned for device; Simulator timing
// is noisier — EMA + stale-sample discard matter more than exact constants.
//
// Feel knobs (start here when tuning):
//   - FlingFriction — how fast speed decays (1/s). Lower = longer, freer coast.
//   - FlingBoost    — multiplies launch velocity after lift (1 = match finger).
//   - FlingMinSpeed / FlingStopSpeed — when coast starts / ends.
var (
	// FlingMinSpeed: finger speed at lift below this → no coast.
	FlingMinSpeed float32 = 70
	// FlingStopSpeed: stop emitting when |vel| decays below this.
	FlingStopSpeed float32 = 18
	// FlingMaxSpeed: clamp tracked / launch velocity (pt/s).
	FlingMaxSpeed float32 = 6000
	// FlingFriction: exponential decay rate (1/s); higher = shorter coast.
	// ~2 feels closer to UIScrollView than the previous 3.5 (too sticky).
	FlingFriction float32 = 2.0
	// FlingBoost: scale launch velocity after measuring the finger (1 = 1:1).
	// Slight boost compensates for EMA lag under-reading peak speed.
	FlingBoost float32 = 1.25
	// FlingVelAlpha: EMA weight for instantaneous samples (0..1).
	FlingVelAlpha float32 = 0.45
	// FlingStaleAfter: if no move sample within this of lift, treat as stopped.
	FlingStaleAfter = 80 * time.Millisecond
)

// Single-finger synthesizer states (browser-like scroll-vs-tap + fling).
const (
	touchIdle    = iota
	touchPending // down; hover only — not yet scroll or click
	touchScroll  // past TouchSlop; emit FrameInput.Scroll, never click
	touchFling   // post-lift momentum: decaying FrameInput.Scroll
)

// offscreen parks the pointer after lift so hover does not stick on the last
// touched control. Prefer off-window over (0,0), which can still hit top-left UI.
var offscreen = shirei.Vec2{-1000, -1000}

var (
	frameFn shirei.FrameFn

	softRenderer shirei.SoftRenderer

	// present skip: hash of last presented surfaces
	lastPresentedHash  uint64
	havePresented      bool
	presentW, presentH int

	// pendingPark: after a gesture ends, move MousePoint offscreen.
	pendingPark  bool
	pendingPaste string // clipboard text to inject as FrameInput.Text next frame

	// Soft keyboard / IME → FrameInput (flushed at the start of produce).
	pendingText        string
	pendingDeleteCount int // each unit → one KeyDeleteBackward frame

	// inputAccessoryView toolbar → one key+mod per frame (FrameInput.Key is a slot).
	pendingAccessoryKey shirei.KeyCode
	pendingAccessoryMod shirei.Modifiers
	// clearAccessoryMods: drop InputState.Modifiers after the frame (iOS has no
	// physical modifier keys; we only set them for Select All / Copy / Paste).
	clearAccessoryMods bool

	// ---- raw touch table (UITouch* → slot) ----
	touchOSKey    [shirei.MaxTouches]uintptr // 0 = free
	nextTouchId   uint32                     // never 0
	pendingBegan  [shirei.MaxTouches]uint32
	pendingBeganN int
	pendingEnded  [shirei.MaxTouches]uint32
	pendingEndedN int

	// ---- single-finger mouse synthesizer (alongside raw Touches) ----
	touchState  int
	touchOrigin shirei.Vec2
	touchLast   shirei.Vec2
	touchLastT  time.Time // last move sample (velocity / stale check)
	// touchVel: smoothed finger velocity (pt/s) while scrolling.
	touchVel shirei.Vec2
	// flingVel: scroll-space velocity (pt/s) during touchFling — same sign as
	// FrameInput.Scroll (already inverted from finger motion).
	flingVel   shirei.Vec2
	flingLastT time.Time
	synthOSKey uintptr // primary finger; 0 = synthesizer idle
	// clickHeld: tap delivered MouseClick; release fires at clickReleaseAt.
	clickHeld      bool
	clickReleaseAt time.Time
)

// Accessory actions from ios.m (keep in sync).
const (
	accessoryLeft = iota
	accessoryRight
	accessorySelectAll
	accessoryCopy
	accessoryPaste
	accessoryDone
)

// SetupWindow records the window title and preferred size. On iOS the view is
// always full-screen (safe-area); width/height are ignored for layout but kept
// for API parity with desktop backends.
func SetupWindow(title string, width, height int) {
	_ = title
	_ = width
	_ = height
}

// CenterWindow is a no-op on iOS (full-screen). Kept for API parity.
func CenterWindow() {}

// PositionWindow is a no-op on iOS (full-screen). Kept for API parity.
func PositionWindow(x, y int) { _, _ = x, y }

// Run installs the UIKit content view and display link, then returns. The host
// process must already be inside UIApplicationMain (see ioshost). Call from the
// main goroutine after SetupWindow.
// mobileComfortScale fattens default control chrome for finger targets
// without crowding dense toolbars (e.g. HN feed segments). 1.5 made
// five min-width segments too wide for a phone title row.
const mobileComfortScale float32 = 1.25

func Run(fn shirei.FrameFn) {
	runtime.LockOSThread()
	frameFn = fn
	shirei.GetHost().GlyphCacheBudgetBytes = glyphCacheBudget
	shirei.GetHost().EscapeHatchBackendContext = Context{}
	shirei.GetHost().WindowFocused = true
	shirei.GetHost().ComfortScale = mobileComfortScale
	// Preference before attach so the root VC's supported orientations and
	// first geometry request are landscape/portrait from the start (avoids a
	// one-frame flash of the device's natural orientation).
	C.ios_syncOrientation(C.int(shirei.GetHost().PreferredOrientation))
	C.ios_attach()
}

//export shireiFrameRequested
func shireiFrameRequested() C.int {
	if shirei.FrameRequested() || pendingPark || pendingPaste != "" ||
		pendingText != "" || pendingDeleteCount > 0 || pendingAccessoryKey != 0 ||
		clickHeld || touchState != touchIdle {
		// touchFling is non-idle — keeps CADisplayLink running for coast.
		return 1
	}
	return 0
}

// accessoryPrimaryMod must match widgets' editPrimaryMod: Cmd on macOS/darwin,
// Ctrl elsewhere. GOOS=ios is not "darwin", so shortcuts use ModCtrl.
func accessoryPrimaryMod() shirei.Modifiers {
	if runtime.GOOS == "darwin" {
		return shirei.ModCmd
	}
	return shirei.ModCtrl
}

//export shireiAccessoryAction
func shireiAccessoryAction(action C.int) {
	switch int(action) {
	case accessoryLeft:
		pendingAccessoryKey = shirei.KeyLeft
		pendingAccessoryMod = 0
	case accessoryRight:
		pendingAccessoryKey = shirei.KeyRight
		pendingAccessoryMod = 0
	case accessorySelectAll:
		pendingAccessoryKey = shirei.KeyA
		pendingAccessoryMod = accessoryPrimaryMod()
	case accessoryCopy:
		pendingAccessoryKey = shirei.KeyC
		pendingAccessoryMod = accessoryPrimaryMod()
	case accessoryPaste:
		pendingAccessoryKey = shirei.KeyV
		pendingAccessoryMod = accessoryPrimaryMod()
	case accessoryDone:
		// Drop text focus so WantsKeyboard clears and ios_syncKeyboard resigns.
		shirei.ClearFocus()
		shirei.RequestNextFrame()
		return
	default:
		return
	}
	shirei.RequestNextFrame()
}

// dist2 returns squared distance between a and b.
func dist2(a, b shirei.Vec2) float32 {
	dx, dy := a[0]-b[0], a[1]-b[1]
	return dx*dx + dy*dy
}

// flushClickReleaseIfDue delivers the MouseRelease half of a synthesized tap
// once ClickHoldDuration has elapsed (button press chrome needs a few frames
// of "down" before up).
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

// endFling finishes momentum: idle + park pointer (hover must not stick).
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

// flushFling integrates decaying fling velocity into FrameInput.Scroll.
// Must run before RunFrameFn. Critical: MousePoint stays at the frozen scroll
// capture point so ScrollOnInput / VirtualList (which require IsHovered) still
// apply — parking on lift was why the earlier fling prototype felt broken.
func flushFling() {
	if touchState != touchFling {
		return
	}
	now := time.Now()
	dt := float32(now.Sub(flingLastT).Seconds())
	if dt < 0 {
		dt = 0
	}
	// Cap dt so a hitch does not dump a huge jump.
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
	// Keep the display link ticking for the next coast sample.
	shirei.RequestNextFrame()
}

// sampleFingerVel updates the EMA of finger velocity (pt/s) from a move.
func sampleFingerVel(pt shirei.Vec2, now time.Time) {
	if touchLastT.IsZero() {
		touchLastT = now
		return
	}
	dt := float32(now.Sub(touchLastT).Seconds())
	if dt <= 0 || dt > 0.1 {
		// Gap too large (stutter / first sample after pause): reset sample clock
		// but keep prior EMA so a brief hitch does not zero launch speed.
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

// tryStartFling after a scroll lift. Leaves MousePoint frozen for hover capture.
func tryStartFling() bool {
	now := time.Now()
	if !touchLastT.IsZero() && now.Sub(touchLastT) > FlingStaleAfter {
		// Finger was still at the end — no coast.
		return false
	}
	// Scroll = -fingerDelta, so scroll velocity = -finger velocity.
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
	// Keep MouseFromTouch set so apps do not treat frozen hover as a mouse hold;
	// keep MousePoint where scroll captured so IsHovered stays true on the list.
	setMouseFromTouch(true)
	shirei.GetInputState().MouseButton = 0
	return true
}

// synthFromPrimary runs browser-like mouse synthesis for the primary finger
// only (in parallel with the raw Touches table). MouseFromTouch stays set for
// the whole gesture, including ClickHoldDuration after a tap and during fling.
func synthFromPrimary(pt shirei.Vec2, phase int) {
	switch phase {
	case touchPhaseBegin:
		// New contact always kills an in-flight fling (browser/UIScrollView).
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
			// Should not happen (fling has no synthOSKey finger); ignore.
			return
		}
		if touchState == touchPending {
			shirei.GetInputState().MousePoint = pt
			if dist2(pt, touchOrigin) > TouchSlop*TouchSlop {
				touchState = touchScroll
				// Fall through: sample velocity + emit scroll for this move
				// (includes slop-crossing delta so the list does not jump).
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
			// Content follows finger: finger down → negative Scroll.y.
			// MousePoint stays frozen (scroll capture / hover for ScrollOnInput).
			shirei.GetFrameInput().Scroll = shirei.Vec2Add(shirei.GetFrameInput().Scroll,
				shirei.Vec2{-d[0], -d[1]})
			shirei.GetInputState().MouseButton = 0
			touchLast = pt
			shirei.RequestNextFrame()
		}

	case touchPhaseEnd:
		switch touchState {
		case touchPending:
			// Tap: synthetic click+hold for buttons. Keep MouseFromTouch set
			// until flushClickReleaseIfDue so apps using IsTouched do not
			// fall through to IsActive during the hold window.
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
				// Coasting: do NOT park — ScrollOnInput needs IsHovered.
				shirei.RequestNextFrame()
			} else {
				touchState = touchIdle
				touchVel = shirei.Vec2{}
				setMouseFromTouch(false)
				pendingPark = true
				shirei.RequestNextFrame()
			}
		case touchFling:
			// Lift while flinging should not occur without a finger; end cleanly.
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

//export shireiTouch
func shireiTouch(x, y C.double, phase C.int, osKey C.uintptr_t) {
	// Always maintain InputState.Touches. Independently, the first finger that
	// begins while the synthesizer is idle also drives mouse/scroll.
	pt := shirei.Vec2{float32(x), float32(y)}
	key := uintptr(osKey)
	if key == 0 {
		return
	}
	ph := int(phase)

	switch ph {
	case touchPhaseBegin:
		slot := findFreeTouchSlot()
		if slot < 0 {
			// Drop extra contacts beyond MaxTouches.
			return
		}
		id := allocTouchId()
		touchOSKey[slot] = key
		shirei.GetInputState().Touches[slot] = shirei.TouchInfo{
			Active: true,
			Id:     id,
			Pos:    pt,
		}
		noteTouchBegan(id)

		// Fling is non-idle but has no finger: a new contact cancels coast and
		// becomes the primary synthesizer (UIScrollView-style interrupt).
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

//export shireiInsertText
func shireiInsertText(ctext *C.char) {
	if ctext == nil {
		return
	}
	s := C.GoString(ctext)
	if !textInputOK(s) {
		return
	}
	pendingText += s
	shirei.RequestNextFrame()
}

//export shireiDeleteBackward
func shireiDeleteBackward() {
	pendingDeleteCount++
	shirei.RequestNextFrame()
}

// UTF-16 unit offsets (UIKit / NSString), same contract as cocoabackend.
//
//export shireiSetComposition
func shireiSetComposition(cchars *C.char, startUTF16 C.int, endUTF16 C.int) {
	text := ""
	if cchars != nil {
		text = C.GoString(cchars)
	}
	setCompositionFromUTF16Offsets(text, int(startUTF16), int(endUTF16))
}

//export shireiClearComposition
func shireiClearComposition() {
	if shirei.GetInputState().Composition == "" && shirei.GetInputState().CompositionSel == [2]int{} {
		return
	}
	shirei.GetInputState().Composition = ""
	shirei.GetInputState().CompositionSel = [2]int{}
	shirei.RequestNextFrame()
}

//export shireiCaretX
func shireiCaretX() C.double {
	return C.double(shirei.GetHost().CaretPos[0])
}

//export shireiCaretY
func shireiCaretY() C.double {
	return C.double(shirei.GetHost().CaretPos[1])
}

//export shireiCaretHeight
func shireiCaretHeight() C.double {
	return C.double(shirei.GetHost().CaretHeight)
}

func setCompositionFromUTF16Offsets(text string, startUTF16 int, endUTF16 int) {
	start := utf16OffsetToRuneOffset(text, startUTF16)
	end := utf16OffsetToRuneOffset(text, endUTF16)
	if start > end {
		start, end = end, start
	}
	shirei.GetInputState().Composition = text
	shirei.GetInputState().CompositionSel = [2]int{start, end}
	shirei.RequestNextFrame()
}

func utf16OffsetToRuneOffset(s string, units int) int {
	if units <= 0 {
		return 0
	}
	u16 := utf16.Encode([]rune(s))
	if units > len(u16) {
		units = len(u16)
	}
	return len(utf16.Decode(u16[:units]))
}

// textInputOK filters soft-keyboard insertText payloads. Allows printable
// runes plus \n/\t (Return / tab); drops empty and private-use function-key
// sentinels (same idea as cocoabackend isPrintable, but keeps newlines).
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
	if !utf8.ValidString(s) {
		return false
	}
	return true
}

//export shireiProduceAndPresent
func shireiProduceAndPresent(w, h C.double) {
	// w,h from the tick are layout size (WindowSize). Drawable may be taller
	// (full safe area) while the keyboard is up — present into that so the
	// layer never scales a short buffer into a tall view or vice versa.
	scale := float32(C.ios_screenScale())
	if scale <= 0 {
		scale = 1
	}

	var drawW, drawH C.double
	C.ios_drawableSize(&drawW, &drawH)
	if drawW <= 0 || drawH <= 0 {
		drawW, drawH = w, h
	}

	layoutW, layoutH := float32(w), float32(h)
	if layoutW <= 0 {
		layoutW = float32(drawW)
	}
	if layoutH <= 0 {
		layoutH = float32(drawH)
	}

	// Physical keyboard presence (GCKeyboard); soft IME does not count.
	shirei.GetHost().HardwareKeyboard = C.ios_hardwareKeyboard() != 0
	// Layout size must not exceed the drawable (defensive).
	if layoutW > float32(drawW) {
		layoutW = float32(drawW)
	}
	if layoutH > float32(drawH) {
		layoutH = float32(drawH)
	}

	shirei.GetHost().WindowSize = shirei.Vec2{layoutW, layoutH}
	shirei.GetHost().WindowScale = scale

	// Synthetic tap release (ClickHoldDuration after MouseClick).
	flushClickReleaseIfDue()
	// Touch began/ended edges for this frame (InputState.Touches already live).
	flushPendingTouchEdges()
	// Momentum: add decaying Scroll before the app frame (needs hover still
	// under the frozen MousePoint — see flushFling).
	flushFling()

	if pendingPaste != "" {
		shirei.GetFrameInput().Text += pendingPaste
		pendingPaste = ""
	}
	if pendingText != "" {
		shirei.GetFrameInput().Text += pendingText
		pendingText = ""
	}
	// Accessory toolbar and soft-keyboard deletes share FrameInput.Key (one slot).
	// Prefer explicit accessory keys over queued deletes.
	if pendingAccessoryKey != 0 {
		shirei.GetFrameInput().Key = pendingAccessoryKey
		shirei.GetInputState().Modifiers = pendingAccessoryMod
		pendingAccessoryKey = 0
		pendingAccessoryMod = 0
		clearAccessoryMods = true
	} else if pendingDeleteCount > 0 && shirei.GetFrameInput().Key == shirei.KeyCodeNone {
		// One delete per frame; held backspace requeues via pendingDeleteCount.
		// While Composition is set, UITextInput handles deletes in ObjC.
		shirei.GetFrameInput().Key = shirei.KeyDeleteBackward
		pendingDeleteCount--
	}

	out := shirei.RunFrameFn(frameFn)

	if clearAccessoryMods {
		shirei.GetInputState().Modifiers = 0
		clearAccessoryMods = false
	}

	// Soft keyboard visibility tracks the frame's WantsKeyboard (TextInput /
	// app WantKeyboard). Independent of caret geometry.
	if shirei.GetHost().WantsKeyboard {
		C.ios_syncKeyboard(1)
	} else {
		C.ios_syncKeyboard(0)
	}

	// Preferred orientation: sticky Host field → OS interface-orientation lock.
	C.ios_syncOrientation(C.int(shirei.GetHost().PreferredOrientation))

	// After a gesture ends (scroll lift, tap release, cancel), park so hover
	// does not stick. Do not park while a synthetic click is still held.
	if pendingPark && !clickHeld && shirei.GetFrameInput().Mouse != shirei.MouseClick {
		shirei.GetInputState().MousePoint = offscreen
		shirei.GetInputState().MouseButton = 0
		pendingPark = false
	}

	if out.Copy != "" {
		setClipboard(out.Copy)
	}
	if out.Paste {
		if s := getClipboard(); s != "" {
			pendingPaste = s
		}
	}
	if out.OpenURL != "" {
		openURL(out.OpenURL)
	}

	var wf C.int
	if out.NextFrameRequested || pendingPark || pendingPaste != "" ||
		pendingText != "" || pendingDeleteCount > 0 || pendingAccessoryKey != 0 ||
		clickHeld || touchState != touchIdle {
		// touchFling keeps the link alive until coast stops.
		wf = 1
	}
	C.ios_setWantsFrame(wf)

	dw := int(float32(drawW)*scale + 0.5)
	dh := int(float32(drawH)*scale + 0.5)
	if dw <= 0 || dh <= 0 {
		return
	}

	// Skip only when content and pixel size both match. A size change must
	// always re-upload: CALayer keeps the previous CGImage until replaced, and
	// a bounds change without a new buffer is the one-frame "shrink" glitch.
	sameHash := havePresented && out.SurfacesHash == lastPresentedHash
	samePx := havePresented && dw == presentW && dh == presentH
	if sameHash && samePx {
		return
	}

	stride := dw * 4
	bufBytes := stride * dh
	// Soft renderer uses WindowSize for the scene; surfaces sit in the top
	// layoutH band of this full-safe-area buffer. If layoutH < drawH there is
	// a black band (keyboard path); if they match, the buffer is fully used.
	buf := make([]byte, bufBytes) // zeroed: black below the layout band
	softRenderer.RenderInto(buf, stride, dw, dh, scale, out.Surfaces)
	C.ios_present_bgra(unsafe.Pointer(&buf[0]), C.int(stride), C.int(dw), C.int(dh))

	lastPresentedHash = out.SurfacesHash
	presentW, presentH, havePresented = dw, dh, true
}

func setClipboard(s string) {
	cs := C.CString(s)
	C.ios_setClipboard(cs)
	C.free(unsafe.Pointer(cs))
}

func getClipboard() string {
	cs := C.ios_getClipboard()
	if cs == nil {
		return ""
	}
	s := C.GoString(cs)
	C.free(unsafe.Pointer(cs))
	return s
}

// openURL opens url in Safari (or whatever handles the scheme) via UIApplication.
// Called from the frame loop when FrameOutputData.OpenURL is set. UIKit work is
// dispatched to main; errors ignored.
func openURL(url string) {
	if url == "" {
		return
	}
	cs := C.CString(url)
	C.ios_openURL(cs)
	C.free(unsafe.Pointer(cs))
}
