package widgets

import (
	"image"
	"runtime"
	"slices"
	"strings"
	"testing"

	. "go.hasen.dev/shirei"
)

// harness: drive a focused TextInput frame by frame with synthetic input,
// the same way a backend does (set GetFrameInput() before RunFrameFn; core
// resets it at frame end). out holds the last frame's output, for
// asserting clipboard requests.
type textInputHarness struct {
	buf   string
	out   FrameOutputData
	frame func()
}

func newInputHarness(t *testing.T, text string, input func(*string)) *textInputHarness {
	t.Helper()
	InitFontSubsystem()
	probe := ShapeText("alpha", DefaultTextStyle())
	if len(probe.Lines) != 1 || len(probe.Lines[0].Segments) == 0 {
		t.Skip("no usable system fonts for text shaping")
	}

	ResetInputSession()
	GetHost().WindowSize = Vec2{400, 100}

	h := &textInputHarness{buf: text}
	scope := new(int)
	h.frame = func() {
		h.out = RunFrameFn(func() {
			ContainerWithKey(scope, Attrs(Viewport), func() {
				input(&h.buf)
			})
		})
	}

	h.frame() // first render: AutoFocus requests focus
	h.frame() // focus lands; ReceivedFocusNow resets the editing state
	h.frame() // steady state; render data (rects) available for hit testing
	return h
}

func newTextInputHarness(t *testing.T, text string) *textInputHarness {
	t.Helper()
	// Pin width: Fill-by-default would grow to WindowSize and break caret geometry.
	return newInputHarness(t, text, func(buf *string) {
		a := DefaultTextInputAttrs()
		a.FixedWidth = true
		TextInputExt(buf, a)
	})
}

func newMultilineInputHarness(t *testing.T, text string) *textInputHarness {
	t.Helper()
	attrs := DefaultTextInputAttrs()
	attrs.MaxLines = 0
	attrs.Rows = 3
	attrs.MinWidth = 220
	attrs.FixedWidth = true
	return newInputHarness(t, text, func(buf *string) {
		TextInputExt(buf, attrs)
	})
}

func (h *textInputHarness) pressKey(k KeyCode, mods Modifiers) {
	GetInputState().Modifiers = mods
	GetFrameInput().Key = k
	h.frame()
	GetInputState().Modifiers = 0
	h.frame()
}

// pointAt returns a screen point just right of the given rune
// boundary — inside the following glyph's left half, so a click there
// places the caret at runeIdx.
func (h *textInputHarness) pointAt(runeIdx int) Vec2 {
	attrs := DefaultTextInputAttrs()
	textAttrs := DefaultTextStyle()
	textAttrs.FontSize = attrs.FontSize
	shaped := ShapeText(h.buf, textAttrs)
	x := computeCursorPos(runeIdx, shaped)[0]
	return Vec2{attrs.Padding[PAD_LEFT] + x + 1, attrs.Padding[PAD_TOP] + attrs.FontSize/2}
}

// clickAt places the caret with a real mouse press+release at the given
// rune boundary, mirroring how a user positions the caret.
func (h *textInputHarness) clickAt(t *testing.T, runeIdx int) {
	t.Helper()
	GetInputState().MousePoint = h.pointAt(runeIdx)
	GetFrameInput().Mouse = MouseClick
	h.frame()
	GetFrameInput().Mouse = MouseRelease
	h.frame()
}

// caretGlyphExists reports whether the caret's rune index sits on a shaped
// glyph cluster (or the end of text). When it doesn't, computeCursorPos
// falls through and draws the caret at the end of the line — the visible
// "caret jumps to end" symptom.
func (h *textInputHarness) caretGlyphExists() bool {
	if activeInput.cursor == len([]rune(h.buf)) {
		return true
	}
	attrs := DefaultTextInputAttrs()
	textAttrs := DefaultTextStyle()
	textAttrs.FontSize = attrs.FontSize
	shaped := ShapeText(h.buf, textAttrs)
	for _, line := range shaped.Lines {
		for _, seg := range line.Segments {
			for _, g := range seg.Glyphs {
				if int(g.Cluster) == activeInput.cursor {
					return true
				}
			}
		}
	}
	return false
}

// TestTextInputArrowKeys pins caret movement against the backend input
// contract: an arrow keypress arrives as GetFrameInput().Key only — never as
// GetFrameInput().Text. One press moves the caret one rune; the buffer must not
// change. (The cocoa backend once relayed NSEvent's private-use function-key
// characters — U+F702 etc. — as typed text, so every arrow press inserted an
// invisible glyphless rune and the caret rendered at end of line.)
func TestTextInputArrowKeys(t *testing.T) {
	h := newTextInputHarness(t, "hello world")
	h.clickAt(t, 5)
	if activeInput.cursor != 5 {
		t.Fatalf("click at rune 5: cursor = %d", activeInput.cursor)
	}

	press := func(k KeyCode) {
		GetFrameInput().Key = k
		h.frame()
		h.frame() // idle frame, like the display link ticking after the event
	}

	press(KeyRight)
	if activeInput.cursor != 6 || h.buf != "hello world" {
		t.Errorf("after ArrowRight: cursor=%d buf=%q, want 6 %q", activeInput.cursor, h.buf, "hello world")
	}
	press(KeyLeft)
	if activeInput.cursor != 5 || h.buf != "hello world" {
		t.Errorf("after ArrowLeft: cursor=%d buf=%q, want 5 %q", activeInput.cursor, h.buf, "hello world")
	}
	press(KeyLeft)
	if activeInput.cursor != 4 || h.buf != "hello world" {
		t.Errorf("after second ArrowLeft: cursor=%d buf=%q, want 4 %q", activeInput.cursor, h.buf, "hello world")
	}
	if !h.caretGlyphExists() {
		t.Errorf("caret at %d has no glyph cluster; would render at end of line", activeInput.cursor)
	}
}

// TestTextInputCut pins cut semantics: with a selection, Cmd/Ctrl+X
// copies it and removes it; with no selection it is a strict no-op —
// it used to fall through to delete(buf, 0), eating the rune after the
// caret.
func TestTextInputCut(t *testing.T) {
	h := newTextInputHarness(t, "hello world")
	h.clickAt(t, 5)

	mod := ModCtrl
	if runtime.GOOS == "darwin" {
		mod = ModCmd
	}
	cut := func() {
		GetInputState().Modifiers = mod
		GetFrameInput().Key = KeyX
		h.frame()
		GetInputState().Modifiers = 0
	}

	// no selection: nothing copied, nothing deleted
	cut()
	if h.buf != "hello world" {
		t.Fatalf("cut with no selection modified buffer: %q", h.buf)
	}
	if h.out.Copy != "" {
		t.Fatalf("cut with no selection copied %q", h.out.Copy)
	}

	// select " w" with shift+right twice, then cut
	GetInputState().Modifiers = ModShift
	GetFrameInput().Key = KeyRight
	h.frame()
	GetFrameInput().Key = KeyRight
	h.frame()
	cut()
	if h.buf != "helloorld" {
		t.Errorf("cut selection: buf = %q, want %q", h.buf, "helloorld")
	}
	if h.out.Copy != " w" {
		t.Errorf("cut selection: copied %q, want %q", h.out.Copy, " w")
	}
}

// TestTextInputWordAndEdgeKeys is a frame-level spot check that word
// and line-edge commands flow through the shell; the full binding
// matrix is pinned in editdecode_test.go, the boundary rule in
// editcore_test.go.
func TestTextInputWordAndEdgeKeys(t *testing.T) {
	h := newTextInputHarness(t, "hello world")
	h.clickAt(t, 8)

	press := func(k KeyCode, mods Modifiers) {
		GetInputState().Modifiers = mods
		GetFrameInput().Key = k
		h.frame()
		GetInputState().Modifiers = 0
	}

	press(KeyHome, 0)
	if activeInput.cursor != 0 {
		t.Errorf("Home: cursor = %d, want 0", activeInput.cursor)
	}
	press(KeyEnd, 0)
	if activeInput.cursor != 11 {
		t.Errorf("End: cursor = %d, want 11", activeInput.cursor)
	}
	press(KeyLeft, ModAlt) // word left, platform-independent binding
	if activeInput.cursor != 6 {
		t.Errorf("Option+Left: cursor = %d, want 6", activeInput.cursor)
	}
	press(KeyDeleteBackward, ModAlt) // deletes "hello " (word run + space)
	if h.buf != "world" || activeInput.cursor != 0 {
		t.Errorf("Option+Backspace: buf = %q cursor = %d, want %q 0", h.buf, activeInput.cursor, "world")
	}
}

