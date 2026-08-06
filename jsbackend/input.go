package jsbackend

import "go.hasen.dev/shirei"

// inputAccum is the browser-independent input state machine for jsbackend.
// DOM (or tests) only feed events; sample() writes Host once per frame.
//
// Pointer capture is a DOM concern: once the shell delivers primaryUp after a
// drag outside the canvas, this accumulator always emits MouseRelease —
// release is not gated on coordinates.
type inputAccum struct {
	appleHost bool

	x, y           float64
	lastX, lastY   float64
	haveLastSample bool

	pendingClick   bool
	pendingRelease bool
	edgeButton     shirei.MouseButton
	held           bool
	heldButton     shirei.MouseButton

	scrollX, scrollY float64

	pendingText string
	pendingKey  shirei.KeyCode
	downKeys    map[shirei.KeyCode]bool
	mods        shirei.Modifiers
	composing   bool
	composition string
}

func newInputAccum(appleHost bool) *inputAccum {
	return &inputAccum{
		appleHost: appleHost,
		downKeys:  make(map[shirei.KeyCode]bool),
	}
}

func (a *inputAccum) setPoint(x, y float64) {
	a.x, a.y = x, y
}

func (a *inputAccum) primaryDown() {
	a.pendingClick = true
	a.held = true
	a.heldButton = shirei.MousePrimary
	a.edgeButton = shirei.MousePrimary
}

func (a *inputAccum) primaryUp() {
	a.pendingRelease = true
	a.held = false
	a.edgeButton = shirei.MousePrimary
}

// wheel accumulates browser wheel deltas. mode is WheelEvent.deltaMode:
// 0 = pixel, 1 = line, 2 = page.
func (a *inputAccum) wheel(dx, dy float64, mode int) {
	switch mode {
	case 1:
		dx *= 16
		dy *= 16
	case 2:
		dx *= 400
		dy *= 400
	}
	a.scrollX += dx
	a.scrollY += dy
}

func (a *inputAccum) setMods(shift, ctrl, alt, meta bool) {
	var m shirei.Modifiers
	if shift {
		m |= shirei.ModShift
	}
	if ctrl {
		m |= shirei.ModCtrl
	}
	if alt {
		m |= shirei.ModAlt
	}
	// metaKey is ⌘ on Apple hosts and the Windows key elsewhere. Match native
	// backends: Command → ModCmd only (not also Super), so exact-combo checks
	// like mods == PrimaryMod() succeed for Cmd+A.
	if meta {
		if a.appleHost {
			m |= shirei.ModCmd
		} else {
			m |= shirei.ModSuper
		}
	}
	a.mods = m
}

// keyDown records a physical key press. Returns whether the browser should
// preventDefault (so Tab/Enter/edit chords and navigation keys do not hit the
// page). While an IME composition is active, editing/navigation keys belong
// to the IME — FrameInput.Key is not set (same gate as Wayland/Cocoa).
func (a *inputAccum) keyDown(code string) (preventDefault bool) {
	k := mapCode(code, a.appleHost)
	if k == 0 {
		return false
	}
	a.downKeys[k] = true
	if a.composing {
		return false
	}
	if !deliversAsFrameKey(k, a.mods) {
		return false
	}
	a.pendingKey = k
	return shouldPreventBrowserDefault(k, a.mods)
}

func (a *inputAccum) keyUp(code string) {
	k := mapCode(code, a.appleHost)
	if k != 0 {
		delete(a.downKeys, k)
	}
}

func (a *inputAccum) compositionStart() {
	a.composing = true
	a.composition = ""
}

func (a *inputAccum) compositionUpdate(s string) {
	a.composition = s
}

func (a *inputAccum) compositionEnd(s string) {
	a.composing = false
	if s != "" {
		a.pendingText += s
	}
	a.composition = ""
}

// textInsert is beforeinput / committed characters outside composition.
func (a *inputAccum) textInsert(s string) {
	if a.composing || s == "" {
		return
	}
	a.pendingText += s
}

func (a *inputAccum) clearKeyboard() {
	for k := range a.downKeys {
		delete(a.downKeys, k)
	}
	a.pendingKey = 0
	a.pendingText = ""
	a.mods = 0
	a.composing = false
	a.composition = ""
}

