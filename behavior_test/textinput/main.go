// Behavior test: TextInput editing over many frames with synthetic input.
//
// Headless regression suite — not in the normal `go test` suite. Exercises
// the same backend contract as widgets/textinput_test.go (set GetFrameInput()
// before RunFrameFn) but as long scripts with a single PASS/FAIL summary.
//
//	go run ./behavior_test/textinput
//	go run ./behavior_test/textinput -v
//	go run ./behavior_test/textinput --window --drive --close
//	go run ./behavior_test/textinput --window
//
// Cases:
//
//  1. type-arrows — typing grows the buffer; arrow keys move without inserting
//  2. cut — selection cut updates buffer and clipboard copy request
//  3. undo-redo — coalesced typing undoes/redoes across idle frames
//  4. scroll-caret — End/Home keep the caret inside a narrow field
//  5. backspace-max-scroll — delete at end while scrolled; caret stays with text
//  6. composition-bidi-underline — JP preedit before Arabic does not underline the RTL run
//
// Public observables only (buffer, CaretPos, FrameOutputData surfaces /
// Copy) — no access to widgets-internal caret state.

package main

import (
	"flag"
	"fmt"
	"os"
	"runtime"
	"strings"

	"go.hasen.dev/shirei/app"
	"go.hasen.dev/shirei/behavior_test/btmode"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

type f32 = float32

const (
	winW, winH f32 = 420, 420
	// Single-line field width used by both the on-screen UI and caret asserts.
	// Host.CaretPos is screen-absolute; asserts compare it to the field rect.
	fieldMinW f32 = 320
	longText      = "the quick brown fox jumps over the lazy dog " +
		"the quick brown fox jumps over the lazy dog"
)

const windowHoldFrames = 42

type caseStatusKind int

const (
	casePending caseStatusKind = iota
	caseRunning
	casePass
	caseFail
)

type caseRow struct {
	name   string
	status caseStatusKind
	detail string // fail message; empty otherwise
}

var (
	verbose       bool
	mode          *btmode.Mode
	verdictDone   bool
	verdictOK     bool
	verdictDetail string
	playgroundBuf string
	caseStatus    string

	// Window+drive: suite goroutine syncs each harness frame with app.Run.
	// Synthetic input is staged in pendingInject and applied on the UI thread
	// when the synced frame starts — never mutate Host input from the suite
	// goroutine (races FrameInput reset; ResetInputSession clears focus).
	live          *harness
	frameReq      chan struct{}
	frameAck      chan struct{}
	savedCopy     string
	savedFieldRect Rect
	liveWindow    bool
	pendingInject inject

	suiteCases = []struct {
		name string
		fn   func() error
	}{
		{"type-arrows", caseTypeAndArrows},
		{"cut", caseCut},
		{"undo-redo", caseUndoRedo},
		{"scroll-caret", caseScrollFollowsCaret},
		{"backspace-max-scroll", caseBackspaceAtMaxScroll},
		{"composition-bidi-underline", caseCompositionBidiUnderline},
	}
	caseRows []caseRow
)

// inject is one frame's worth of synthetic input, applied on the UI thread.
type inject struct {
	text           string
	key            KeyCode
	mods           Modifiers
	mousePoint     Vec2
	setMousePoint  bool
	composition    string
	compositionSel [2]int
	setComposition bool
	resetSession   bool
}

func main() {
	mode = btmode.RegisterFlags(nil)
	flag.BoolVar(&verbose, "v", false, "verbose per-case detail")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: go run ./behavior_test/textinput [flags]\n\n%s  -v         verbose per-case detail\n", btmode.FlagHelp())
	}
	flag.Parse()
	mode.AfterParse()
	if err := mode.Validate(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	// Fonts scan on package init; probe for a usable face before asserting text.
	probe := ShapeText("alpha", DefaultTextStyle())
	if len(probe.Lines) != 1 || len(probe.Lines[0].Segments) == 0 {
		fmt.Println("=== behavior_test: textinput ===")
		fmt.Println("SKIP: no usable system fonts for text shaping")
		os.Exit(0)
	}

	fmt.Println("=== behavior_test: textinput ===")
	initCaseRows()

	if !mode.Window {
		failed, detail := runSuite()
		if failed > 0 {
			fmt.Printf("RESULT: %s\n", detail)
			os.Exit(1)
		}
		fmt.Println("RESULT: all cases passed")
		os.Exit(0)
	}

	// Window first — drive runs in a goroutine so typing is visible.
	playgroundBuf = "type here…"
	if mode.Drive {
		liveWindow = true
		frameReq = make(chan struct{})
		frameAck = make(chan struct{})
		go func() {
			failed, detail := runSuite()
			verdictDone = true
			verdictOK = failed == 0
			verdictDetail = detail
			if failed == 0 {
				caseStatus = "all cases passed"
			} else {
				caseStatus = detail
			}
		}()
	} else {
		caseStatus = "manual playground — edit the field below"
	}
	app.SetupWindow("behavior_test: textinput", int(winW), int(winH))
	app.Run(windowFn)
}

func initCaseRows() {
	caseRows = make([]caseRow, len(suiteCases))
	for i, c := range suiteCases {
		caseRows[i] = caseRow{name: c.name, status: casePending}
	}
}

func runSuite() (failed int, detail string) {
	for i, c := range suiteCases {
		caseStatus = "case: " + c.name
		caseRows[i].status = caseRunning
		if err := c.fn(); err != nil {
			caseRows[i].status = caseFail
			caseRows[i].detail = err.Error()
			fmt.Printf("FAIL %s: %v\n", c.name, err)
			failed++
		} else {
			caseRows[i].status = casePass
			caseRows[i].detail = ""
			fmt.Printf("PASS %s\n", c.name)
		}
		pauseVisible()
	}
	if failed > 0 {
		return failed, fmt.Sprintf("%d case(s) failed", failed)
	}
	return 0, "all cases passed"
}

func pauseVisible() {
	if !liveWindow {
		return
	}
	// Hold on the last harness buffer so the window can show the result.
	if live == nil {
		return
	}
	for range windowHoldFrames {
		live.idle()
	}
}

func applyInject(inj inject) {
	if inj.resetSession {
		ResetInputSession()
		// Backends own WindowSize under --window; forcing it fights the view
		// and can stretch the presented frame.
		if !liveWindow {
			GetHost().WindowSize = Vec2{winW, winH}
		}
	}
	if inj.setMousePoint {
		GetInputState().MousePoint = inj.mousePoint
	}
	if inj.setComposition {
		GetInputState().Composition = inj.composition
		GetInputState().CompositionSel = inj.compositionSel
	}
	GetInputState().Modifiers = inj.mods
	GetFrameInput().Text = inj.text
	GetFrameInput().Key = inj.key
	GetFrameInput().Mouse = 0
}

func windowFn() {
	requested := false
	if liveWindow && !verdictDone {
		select {
		case <-frameReq:
			requested = true
			applyInject(pendingInject)
			pendingInject = inject{}
		default:
		}
		RequestNextFrame()
	}

	buf := &playgroundBuf
	multi := false
	if live != nil {
		buf = &live.buf
		multi = live.multi
	}

	ModAttrs(func(a *AttrSet) { a.Animations = 0 })
	Container(Attrs(Viewport, Pad(12), Gap(8), Background(220, 25, 96, 1)), func() {
		Label("behavior_test: textinput", FontWeight(WeightBold), FontSize(16))
		Label(caseStatus, FontSize(12), TextColor(0, 0, 40, 1))
		if live != nil {
			Label(fmt.Sprintf("buf=%q", *buf), FontSize(12), TextColor(0, 0, 45, 1))
		}
		attrs := singleLineFieldAttrs()
		if multi {
			attrs = multilineFieldAttrs()
		}
		// Fresh key per harness so AutoFocus (FirstRender-only) runs again after
		// ResetInputSession. A stable call-site identity is FirstRender once for
		// the playground field, then never re-autofocuses — typing lands nowhere.
		fieldKey := any("playground")
		if live != nil {
			fieldKey = live.scope
		}
		ContainerWithKey(fieldKey, Attrs(), func() {
			TextInputExt(buf, attrs)
			if id := GetLastId(); id != nil {
				savedFieldRect = GetScreenRectOf(id)
			}
		})

		Label("Cases", FontSize(12), FontWeight(WeightBold), TextColor(0, 0, 35, 1))
		caseChecklist()
	})

	savedCopy = GetHost().Copy
	if live != nil {
		live.caretPos = GetHost().CaretPos
		live.fieldRect = savedFieldRect
	}

	if requested {
		frameAck <- struct{}{}
	}

	if mode.Drive {
		btmode.VerdictBanner(verdictDone, verdictOK, verdictDetail)
		mode.TickClose(verdictDone, verdictOK)
		if verdictDone && !mode.Close {
			RequestNextFrame()
		}
	}
}

func caseChecklist() {
	Container(Attrs(Gap(2)), func() {
		for _, row := range caseRows {
			mark := "·"
			markColor := Vec4{0, 0, 55, 1}
			nameColor := Vec4{0, 0, 30, 1}
			switch row.status {
			case caseRunning:
				mark = "…"
				markColor = Vec4{210, 60, 45, 1}
				nameColor = Vec4{0, 0, 15, 1}
			case casePass:
				mark = "✓"
				markColor = Vec4{140, 65, 35, 1}
			case caseFail:
				mark = "✗"
				markColor = Vec4{8, 75, 45, 1}
				nameColor = Vec4{8, 70, 35, 1}
			}
			Container(Attrs(Row, Gap(8), CrossMid), func() {
				Label(mark, FontSize(14), FontWeight(WeightBold), TextColorVec(markColor))
				Label(row.name, FontSize(12), TextColorVec(nameColor))
			})
			if row.status == caseFail && row.detail != "" {
				Label("    "+row.detail, FontSize(10), TextColor(8, 55, 40, 1))
			}
		}
	})
}

func singleLineFieldAttrs() TextInputAttrs {
	attrs := DefaultTextInputAttrs()
	attrs.FixedWidth = true
	attrs.MinWidth = fieldMinW
	return attrs
}

func multilineFieldAttrs() TextInputAttrs {
	attrs := DefaultTextInputAttrs()
	attrs.MaxLines = 0
	attrs.Rows = 3
	attrs.MinWidth = 220
	attrs.FixedWidth = true
	return attrs
}

// ── harness ───────────────────────────────────────────────────────────────

type harness struct {
	buf       string
	out       FrameOutputData
	caretPos  Vec2
	fieldRect Rect
	frame     func()
	multi     bool
	scope     *int // per-harness key so AutoFocus FirstRender fires again
}

func newSingleLine(initial string) *harness {
	return newHarness(initial, false)
}

func newHarness(initial string, multiline bool) *harness {
	h := &harness{buf: initial, multi: multiline, scope: new(int)}
	h.frame = func() {
		if liveWindow {
			live = h
			frameReq <- struct{}{}
			<-frameAck
			h.out = FrameOutputData{Copy: savedCopy}
			h.fieldRect = savedFieldRect
			return
		}
		applyInject(pendingInject)
		pendingInject = inject{}
		h.out = RunFrameFn(func() {
			ModAttrs(func(a *AttrSet) { a.Animations = 0 })
			ContainerWithKey(h.scope, Attrs(Viewport, Pad(8)), func() {
				attrs := singleLineFieldAttrs()
				if multiline {
					attrs = multilineFieldAttrs()
				}
				TextInputExt(&h.buf, attrs)
				if id := GetLastId(); id != nil {
					h.fieldRect = GetScreenRectOf(id)
				}
			})
		})
		h.caretPos = GetHost().CaretPos
	}

	// Fresh input session + three settle frames (AutoFocus lands).
	pendingInject = inject{resetSession: true}
	h.frame()
	h.frame()
	h.frame()
	return h
}

func (h *harness) step(inj inject) {
	pendingInject = inj
	h.frame()
}

func (h *harness) idle() {
	h.step(inject{})
}

func (h *harness) typeText(s string) {
	for _, r := range s {
		h.step(inject{text: string(r)})
		h.idle()
	}
}

func (h *harness) pressKey(k KeyCode, mods Modifiers) {
	h.step(inject{key: k, mods: mods})
	keyOut := h.out
	h.idle()
	h.out = keyOut
}

func (h *harness) pressKeySettle(k KeyCode) {
	h.step(inject{key: k})
	h.idle()
	h.idle()
}

func (h *harness) setMousePoint(p Vec2) {
	h.step(inject{mousePoint: p, setMousePoint: true})
}

func (h *harness) setComposition(text string, sel [2]int) {
	h.step(inject{
		composition:    text,
		compositionSel: sel,
		setComposition: true,
	})
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

func caseCut() error {
	h := newSingleLine("")
	h.typeText("hello world")
	mod := primaryMod()

	h.pressKey(KeyX, mod)
	if h.buf != "hello world" {
		return fmt.Errorf("cut no-sel modified buf: %q", h.buf)
	}
	if h.out.Copy != "" {
		return fmt.Errorf("cut no-sel copied %q", h.out.Copy)
	}

	h.pressKey(KeyA, mod)
	h.pressKey(KeyX, mod)
	if h.buf != "" {
		return fmt.Errorf("cut all: buf=%q want empty", h.buf)
	}
	if h.out.Copy != "hello world" {
		return fmt.Errorf("cut all: copy=%q want %q", h.out.Copy, "hello world")
	}
	logf("cut all → copy=%q", h.out.Copy)

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

func caseUndoRedo() error {
	h := newSingleLine("")
	mod := primaryMod()

	h.typeText("abc")
	if h.buf != "abc" {
		return fmt.Errorf("setup: buf=%q", h.buf)
	}
	h.pressKey(KeyZ, mod)
	if h.buf != "" {
		return fmt.Errorf("undo: buf=%q want empty", h.buf)
	}
	h.pressKey(KeyZ, mod|ModShift)
	if h.buf != "abc" {
		return fmt.Errorf("redo: buf=%q want %q", h.buf, "abc")
	}
	logf("undo/redo round-trip ok")

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

// caretInFieldX is Host.CaretPos.x relative to the field's screen origin.
func (h *harness) caretInFieldX() f32 {
	return h.caretPos[0] - h.fieldRect.Origin[0]
}

func caseScrollFollowsCaret() error {
	h := newSingleLine(longText)
	h.setMousePoint(Vec2{winW - 10, winH - 10})

	h.pressKeySettle(KeyEnd)
	boxW := h.fieldRect.Size[0]
	if boxW < 1 {
		return fmt.Errorf("End: field rect not laid out")
	}
	x := h.caretInFieldX()
	if x > boxW+2 {
		return fmt.Errorf("End: caret x=%.1f outside field width≈%.0f (screen=%.1f field@%.1f)",
			x, boxW, h.caretPos[0], h.fieldRect.Origin[0])
	}
	if x < boxW/2 {
		return fmt.Errorf("End: caret x=%.1f expected near right edge of ≈%.0f field", x, boxW)
	}
	logf("End: caret-in-field x=%.1f boxW=%.0f", x, boxW)

	h.pressKeySettle(KeyHome)
	x = h.caretInFieldX()
	if x > 40 {
		return fmt.Errorf("Home: caret x=%.1f expected near left edge of field", x)
	}
	logf("Home: caret-in-field x=%.1f", x)
	return nil
}

func caseBackspaceAtMaxScroll() error {
	h := newSingleLine(longText)
	h.setMousePoint(Vec2{winW - 10, winH - 10})

	h.pressKeySettle(KeyEnd)
	boxW := h.fieldRect.Size[0]
	endX := h.caretInFieldX()
	if boxW < 1 {
		return fmt.Errorf("setup End: field rect not laid out")
	}
	if endX < boxW/2 {
		return fmt.Errorf("setup End: caret x=%.1f not near right edge of ≈%.0f field", endX, boxW)
	}
	before := len([]rune(h.buf))
	for range 4 {
		h.pressKeySettle(KeyDeleteBackward)
	}
	if got := len([]rune(h.buf)); got != before-4 {
		return fmt.Errorf("backspace deleted %d runes, want 4 (buf=%q)", before-got, h.buf)
	}
	if h.caretInFieldX() < endX-6 {
		return fmt.Errorf("caret drifted left after backspace: x=%.1f was %.1f", h.caretInFieldX(), endX)
	}
	if !strings.HasPrefix(longText, h.buf[:min(20, len(h.buf))]) && !strings.Contains(longText, h.buf[len(h.buf)-10:]) {
		logf("buf after backspace: %q", h.buf)
	}
	logf("backspace@maxScroll: caret x=%.1f (end was %.1f) bufLen=%d", h.caretInFieldX(), endX, len([]rune(h.buf)))
	return nil
}

func caseCompositionBidiUnderline() error {
	probe := ShapeText("にع", DefaultTextStyle())
	if len(probe.Runes) < 2 || len(probe.Lines) == 0 {
		return nil
	}
	var hasJP, hasAR bool
	for _, seg := range probe.Lines[0].Segments {
		for _, g := range seg.Glyphs {
			if g.XAdvance > 0 {
				if int(g.Cluster) < len(probe.Runes) {
					r := probe.Runes[g.Cluster]
					if r == 'に' {
						hasJP = true
					}
					if r == 'ع' {
						hasAR = true
					}
				}
			}
		}
	}
	if !hasJP || !hasAR {
		logf("skip composition-bidi-underline: missing JP or Arabic glyphs")
		return nil
	}

	const buf = "heyعربيworld"
	const composition = "にほ"
	h := newSingleLine(buf)
	for range 3 {
		h.pressKey(KeyRight, 0)
	}

	display := "hey" + composition + "عربيworld"
	attrs := DefaultTextStyle()
	attrs.FontSize = DefaultTextInputAttrs().FontSize
	shaped := ShapeText(display, attrs)
	var jpW float32
	for _, line := range shaped.Lines {
		for _, seg := range line.Segments {
			for _, g := range seg.Glyphs {
				c := int(g.Cluster)
				if c >= 3 && c < 5 {
					jpW += g.XAdvance
				}
			}
		}
	}
	if jpW < 1 {
		return fmt.Errorf("could not measure JP composition advances")
	}

	h.setComposition(composition, [2]int{2, 2})
	h.idle()
	if h.buf != buf {
		return fmt.Errorf("composition mutated buffer: %q", h.buf)
	}
	// Capture surfaces while composition is still active.
	compOut := h.out

	if liveWindow {
		h.setComposition("", [2]int{})
		// Surfaces are only returned from RunFrameFn; window drive cannot read
		// them. Buffer/composition stability above still ran live on screen.
		logf("composition-bidi-underline: skip surface width assert in window drive (jpW=%.1f)", jpW)
		return nil
	}

	var underW float32
	var n int
	for _, s := range compOut.Surfaces {
		if s.Stroke == 0 &&
			s.Color1 == (Vec4{0, 0, 30, 1}) &&
			abs32(s.Rect.Size[1]-1) < 0.1 &&
			s.Rect.Size[0] > 0 {
			underW += s.Rect.Size[0]
			n++
		}
	}

	h.setComposition("", [2]int{})

	if n < 1 {
		return fmt.Errorf("no composition underline surfaces")
	}
	if underW > jpW+4 {
		return fmt.Errorf("underline width %.1f exceeds JP width %.1f (Arabic likely included)", underW, jpW)
	}
	logf("composition-bidi-underline: underW=%.1f jpW=%.1f surfaces=%d", underW, jpW, n)
	return nil
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}
