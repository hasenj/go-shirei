//go:build linux

package x11backend

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/godbus/dbus/v5"
	"github.com/jezek/xgb/xproto"

	"go.hasen.dev/shirei"
)

// X11 IME via IBus over D-Bus. GNOME Text Editor (and most GTK apps) talk to
// ibus-daemon this way — not via classic Xlib XIM — so this matches the
// environment the user already verified works. The pure-Go xgb connection
// cannot feed XFilterEvent, so XIM is a poor fit; IBus D-Bus is the right seam.
//
// Core contract matches Wayland/Cocoa/Win32: display-only Composition +
// FrameInput.Text commits through a pending buffer.

const (
	ibusDest     = "org.freedesktop.IBus"
	ibusPath     = "/org/freedesktop/IBus"
	ibusIface    = "org.freedesktop.IBus"
	ibusICIface  = "org.freedesktop.IBus.InputContext"
	ibusService  = "org.freedesktop.IBus"

	// IBus capability bits (ibus/types.h).
	ibusCapPreeditText = 1 << 0
	ibusCapFocus       = 1 << 3

	// IBusModifierType — aligns with X/GDK for the low bits we use.
	ibusReleaseMask = 1 << 30
)

var (
	imeMu       sync.Mutex
	imeConn     *dbus.Conn
	imeCtxPath  dbus.ObjectPath
	imeReady    bool
	imeFocused  bool
	pendingText string

	lastImeCursorX, lastImeCursorY, lastImeCursorW, lastImeCursorH int32
	haveLastImeCursor                                              bool
)

// imeInit connects to ibus-daemon and creates an input context. No-op when
// IBus is unavailable (plain keysym text path keeps working).
func imeInit() {
	// Prefer the dedicated IBus bus (IBUS_ADDRESS / bus file); fall back to the
	// session bus where some desktops also expose org.freedesktop.IBus.
	if tryIBusDial(ibusAddress()) {
		return
	}
	if conn, err := dbus.ConnectSessionBus(); err == nil {
		if tryIBusConn(conn, "session-bus") {
			return
		}
		conn.Close()
	}
	x11IMELog("ibus unavailable; using keysym text only")
}

func tryIBusDial(addr string) bool {
	if addr == "" {
		return false
	}
	conn, err := dbus.Dial(addr)
	if err != nil {
		x11IMELog("dial %s: %v", addr, err)
		return false
	}
	if err := conn.Auth(nil); err != nil {
		conn.Close()
		x11IMELog("auth: %v", err)
		return false
	}
	if err := conn.Hello(); err != nil {
		conn.Close()
		x11IMELog("hello: %v", err)
		return false
	}
	return tryIBusConn(conn, addr)
}

func tryIBusConn(conn *dbus.Conn, via string) bool {
	var path dbus.ObjectPath
	obj := conn.Object(ibusDest, ibusPath)
	if err := obj.Call(ibusIface+".CreateInputContext", 0, "shirei").Store(&path); err != nil {
		x11IMELog("CreateInputContext via %s: %v", via, err)
		return false
	}

	imeMu.Lock()
	imeConn = conn
	imeCtxPath = path
	imeReady = true
	imeMu.Unlock()

	// Capabilities: we render preedit inline and track focus.
	_ = imeCall("SetCapabilities", uint32(ibusCapPreeditText|ibusCapFocus))

	if err := conn.AddMatchSignal(
		dbus.WithMatchObjectPath(path),
		dbus.WithMatchInterface(ibusICIface),
	); err != nil {
		x11IMELog("AddMatchSignal: %v", err)
	}

	go imeSignalLoop(conn)
	x11IMELog("ibus ready via %s path=%s", via, path)
	return true
}

func imeClose() {
	imeMu.Lock()
	conn := imeConn
	imeConn = nil
	imeReady = false
	imeCtxPath = ""
	imeMu.Unlock()
	if conn != nil {
		_ = conn.Close()
	}
}