// TestTextInputMultiClick pins double-click word selection, triple-
// click select-all, and that dragging while a multi-click selection is
// held does not collapse it.
func TestTextInputMultiClick(t *testing.T) {
	h := newTextInputHarness(t, "hello world")

	h.clickAt(t, 7) // single: place caret
	if activeInput.cursor != 7 || activeInput.anchor != 7 {
		t.Fatalf("single click: cursor=%d anchor=%d", activeInput.cursor, activeInput.anchor)
	}
	h.clickAt(t, 7) // double: select "world"
	if activeInput.anchor != 6 || activeInput.cursor != 11 {
		t.Errorf("double click: selection %d..%d, want 6..11", activeInput.anchor, activeInput.cursor)
	}

	// triple click, and keep the button held
	p := h.pointAt(7)
	GetInputState().MousePoint = p
	GetFrameInput().Mouse = MouseClick
	h.frame()
	if activeInput.anchor != 0 || activeInput.cursor != 11 {
		t.Errorf("triple click: selection %d..%d, want 0..11", activeInput.anchor, activeInput.cursor)
	}

	// drag while held: the multi-click selection must not collapse
	GetInputState().MousePoint = Vec2{p[0] - 30, p[1]}
	h.frame()
	if activeInput.anchor != 0 || activeInput.cursor != 11 {
		t.Errorf("drag after triple click collapsed selection to %d..%d", activeInput.anchor, activeInput.cursor)
	}
	GetFrameInput().Mouse = MouseRelease
	h.frame()
}

// TestTextInputPasteSanitized pins that incoming text (typing and
// paste both arrive as GetFrameInput().Text) is sanitized for the
// single-line buffer: newlines/tabs become spaces, control runes drop.
func TestTextInputPasteSanitized(t *testing.T) {
	h := newTextInputHarness(t, "")
	GetFrameInput().Text = "line1\nline2\r\nline3\tend"
	h.frame()
	if h.buf != "line1 line2 line3 end" {
		t.Errorf("pasted multiline: buf = %q, want %q", h.buf, "line1 line2 line3 end")
	}
}

func TestTextInputMultilineInsert(t *testing.T) {
	attrs := DefaultTextInputAttrs()
	attrs.MaxLines = 0
	attrs.Rows = 2

	h := newInputHarness(t, "", func(buf *string) {
		TextInputExt(buf, attrs)
	})

	GetFrameInput().Text = "line1\nline2\tend"
	h.frame()
	if h.buf != "line1\nline2\tend" {
		t.Fatalf("multiline paste: buf = %q", h.buf)
	}

	GetFrameInput().Key = KeyEnter
	h.frame()
	if h.buf != "line1\nline2\tend\n" {
		t.Errorf("Enter in multiline input: buf = %q", h.buf)
	}
}

func TestTextInputMaxLines(t *testing.T) {
	attrs := DefaultTextInputAttrs()
	attrs.MaxLines = 2
	attrs.Rows = 2

	h := newInputHarness(t, "", func(buf *string) {
		TextInputExt(buf, attrs)
	})

	GetFrameInput().Text = "a\nb\nc"
	h.frame()
	if h.buf != "a\nb c" {
		t.Fatalf("paste capped to two lines: buf = %q", h.buf)
	}

	GetFrameInput().Key = KeyEnter
	h.frame()
	if h.buf != "a\nb c" {
		t.Errorf("pure newline at line cap should be dropped: buf = %q", h.buf)
	}
}

func TestTextInputVerticalMotionKeepsGoalColumn(t *testing.T) {
	h := newMultilineInputHarness(t, "abcdef\nxy\nabcdef")

	for range 4 {
		h.pressKey(KeyRight, 0)
	}
	if activeInput.cursor != 4 {
		t.Fatalf("setup cursor = %d, want 4", activeInput.cursor)
	}

	h.pressKey(KeyDown, 0)
	if activeInput.cursor != 9 {
		t.Fatalf("Down to short line: cursor = %d, want 9", activeInput.cursor)
	}

	h.pressKey(KeyDown, 0)
	if activeInput.cursor != 14 {
		t.Fatalf("Down keeps original column: cursor = %d, want 14", activeInput.cursor)
	}

	h.pressKey(KeyUp, 0)
	if activeInput.cursor != 9 {
		t.Fatalf("Up to short line: cursor = %d, want 9", activeInput.cursor)
	}

	h.pressKey(KeyUp, 0)
	if activeInput.cursor != 4 {
		t.Fatalf("Up keeps original column: cursor = %d, want 4", activeInput.cursor)
	}

	h.pressKey(KeyUp, 0)
	if activeInput.cursor != 0 {
		t.Fatalf("Up on first line moves to document start: cursor = %d, want 0", activeInput.cursor)
	}
}

func TestTextInputVerticalMotionThroughEmptyLine(t *testing.T) {
	h := newMultilineInputHarness(t, "abcdef\n\nabcdef")

	for range 4 {
		h.pressKey(KeyRight, 0)
	}
	if activeInput.cursor != 4 {
		t.Fatalf("setup cursor = %d, want 4", activeInput.cursor)
	}

	h.pressKey(KeyDown, 0)
	if activeInput.cursor != 7 {
		t.Fatalf("Down to empty line: cursor = %d, want 7", activeInput.cursor)
	}

	h.pressKey(KeyDown, 0)
	if activeInput.cursor != 12 {
		t.Fatalf("Down from empty line keeps goal column: cursor = %d, want 12", activeInput.cursor)
	}

	h.pressKey(KeyUp, 0)
	if activeInput.cursor != 7 {
		t.Fatalf("Up to empty line: cursor = %d, want 7", activeInput.cursor)
	}

	h.pressKey(KeyUp, 0)
	if activeInput.cursor != 4 {
		t.Fatalf("Up from empty line keeps goal column: cursor = %d, want 4", activeInput.cursor)
	}
}

func TestTextInputCaretPositionOnEmptyLine(t *testing.T) {
	InitFontSubsystem()
	probe := ShapeText("alpha", DefaultTextStyle())
	if len(probe.Lines) != 1 || len(probe.Lines[0].Segments) == 0 {
		t.Skip("no usable system fonts for text shaping")
	}

	attrs := DefaultTextStyle()
	attrs.FontSize = DefaultTextInputAttrs().FontSize
	shaped := ShapeText("abcdef\n\nabcdef", attrs)
	starts := lineStarts(shaped)
	if !slices.Equal(starts, []int{0, 7, 8}) {
		t.Fatalf("lineStarts = %v, want [0 7 8]", starts)
	}

	emptyPos := computeCursorPos(7, shaped)
	if want := (Vec2{0, lineTop(1, shaped)}); emptyPos != want {
		t.Fatalf("empty-line caret pos = %v, want %v", emptyPos, want)
	}

	nextPos := computeCursorPos(8, shaped)
	if emptyPos[1] >= nextPos[1] {
		t.Fatalf("empty-line caret y %.2f should be above next-line y %.2f", emptyPos[1], nextPos[1])
	}
}

