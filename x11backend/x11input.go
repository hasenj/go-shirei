//go:build linux || (darwin && x11darwin)

package x11backend

import (
	"fmt"
	"os"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"

	g "go.hasen.dev/generic"
	"go.hasen.dev/shirei"
	"go.hasen.dev/shirei/internal/qwerty"
)

// x11Debug logs raw button/key events when SHIREI_X11_DEBUG=1 — useful for
// backend bringup (e.g. confirming the server delivers wheel buttons 4-7).
var x11Debug = os.Getenv("SHIREI_X11_DEBUG") != ""

// X11 modifier bits (event State field).
const (
	x11Shift = 1 << 0
	x11Ctrl  = 1 << 2
	x11Alt   = 1 << 3 // Mod1
	x11Super = 1 << 6 // Mod4
)

const wheelNotchPts = 30 // points per wheel detent; matches the other backends

// keyboard mapping (keycode -> keysyms), loaded once from the server.
var (
	keysyms     []xproto.Keysym
	perKeycode  int
	minKeycode  xproto.Keycode
	keymapReady bool
)

func loadKeymap() {
	setup := xproto.Setup(X)
	minKeycode = setup.MinKeycode
	count := int(setup.MaxKeycode) - int(setup.MinKeycode) + 1
	r, err := xproto.GetKeyboardMapping(X, minKeycode, byte(count)).Reply()
	if err != nil || r == nil {
		return
	}
	keysyms = r.Keysyms
	perKeycode = int(r.KeysymsPerKeycode)
	keymapReady = perKeycode > 0
}

func keysymAt(kc xproto.Keycode, level int) xproto.Keysym {
	if !keymapReady {
		return 0
	}
	i := (int(kc)-int(minKeycode))*perKeycode + level
	if i < 0 || i >= len(keysyms) {
		return 0
	}
	return keysyms[i]
}

// handleEvent translates one X event into shirei's input globals (sample, not
// queue) or window state, and reports whether a new frame should be produced.
func handleEvent(ev xgb.Event) bool {
	switch e := ev.(type) {
	case xproto.ExposeEvent:
		return true

	case xproto.ConfigureNotifyEvent:
		if int(e.Width) != curW || int(e.Height) != curH {
			curW, curH = int(e.Width), int(e.Height)
			return true
		}
		return false

	case xproto.ClientMessageEvent:
		// WM_DELETE_WINDOW: the close button.
		if e.Type == wmProtocols && e.Format == 32 && xproto.Atom(e.Data.Data32[0]) == wmDelete {
			quit = true
		}
		return false

	case xproto.MotionNotifyEvent:
		lastEventTime = e.Time
		setMouse(float32(e.EventX), float32(e.EventY))
		updateModifiers(e.State)
		return true

	case xproto.ButtonPressEvent:
		lastEventTime = e.Time
		if x11Debug {
			fmt.Fprintf(os.Stderr, "[x11] ButtonPress detail=%d state=%#x\n", e.Detail, e.State)
		}
		updateModifiers(e.State)
		// Click-commit: accept preedit before the click moves focus.
		if e.Detail >= 1 && e.Detail <= 3 {
			commitIMEBeforeClick()
		}
		switch e.Detail {
		case 1:
			mouseButton(shirei.MousePrimary, shirei.MouseClick, e.EventX, e.EventY)
		case 2:
			mouseButton(shirei.MouseTertiary, shirei.MouseClick, e.EventX, e.EventY)
		case 3:
			mouseButton(shirei.MouseSecondary, shirei.MouseClick, e.EventX, e.EventY)
		case 4: // wheel up
			scroll(0, -wheelNotchPts)
		case 5: // wheel down
			scroll(0, wheelNotchPts)
		case 6: // wheel left
			scroll(-wheelNotchPts, 0)
		case 7: // wheel right
			scroll(wheelNotchPts, 0)
		}
		return true

	case xproto.ButtonReleaseEvent:
		lastEventTime = e.Time
		updateModifiers(e.State)
		switch e.Detail {
		case 1:
			mouseButton(shirei.MousePrimary, shirei.MouseRelease, e.EventX, e.EventY)
		case 2:
			mouseButton(shirei.MouseTertiary, shirei.MouseRelease, e.EventX, e.EventY)
		case 3:
			mouseButton(shirei.MouseSecondary, shirei.MouseRelease, e.EventX, e.EventY)
		}
		return true

	case xproto.KeyPressEvent:
		lastEventTime = e.Time
		updateModifiers(e.State)
		onKey(e.Detail, e.State, true)
		return true

	case xproto.KeyReleaseEvent:
		lastEventTime = e.Time
		updateModifiers(e.State)
		onKey(e.Detail, e.State, false)
		return true

	case xproto.FocusInEvent:
		imeFocusIn()
		return false

	case xproto.FocusOutEvent:
		imeFocusOut()
		return true // clear composition underline

	case xproto.SelectionRequestEvent:
		// Another client is pasting text we copied; hand it over.
		lastEventTime = e.Time
		handleSelectionRequest(e)
		return false

	case xproto.SelectionNotifyEvent:
		// A paste we requested came back; read it and produce a frame to inject it.
		lastEventTime = e.Time
		return handleSelectionNotify(e)

	case xproto.SelectionClearEvent:
		// We lost CLIPBOARD ownership (another app copied); drop our stale text.
		lastEventTime = e.Time
		clipboardText = ""
		return false
	}
	return false
}