func ibusAddress() string {
	if a := os.Getenv("IBUS_ADDRESS"); a != "" {
		return a
	}
	// ibus-daemon writes one file under $XDG_CONFIG_HOME/ibus/bus or
	// ~/.config/ibus/bus whose content is the address.
	cfg := os.Getenv("XDG_CONFIG_HOME")
	if cfg == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		cfg = filepath.Join(home, ".config")
	}
	dir := filepath.Join(cfg, "ibus", "bus")
	ents, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		// File format: IBUS_ADDRESS=unix:…\n or just the address line.
		for _, line := range strings.Split(string(b), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			if strings.HasPrefix(line, "IBUS_ADDRESS=") {
				return strings.TrimPrefix(line, "IBUS_ADDRESS=")
			}
			if strings.HasPrefix(line, "unix:") || strings.HasPrefix(line, "tcp:") {
				return line
			}
		}
	}
	return ""
}

func imeCall(method string, args ...interface{}) error {
	imeMu.Lock()
	conn, path, ok := imeConn, imeCtxPath, imeReady
	imeMu.Unlock()
	if !ok || conn == nil {
		return fmt.Errorf("ime not ready")
	}
	return conn.Object(ibusService, path).Call(ibusICIface+"."+method, 0, args...).Err
}

func imeCallStore(method string, args []interface{}, out ...interface{}) error {
	imeMu.Lock()
	conn, path, ok := imeConn, imeCtxPath, imeReady
	imeMu.Unlock()
	if !ok || conn == nil {
		return fmt.Errorf("ime not ready")
	}
	c := conn.Object(ibusService, path).Call(ibusICIface+"."+method, 0, args...)
	if c.Err != nil {
		return c.Err
	}
	return c.Store(out...)
}

func imeFocusIn() {
	if !imeIsReady() {
		return
	}
	if err := imeCall("FocusIn"); err != nil {
		x11IMELog("FocusIn: %v", err)
		return
	}
	imeFocused = true
	x11IMELog("FocusIn")
}

func imeFocusOut() {
	if !imeIsReady() {
		return
	}
	_ = imeCall("FocusOut")
	imeFocused = false
	clearComposition()
	x11IMELog("FocusOut")
}

func imeIsReady() bool {
	imeMu.Lock()
	defer imeMu.Unlock()
	return imeReady
}

// imeProcessKey feeds a key to IBus. Returns true if IBus consumed it (caller
// must not also insert text or deliver navigation as an edit key).
func imeProcessKey(kc xproto.Keycode, state uint16, down bool) bool {
	if !imeIsReady() {
		return false
	}
	if !imeFocused {
		imeFocusIn()
	}
	keyval := uint32(keysymAt(kc, keysymLevel(state)))
	// On Shift, keyval for letters is often still lowercase from level 0; IBus
	// wants the actual keyval. Prefer level matching modifiers.
	if keyval == 0 {
		keyval = uint32(keysymAt(kc, 0))
	}
	st := uint32(state)
	if !down {
		st |= ibusReleaseMask
	}
	var handled bool
	err := imeCallStore("ProcessKeyEvent", []interface{}{keyval, uint32(kc), st}, &handled)
	if err != nil {
		x11IMELog("ProcessKeyEvent: %v", err)
		return false
	}
	if handled {
		x11IMELog("ProcessKeyEvent keyval=%#x kc=%d down=%v handled", keyval, kc, down)
	}
	return handled
}

func keysymLevel(state uint16) int {
	if state&x11Shift != 0 {
		return 1
	}
	return 0
}