func TestTextInputMultilineGeometryEdges(t *testing.T) {
	InitFontSubsystem()
	probe := ShapeText("alpha", DefaultTextStyle())
	if len(probe.Lines) != 1 || len(probe.Lines[0].Segments) == 0 {
		t.Skip("no usable system fonts for text shaping")
	}

	attrs := DefaultTextStyle()
	attrs.FontSize = DefaultTextInputAttrs().FontSize

	t.Run("hard break delimiter draws at previous line end", func(t *testing.T) {
		shaped := ShapeText("ab\ncd", attrs)
		if got, want := lineStarts(shaped), []int{0, 3}; !slices.Equal(got, want) {
			t.Fatalf("lineStarts = %v, want %v", got, want)
		}

		newlinePos := computeCursorPos(2, shaped)
		if want := (Vec2{shaped.Lines[0].Width, lineTop(0, shaped)}); newlinePos != want {
			t.Fatalf("newline caret pos = %v, want %v", newlinePos, want)
		}

		nextLinePos := computeCursorPos(3, shaped)
		if want := (Vec2{0, lineTop(1, shaped)}); nextLinePos != want {
			t.Fatalf("next-line caret pos = %v, want %v", nextLinePos, want)
		}
	})

	t.Run("empty line and trailing newline draw as visual rows", func(t *testing.T) {
		empty := ShapeText("a\n\nb", attrs)
		if got, want := lineStarts(empty), []int{0, 2, 3}; !slices.Equal(got, want) {
			t.Fatalf("empty lineStarts = %v, want %v", got, want)
		}
		if got, want := computeCursorPos(2, empty), (Vec2{0, lineTop(1, empty)}); got != want {
			t.Fatalf("empty-line caret pos = %v, want %v", got, want)
		}

		trailing := ShapeText("ab\n", attrs)
		if got, want := lineStarts(trailing), []int{0, 3}; !slices.Equal(got, want) {
			t.Fatalf("trailing lineStarts = %v, want %v", got, want)
		}
		if got, want := computeCursorPos(3, trailing), (Vec2{0, lineTop(1, trailing)}); got != want {
			t.Fatalf("trailing-newline caret pos = %v, want %v", got, want)
		}
	})

	t.Run("leading newline draws following text on second row", func(t *testing.T) {
		shaped := ShapeText("\nWord", attrs)
		if got, want := lineStarts(shaped), []int{0, 1}; !slices.Equal(got, want) {
			t.Fatalf("lineStarts = %v, want %v", got, want)
		}
		if lineTop(1, shaped) <= lineTop(0, shaped) {
			t.Fatalf("leading-newline second row top = %.2f, want below %.2f", lineTop(1, shaped), lineTop(0, shaped))
		}
		if got, want := computeCursorPos(1, shaped), (Vec2{0, lineTop(1, shaped)}); got != want {
			t.Fatalf("leading-newline caret pos = %v, want %v", got, want)
		}
	})

	t.Run("hit testing empty visual rows returns their line start", func(t *testing.T) {
		empty := ShapeText("a\n\nb", attrs)
		emptyLinePoint := Vec2{0, lineTop(1, empty) + empty.Lines[1].Height/2}
		if got := computeCursorIndexInText(emptyLinePoint, empty); got != 2 {
			t.Fatalf("empty-line hit index = %d, want 2", got)
		}

		trailing := ShapeText("ab\n", attrs)
		trailingLinePoint := Vec2{0, lineTop(1, trailing) + trailing.Lines[1].Height/2}
		if got := computeCursorIndexInText(trailingLinePoint, trailing); got != 3 {
			t.Fatalf("trailing-line hit index = %d, want 3", got)
		}
	})

	t.Run("soft wrap boundary supports both affinities", func(t *testing.T) {
		wrapW := ShapeText("hello ", attrs).Lines[0].Width + 0.1
		shaped := ShapeTextMax("hello world", attrs, wrapW)
		if len(shaped.Lines) < 2 {
			t.Fatalf("text did not wrap; lines = %d", len(shaped.Lines))
		}
		if got, want := lineStarts(shaped), []int{0, 6}; !slices.Equal(got, want) {
			t.Fatalf("wrapped lineStarts = %v, want %v", got, want)
		}
		if got, want := computeCursorPos(6, shaped), (Vec2{0, lineTop(1, shaped)}); got != want {
			t.Fatalf("default soft-wrap boundary caret pos = %v, want %v", got, want)
		}
		if got, want := computeCursorPosWithAffinity(6, shaped, caretAffinityPreviousLine), (Vec2{shaped.Lines[0].Width, lineTop(0, shaped)}); got != want {
			t.Fatalf("previous-line soft-wrap boundary caret pos = %v, want %v", got, want)
		}
	})
}

func TestTextInputHardBreakRightAndEnd(t *testing.T) {
	h := newMultilineInputHarness(t, "ab\ncd")
	attrs := DefaultTextStyle()
	attrs.FontSize = DefaultTextInputAttrs().FontSize

	h.pressKey(KeyRight, 0)
	h.pressKey(KeyRight, 0)
	if activeInput.cursor != 2 {
		t.Fatalf("Right onto hard break: cursor = %d, want 2", activeInput.cursor)
	}
	shaped := ShapeText(h.buf, attrs)
	if got, want := computeCursorPos(activeInput.cursor, shaped), (Vec2{shaped.Lines[0].Width, lineTop(0, shaped)}); got != want {
		t.Fatalf("Right onto hard break caret pos = %v, want %v", got, want)
	}

	h.pressKey(KeyLeft, 0)
	if activeInput.cursor != 1 {
		t.Fatalf("setup before End: cursor = %d, want 1", activeInput.cursor)
	}
	h.pressKey(KeyEnd, 0)
	if activeInput.cursor != 2 {
		t.Fatalf("End on hard line: cursor = %d, want 2", activeInput.cursor)
	}
	if got, want := computeCursorPos(activeInput.cursor, shaped), (Vec2{shaped.Lines[0].Width, lineTop(0, shaped)}); got != want {
		t.Fatalf("End on hard line caret pos = %v, want %v", got, want)
	}
}

func TestTextInputSoftWrapRightAndEndAffinity(t *testing.T) {
	attrs := DefaultTextInputAttrs()
	attrs.MaxLines = 0
	attrs.Rows = 2
	attrs.Wrap = true

	textAttrs := DefaultTextStyle()
	textAttrs.FontSize = attrs.FontSize
	wrapW := ShapeText("hello ", textAttrs).Lines[0].Width + 0.1
	attrs.MinWidth = wrapW + PadSize(attrs.Padding)[0]
	attrs.MaxWidth = attrs.MinWidth

	h := newInputHarness(t, "hello world", func(buf *string) {
		TextInputExt(buf, attrs)
	})

	for range 6 {
		h.pressKey(KeyRight, 0)
	}
	if activeInput.cursor != 6 {
		t.Fatalf("Right to soft-wrap boundary: cursor = %d, want 6", activeInput.cursor)
	}
	shaped := ShapeTextMax(h.buf, textAttrs, wrapW)
	aff := caretAffinityDefault
	if activeInput.preferPrevLineCaret {
		aff = caretAffinityPreviousLine
	}
	if got, want := computeCursorPosWithAffinity(activeInput.cursor, shaped, aff), (Vec2{shaped.Lines[0].Width, lineTop(0, shaped)}); got != want {
		t.Fatalf("Right to soft-wrap boundary caret pos = %v, want %v", got, want)
	}

	h.pressKey(KeyLeft, 0)
	if activeInput.cursor != 5 {
		t.Fatalf("setup before End: cursor = %d, want 5", activeInput.cursor)
	}
	h.pressKey(KeyEnd, 0)
	if activeInput.cursor != 6 {
		t.Fatalf("End on soft-wrapped line: cursor = %d, want 6", activeInput.cursor)
	}
	aff = caretAffinityDefault
	if activeInput.preferPrevLineCaret {
		aff = caretAffinityPreviousLine
	}
	if got, want := computeCursorPosWithAffinity(activeInput.cursor, shaped, aff), (Vec2{shaped.Lines[0].Width, lineTop(0, shaped)}); got != want {
		t.Fatalf("End on soft-wrapped line caret pos = %v, want %v", got, want)
	}

	h.pressKey(KeyHome, 0)
	if activeInput.cursor != 0 {
		t.Fatalf("Home from soft-wrapped line end: cursor = %d, want 0", activeInput.cursor)
	}
	aff = caretAffinityDefault
	if activeInput.preferPrevLineCaret {
		aff = caretAffinityPreviousLine
	}
	if got, want := computeCursorPosWithAffinity(activeInput.cursor, shaped, aff), (Vec2{}); got != want {
		t.Fatalf("Home from soft-wrapped line end caret pos = %v, want %v", got, want)
	}

	h.pressKey(KeyEnd, 0)
	h.pressKey(KeyEnd, 0)
	if activeInput.cursor != 6 {
		t.Fatalf("repeated End from soft-wrapped line end: cursor = %d, want 6", activeInput.cursor)
	}
	aff = caretAffinityDefault
	if activeInput.preferPrevLineCaret {
		aff = caretAffinityPreviousLine
	}
	if got, want := computeCursorPosWithAffinity(activeInput.cursor, shaped, aff), (Vec2{shaped.Lines[0].Width, lineTop(0, shaped)}); got != want {
		t.Fatalf("repeated End from soft-wrapped line end caret pos = %v, want %v", got, want)
	}
}

