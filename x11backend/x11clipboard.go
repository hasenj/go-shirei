//go:build linux || (darwin && x11darwin)

package x11backend

import (
	"fmt"
	"os"

	"github.com/jezek/xgb/xproto"
)

// clipDebugf logs clipboard/selection activity when SHIREI_X11_DEBUG=1.
func clipDebugf(format string, args ...any) {
	if x11Debug {
		fmt.Fprintf(os.Stderr, "[x11/clip] "+format+"\n", args...)
	}
}

// atomLabel gives a short readable name for the selection atoms we care about.
func atomLabel(a xproto.Atom) string {
	switch a {
	case atomTargets:
		return "TARGETS"
	case atomUTF8:
		return "UTF8_STRING"
	case xproto.AtomString:
		return "STRING"
	case atomIncr:
		return "INCR"
	case 0:
		return "None"
	}
	return fmt.Sprintf("atom#%d", a)
}

// X11 clipboard via selections (ICCCM). X has no clipboard "store": the app that
// copied keeps owning the CLIPBOARD selection and hands the text to whoever asks.
//
//   - Copy: become owner of CLIPBOARD and answer the SelectionRequest events that
//     other clients send when they paste (handleSelectionRequest).
//   - Paste: ConvertSelection asks the current owner to write the text onto a
//     property of our window; the owner replies asynchronously with a
//     SelectionNotify, at which point we read the property. Because that round
//     trip can't complete within the requesting frame, the result is stashed and
//     injected at the start of the next frame — exactly like cocoabackend's
//     pendingPaste.
//
// INCR (chunked transfer of very large selections) is not handled; ordinary text
// pastes fit in a single property.

var (
	atomClipboard xproto.Atom // CLIPBOARD selection
	atomUTF8      xproto.Atom // UTF8_STRING target
	atomTargets   xproto.Atom // TARGETS meta-target
	atomIncr      xproto.Atom // INCR (chunked transfer; we only detect it)
	atomPasteProp xproto.Atom // property on our window the selection lands on

	clipboardText string // text we currently offer as CLIPBOARD owner

	pendingPaste    string // selection text read back, awaiting injection
	hasPendingPaste bool

	lastEventTime xproto.Timestamp // most recent server timestamp, for selection ops
)

// initClipboard interns the atoms the selection protocol needs. Called from Run
// after the window exists.
func initClipboard() {
	atomClipboard = internAtom("CLIPBOARD")
	atomUTF8 = internAtom("UTF8_STRING")
	atomTargets = internAtom("TARGETS")
	atomIncr = internAtom("INCR")
	atomPasteProp = internAtom("SHIREI_SELECTION")
}

// setClipboard records the text and takes ownership of CLIPBOARD so other clients
// fetch it from us on demand. Mirrors the explicit-copy semantics of the Windows
// and macOS backends (the system "clipboard", not the X PRIMARY selection).
func setClipboard(s string) {
	clipboardText = s
	xproto.SetSelectionOwner(X, win, atomClipboard, lastEventTime)
	if x11Debug {
		X.Sync() // make the request take effect before we query ownership
		owner, _ := xproto.GetSelectionOwner(X, atomClipboard).Reply()
		got := xproto.Window(0)
		if owner != nil {
			got = owner.Owner
		}
		clipDebugf("copy: %d bytes, SetSelectionOwner(win=%#x, time=%d); now owner=%#x",
			len(s), win, lastEventTime, got)
	}
}

// requestPaste asks the current CLIPBOARD owner to convert its selection to UTF-8
// onto our paste property. The reply arrives later as a SelectionNotify.
func requestPaste() {
	clipDebugf("paste: ConvertSelection(CLIPBOARD -> UTF8_STRING, time=%d)", lastEventTime)
	xproto.ConvertSelection(X, win, atomClipboard, atomUTF8, atomPasteProp, lastEventTime)
}

// handleSelectionRequest answers another client that is pasting our CLIPBOARD: we
// write the requested representation onto its window and confirm with a
// SelectionNotify (or refuse with property None for targets we don't support).
func handleSelectionRequest(e xproto.SelectionRequestEvent) {
	reply := xproto.SelectionNotifyEvent{
		Time:      e.Time,
		Requestor: e.Requestor,
		Selection: e.Selection,
		Target:    e.Target,
		Property:  0, // None = refused, unless we fill it in below
	}

	prop := e.Property
	if prop == 0 {
		prop = e.Target // obsolete clients send Property=None; ICCCM says use Target
	}

	clipDebugf("SelectionRequest from=%#x target=%s prop=%d (have %d bytes)",
		e.Requestor, atomLabel(e.Target), e.Property, len(clipboardText))

	switch e.Target {
	case atomTargets:
		// Advertise the representations we can provide.
		offered := []xproto.Atom{atomTargets, atomUTF8, xproto.AtomString}
		buf := make([]byte, 0, len(offered)*4)
		for _, a := range offered {
			buf = append(buf, byte(a), byte(a>>8), byte(a>>16), byte(a>>24))
		}
		xproto.ChangeProperty(X, xproto.PropModeReplace, e.Requestor, prop,
			xproto.AtomAtom, 32, uint32(len(offered)), buf)
		reply.Property = prop

	case atomUTF8, xproto.AtomString:
		data := []byte(clipboardText)
		xproto.ChangeProperty(X, xproto.PropModeReplace, e.Requestor, prop,
			e.Target, 8, uint32(len(data)), data)
		reply.Property = prop
	}

	clipDebugf("  -> answered target=%s prop=%s", atomLabel(e.Target), atomLabel(reply.Property))
	xproto.SendEvent(X, false, e.Requestor, 0, string(reply.Bytes()))
	X.Sync()
}

// handleSelectionNotify reads the converted selection off our property and stashes
// it for injection next frame. Reports whether a frame should run.
func handleSelectionNotify(e xproto.SelectionNotifyEvent) bool {
	if e.Property == 0 {
		clipDebugf("SelectionNotify: property None (no owner / target refused)")
		return false // no owner, or the owner refused the UTF8_STRING target
	}
	// Delete=true clears the property after reading, per ICCCM. LongLength is in
	// 4-byte units; 1<<22 covers up to 16 MiB, far beyond any real text paste.
	r, err := xproto.GetProperty(X, true, win, e.Property,
		xproto.GetPropertyTypeAny, 0, 1<<22).Reply()
	if err != nil || r == nil {
		return false
	}
	if r.Type == atomIncr {
		clipDebugf("SelectionNotify: INCR (huge selection) — unsupported")
		return false // chunked transfer (huge selection) — unsupported for now
	}
	clipDebugf("SelectionNotify: read %d bytes (type=%s)", len(r.Value), atomLabel(r.Type))
	pendingPaste = string(r.Value)
	hasPendingPaste = true
	return true
}

// injectPendingPaste delivers a previously-read paste as typed text at the start
// of a frame, before frameFn runs (FrameInput is reset at the end of each frame).
// injectPendingPaste folds a clipboard read into the pending committed-text
// buffer so flushPendingText delivers paste and IME/typed text together.
func injectPendingPaste() {
	if hasPendingPaste {
		appendPendingText(pendingPaste)
		pendingPaste = ""
		hasPendingPaste = false
	}
}