func imeSignalLoop(conn *dbus.Conn) {
	ch := make(chan *dbus.Signal, 32)
	conn.Signal(ch)
	for sig := range ch {
		if sig == nil {
			continue
		}
		switch sig.Name {
		case ibusICIface + ".CommitText":
			if len(sig.Body) < 1 {
				continue
			}
			text := extractIBusText(sig.Body[0])
			if text == "" {
				continue
			}
			appendPendingText(text)
			clearComposition()
			// Ask for a frame so pending text flushes.
			noteIMEInput()
			x11IMELog("CommitText %q", truncateIME(text))
		case ibusICIface + ".UpdatePreeditText":
			// (text, cursor_pos, visible)
			if len(sig.Body) < 3 {
				continue
			}
			text := extractIBusText(sig.Body[0])
			cursor, _ := asUint32(sig.Body[1])
			visible, _ := asBool(sig.Body[2])
			if !visible || text == "" {
				clearComposition()
			} else {
				setCompositionRunes(text, int(cursor))
			}
			noteIMEInput()
			x11IMELog("UpdatePreeditText %q cursor=%d vis=%v", truncateIME(text), cursor, visible)
		case ibusICIface + ".UpdatePreeditTextWithMode":
			// (text, cursor_pos, visible, mode) — treat like UpdatePreeditText
			if len(sig.Body) < 3 {
				continue
			}
			text := extractIBusText(sig.Body[0])
			cursor, _ := asUint32(sig.Body[1])
			visible, _ := asBool(sig.Body[2])
			if !visible || text == "" {
				clearComposition()
			} else {
				setCompositionRunes(text, int(cursor))
			}
			noteIMEInput()
			x11IMELog("UpdatePreeditTextWithMode %q cursor=%d vis=%v", truncateIME(text), cursor, visible)
		case ibusICIface + ".ShowPreeditText":
			// no-op; we already show when composition non-empty
		case ibusICIface + ".HidePreeditText":
			clearComposition()
			noteIMEInput()
		case ibusICIface + ".ForwardKeyEvent":
			// IBus wants us to handle this key ourselves (e.g. passthrough).
			if len(sig.Body) < 3 {
				continue
			}
			keyval, _ := asUint32(sig.Body[0])
			// keycode, _ := asUint32(sig.Body[1])
			st, _ := asUint32(sig.Body[2])
			if st&ibusReleaseMask != 0 {
				continue
			}
			if r := keysymRune(xproto.Keysym(keyval)); r >= 0x20 && r != 0x7f {
				if shirei.InputState.Modifiers&(shirei.ModCtrl|shirei.ModCmd|shirei.ModAlt) == 0 {
					appendPendingText(string(r))
					noteIMEInput()
				}
			}
		}
	}
}

// noteIMEInput requests a frame from the event loop. Signal handlers run on a
// background goroutine; the main loop polls FrameRequested / a shared dirty
// flag. We set a package flag the event loop already checks via FrameRequested
// is for apps — use a dedicated atomic-style bool.
var imeDirty bool

func noteIMEInput() {
	imeDirty = true
	shirei.RequestNextFrame()
}

func imeNeedsFrame() bool {
	if !imeDirty {
		return false
	}
	imeDirty = false
	return true
}

// updateIMECursor publishes the candidate-window anchor after a frame.
//
// Known issue: on GNOME Classic / Xorg the IBus suggestions popup is often
// offset from the caret — GNOME Text Editor shows the same misplacement on
// the same session, so we treat it as an IBus/X11 desktop quirk, not a
// shirei layout bug. Inline preedit (Composition) placement is correct.
func updateIMECursor() {
	if !imeIsReady() {
		return
	}
	pos := shirei.CompositionPos
	if shirei.InputState.Composition == "" {
		pos = shirei.CaretPos
	}
	h := shirei.CaretHeight
	if h <= 0 {
		h = 16
	}
	if pos[0] == 0 && pos[1] == 0 && shirei.CaretHeight == 0 {
		return
	}
	scale := shirei.WindowScale
	if scale <= 0 {
		scale = 1
	}
	// Client-window coordinates in device pixels (GTK/IBus convention).
	x := int32(pos[0]*scale + 0.5)
	y := int32((pos[1]-h)*scale + 0.5)
	if y < 0 {
		y = 0
	}
	w := int32(1)
	hh := int32(h*scale + 0.5)
	if haveLastImeCursor && x == lastImeCursorX && y == lastImeCursorY && w == lastImeCursorW && hh == lastImeCursorH {
		return
	}
	if err := imeCall("SetCursorLocation", x, y, w, hh); err != nil {
		return
	}
	lastImeCursorX, lastImeCursorY, lastImeCursorW, lastImeCursorH = x, y, w, hh
	haveLastImeCursor = true
}