func TestTextInputVerticalMotionExtendsSelection(t *testing.T) {
	h := newMultilineInputHarness(t, "abcd\nabcd")

	h.pressKey(KeyRight, 0)
	h.pressKey(KeyRight, 0)
	if activeInput.cursor != 2 || activeInput.anchor != 2 {
		t.Fatalf("setup selection = %d..%d, want 2..2", activeInput.anchor, activeInput.cursor)
	}

	h.pressKey(KeyDown, ModShift)
	if activeInput.cursor != 7 || activeInput.anchor != 2 {
		t.Fatalf("Shift+Down selection = %d..%d, want 2..7", activeInput.anchor, activeInput.cursor)
	}
}

func TestTextInputVerticalDocumentEdges(t *testing.T) {
	h := newMultilineInputHarness(t, "one\ntwo\nthree")
	primary := ModCtrl
	if runtime.GOOS == "darwin" {
		primary = ModCmd
	}

	h.pressKey(KeyDown, primary)
	if activeInput.cursor != len([]rune(h.buf)) {
		t.Fatalf("primary+Down: cursor = %d, want %d", activeInput.cursor, len([]rune(h.buf)))
	}

	h.pressKey(KeyUp, primary|ModShift)
	if activeInput.cursor != 0 || activeInput.anchor != len([]rune(h.buf)) {
		t.Fatalf("primary+Shift+Up selection = %d..%d, want %d..0",
			activeInput.anchor, activeInput.cursor, len([]rune(h.buf)))
	}
}

func TestTextInputRows(t *testing.T) {
	attrs := DefaultTextInputAttrs()
	if got := textInputRows(attrs); got != 1 {
		t.Errorf("default rows = %d, want 1", got)
	}

	attrs.MaxLines = 0
	if got := textInputRows(attrs); got != 4 {
		t.Errorf("unlimited default rows = %d, want 4", got)
	}

	attrs.MaxLines = 3
	if got := textInputRows(attrs); got != 3 {
		t.Errorf("MaxLines=3 rows = %d, want 3", got)
	}

	attrs.MaxLines = 10
	if got := textInputRows(attrs); got != 4 {
		t.Errorf("MaxLines=10 rows = %d, want 4", got)
	}

	attrs.Rows = 2
	if got := textInputRows(attrs); got != 2 {
		t.Errorf("explicit rows = %d, want 2", got)
	}
}

func TestDefaultMultilineTextInputAttrs(t *testing.T) {
	attrs := DefaultMultilineTextInputAttrs()
	if !attrs.Wrap {
		t.Errorf("DefaultMultilineTextInputAttrs Wrap = false, want true")
	}
	if attrs.MaxLines != 0 {
		t.Errorf("DefaultMultilineTextInputAttrs MaxLines = %d, want 0", attrs.MaxLines)
	}
	if attrs.Rows != 4 {
		t.Errorf("DefaultMultilineTextInputAttrs Rows = %d, want 4", attrs.Rows)
	}
	if got := textInputRows(attrs); got != 4 {
		t.Errorf("DefaultMultilineTextInputAttrs effective rows = %d, want 4", got)
	}
}

func TestTextAreaUsesMultilineDefaults(t *testing.T) {
	h := newInputHarness(t, "alpha", TextArea)
	h.pressKey(KeyEnter, 0)
	if h.buf != "\nalpha" {
		t.Fatalf("TextArea Enter: buf = %q, want %q", h.buf, "\nalpha")
	}
}

func TestTextInputLineStartsFromShapedText(t *testing.T) {
	InitFontSubsystem()
	probe := ShapeText("alpha", DefaultTextStyle())
	if len(probe.Lines) != 1 || len(probe.Lines[0].Segments) == 0 {
		t.Skip("no usable system fonts for text shaping")
	}

	attrs := DefaultTextStyle()
	attrs.FontSize = DefaultTextInputAttrs().FontSize

	cases := []struct {
		text string
		want []int
	}{
		{"hello", []int{0}},
		{"a\n\nb", []int{0, 2, 3}},
		{"ab\n", []int{0, 3}},
	}
	for _, c := range cases {
		if got := lineStarts(ShapeText(c.text, attrs)); !slices.Equal(got, c.want) {
			t.Errorf("lineStarts(%q) = %v, want %v", c.text, got, c.want)
		}
	}
}

const longText = "the quick brown fox jumps over the lazy dog " +
	"the quick brown fox jumps over the lazy dog"

// TestTextInputScrollFollowsCaret pins the viewport: text much wider
// than the box scrolls so the caret stays inside it. CaretPos (the
// IME anchor, derived from the caret's screen rect) is the observable:
// it lags one frame, hence the extra idle frames after each key.
func TestTextInputScrollFollowsCaret(t *testing.T) {
	h := newTextInputHarness(t, longText)
	h.clickAt(t, 2)

	boxW := DefaultTextInputAttrs().Padding[PAD_LEFT]*2 + DefaultTextInputAttrs().FontSize*10

	press := func(k KeyCode) {
		GetFrameInput().Key = k
		h.frame()
		h.frame()
		h.frame() // CaretPos reads the caret's previous-frame screen rect
	}

	press(KeyEnd)
	if activeInput.cursor != len([]rune(h.buf)) {
		t.Fatalf("End: cursor = %d", activeInput.cursor)
	}
	if GetHost().CaretPos[0] > boxW+2 {
		t.Errorf("End: caret at x=%.1f, outside the %.0f-wide box (not scrolled?)", GetHost().CaretPos[0], boxW)
	}
	if GetHost().CaretPos[0] < boxW/2 {
		t.Errorf("End: caret at x=%.1f, expected near the right edge of the %.0f-wide box", GetHost().CaretPos[0], boxW)
	}

	press(KeyHome)
	if GetHost().CaretPos[0] > 20 {
		t.Errorf("Home: caret at x=%.1f, expected near the left edge", GetHost().CaretPos[0])
	}
}

// TestTextInputBackspaceAtMaxScroll pins caret-vs-text agreement when the
// content shrinks while scrolled to the end (a long path, caret at the end,
// then backspace): the scroll hook held a desire beyond the new scrollable
// range, layout rendered the text with the clamped offset (end pinned to
// the right edge) but the caret drew with the stale hook value — walking
// left, detached from the end it was deleting at.
func TestTextInputBackspaceAtMaxScroll(t *testing.T) {
	h := newTextInputHarness(t, longText)
	// park the mouse away from the field: hovering runs ScrollOnInput's
	// own previous-frame clamp, which would mask the stale hook
	GetInputState().MousePoint = Vec2{390, 90}

	press := func(k KeyCode) {
		GetFrameInput().Key = k
		h.frame()
		h.frame()
		h.frame() // CaretPos reads the caret's previous-frame screen rect
	}

	press(KeyEnd)
	endX := GetHost().CaretPos[0]
	boxW := DefaultTextInputAttrs().Padding[PAD_LEFT]*2 + DefaultTextInputAttrs().FontSize*10
	if endX < boxW/2 {
		t.Fatalf("setup: caret at x=%.1f, expected near the right edge of the %.0f-wide box", endX, boxW)
	}

	before := len([]rune(h.buf))
	for range 4 {
		press(KeyDeleteBackward)
	}
	if got := len([]rune(h.buf)); got != before-4 {
		t.Fatalf("backspaces deleted %d runes, want 4", before-got)
	}
	// the text end stays pinned at the right edge while scrolled; the
	// caret must stay with it (allow a few px for the reveal margin)
	if GetHost().CaretPos[0] < endX-6 {
		t.Errorf("caret drifted off the text end: x=%.1f, was %.1f before backspacing", GetHost().CaretPos[0], endX)
	}
}