func setMouse(x, y float32) {
	scale := shirei.WindowScale
	if scale <= 0 {
		scale = 1
	}
	np := shirei.Vec2{x / scale, y / scale}
	prev := shirei.InputState.MousePoint
	shirei.FrameInput.Motion = shirei.Vec2Add(shirei.FrameInput.Motion, shirei.Vec2Sub(np, prev))
	shirei.InputState.MousePoint = np
}

func mouseButton(button shirei.MouseButton, action shirei.MouseAction, x, y int16) {
	setMouse(float32(x), float32(y))
	shirei.InputState.MouseButton = button
	shirei.FrameInput.Mouse = action
}

func scroll(dx, dy float32) {
	shirei.FrameInput.Scroll = shirei.Vec2Add(shirei.FrameInput.Scroll, shirei.Vec2{dx, dy})
}

func updateModifiers(state uint16) {
	var m shirei.Modifiers
	if state&x11Shift != 0 {
		m |= shirei.ModShift
	}
	if state&x11Ctrl != 0 {
		m |= shirei.ModCtrl
	}
	if state&x11Alt != 0 {
		m |= shirei.ModAlt
	}
	if state&x11Super != 0 {
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

func onKey(kc xproto.Keycode, state uint16, down bool) {
	// the writing block resolves by position (X keycode - 8 = evdev): KeyW is
	// the physical key at the US-QWERTY W position no matter the layout; the
	// layout still drives the typed text below. Other keys resolve by keysym.
	code := qwerty.FromScan(uint16(kc) - 8)
	if code == shirei.KeyCodeNone {
		code = mapKeysym(keysymAt(kc, 0))
	}

	// IBus first: when it handles the key, composition/commit arrive via D-Bus
	// signals — do not also insert keysym text or deliver navigation as edits.
	// Still track DownKeys so modifier chords stay coherent.
	handled := imeProcessKey(kc, state, down)

	if code != shirei.KeyCodeNone {
		if down {
			if !handled && !imeComposing() {
				shirei.FrameInput.Key = code
			}
			g.SliceAddUniq(&shirei.InputState.DownKeys, code)
		} else {
			g.SliceRemove(&shirei.InputState.DownKeys, code)
		}
	}
	if !down || handled || imeComposing() {
		return
	}
	// Typed text: pick the shifted level when Shift is held, suppress control/cmd
	// combos and control characters (those arrive as Key). Accumulate so multi-
	// key frames keep every character (assign would drop earlier ones).
	if shirei.InputState.Modifiers&(shirei.ModCtrl|shirei.ModCmd|shirei.ModAlt) != 0 {
		return
	}
	level := 0
	if state&x11Shift != 0 {
		level = 1
	}
	if r := keysymRune(keysymAt(kc, level)); r >= 0x20 && r != 0x7f {
		appendPendingText(string(r))
	}
}

// X11 keysym constants (only the ones mapped).
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

func mapKeysym(ks xproto.Keysym) shirei.KeyCode {
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

// keysymRune maps a keysym to a typed rune (0 if not a printable character).
func keysymRune(ks xproto.Keysym) rune {
	switch {
	case ks >= 0x20 && ks <= 0x7e: // Latin-1 / ASCII printable
		return rune(ks)
	case ks >= 0xa0 && ks <= 0xff:
		return rune(ks)
	case ks&0xff000000 == 0x01000000: // direct Unicode keysym
		return rune(ks & 0x00ffffff)
	}
	return 0
}