func clearComposition() {
	shirei.InputState.Composition = ""
	shirei.InputState.CompositionSel = [2]int{}
}

// setCompositionRunes publishes preedit. cursor is a rune offset into text
// (IBus cursor_pos is in Unicode characters ≈ runes for BMP/JP).
func setCompositionRunes(text string, cursor int) {
	n := utf8.RuneCountInString(text)
	if cursor < 0 {
		cursor = 0
	}
	if cursor > n {
		cursor = n
	}
	shirei.InputState.Composition = text
	shirei.InputState.CompositionSel = [2]int{cursor, cursor}
}

func appendPendingText(s string) {
	if s == "" {
		return
	}
	pendingText += s
}

func flushPendingText() {
	if pendingText == "" {
		return
	}
	shirei.FrameInput.Text += pendingText
	pendingText = ""
}

func imeComposing() bool {
	return shirei.InputState.Composition != ""
}

// commitIMEBeforeClick accepts preedit into the document (click-commit policy
// matching Cocoa/Win32/Wayland). IBus Reset drops composition without commit,
// so we promote the shadow preedit ourselves then Reset.
func commitIMEBeforeClick() {
	if shirei.InputState.Composition == "" {
		return
	}
	appendPendingText(shirei.InputState.Composition)
	clearComposition()
	_ = imeCall("Reset")
	haveLastImeCursor = false
	if pendingText != "" {
		// Flush on next frame(); mark dirty.
		noteIMEInput()
	}
}

// --- IBus.Text extraction ---------------------------------------------------

// extractIBusText pulls the UTF-8 string out of an IBus.Text dbus value. The
// wire form is a nested structure; we walk for the payload string rather than
// depending on one exact layout across ibus versions.
func extractIBusText(v interface{}) string {
	switch t := v.(type) {
	case string:
		return t
	case dbus.Variant:
		return extractIBusText(t.Value())
	case []interface{}:
		// Common: ["IBusText", [attachments, text, attrs]] or deeper nesting.
		// Prefer a string that is not the type name.
		var found string
		var walk func(interface{})
		walk = func(x interface{}) {
			switch n := x.(type) {
			case string:
				if n == "" || n == "IBusText" || n == "IBusAttribute" || n == "IBusAttrList" {
					return
				}
				// Keep the longest non-meta string (preedit can be multi-char).
				if len(n) >= len(found) {
					found = n
				}
			case dbus.Variant:
				walk(n.Value())
			case []interface{}:
				for _, e := range n {
					walk(e)
				}
			case map[string]dbus.Variant:
				for _, e := range n {
					walk(e.Value())
				}
			case map[string]interface{}:
				for _, e := range n {
					walk(e)
				}
			}
		}
		walk(t)
		return found
	default:
		return ""
	}
}

func asUint32(v interface{}) (uint32, bool) {
	switch t := v.(type) {
	case uint32:
		return t, true
	case int32:
		return uint32(t), true
	case uint64:
		return uint32(t), true
	case int64:
		return uint32(t), true
	case dbus.Variant:
		return asUint32(t.Value())
	default:
		return 0, false
	}
}

func asBool(v interface{}) (bool, bool) {
	switch t := v.(type) {
	case bool:
		return t, true
	case dbus.Variant:
		return asBool(t.Value())
	default:
		return false, false
	}
}

func truncateIME(s string) string {
	const max = 32
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

func x11IMELog(format string, args ...interface{}) {
	if !x11Debug && os.Getenv("SHIREI_X11_IME_LOG") == "" {
		return
	}
	fmt.Fprintf(os.Stderr, "[x11-ime] "+format+"\n", args...)
}
