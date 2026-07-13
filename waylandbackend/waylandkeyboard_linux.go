//go:build linux

package waylandbackend

import (
	wos "go.hasen.dev/shirei/internal/wayland/os"
	"go.hasen.dev/shirei/internal/wayland/wl"
	"go.hasen.dev/shirei/internal/wayland/wlclient"
	xkb "go.hasen.dev/shirei/internal/wayland/xkbcommon"

	g "go.hasen.dev/generic"
	"go.hasen.dev/shirei"
	"go.hasen.dev/shirei/internal/qwerty"
)

// Keyboard input. The compositor hands us an xkb keymap over an fd; we compile it
// with libxkbcommon (via neurlang's purego binding — no cgo) and keep an xkb state
// updated from wl_keyboard.modifiers. Per key we get the keysym (an X11 keysym
// value, same as the X11 backend uses) for shortcut mapping, and the UTF-32
// codepoint for typed text. Sampled into the shirei globals (sample, not queue).

var (
	keyboard   *wl.Keyboard
	xkbContext *xkb.Context
	xkbKeymap  *xkb.Keymap
	xkbState   *xkb.State
)

// Bits of the serialized xkb modifier mask (standard keymaps use the X11
// convention, matching x11backend).
const (
	wlModShift = 1 << 0
	wlModCtrl  = 1 << 2
	wlModAlt   = 1 << 3
	wlModSuper = 1 << 6
)

// ensureKeyboard attaches a wl_keyboard listener once the seat advertises one.
func ensureKeyboard() {
	if keyboard != nil {
		return
	}
	if xkbContext == nil {
		xkbContext = xkb.ContextNew(xkb.ContextNoFlags)
	}
	kb, err := seat.GetKeyboard()
	if err != nil {
		return
	}
	keyboard = kb
	wlclient.KeyboardAddListener(keyboard, h)
}

// HandleKeyboardKeymap compiles the keymap the compositor sends over an fd.
func (*handler) HandleKeyboardKeymap(ev wl.KeyboardKeymapEvent) {
	if ev.FdError != nil {
		return
	}
	defer wos.Close(int(ev.Fd))
	if ev.Format != xkb.KeymapFormatTextV1 || xkbContext == nil {
		return
	}
	data, err := wos.Mmap(int(ev.Fd), 0, int(ev.Size), wos.ProtRead, wos.MapPrivate)
	if err != nil {
		return
	}
	defer wos.Munmap(data)

	km := xkbContext.KeymapNewFromString(data, xkb.KeymapFormatTextV1, 0)
	if km == nil {
		perfLog("[wl] keymap compile failed")
		return
	}
	st := km.StateNew()
	if st == nil {
		return
	}
	xkbKeymap, xkbState = km, st
}

// HandleKeyboardModifiers feeds the serialized modifier state into xkb (so text
// reflects shift/caps/layout) and into the shirei modifier flags.
func (*handler) HandleKeyboardModifiers(ev wl.KeyboardModifiersEvent) {
	if xkbState == nil {
		return
	}
	xkbState.UpdateMask(ev.ModsDepressed, ev.ModsLatched, ev.ModsLocked, 0, 0, ev.Group)
	updateModifiers(ev.ModsDepressed | ev.ModsLatched)
	dirty = true
	wlDebug("modifiers: depressed=%#x latched=%#x locked=%#x -> shirei mods=%04b",
		ev.ModsDepressed, ev.ModsLatched, ev.ModsLocked, shirei.InputState.Modifiers)
}

func (*handler) HandleKeyboardKey(ev wl.KeyboardKeyEvent) {
	lastSerial = ev.Serial // for set_selection (copy via Ctrl+C)
	if xkbState == nil {
		return
	}
	code := ev.Key + 8 // evdev keycode -> xkb keycode
	down := ev.State != wl.KeyboardKeyStateReleased
	onKey(code, xkbState.KeyGetOneSym(code), down)
	dirty = true
	wlDebug("key: evdev=%d down=%v (mods now %04b)", ev.Key, down, shirei.InputState.Modifiers)
}

func (*handler) HandleKeyboardEnter(wl.KeyboardEnterEvent) { wlDebug("keyboard enter") }

// HandleKeyboardLeave: focus lost — drop held keys so none stick. Composition
// is also cleared by text-input leave (which tracks the same focus); clear
// here too so a compositor that omits text-input leave cannot leave a stale
// underline.
func (*handler) HandleKeyboardLeave(wl.KeyboardLeaveEvent) {
	shirei.InputState.DownKeys = shirei.InputState.DownKeys[:0]
	shirei.InputState.Modifiers = 0
	clearComposition()
	dirty = true
	wlDebug("keyboard leave")
}