// TestTextInputPasteAtMaxScroll pins caret-vs-text agreement when the
// content grows while already scrolled to the end — the autosuggest and
// paste cases that leave the ti-scroll hook at the old maximum until
// revealCaret catches up.
func TestTextInputPasteAtMaxScroll(t *testing.T) {
	h := newTextInputHarness(t, longText)
	GetInputState().MousePoint = Vec2{390, 90}

	press := func(k KeyCode) {
		GetFrameInput().Key = k
		h.frame()
		h.frame()
		h.frame()
	}

	press(KeyEnd)
	endX := GetHost().CaretPos[0]
	boxW := DefaultTextInputAttrs().Padding[PAD_LEFT]*2 + DefaultTextInputAttrs().FontSize*10
	if endX < boxW/2 {
		t.Fatalf("setup: caret at x=%.1f, expected near the right edge of the %.0f-wide box", endX, boxW)
	}

	GetFrameInput().Text = "suffix"
	h.frame()
	h.frame()
	h.frame()

	if !strings.HasSuffix(h.buf, "suffix") {
		t.Fatalf("paste: buf = %q", h.buf)
	}
	if activeInput.cursor != runeLen(h.buf) {
		t.Fatalf("paste: cursor = %d, want %d", activeInput.cursor, runeLen(h.buf))
	}
	if GetHost().CaretPos[0] < endX-6 {
		t.Errorf("paste at max scroll: caret drifted left to x=%.1f, was %.1f before paste", GetHost().CaretPos[0], endX)
	}
}

// TestTextInputExternalSetCursorAtMaxScroll covers host code that replaces
// the buffer and calls EditorSetCursor in the same frame (path-picker
// acceptance): the scroll hook must catch up on the next frame.
func TestTextInputExternalSetCursorAtMaxScroll(t *testing.T) {
	var editorId ContainerId
	accept := false
	h := newInputHarness(t, longText, func(buf *string) {
		a := DefaultTextInputAttrs()
		a.FixedWidth = true
		TextInputExt(buf, a)
		editorId = GetLastId()
		if accept {
			*buf += "/accepted/"
			EditorSetCursor(editorId, runeLen(*buf))
			accept = false
		}
	})
	GetInputState().MousePoint = Vec2{390, 90}

	press := func(k KeyCode) {
		GetFrameInput().Key = k
		h.frame()
		h.frame()
		h.frame()
	}

	press(KeyEnd)
	endX := GetHost().CaretPos[0]
	boxW := DefaultTextInputAttrs().Padding[PAD_LEFT]*2 + DefaultTextInputAttrs().FontSize*10
	if endX < boxW/2 {
		t.Fatalf("setup: caret at x=%.1f, expected near the right edge of the %.0f-wide box", endX, boxW)
	}

	accept = true
	h.frame()
	h.frame()
	h.frame()

	if activeInput.cursor != runeLen(h.buf) {
		t.Fatalf("external set: cursor = %d, want %d", activeInput.cursor, runeLen(h.buf))
	}
	if GetHost().CaretPos[0] < endX-6 {
		t.Errorf("external set at max scroll: caret drifted left to x=%.1f, was %.1f before", GetHost().CaretPos[0], endX)
	}
}

// TestTextInputExternalBufferClamp covers host code that shortens *buf
// without going through Apply: the caret must clamp to the new length.
func TestTextInputExternalBufferClamp(t *testing.T) {
	replace := false
	h := newInputHarness(t, "hello world", func(buf *string) {
		a := DefaultTextInputAttrs()
		a.FixedWidth = true
		TextInputExt(buf, a)
		if replace {
			*buf = "hi"
			replace = false
		}
	})

	h.pressKey(KeyEnd, 0)
	if activeInput.cursor != runeLen("hello world") {
		t.Fatalf("setup: cursor = %d, want %d", activeInput.cursor, runeLen("hello world"))
	}

	replace = true
	h.frame()
	h.frame()

	if h.buf != "hi" {
		t.Fatalf("buf = %q, want %q", h.buf, "hi")
	}
	if activeInput.cursor != 2 || activeInput.anchor != 2 {
		t.Fatalf("after external shorten: cursor/anchor = %d/%d, want 2/2", activeInput.cursor, activeInput.anchor)
	}
}

