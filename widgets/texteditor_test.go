package widgets

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	. "go.hasen.dev/shirei"
)

func TestTextEditorTabGeometryAndEditing(t *testing.T) {
	h := newDocumentHarness(t, "a\tb\nabc\tb\nabcd\tb", false)
	style := TextStyle(Fonts(Monospace...))
	style.FontSize *= ComfortScale()
	space := ShapeText(" ", style).Lines[0].Width
	x0 := h.view.Caret.Origin[0]
	h.key(KeyRight, 0)
	h.key(KeyRight, 0)
	if abs32(h.view.Caret.Origin[0]-x0-space*4) > .01 {
		t.Fatalf("caret after tab: %+v", h.view.Caret)
	}
	h.key(KeyDown, 0)
	if _, p := h.doc.Selection(); p != (LinePos{1, 4}) {
		t.Fatalf("vertical movement loses visual tab column: %v", p)
	}
	GetInputState().MousePoint = Vec2Add(h.view.Caret.Origin, Vec2{.1, 3})
	h.doc.SetSelection(LinePos{}, LinePos{})
	h.frame()
	GetFrameInput().Mouse = MouseClick
	h.frame()
	GetFrameInput().Mouse = MouseRelease
	h.frame()
	if _, p := h.doc.Selection(); p != (LinePos{1, 4}) {
		t.Fatalf("click after tab: %v", p)
	}
	h.key(KeyLeft, ModShift)
	if h.doc.SelectedText() != "\t" {
		t.Fatalf("tab selection does not preserve document text: %q", h.doc.SelectedText())
	}
	h.key(KeyDeleteBackward, 0)
	if h.doc.Line(1) != "abcb" {
		t.Fatal("backspace does not delete one tab", h.doc.Line(1))
	}
	h.key(KeyZ, PrimaryMod())
	if h.doc.Line(1) != "abc\tb" {
		t.Fatal("undo does not restore the tab")
	}
}

func TestTextEditorSelectionFillsRows(t *testing.T) {
	h := newDocumentHarness(t, "a\tb\n\nabc\tb\nabאבcd", false)
	firstY := h.view.Caret.Origin[1]
	h.doc.SetSelection(LinePos{1, 0}, LinePos{1, 0})
	h.frame()
	rowHeight := h.view.Caret.Origin[1] - firstY
	for _, readOnly := range []bool{false, true} {
		h.attrs.ReadOnly = readOnly
		// The final range ends inside the RTL run and has two visual bands.
		h.doc.SetSelection(LinePos{0, 1}, LinePos{3, 3})
		h.frame()
		var rows []float32
		lastBands := 0
		for _, s := range h.out.Surfaces {
			if s.Color1 != CurrentColorScheme.TextInput.Selection || s.Stroke != 0 || s.GlyphRunCount != 0 {
				continue
			}
			if abs32(s.Rect.Size[1]-rowHeight) > .01 {
				t.Fatalf("selection does not fill its row: %+v; row height %g", s.Rect, rowHeight)
			}
			y := s.Rect.Origin[1]
			if !slices.Contains(rows, y) {
				rows = append(rows, y)
			}
			if len(rows) == 4 {
				lastBands++
			}
		}
		if len(rows) != 4 || lastBands != 2 {
			t.Fatalf("selection misses a blank line or bridges bidi bands: rows=%v last bands=%d", rows, lastBands)
		}
		for i := 1; i < len(rows); i++ {
			if abs32(rows[i]-rows[i-1]-rowHeight) > .01 {
				t.Fatalf("gap between selected rows: %v", rows)
			}
		}
	}
}