// HandleKeyboardRepeatInfo: client-side key repeat isn't implemented yet.
func (*handler) HandleKeyboardRepeatInfo(wl.KeyboardRepeatInfoEvent) {}

// onKey maps a key event to a shirei key code and, for printable presses, the
// typed text. Mirrors x11backend.onKey. The writing block resolves by evdev
// position — KeyW is the physical key at the US-QWERTY W position no matter
// the layout; the layout still drives the typed text below. Other keys
// resolve by keysym.
//
// While an IME composition is active, editing/navigation keys belong to the
// IME (Cocoa B1 hasMarkedText gate / Win32 VK_PROCESSKEY). text-input-v3 has
// no per-key consumed flag, so we gate on non-empty Composition.
//
// When text-input-v3 is enabled, committed characters arrive via commit_string
// — suppress the xkb→text path to avoid double-insert (the #1 botch on every
// IME backend). Without the protocol (or before enter), fall back to xkb utf32.
func onKey(code, keysym uint32, down bool) {
	kc := qwerty.FromScan(uint16(code - 8)) // xkb keycode -> evdev
	if kc == shirei.KeyCodeNone {
		kc = mapKeysym(keysym)
	}
	composing := textInputConsumesKeys()
	if kc != shirei.KeyCodeNone {
		if down {
			if !composing {
				shirei.FrameInput.Key = kc
			}
			g.SliceAddUniq(&shirei.InputState.DownKeys, kc)
		} else {
			g.SliceRemove(&shirei.InputState.DownKeys, kc)
		}
	}
	if !down || composing {
		return
	}
	// Suppress text for shortcut combos and control characters (delivered as Key).
	if shirei.InputState.Modifiers&(shirei.ModCtrl|shirei.ModCmd|shirei.ModAlt) != 0 {
		return
	}
	// text-input-v3 owns typed text while enabled (commit_string). Without it,
	// xkb utf32 is the only channel — accumulate so multi-key frames keep all
	// characters (assign would drop earlier ones, same class of bug as Win32 W0).
	if textInputEnabled {
		return
	}
	if r := rune(xkbState.KeyGetUtf32(code)); r >= 0x20 && r != 0x7f {
		appendPendingText(string(r))
	}
}

func updateModifiers(mask uint32) {
	var m shirei.Modifiers
	if mask&wlModShift != 0 {
		m |= shirei.ModShift
	}
	if mask&wlModCtrl != 0 {
		m |= shirei.ModCtrl
	}
	if mask&wlModAlt != 0 {
		m |= shirei.ModAlt
	}
	if mask&wlModSuper != 0 {
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

// X11 keysym constants (xkb uses the same values). Mirrors x11backend; a shared
// keysym->KeyCode helper is a candidate cleanup.
const (
	xkBackSpace = 0xff08
	xkTab       = 0xff09
	xkReturn    = 0xff0d
	xkEscape    = 0xff1b
	xkHome      = 0xff50
	xkLeft      = 0xff51
	xkUp        = 0xff52
	xkRight     = 0xff53
	xkDown      = 0xff54
	xkPrior     = 0xff55 // Page Up
	xkNext      = 0xff56 // Page Down
	xkEnd       = 0xff57
	xkDelete    = 0xffff
	xkSpace     = 0x0020
	xkF1        = 0xffbe
	xkF12       = 0xffc9
)

func mapKeysym(ks uint32) shirei.KeyCode {
	switch ks {
	case xkLeft:
		return shirei.KeyLeft
	case xkRight:
		return shirei.KeyRight
	case xkUp:
		return shirei.KeyUp
	case xkDown:
		return shirei.KeyDown
	case xkReturn:
		return shirei.KeyEnter
	case xkEscape:
		return shirei.KeyEscape
	case xkBackSpace:
		return shirei.KeyDeleteBackward
	case xkDelete:
		return shirei.KeyDeleteForward
	case xkHome:
		return shirei.KeyHome
	case xkEnd:
		return shirei.KeyEnd
	case xkPrior:
		return shirei.KeyPageUp
	case xkNext:
		return shirei.KeyPageDown
	case xkTab:
		return shirei.KeyTab
	case xkSpace:
		return shirei.KeySpace
	}
	if ks >= xkF1 && ks <= xkF12 {
		return shirei.KeyF1 + shirei.KeyCode(ks-xkF1)
	}
	if ks >= 'a' && ks <= 'z' {
		return shirei.KeyA + shirei.KeyCode(ks-'a')
	}
	if ks >= 'A' && ks <= 'Z' {
		return shirei.KeyA + shirei.KeyCode(ks-'A')
	}
	if ks >= '0' && ks <= '9' {
		return shirei.Key0 + shirei.KeyCode(ks-'0')
	}
	return shirei.KeyCodeNone
}
