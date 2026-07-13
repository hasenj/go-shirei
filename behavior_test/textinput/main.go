package main

// Behavior test: TextInput editing over many frames with synthetic input.
//
// Headless regression suite — not in the normal `go test` suite. Exercises
// the same backend contract as widgets/textinput_test.go (set FrameInput
// before RunFrameFn) but as long scripts with a single PASS/FAIL summary.
//
//	go run ./behavior_test/textinput
//	go run ./behavior_test/textinput -v
//
// Cases:
//
//  1. type-arrows — typing grows the buffer; arrow keys move without inserting
//  2. cut — selection cut updates buffer and clipboard copy request
//  3. undo-redo — coalesced typing undoes/redoes across idle frames
//  4. scroll-caret — End/Home keep the caret inside a narrow field
//  5. backspace-max-scroll — delete at end while scrolled; caret stays with text
//
// Public observables only (buffer, CaretPos, FrameOutputData.Copy) — no
// access to widgets-internal caret state.

import (
	"flag"
	"fmt"
	"os"
	"runtime"
	"strings"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

type f32 = float32

const (
	winW, winH f32 = 400, 100
	longText       = "the quick brown fox jumps over the lazy dog " +
		"the quick brown fox jumps over the lazy dog"
)

var verbose bool

func main() {
	flag.BoolVar(&verbose, "v", false, "verbose per-case detail")
	flag.Parse()

	// Fonts scan on package init; probe for a usable face before asserting text.
	probe := ShapeText("alpha", DefaultTextAttrs())
	if len(probe.Lines) != 1 || len(probe.Lines[0].Segments) == 0 {
		fmt.Println("=== behavior_test: textinput ===")
		fmt.Println("SKIP: no usable system fonts for text shaping")
		os.Exit(0)
	}

	fmt.Println("=== behavior_test: textinput ===")
	failed := 0
	for _, c := range []struct {
		name string
		fn   func() error
	}{
		{"type-arrows", caseTypeAndArrows},
		{"cut", caseCut},
		{"undo-redo", caseUndoRedo},
		{"scroll-caret", caseScrollFollowsCaret},
		{"backspace-max-scroll", caseBackspaceAtMaxScroll},
	} {
		if err := c.fn(); err != nil {
			fmt.Printf("FAIL %s: %v\n", c.name, err)
			failed++
		} else {
			fmt.Printf("PASS %s\n", c.name)
		}
	}
	if failed > 0 {
		fmt.Printf("RESULT: %d case(s) failed\n", failed)
		os.Exit(1)
	}
	fmt.Println("RESULT: all cases passed")
}

// ── harness ───────────────────────────────────────────────────────────────

type harness struct {
	buf   string
	out   FrameOutputData
	frame func()
	multi bool
}

func newSingleLine(initial string) *harness {
	return newHarness(initial, false)
}

func newHarness(initial string, multiline bool) *harness {
	ResetInputSession()
	WindowSize = Vec2{winW, winH}

	h := &harness{buf: initial, multi: multiline}
	scope := new(int)
	h.frame = func() {
		// clear transient input each caller's responsibility before set
		h.out = RunFrameFn(func() {
			ModAttrs(func(a *AttrSet) { a.NoAnimate = true })
			ContainerWithKey(scope, Attrs(Viewport, Pad(8)), func() {
				if multiline {
					attrs := DefaultTextInputAttrs()
					attrs.MaxLines = 0
					attrs.Rows = 3
					attrs.MinWidth = 220
					TextInputExt(&h.buf, attrs)
				} else {
					TextInput(&h.buf)
				}
			})
		})
	}
	// AutoFocus lands over a few frames (same as unit harness).
	h.frame()
	h.frame()
	h.frame()
	return h
}

func (h *harness) idle() {
	FrameInput.Key = 0
	FrameInput.Text = ""
	FrameInput.Mouse = 0
	InputState.Modifiers = 0
	h.frame()
}

func (h *harness) typeText(s string) {
	for _, r := range s {
		FrameInput.Text = string(r)
		FrameInput.Key = 0
		h.frame()
		h.idle() // like a real typist gap — also keeps undo coalescing realistic
	}
}

func (h *harness) pressKey(k KeyCode, mods Modifiers) {
	InputState.Modifiers = mods
	FrameInput.Key = k
	FrameInput.Text = ""
	h.frame()
	// Keep h.out from the key frame (clipboard copy lives there); then idle.
	keyOut := h.out
	InputState.Modifiers = 0
	FrameInput.Key = 0
	h.idle()
	h.out = keyOut
}

func (h *harness) pressKeySettle(k KeyCode) {
	// Extra idles so CaretPos (previous-frame screen rect) catches up.
	FrameInput.Key = k
	FrameInput.Text = ""
	h.frame()
	h.idle()
	h.idle()
}

func primaryMod() Modifiers {
	if runtime.GOOS == "darwin" {
		return ModCmd
	}
	return ModCtrl
}

func logf(format string, args ...any) {
	if verbose {
		fmt.Printf("  "+format+"\n", args...)
	}
}

// ── cases ─────────────────────────────────────────────────────────────────

// Typing grows the buffer; pure arrow keys must not insert (regression:
// backends once relayed function-key private-use chars as FrameInput.Text).
func caseTypeAndArrows() error {
	h := newSingleLine("")
	h.typeText("hello")
	if h.buf != "hello" {
		return fmt.Errorf("after type: buf=%q want %q", h.buf, "hello")
	}
	logf("typed %q", h.buf)

	before := h.buf
	h.pressKey(KeyLeft, 0)
	h.pressKey(KeyLeft, 0)
	if h.buf != before {
		return fmt.Errorf("arrows modified buffer: %q → %q", before, h.buf)
	}
	h.typeText("X")
	if h.buf != "helXlo" {
		return fmt.Errorf("insert after arrows: buf=%q want %q", h.buf, "helXlo")
	}
	logf("after left×2 + X: %q", h.buf)

	h.pressKey(KeyRight, 0)
	h.pressKey(KeyRight, 0)
	h.typeText("!")
	if h.buf != "helXlo!" {
		return fmt.Errorf("end insert: buf=%q want %q", h.buf, "helXlo!")
	}
	return nil
}

// Cut with no selection is a no-op; cut with selection removes and copies.
func caseCut() error {
	h := newSingleLine("")
	h.typeText("hello world")
	mod := primaryMod()

	// No selection: cut must not change buffer or copy.
	h.pressKey(KeyX, mod)
	if h.buf != "hello world" {
		return fmt.Errorf("cut no-sel modified buf: %q", h.buf)
	}
	if h.out.Copy != "" {
		return fmt.Errorf("cut no-sel copied %q", h.out.Copy)
	}

	// Select all + cut.
	h.pressKey(KeyA, mod)
	h.pressKey(KeyX, mod)
	if h.buf != "" {
		return fmt.Errorf("cut all: buf=%q want empty", h.buf)
	}
	if h.out.Copy != "hello world" {
		return fmt.Errorf("cut all: copy=%q want %q", h.out.Copy, "hello world")
	}
	logf("cut all → copy=%q", h.out.Copy)

	// Partial selection: type, home, shift-right×5, cut "hello".
	h.typeText("hello world")
	h.pressKey(KeyHome, 0)
	for range 5 {
		h.pressKey(KeyRight, ModShift)
	}
	h.pressKey(KeyX, mod)
	if h.buf != " world" {
		return fmt.Errorf("cut partial: buf=%q want %q", h.buf, " world")
	}
	if h.out.Copy != "hello" {
		return fmt.Errorf("cut partial: copy=%q want %q", h.out.Copy, "hello")
	}
	return nil
}

// Undo/redo history must survive idle frames between events.
func caseUndoRedo() error {
	h := newSingleLine("")
	mod := primaryMod()

	h.typeText("abc")
	if h.buf != "abc" {
		return fmt.Errorf("setup: buf=%q", h.buf)
	}
	h.pressKey(KeyZ, mod) // undo coalesced run
	if h.buf != "" {
		return fmt.Errorf("undo: buf=%q want empty", h.buf)
	}
	h.pressKey(KeyZ, mod|ModShift) // redo
	if h.buf != "abc" {
		return fmt.Errorf("redo: buf=%q want %q", h.buf, "abc")
	}
	logf("undo/redo round-trip ok")

	// Motion splits the undo run.
	h.pressKey(KeyLeft, 0)
	h.typeText("X")
	if h.buf != "abXc" {
		return fmt.Errorf("post-motion type: buf=%q want %q", h.buf, "abXc")
	}
	h.pressKey(KeyZ, mod)
	if h.buf != "abc" {
		return fmt.Errorf("undo motion-run: buf=%q want %q", h.buf, "abc")
	}
	h.pressKey(KeyZ, mod)
	if h.buf != "" {
		return fmt.Errorf("undo first run: buf=%q want empty", h.buf)
	}
	return nil
}

// Long text: End scrolls caret into the field; Home brings it back left.
func caseScrollFollowsCaret() error {
	h := newSingleLine(longText)
	// Park mouse off the field so hover scroll does not interfere.
	InputState.MousePoint = Vec2{winW - 10, winH - 10}

	attrs := DefaultTextInputAttrs()
	boxW := attrs.Padding[PAD_LEFT]*2 + attrs.FontSize*10

	h.pressKeySettle(KeyEnd)
	if CaretPos[0] > boxW+2 {
		return fmt.Errorf("End: caret x=%.1f outside box width≈%.0f", CaretPos[0], boxW)
	}
	if CaretPos[0] < boxW/2 {
		return fmt.Errorf("End: caret x=%.1f expected near right edge of ≈%.0f box", CaretPos[0], boxW)
	}
	logf("End: CaretPos.x=%.1f boxW=%.0f", CaretPos[0], boxW)

	h.pressKeySettle(KeyHome)
	if CaretPos[0] > 40 {
		return fmt.Errorf("Home: caret x=%.1f expected near left edge", CaretPos[0])
	}
	logf("Home: CaretPos.x=%.1f", CaretPos[0])
	return nil
}

// At max scroll, backspace must keep caret glued to the text end.
func caseBackspaceAtMaxScroll() error {
	h := newSingleLine(longText)
	InputState.MousePoint = Vec2{winW - 10, winH - 10}

	attrs := DefaultTextInputAttrs()
	boxW := attrs.Padding[PAD_LEFT]*2 + attrs.FontSize*10

	h.pressKeySettle(KeyEnd)
	endX := CaretPos[0]
	if endX < boxW/2 {
		return fmt.Errorf("setup End: caret x=%.1f not near right edge", endX)
	}
	before := len([]rune(h.buf))
	for range 4 {
		h.pressKeySettle(KeyDeleteBackward)
	}
	if got := len([]rune(h.buf)); got != before-4 {
		return fmt.Errorf("backspace deleted %d runes, want 4 (buf=%q)", before-got, h.buf)
	}
	// Text end stays pinned at the right; caret must stay with it.
	if CaretPos[0] < endX-6 {
		return fmt.Errorf("caret drifted left after backspace: x=%.1f was %.1f", CaretPos[0], endX)
	}
	if !strings.HasPrefix(longText, h.buf[:min(20, len(h.buf))]) && !strings.Contains(longText, h.buf[len(h.buf)-10:]) {
		// soft check: still looks like truncated longText
		logf("buf after backspace: %q", h.buf)
	}
	logf("backspace@maxScroll: caret x=%.1f (end was %.1f) bufLen=%d", CaretPos[0], endX, len([]rune(h.buf)))
	return nil
}