// sample writes one frame of accumulated input into Host and clears edges.
// Call once per rAF tick before RunFrameFn. FrameInput must already be zero
// (core clears it at end of each pass).
func (a *inputAccum) sample() {
	in := shirei.GetInputState()
	fi := shirei.GetFrameInput()

	np := shirei.Vec2{float32(a.x), float32(a.y)}
	if a.haveLastSample {
		prev := shirei.Vec2{float32(a.lastX), float32(a.lastY)}
		fi.Motion = shirei.Vec2Add(fi.Motion, shirei.Vec2Sub(np, prev))
	}
	a.lastX, a.lastY = a.x, a.y
	a.haveLastSample = true
	in.MousePoint = np

	in.Modifiers = a.mods
	in.DownKeys = in.DownKeys[:0]
	for k := range a.downKeys {
		in.DownKeys = append(in.DownKeys, k)
	}
	if a.composing {
		in.Composition = a.composition
		n := len([]rune(a.composition))
		in.CompositionSel = [2]int{n, n}
	} else {
		in.Composition = ""
		in.CompositionSel = [2]int{}
	}

	// Prefer click over release when both arrive in one tick (quick click);
	// release is delivered on the following sample while held is already false.
	if a.pendingClick {
		fi.Mouse = shirei.MouseClick
		in.MouseButton = a.edgeButton
		a.pendingClick = false
	} else if a.pendingRelease {
		fi.Mouse = shirei.MouseRelease
		in.MouseButton = a.edgeButton
		a.pendingRelease = false
	} else if a.held {
		in.MouseButton = a.heldButton
	}

	if a.scrollX != 0 || a.scrollY != 0 {
		// WheelEvent is already shirei-space: positive deltaY = scroll down
		// (increase ScrollOffset). Do not negate — Cocoa negates AppKit deltas
		// because their sign differs from the web event.
		fi.Scroll = shirei.Vec2{float32(a.scrollX), float32(a.scrollY)}
		a.scrollX, a.scrollY = 0, 0
	}
	if a.pendingKey != 0 {
		fi.Key = a.pendingKey
		a.pendingKey = 0
	}
	if a.pendingText != "" {
		fi.Text = a.pendingText
		a.pendingText = ""
	}
}

// deliversAsFrameKey reports keys that must appear on FrameInput.Key (not only
// as typed text). Includes Space so hotkeys see KeySpace; printable letters
// without modifiers stay on the text path via beforeinput.
func deliversAsFrameKey(k shirei.KeyCode, mods shirei.Modifiers) bool {
	switch k {
	case shirei.KeyLeft, shirei.KeyRight, shirei.KeyUp, shirei.KeyDown,
		shirei.KeyEnter, shirei.KeyEscape, shirei.KeyHome, shirei.KeyEnd,
		shirei.KeyDeleteBackward, shirei.KeyDeleteForward,
		shirei.KeyPageUp, shirei.KeyPageDown, shirei.KeyTab,
		shirei.KeySpace,
		shirei.KeyF1, shirei.KeyF2, shirei.KeyF3, shirei.KeyF4,
		shirei.KeyF5, shirei.KeyF6, shirei.KeyF7, shirei.KeyF8,
		shirei.KeyF9, shirei.KeyF10, shirei.KeyF11, shirei.KeyF12:
		return true
	}
	// Modifier chords (shortcuts) deliver the letter key.
	if mods&(shirei.ModCtrl|shirei.ModCmd|shirei.ModSuper|shirei.ModAlt) != 0 {
		return k != 0
	}
	return false
}

func shouldPreventBrowserDefault(k shirei.KeyCode, mods shirei.Modifiers) bool {
	// Space is intentionally omitted: preventDefault on keydown suppresses
	// beforeinput " " into the hidden field, so typing a space would fail.
	// Page-scroll on Space when no text field is focused is acceptable.
	switch k {
	case shirei.KeyTab, shirei.KeyEnter,
		shirei.KeyLeft, shirei.KeyRight, shirei.KeyUp, shirei.KeyDown,
		shirei.KeyPageUp, shirei.KeyPageDown, shirei.KeyHome, shirei.KeyEnd:
		return true
	}
	if mods&(shirei.ModCtrl|shirei.ModCmd|shirei.ModSuper) != 0 && isEditShortcutKey(k) {
		return true
	}
	return false
}

func isEditShortcutKey(k shirei.KeyCode) bool {
	switch k {
	case shirei.KeyA, shirei.KeyC, shirei.KeyV, shirei.KeyX, shirei.KeyZ, shirei.KeyY:
		return true
	}
	return false
}