func TestTextEditorWordMotionDragAndHorizontalScroll(t *testing.T) {
	h := newDocumentHarness(t, "alpha  \n\n   beta\n"+strings.Repeat("long ", 200), false)
	h.doc.SetSelection(LinePos{0, 5}, LinePos{0, 5})
	h.frame()
	h.key(KeyRight, ModAlt)
	if _, p := h.doc.Selection(); p != (LinePos{2, 7}) {
		t.Fatalf("word forward across whitespace: %v", p)
	}
	h.key(KeyLeft, ModAlt)
	h.key(KeyLeft, ModAlt)
	if _, p := h.doc.Selection(); p != (LinePos{}) {
		t.Fatalf("word backward across whitespace: %v", p)
	}
	h.doc.SetSelection(LinePos{1, 0}, LinePos{2, 4})
	h.frame()
	h.key(KeyLeft, 0)
	if a, p := h.doc.Selection(); a != (LinePos{1, 0}) || p != a {
		t.Fatalf("left collapse: %v %v", a, p)
	}
	h.doc.SetSelection(LinePos{0, 1}, LinePos{0, 1})
	h.frame()
	start := Vec2Add(h.view.Caret.Origin, Vec2{1, 4})
	h.doc.SetSelection(LinePos{2, 4}, LinePos{2, 4})
	h.frame()
	end := Vec2Add(h.view.Caret.Origin, Vec2{1, 4})
	GetInputState().MousePoint = start
	h.frame()
	GetFrameInput().Mouse = MouseClick
	h.frame()
	GetInputState().MousePoint = end
	GetFrameInput().Motion = Vec2{10, 30}
	h.frame()
	GetFrameInput().Mouse = MouseRelease
	h.frame()
	if text := h.doc.SelectedText(); text != "lpha  \n\n   b" {
		t.Fatalf("drag selection: %q", text)
	}
	h.key(KeyDown, PrimaryMod())
	if h.view.Scroll[0] < 1000 {
		t.Fatal("long line caret does not scroll horizontally")
	}
	h.key(KeyHome, 0)
	if h.view.Scroll[0] != 0 {
		t.Fatal("Home does not reveal line start")
	}
}

func TestTextEditorTabFocusEscape(t *testing.T) {
	h := newDocumentHarness(t, "body", false)
	scope := new(int)
	var before ContainerId
	h.frame = func() {
		h.out = RunFrameFn(func() {
			ContainerWithKey(scope, Attrs(Viewport), func() {
				before = Container(Attrs(Focusable, FixSize(20, 20)), func() {})
				h.view = TextEditor(&h.doc)
			})
		})
	}
	ClearFocus()
	for range 3 {
		h.frame()
	}
	h.key(KeyTab, 0)
	h.text("x")
	if h.doc.Text() != "\txbody" {
		t.Fatal("plain Tab cycles focus", h.doc.Text())
	}
	h.key(KeyTab, ModShift)
	if !IdHasFocus(before) {
		t.Fatal("Shift+Tab cannot leave the editor")
	}
}

func TestTextEditorScrollbarsKeepBothAxes(t *testing.T) {
	h := newDocumentHarness(t, strings.Repeat(strings.Repeat("long ", 200)+"\n", 300), false)
	click := func(point Vec2) {
		GetInputState().MousePoint = point
		h.frame()
		GetFrameInput().Mouse = MouseClick
		h.frame()
		GetFrameInput().Mouse = MouseRelease
		h.frame()
	}
	click(Vec2{300, 275})
	x := h.view.Scroll[0]
	if x < 100 {
		t.Fatalf("horizontal track does not scroll: %+v", h.view)
	}
	click(Vec2{515, 140})
	if h.view.Scroll[1] < 100 || h.view.Scroll[0] != x {
		t.Fatalf("vertical track resets horizontal position: %+v", h.view)
	}
	if a, c := h.doc.Selection(); a != (LinePos{}) || c != a {
		t.Fatalf("scrollbar changes selection: %v %v", a, c)
	}
}

func BenchmarkTextEditorViewport(b *testing.B) {
	InitFontSubsystem()
	for _, lines := range []int{1000, 100000} {
		b.Run(fmt.Sprint(lines), func(b *testing.B) {
			ResetInputSession()
			GetHost().WindowSize = Vec2{800, 600}
			var doc TextDocument
			doc.SetText(strings.Repeat("Text with 日本語 and an ordinary source line.\n", lines))
			frame := func() { RunFrameFn(func() { TextEditor(&doc) }) }
			for range 3 {
				frame()
			}
			b.ResetTimer()
			b.ReportAllocs()
			for range b.N {
				frame()
			}
		})
	}
}