func runeLen(s string) int {
	return len([]rune(s))
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

func countTextInputUnderlineSurfaces(out FrameOutputData, height float32) int {
	var n int
	for _, s := range out.Surfaces {
		if s.Stroke == 0 &&
			s.Color1 == (Vec4{0, 0, 30, 1}) &&
			abs32(s.Rect.Size[1]-height) < 0.1 &&
			s.Rect.Size[0] > 0 {
			n++
		}
	}
	return n
}

func TestTextInputCompositionIsDisplayState(t *testing.T) {
	h := newTextInputHarness(t, "abc")
	h.pressKey(KeyRight, 0)

	GetInputState().Composition = "かな"
	GetInputState().CompositionSel = [2]int{2, 2}
	h.frame()
	h.frame()

	if h.buf != "abc" {
		t.Fatalf("composition mutated buffer: %q", h.buf)
	}
	if activeInput.cursor != 1 || activeInput.anchor != 1 {
		t.Fatalf("composition moved document caret/selection to %d..%d, want 1..1", activeInput.anchor, activeInput.cursor)
	}

	textAttrs := DefaultTextStyle()
	textAttrs.FontSize = DefaultTextInputAttrs().FontSize
	shaped := ShapeText("aかなbc", textAttrs)
	wantX := DefaultTextInputAttrs().Padding[PAD_LEFT] + computeCursorPos(3, shaped)[0]
	if abs32(GetHost().CaretPos[0]-wantX) > 2 {
		t.Fatalf("composition caret x = %.1f, want near %.1f", GetHost().CaretPos[0], wantX)
	}
	wantAnchorX := DefaultTextInputAttrs().Padding[PAD_LEFT] + computeCursorPos(1, shaped)[0]
	if abs32(GetHost().CompositionPos[0]-wantAnchorX) > 2 {
		t.Fatalf("composition anchor x = %.1f, want near %.1f", GetHost().CompositionPos[0], wantAnchorX)
	}
	if GetHost().CaretHeight <= 0 {
		t.Fatalf("composition caret height = %.1f, want positive", GetHost().CaretHeight)
	}
	if countTextInputUnderlineSurfaces(h.out, 1) != 1 {
		t.Fatalf("composition underline count = %d, want 1", countTextInputUnderlineSurfaces(h.out, 1))
	}

	GetInputState().Composition = ""
	GetInputState().CompositionSel = [2]int{}
	mod := ModCtrl
	if runtime.GOOS == "darwin" {
		mod = ModCmd
	}
	h.pressKey(KeyZ, mod)
	if h.buf != "abc" {
		t.Fatalf("undo after composition-only frame changed buffer to %q", h.buf)
	}
}

func TestTextInputCommitSurvivesSettlePass(t *testing.T) {
	InitFontSubsystem()
	probe := ShapeText("alpha", DefaultTextStyle())
	if len(probe.Lines) != 1 || len(probe.Lines[0].Segments) == 0 {
		t.Skip("no usable system fonts for text shaping")
	}

	ResetInputSession()
	GetHost().WindowSize = Vec2{400, 100}
	buf := ""
	scope := new(int)
	var deadID ContainerId

	frame := func() {
		RunFrameFn(func() {
			ContainerWithKey(scope, Attrs(Viewport), func() {
				_ = GetResolvedRectOf(deadID)
				TextInput(&buf)
			})
		})
	}

	frame()
	frame()
	frame()

	GetFrameInput().Text = "入力"
	before := ActiveUI().FrameNumber
	frame()
	if got := ActiveUI().FrameNumber - before; got != 2 {
		t.Fatalf("setup did not force settle pass: ran %d passes, want 2", got)
	}
	if buf != "入力" {
		t.Fatalf("committed text after settle pass = %q, want %q", buf, "入力")
	}
}

func TestTextInputCompositionSurvivesSettlePass(t *testing.T) {
	InitFontSubsystem()
	probe := ShapeText("alpha", DefaultTextStyle())
	if len(probe.Lines) != 1 || len(probe.Lines[0].Segments) == 0 {
		t.Skip("no usable system fonts for text shaping")
	}

	ResetInputSession()
	GetHost().WindowSize = Vec2{400, 100}
	buf := "abc"
	scope := new(int)
	var deadID ContainerId

	frame := func() FrameOutputData {
		return RunFrameFn(func() {
			ContainerWithKey(scope, Attrs(Viewport), func() {
				_ = GetResolvedRectOf(deadID)
				TextInput(&buf)
			})
		})
	}

	frame()
	frame()
	frame()
	GetFrameInput().Key = KeyRight
	frame()
	frame()

	GetInputState().Composition = "かな"
	GetInputState().CompositionSel = [2]int{2, 2}
	before := ActiveUI().FrameNumber
	out := frame()
	if got := ActiveUI().FrameNumber - before; got != 2 {
		t.Fatalf("setup did not force settle pass: ran %d passes, want 2", got)
	}
	if buf != "abc" {
		t.Fatalf("composition mutated buffer after settle pass: %q", buf)
	}
	if GetHost().CaretHeight <= 0 {
		t.Fatalf("composition caret height after settle pass = %.1f, want positive", GetHost().CaretHeight)
	}
	if countTextInputUnderlineSurfaces(out, 1) != 1 {
		t.Fatalf("composition underline count after settle pass = %d, want 1", countTextInputUnderlineSurfaces(out, 1))
	}
}

func TestTextInputCompositionStartDeletesSelectionOnce(t *testing.T) {
	h := newTextInputHarness(t, "abcde")
	h.clickAt(t, 1)
	for range 3 {
		h.pressKey(KeyRight, ModShift)
	}
	if activeInput.anchor != 1 || activeInput.cursor != 4 {
		t.Fatalf("setup selection = %d..%d, want 1..4", activeInput.anchor, activeInput.cursor)
	}

	GetInputState().Composition = "に"
	GetInputState().CompositionSel = [2]int{1, 1}
	h.frame()
	if h.buf != "ae" || activeInput.cursor != 1 || activeInput.anchor != 1 {
		t.Fatalf("composition-start delete: buf=%q selection=%d..%d, want ae 1..1",
			h.buf, activeInput.anchor, activeInput.cursor)
	}

	GetInputState().Composition = "にほ"
	GetInputState().CompositionSel = [2]int{2, 2}
	h.frame()
	if h.buf != "ae" {
		t.Fatalf("composition update deleted selection again: buf=%q", h.buf)
	}

	GetInputState().Composition = ""
	GetInputState().CompositionSel = [2]int{}
	mod := ModCtrl
	if runtime.GOOS == "darwin" {
		mod = ModCmd
	}
	h.pressKey(KeyZ, mod)
	if h.buf != "abcde" {
		t.Fatalf("undo selection delete: buf=%q, want abcde", h.buf)
	}
}

func TestTextInputCompositionSelectedClauseUnderline(t *testing.T) {
	h := newTextInputHarness(t, "")

	GetInputState().Composition = "にほんご"
	GetInputState().CompositionSel = [2]int{2, 4}
	h.frame()
	h.frame()

	if h.buf != "" {
		t.Fatalf("composition mutated buffer: %q", h.buf)
	}
	if got := countTextInputUnderlineSurfaces(h.out, 1); got != 1 {
		t.Fatalf("preedit underline count = %d, want 1", got)
	}
	if got := countTextInputUnderlineSurfaces(h.out, 2); got != 1 {
		t.Fatalf("selected-clause underline count = %d, want 1", got)
	}

	textAttrs := DefaultTextStyle()
	textAttrs.FontSize = DefaultTextInputAttrs().FontSize
	shaped := ShapeText("にほんご", textAttrs)
	wantX := DefaultTextInputAttrs().Padding[PAD_LEFT] + computeCursorPos(4, shaped)[0]
	if abs32(GetHost().CaretPos[0]-wantX) > 2 {
		t.Fatalf("selected composition caret x = %.1f, want near %.1f", GetHost().CaretPos[0], wantX)
	}
}

// Composition right before an RTL run used to underline the Arabic as well:
// caret-to-caret geometry bridged across the bidi boundary. Glyph-cluster
// boxes only cover the preedit clusters.
func TestCompositionUnderlineDoesNotBridgeBidi(t *testing.T) {
	InitFontSubsystem()
	attrs := DefaultTextStyle()
	attrs.FontSize = DefaultTextSize
	// display string while composing "にほ" after "hey" and before "عربيworld"
	shaped := ShapeText("heyにほعربيworld", attrs)
	if len(shaped.Lines) == 0 || len(shaped.Runes) < 9 {
		t.Skip("no usable fonts for mixed JP/Arabic shaping")
	}
	// にほ are display indices 3..5
	const from, to = 3, 5
	glyphRects := mergeAdjacentRects(glyphBoxesForClusters(shaped, from, to, 1))
	spanRects := textSpanRects(shaped, from, to, 1)
	if len(glyphRects) == 0 {
		t.Fatal("glyph-cluster underline produced no rects")
	}
	var glyphW, spanW float32
	for _, r := range glyphRects {
		glyphW += r.Size[0]
	}
	for _, r := range spanRects {
		spanW += r.Size[0]
	}
	// Caret-to-caret must be wider (it eats the Arabic); glyph boxes stay
	// near the two JP advances only.
	if spanW <= glyphW+1 {
		t.Fatalf("expected caret-to-caret span (%.1f) to bridge past glyph width (%.1f); bidi probe invalid?", spanW, glyphW)
	}
	// Arabic "ع" is cluster 5 — its glyph center must not sit inside any
	// composition underline rect.
	var arabX, arabW float32
	var x float32
	found := false
	for _, seg := range shaped.Lines[0].Segments {
		for _, g := range seg.Glyphs {
			if int(g.Cluster) == 5 {
				arabX, arabW = x, g.XAdvance
				found = true
			}
			x += g.XAdvance
		}
	}
	if !found {
		t.Fatal("Arabic cluster 5 not found in shaped line")
	}
	arabMid := arabX + arabW/2
	for _, r := range glyphRects {
		if arabMid >= r.Origin[0] && arabMid < r.Origin[0]+r.Size[0] {
			t.Fatalf("composition underline [%g,%g) covers Arabic glyph mid %g",
				r.Origin[0], r.Origin[0]+r.Size[0], arabMid)
		}
	}

	// End-to-end: field with the bidi buffer, composition at the LTR/RTL edge.
	h := newTextInputHarness(t, "heyعربيworld")
	// place caret after "hey" (3 runes)
	for i := 0; i < 3; i++ {
		h.pressKey(KeyRight, 0)
	}
	GetInputState().Composition = "にほ"
	GetInputState().CompositionSel = [2]int{2, 2}
	h.frame()
	h.frame()
	if got := countTextInputUnderlineSurfaces(h.out, 1); got < 1 {
		t.Fatalf("expected composition underline surfaces, got %d", got)
	}
	// Total underline width should stay near JP only (~2 em), not JP+Arabic.
	var underW float32
	for _, s := range h.out.Surfaces {
		if s.Stroke == 0 && s.Color1 == (Vec4{0, 0, 30, 1}) && abs32(s.Rect.Size[1]-1) < 0.1 {
			underW += s.Rect.Size[0]
		}
	}
	if underW > glyphW+4 {
		t.Fatalf("live composition underline width %.1f exceeds JP glyph width %.1f (Arabic leaked in)", underW, glyphW)
	}
}

func TestTextAreaCompositionUnderlineWrapsAndReveals(t *testing.T) {
	attrs := DefaultMultilineTextInputAttrs()
	attrs.MinWidth = 60
	attrs.MaxWidth = 60
	h := newInputHarness(t, "", func(buf *string) {
		TextInputExt(buf, attrs)
	})

	composition := strings.Repeat("nihongo ", 20)
	GetInputState().Composition = composition
	GetInputState().CompositionSel = [2]int{runeLen(composition), runeLen(composition)}
	h.frame()
	h.frame()
	h.frame()

	if h.buf != "" {
		t.Fatalf("composition mutated TextArea buffer: %q", h.buf)
	}
	if got := countTextInputUnderlineSurfaces(h.out, 1); got < 2 {
		t.Fatalf("wrapped TextArea preedit underline segments = %d, want at least 2", got)
	}
	boxW := attrs.MaxWidth
	if GetHost().CaretPos[0] > boxW+2 {
		t.Fatalf("composition reveal caret x=%.1f outside %.1f-wide TextArea", GetHost().CaretPos[0], boxW)
	}
}

func TestTextInputCompositionIgnoresStaleSoftWrapAffinity(t *testing.T) {
	attrs := DefaultTextInputAttrs()
	attrs.MaxLines = 0
	attrs.Rows = 2
	attrs.Wrap = true

	textAttrs := DefaultTextStyle()
	textAttrs.FontSize = attrs.FontSize
	wrapW := ShapeText("hello ", textAttrs).Lines[0].Width + 0.1
	attrs.MinWidth = wrapW + PadSize(attrs.Padding)[0]
	attrs.MaxWidth = attrs.MinWidth

	h := newInputHarness(t, "hello world", func(buf *string) {
		TextInputExt(buf, attrs)
	})

	for range 5 {
		h.pressKey(KeyRight, 0)
	}
	h.pressKey(KeyEnd, 0)
	if activeInput.cursor != 6 || !activeInput.preferPrevLineCaret {
		t.Fatalf("setup cursor=%d preferPrev=%v, want soft-wrap boundary with prefer-prev",
			activeInput.cursor, activeInput.preferPrevLineCaret)
	}

	GetInputState().Composition = "X"
	GetInputState().CompositionSel = [2]int{0, 0}
	h.frame()
	h.frame()

	if GetHost().CaretPos[0] > attrs.Padding[PAD_LEFT]+3 {
		t.Fatalf("composition caret used stale previous-line affinity: x=%.1f", GetHost().CaretPos[0])
	}
}

func TestTextInputVerticalScrollFollowsCaret(t *testing.T) {
	attrs := DefaultTextInputAttrs()
	attrs.MaxLines = 0
	attrs.Rows = 2
	h := newInputHarness(t, "one\ntwo\nthree\nfour\nfive", func(buf *string) {
		TextInputExt(buf, attrs)
	})

	primary := ModCtrl
	if runtime.GOOS == "darwin" {
		primary = ModCmd
	}
	h.pressKey(KeyDown, primary)
	h.frame()
	h.frame()

	if activeInput.cursor != len([]rune(h.buf)) {
		t.Fatalf("primary+Down: cursor = %d, want %d", activeInput.cursor, len([]rune(h.buf)))
	}
	boxH := attrs.Padding[PAD_TOP] + attrs.FontSize*float32(attrs.Rows) + attrs.Padding[PAD_BOTTOM]
	if GetHost().CaretPos[1] < attrs.Padding[PAD_TOP]+attrs.FontSize {
		t.Fatalf("vertical reveal: caret y=%.1f, expected in lower visible row", GetHost().CaretPos[1])
	}
	if GetHost().CaretPos[1] > boxH+1 {
		t.Fatalf("vertical reveal: caret y=%.1f outside %.1f-high box", GetHost().CaretPos[1], boxH)
	}
}

func TestTextInputWheelScrollsFocusedMultiline(t *testing.T) {
	attrs := DefaultTextInputAttrs()
	attrs.MaxLines = 0
	attrs.Rows = 2
	h := newInputHarness(t, "one\ntwo\nthree\nfour\nfive", func(buf *string) {
		TextInputExt(buf, attrs)
	})
	for range 3 {
		h.frame()
	}
	if activeInput.revealCaret {
		t.Fatalf("setup: revealCaret still set after idle frames")
	}

	GetInputState().MousePoint = Vec2{attrs.Padding[PAD_LEFT] + 1, attrs.Padding[PAD_TOP] + attrs.FontSize/2}
	GetFrameInput().Scroll = Vec2{0, attrs.FontSize * 3}
	h.frame()
	GetFrameInput().Scroll = Vec2{}
	h.frame()

	GetFrameInput().Mouse = MouseClick
	h.frame()
	GetFrameInput().Mouse = MouseRelease
	h.frame()
	if activeInput.cursor == 0 {
		t.Fatalf("wheel scroll did not affect hit testing; click at viewport top stayed at cursor 0")
	}
}

// TestTextInputDragAutoScroll pins that drag-selecting past the right
// edge keeps scrolling more text into view, extending the selection
// far beyond the initially visible characters.
func TestTextInputDragAutoScroll(t *testing.T) {
	h := newTextInputHarness(t, longText)

	// press at rune 2 and hold
	p := h.pointAt(2)
	GetInputState().MousePoint = p
	GetFrameInput().Mouse = MouseClick
	h.frame()
	if activeInput.cursor != 2 {
		t.Fatalf("press: cursor = %d", activeInput.cursor)
	}

	// drag well past the right edge of the box and let frames run
	GetInputState().MousePoint = Vec2{300, p[1]}
	for range 40 {
		h.frame()
	}
	GetFrameInput().Mouse = MouseRelease
	h.frame()

	runeCount := len([]rune(h.buf))
	if activeInput.cursor < runeCount-1 {
		t.Errorf("drag auto-scroll: cursor reached %d of %d runes", activeInput.cursor, runeCount)
	}
	if activeInput.anchor != 2 {
		t.Errorf("drag auto-scroll: anchor = %d, want 2", activeInput.anchor)
	}
}

// TestTextInputUndoRedo drives undo through real frames: typing in
// separate frames coalesces into one undo step; a caret motion splits
// the runs; redo restores; the history survives between event frames
// (the hook must be claimed every frame, or it silently expires).
func TestTextInputUndoRedo(t *testing.T) {
	h := newTextInputHarness(t, "")

	typeText := func(s string) {
		for _, r := range s {
			GetFrameInput().Text = string(r)
			h.frame()
			h.frame() // idle frame between keystrokes, like a real typist
		}
	}
	mod := ModCtrl
	if runtime.GOOS == "darwin" {
		mod = ModCmd
	}
	combo := func(k KeyCode, m Modifiers) {
		GetInputState().Modifiers = m
		GetFrameInput().Key = k
		h.frame()
		GetInputState().Modifiers = 0
		h.frame()
	}

	typeText("abc")
	combo(KeyZ, mod) // undo the whole coalesced run
	if h.buf != "" {
		t.Fatalf("undo after typing: buf = %q, want empty", h.buf)
	}
	combo(KeyZ, mod|ModShift) // redo
	if h.buf != "abc" {
		t.Fatalf("redo: buf = %q, want %q", h.buf, "abc")
	}

	// motion splits the run
	GetFrameInput().Key = KeyLeft
	h.frame()
	typeText("X") // "abXc"
	if h.buf != "abXc" {
		t.Fatalf("setup: buf = %q", h.buf)
	}
	combo(KeyZ, mod)
	if h.buf != "abc" {
		t.Errorf("undo post-motion typing: buf = %q, want %q", h.buf, "abc")
	}
	combo(KeyZ, mod)
	if h.buf != "" {
		t.Errorf("undo initial run: buf = %q, want empty", h.buf)
	}
}

// TestTextInputClusterMotion exercises cluster snapping end to end
// with real shaping: a combining acute accent merges with its base
// into one cluster, so the caret steps over both and backspace deletes
// both. Skipped if the host shaper/font doesn't merge them.
func TestTextInputClusterMotion(t *testing.T) {
	const text = "café!" // 6 runes; e+◌́ shape as one cluster at 3
	h := newTextInputHarness(t, text)

	textAttrs := DefaultTextStyle()
	textAttrs.FontSize = DefaultTextInputAttrs().FontSize
	if slices.Contains(clusterBounds(ShapeText(text, textAttrs)), 4) {
		t.Skip("shaper did not merge the combining mark into its base cluster")
	}

	press := func(k KeyCode) {
		GetFrameInput().Key = k
		h.frame()
		h.frame()
	}
	press(KeyEnd) // cursor 6
	press(KeyLeft)
	if activeInput.cursor != 5 {
		t.Fatalf("Left from end: cursor = %d, want 5", activeInput.cursor)
	}
	press(KeyLeft) // skips the mark: 5 -> 3
	if activeInput.cursor != 3 {
		t.Errorf("Left over the accent cluster: cursor = %d, want 3", activeInput.cursor)
	}
	// caret renders at the cluster start, not end of line, even for a
	// stale mid-cluster index
	shaped := ShapeText(h.buf, textAttrs)
	if pos, start := computeCursorPos(4, shaped), computeCursorPos(3, shaped); pos != start {
		t.Errorf("mid-cluster caret at %v, want cluster start %v", pos, start)
	}

	press(KeyEnd)
	press(KeyLeft) // before '!'
	GetFrameInput().Key = KeyDeleteBackward
	h.frame()
	if h.buf != "caf!" {
		t.Errorf("backspace over the cluster: buf = %q, want %q", h.buf, "caf!")
	}
}

// TestTextInputDescendersNotClipped pins that the clip lives on the
// box, not the text run: glyph descenders (j, q, y) extend below the
// em box into the bottom padding and must survive clipping.
func TestTextInputDescendersNotClipped(t *testing.T) {
	InitFontSubsystem()
	probe := ShapeText("alpha", DefaultTextStyle())
	if len(probe.Lines) != 1 || len(probe.Lines[0].Segments) == 0 {
		t.Skip("no usable system fonts for text shaping")
	}

	buf := "jqy"
	attrs := DefaultTextInputAttrs()
	attrs.NoAutoFocus = true // no caret; only glyph ink in the box
	img := RenderToImage(200, 60, func() {
		Container(Attrs(Viewport), func() {
			TextInputExt(&buf, attrs)
		})
	})

	// em box spans y = padTop .. padTop+FontSize; descender ink lives at
	// and just below its bottom edge (inside the bottom padding). The
	// old viewport clip cut exactly at that edge. Sample in device pixels
	// (RenderToImage uses HeadlessScale).
	s := int(HeadlessScale)
	if s < 1 {
		s = 1
	}
	top := int(attrs.Padding[PAD_TOP]+attrs.FontSize) * s
	found := false
	for y := top; y <= top+4*s && !found; y++ {
		for x := 0; x < 80*s; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			if r>>8 < 100 && g>>8 < 100 && b>>8 < 100 {
				found = true
				break
			}
		}
	}
	if !found {
		t.Errorf("no descender ink below the em box (y %d..%d) — clipping descenders?", top+1, top+4*s)
	}
}

// TestPasswordInputClipboard pins that a masked input never places its
// content on the clipboard: copy and cut are suppressed in the shell.
// (Historically copy produced the mask bullets — useless; once the
// editing model started producing the copy text, an unfiltered copy
// would leak the real password, so masked inputs block both.)
// Selection and typing still work: type-over-selection replaces.
func TestPasswordInputClipboard(t *testing.T) {
	h := newInputHarness(t, "hunter2", PasswordInput)

	mod := ModCtrl
	if runtime.GOOS == "darwin" {
		mod = ModCmd
	}
	combo := func(k KeyCode) {
		GetInputState().Modifiers = mod
		GetFrameInput().Key = k
		h.frame()
		GetInputState().Modifiers = 0
	}

	combo(KeyA) // select all
	combo(KeyC)
	if h.out.Copy != "" {
		t.Errorf("copy from masked input produced %q", h.out.Copy)
	}
	combo(KeyX)
	if h.out.Copy != "" || h.buf != "hunter2" {
		t.Errorf("cut from masked input: copied %q, buf %q", h.out.Copy, h.buf)
	}

	// the selection itself still works: typing replaces it
	GetFrameInput().Text = "!"
	h.frame()
	if h.buf != "!" {
		t.Errorf("type over select-all: buf = %q, want %q", h.buf, "!")
	}
}