type documentHarness struct {
	doc   TextDocument
	attrs TextEditorAttrs
	view  TextEditorState
	out   FrameOutputData
	frame func()
}

func newDocumentHarness(t *testing.T, text string, readOnly bool) *documentHarness {
	t.Helper()
	InitFontSubsystem()
	ResetInputSession()
	GetHost().WindowSize = Vec2{520, 280}
	GetHost().WindowFocused = true
	GetInputState().Composition = ""
	GetInputState().Modifiers = 0
	h := &documentHarness{attrs: TextEditorAttrs{LineNumbers: true, ReadOnly: readOnly}}
	h.doc.SetText(text)
	scope := new(int)
	h.frame = func() {
		h.out = RunFrameFn(func() {
			ContainerWithKey(scope, Attrs(Viewport), func() {
				h.view = TextEditorExt(&h.doc, h.attrs)
			})
		})
	}
	for range 3 {
		h.frame()
	}
	return h
}

func (h *documentHarness) key(key KeyCode, mods Modifiers) {
	GetInputState().Modifiers = mods
	GetFrameInput().Key = key
	h.frame()
	GetInputState().Modifiers = 0
	h.frame()
}
func (h *documentHarness) text(s string) { GetFrameInput().Text = s; h.frame(); h.frame() }

func TestTextEditorDocumentNavigationAndEditing(t *testing.T) {
	h := newDocumentHarness(t, "abcdef\nx\nabcdef\n\n日本語🙂\n", false)
	h.doc.SetSelection(LinePos{0, 5}, LinePos{0, 5})
	h.frame()
	h.key(KeyDown, 0)
	h.key(KeyDown, 0)
	if _, p := h.doc.Selection(); p != (LinePos{2, 5}) {
		t.Fatalf("sticky column: %v", p)
	}
	h.key(KeyEnd, 0)
	h.key(KeyRight, 0)
	if _, p := h.doc.Selection(); p != (LinePos{3, 0}) {
		t.Fatalf("empty line: %v", p)
	}
	h.key(KeyEnter, 0)
	h.text("new")
	h.key(KeyZ, PrimaryMod())
	h.key(KeyZ, PrimaryMod())
	if h.doc.Text() != "abcdef\nx\nabcdef\n\n日本語🙂\n" {
		t.Fatal("undo sequence", h.doc.Text())
	}
	h.key(KeyDown, PrimaryMod())
	h.text("tail")
	if h.doc.Line(5) != "tail" {
		t.Fatal("trailing line is not editable")
	}
	h.key(KeyTab, 0)
	h.text("indented")
	if h.doc.Line(5) != "tail\tindented" {
		t.Fatal("tab leaves editor", h.doc.Line(5))
	}
	h.doc.SetSelection(LinePos{1, 0}, LinePos{4, 3})
	h.frame()
	h.key(KeyC, PrimaryMod())
	// Capture the copy request on the actual input frame, before its settle.
	GetInputState().Modifiers = PrimaryMod()
	GetFrameInput().Key = KeyC
	h.frame()
	if h.out.Copy != "x\nabcdef\n\n日本語" {
		t.Fatalf("copy: %q", h.out.Copy)
	}
	GetInputState().Modifiers = 0
	h.text("replacement\nsecond")
	if h.doc.Text() != "abcdef\nreplacement\nsecond🙂\ntail\tindented" {
		t.Fatal(h.doc.Text())
	}
}