// TestTextInputGlyphlessTextFailureMode is an executable record of the
// "arrow key jumps to end of line" bug (fixed in cocoabackend's
// isPrintable): a backend that relays NSEvent function-key characters
// as Text makes the widget insert an invisible glyphless rune. The
// buffer-corruption half still reproduces (the widget trusts Text; the
// contract is on backends). The caret half is defanged since phase 5:
// glyphless runes contribute no cluster boundary, so the caret SKIPS
// them — it can no longer land mid-junk, let alone render at end of
// line, and a backspace sweeps the junk out with its cluster.
func TestTextInputGlyphlessTextFailureMode(t *testing.T) {
	h := newTextInputHarness(t, "hello world")
	h.clickAt(t, 5)

	// what pre-fix cocoa delivered for a right-arrow press
	GetFrameInput().Key = KeyRight
	GetFrameInput().Text = "\uF703"
	h.frame()
	h.frame()

	if !strings.ContainsRune(h.buf, '\uF703') {
		t.Fatalf("expected the function-key rune in the buffer, got %q", h.buf)
	}
	if activeInput.cursor != 7 {
		t.Fatalf("setup: cursor = %d, want 7 (after the inserted rune)", activeInput.cursor)
	}
	// left-arrow skips the glyphless rune: 7 -> 5, not 6
	GetFrameInput().Key = KeyLeft
	h.frame()
	if activeInput.cursor != 5 {
		t.Errorf("ArrowLeft over glyphless rune: cursor = %d, want 5", activeInput.cursor)
	}
	// and even a cursor forced onto the junk index renders at the
	// previous boundary, never at end of line
	textAttrs := DefaultTextStyle()
	textAttrs.FontSize = DefaultTextInputAttrs().FontSize
	shaped := ShapeText(h.buf, textAttrs)
	if pos, prev := computeCursorPos(6, shaped), computeCursorPos(5, shaped); pos != prev {
		t.Errorf("mid-junk caret drawn at %v, want the previous boundary %v", pos, prev)
	}
}

// TestTextInputPlaceholder pins the placeholder contract: drawn (dimmed)
// while the buffer is empty, and replaced by real text once typed.
func TestTextInputPlaceholder(t *testing.T) {
	InitFontSubsystem()
	probe := ShapeText("alpha", DefaultTextStyle())
	if len(probe.Lines) != 1 || len(probe.Lines[0].Segments) == 0 {
		t.Skip("no usable system fonts for text shaping")
	}

	render := func(buf string) *image.RGBA {
		attrs := DefaultTextInputAttrs()
		attrs.NoAutoFocus = true // no caret ink; only text pixels
		attrs.Placeholder = "enter a search query to find what you need"
		return RenderToImage(300, 60, func() {
			Container(Attrs(Viewport), func() {
				TextInputExt(&buf, attrs)
			})
		})
	}

	attrs := DefaultTextInputAttrs()
	s := int(HeadlessScale)
	if s < 1 {
		s = 1
	}
	y0 := int(attrs.Padding[PAD_TOP]) * s
	y1 := y0 + int(attrs.FontSize)*s
	inkInRow := func(img *image.RGBA, x0, x1 int) bool {
		for y := y0; y <= y1; y++ {
			for x := x0 * s; x < x1*s; x++ {
				r, g, b, _ := img.At(x, y).RGBA()
				if r>>8 < 160 && g>>8 < 160 && b>>8 < 160 {
					return true
				}
			}
		}
		return false
	}

	empty := render("")
	if !inkInRow(empty, 10, 280) {
		t.Error("empty buffer: placeholder text not drawn")
	}

	// a typed character replaces the placeholder: the stretch only the
	// placeholder glyphs could reach must fall back to the background
	filled := render("x")
	if inkInRow(filled, 150, 280) {
		t.Error("non-empty buffer: placeholder ink still visible")
	}
}

func TestTextInputPlaceholderColor(t *testing.T) {
	cfg := TextInputConfig{}.withDefaults()
	got := textInputPlaceholderColor(cfg)
	if want := cfg.TextColor[3] * 0.4; got[3] != want {
		t.Errorf("derived placeholder alpha = %v, want %v", got[3], want)
	}
	if got[0] != cfg.TextColor[0] || got[1] != cfg.TextColor[1] || got[2] != cfg.TextColor[2] {
		t.Errorf("derived placeholder rgb = %v, want text color %v", got, cfg.TextColor)
	}
	explicit := cfg
	explicit.PlaceholderColor = Vec4{0, 0, 50, 0.5}
	if got := textInputPlaceholderColor(explicit); got != explicit.PlaceholderColor {
		t.Errorf("explicit placeholder color = %v, want %v", got, explicit.PlaceholderColor)
	}
}