func TestTextEditorVirtualizationRevealAndWheel(t *testing.T) {
	h := newDocumentHarness(t, strings.Repeat("one two 日本語 three\n", 10000), false)
	if h.view.LastVisible-h.view.FirstVisible > 30 {
		t.Fatal("view builds offscreen lines")
	}
	h.key(KeyPageDown, 0)
	if _, p := h.doc.Selection(); p.Line < 5 {
		t.Fatalf("Page Down does not advance a page: %v", p)
	}
	h.key(KeyPageUp, ModShift)
	if a, p := h.doc.Selection(); a == p {
		t.Fatal("Shift+Page Up does not extend selection")
	}
	h.key(KeyDown, PrimaryMod())
	if h.view.LastVisible != 10000 || h.view.Scroll[1] < 100000 {
		t.Fatalf("document end not visible: %+v", h.view)
	}
	h.text("last")
	if h.doc.Line(10000) != "last" {
		t.Fatal("cannot edit document end")
	}
	GetInputState().MousePoint = Vec2{250, 120}
	h.frame()
	GetFrameInput().Scroll = Vec2{0, -200}
	h.frame()
	after := h.view.Scroll[1]
	for range 3 {
		h.frame()
	}
	if h.view.Scroll[1] != after {
		t.Fatal("wheel snaps back to caret")
	}
	h.key(KeyLeft, 0)
	if h.view.Scroll[1] <= after {
		t.Fatal("motion does not reveal offscreen caret")
	}
	h.key(KeyA, PrimaryMod())
	h.text("small")
	if h.doc.Text() != "small" || h.view.Scroll[1] != 0 {
		t.Fatalf("shrink leaves stale scroll: %+v", h.view)
	}
	h.key(KeyZ, PrimaryMod())
	if h.doc.LineCount() != 10001 {
		t.Fatal("large selection undo")
	}
}

func TestTextEditorCompositionAndClusterDeletion(t *testing.T) {
	h := newDocumentHarness(t, "hello\n", false)
	h.doc.SetSelection(LinePos{1, 0}, LinePos{1, 0})
	h.frame()
	GetInputState().Composition = "にほんご"
	GetInputState().CompositionSel = [2]int{4, 4}
	h.frame()
	if h.doc.Modified() || h.doc.Text() != "hello\n" {
		t.Fatal("preedit enters document")
	}
	if GetHost().CompositionPos[1] <= 0 {
		t.Fatal("missing IME candidate anchor")
	}
	GetInputState().Composition = ""
	h.text("日本語")
	h.key(KeyZ, PrimaryMod())
	if h.doc.Text() != "hello\n" {
		t.Fatal("IME undo")
	}
	h.text("a\u0301")
	h.key(KeyDeleteBackward, 0)
	if h.doc.Text() != "hello\n" {
		t.Fatal("backspace splits combining cluster", h.doc.Text())
	}
	h.doc.SetSelection(LinePos{0, 0}, LinePos{0, 5})
	h.frame()
	GetInputState().Composition = "か"
	h.frame()
	GetInputState().Composition = ""
	h.frame()
	if h.doc.Text() != "hello\n" {
		t.Fatal("cancelled composition destroys selection")
	}
}

func TestTextEditorReadOnlyAndMouseSelection(t *testing.T) {
	h := newDocumentHarness(t, "first line\nsecond line\nthird", false)
	h.doc.SetSelection(LinePos{1, 3}, LinePos{1, 3})
	h.frame()
	point := Vec2Add(h.view.Caret.Origin, Vec2{1, 4})
	h.doc.SetSelection(LinePos{}, LinePos{})
	h.frame()
	GetInputState().MousePoint = point
	h.frame()
	GetFrameInput().Mouse = MouseClick
	h.frame()
	GetFrameInput().Mouse = MouseRelease
	h.frame()
	if _, p := h.doc.Selection(); p != (LinePos{1, 3}) {
		t.Fatalf("click position: %v at %v", p, point)
	}
	h.text("X")
	if h.doc.Line(1) != "secXond line" {
		t.Fatal("clicked insertion", h.doc.Text())
	}
	h = newDocumentHarness(t, "read only", true)
	h.key(KeyA, PrimaryMod())
	h.text("replacement")
	h.key(KeyDeleteBackward, 0)
	if h.doc.Text() != "read only" || h.doc.Modified() {
		t.Fatal("read-only mutation")
	}
}

func TestTextEditorReadOnlyTransitionPreservesDocument(t *testing.T) {
	h := newDocumentHarness(t, "original 日本語\nsecond line", false)
	h.text("draft ")
	want := h.doc.Text()
	h.key(KeyA, PrimaryMod())
	anchor, cursor := h.doc.Selection()
	GetInputState().Composition = "か"
	h.frame()
	h.attrs.ReadOnly = true
	h.frame()
	GetInputState().Composition = ""
	h.text("committed composition")
	for _, key := range []KeyCode{KeyDeleteBackward, KeyDeleteForward, KeyEnter, KeyTab} {
		h.key(key, 0)
	}
	for _, key := range []KeyCode{KeyX, KeyV, KeyZ} {
		h.key(key, PrimaryMod())
	}
	h.key(KeyZ, PrimaryMod()|ModShift)
	if h.doc.Text() != want || !h.doc.Modified() {
		t.Fatal("locking changes document or dirty state", h.doc.Text())
	}
	if a, c := h.doc.Selection(); a != anchor || c != cursor {
		t.Fatal("locking loses selection", a, c)
	}
	GetInputState().Modifiers = PrimaryMod()
	GetFrameInput().Key = KeyC
	h.frame()
	if h.out.Copy != want {
		t.Fatal("locked selection cannot be copied", h.out.Copy)
	}
	GetInputState().Modifiers = 0
	h.key(KeyLeft, 0)
	if _, c := h.doc.Selection(); c != (LinePos{}) {
		t.Fatal("locked view cannot navigate", c)
	}
	h.attrs.ReadOnly = false
	h.frame()
	h.key(KeyZ, PrimaryMod())
	if h.doc.Text() != "original 日本語\nsecond line" || h.doc.Modified() {
		t.Fatal("unlocking loses undo or saved state", h.doc.Text())
	}
	h.key(KeyZ, PrimaryMod()|ModShift)
	if h.doc.Text() != want {
		t.Fatal("unlocking loses redo")
	}
}

func TestTextEditorGutterPointerAndReveal(t *testing.T) {
	h := newDocumentHarness(t, strings.Repeat("\t日本語 alpha\n", 1000), true)
	var gutter Rect
	clicked := -1
	h.attrs.GutterWidth = 24
	h.attrs.GutterView = func(line int) {
		if line == 0 {
			gutter = GetScreenRect()
		}
		if IsClicked() {
			clicked = line
		}
		Label("•")
	}
	h.attrs.LineBackground = func(line int) Vec4 { return Vec4{50, 60, 90, 1} }
	h.attrs.LineSpans = func(line int, base TextStyleAttrs) []StyleSpan {
		return []StyleSpan{ResolveSpan(1, 4, base, TextUnderline(true))}
	}
	h.doc.SetSelection(LinePos{0, 2}, LinePos{0, 3})
	for range 3 {
		h.frame()
	}
	click := func(point Vec2) {
		GetInputState().MousePoint = point
		h.frame()
		GetFrameInput().Mouse = MouseClick
		h.frame()
		GetFrameInput().Mouse = MouseRelease
		h.frame()
	}
	click(Vec2Add(gutter.Origin, Vec2{8, 8}))
	if a, c := h.doc.Selection(); clicked != 0 || a != (LinePos{0, 2}) || c != (LinePos{0, 3}) {
		t.Fatalf("gutter input changes selection: click=%d, %v %v", clicked, a, c)
	}
	var pointer LinePos
	h.attrs.OnPointer = func(pos LinePos) bool { pointer = pos; return true }
	click(Vec2Add(h.view.Caret.Origin, Vec2{1, 4}))
	if a, c := h.doc.Selection(); pointer != c || a != (LinePos{0, 2}) || c != (LinePos{0, 3}) {
		t.Fatalf("pointer geometry or consumed click: pointer=%v, %v %v", pointer, a, c)
	}
	TextEditorScrollToLine(&h.doc, 600, .4)
	h.frame()
	h.frame()
	if h.view.FirstVisible >= 600 || h.view.LastVisible <= 600 {
		t.Fatal("requested line is not in view", h.view)
	}
	if a, c := h.doc.Selection(); a != (LinePos{0, 2}) || c != (LinePos{0, 3}) {
		t.Fatal("scroll request moves selection", a, c)
	}
	TextEditorScrollToLine(&h.doc, 600, -1)
	before := h.view.Scroll
	h.frame()
	if before != h.view.Scroll {
		t.Fatal("revealing a visible line moves viewport")
	}
	TextEditorScrollToLine(&h.doc, 300, 0)
	h.frame()
	if h.view.FirstVisible != 300 {
		t.Fatal("top alignment does not restore scroll", h.view)
	}
}
