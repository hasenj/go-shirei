package widgets

// Multi-frame semantic tests pinning the identity contract: container
// identity is inherently cross-frame behavior — scroll offsets, hook state,
// and focus must survive re-renders, and view toggles must not destroy the
// hidden view's state. A single-frame render can't see any of this.

import (
	"testing"

	"go.hasen.dev/shirei"

	. "go.hasen.dev/shirei"
)

type semFrameInput struct {
	mouse  Vec2
	action MouseAction
	scroll Vec2
	text   string
}

// runSemFrame runs one frame with the given input. Every GetFrameInput() field
// is assigned every frame (they're globals — stale values would leak).
func runSemFrame(scope any, in semFrameInput, fn func()) {
	shirei.GetHost().WindowSize = Vec2{600, 400}
	shirei.GetInputState().MousePoint = in.mouse
	shirei.GetFrameInput().Mouse = in.action
	shirei.GetFrameInput().Scroll = in.scroll
	shirei.GetFrameInput().Motion = Vec2{}
	shirei.GetFrameInput().Key = 0
	shirei.GetFrameInput().Text = in.text
	shirei.RunFrameFn(func() {
		shirei.ModAttrs(func(a *shirei.AttrSet) { a.Animations = 0 })
		shirei.ContainerWithKey(scope, Attrs(Viewport), fn)
	})
}

var offscreen = Vec2{-1000, -1000}

// TestScrollOffsetPersists: scrolling a container moves its content, and
// the offset must hold on subsequent frames with no input — previous-frame
// state lookups are exactly what unstable identity silently breaks.
func TestScrollOffsetPersists(t *testing.T) {
	initFontsOnce.Do(shirei.InitFontSubsystem)

	scope := new(int)
	items := make([]*int, 30) // pointers double as stable row ids
	for i := range items {
		items[i] = new(int)
	}
	probe := items[10]
	var probeId shirei.ContainerId
	view := func() {
		Container(Attrs(Viewport), func() {
			ScrollOnInput()
			for _, it := range items {
				id := shirei.ContainerWithKey(it, Attrs(FixHeight(20), Expand), func() {})
				if it == probe {
					probeId = id
				}
			}
		})
	}

	// GetResolvedRectOf, not GetScreenRectOf: screenRect is post-clipping,
	// so a row scrolled out of view clips to a degenerate rect and can't be
	// compared. Watch a mid-list row that stays visible either way.
	rowY := func() float32 { return shirei.GetResolvedRectOf(probeId).Origin[1] }

	inside := Vec2{300, 200}
	for range 3 {
		runSemFrame(scope, semFrameInput{mouse: offscreen}, view)
	}
	y0 := rowY()

	runSemFrame(scope, semFrameInput{mouse: inside, scroll: Vec2{0, 100}}, view)
	runSemFrame(scope, semFrameInput{mouse: offscreen}, view)
	y1 := rowY()
	if y1 != y0-100 {
		t.Fatalf("scroll(+100) should move row from y=%v to y=%v, got %v", y0, y0-100, y1)
	}

	// no further input: the offset must hold
	for range 3 {
		runSemFrame(scope, semFrameInput{mouse: offscreen}, view)
	}
	y2 := rowY()
	if y2 != y1 {
		t.Errorf("scroll offset drifted with no input: row y %v -> %v", y1, y2)
	}
}

type semRow struct {
	Name string
	Val  int
}

// TestViewToggleResetsTableSort pins TODAY's retention semantics: UI hook
// state is garbage-collected every frame it isn't used (hooks.go's
// hooksMap/hooksMapNext double buffer), so click-sorting a table, hiding
// it behind another view, and coming back finds the sort RESET to the
// default. (In see_pprof: entering and exiting the peek view resets the
// stats table's sort.) If the identity tree's retention policy ever moves
// to keep-alive state, this test should flip to asserting preservation.
func TestViewToggleResetsTableSort(t *testing.T) {
	initFontsOnce.Do(shirei.InitFontSubsystem)

	scope := new(int)
	rows := []*semRow{
		{"zeta", 9},
		{"alpha", 1},
		{"mid", 5},
	}
	rowIds := map[*semRow]shirei.ContainerId{}
	cols := []TableColumn[*semRow]{
		{
			Label:  "Name",
			Render: func(r *semRow) { rowIds[r] = shirei.CurrentId(); Label(r.Name) },
			Less:   func(a, b *semRow) bool { return a.Name < b.Name },
		},
		{
			Label: "Val", Width: 80, DefaultDesc: true,
			Render: func(r *semRow) { Label("v") },
			Less:   func(a, b *semRow) bool { return a.Val < b.Val },
		},
	}

	showTable := true
	view := func() {
		if showTable {
			Table(nil, 24, cols, rows, func(r *semRow) any { return r }, 1)
		} else {
			Container(Attrs(Viewport), func() {
				Label("other view")
			})
		}
	}

	rowY := func(r *semRow) float32 { return shirei.GetScreenRectOf(rowIds[r]).Origin[1] }

	for range 3 {
		runSemFrame(scope, semFrameInput{mouse: offscreen}, view)
	}
	// default sort: Val descending -> zeta(9) on top
	if !(rowY(rows[0]) < rowY(rows[1])) {
		t.Fatalf("expected default Val-desc sort (zeta above alpha); got zeta=%v alpha=%v",
			rowY(rows[0]), rowY(rows[1]))
	}

	// click the Name header -> ascending by name -> alpha on top
	header := Vec2{60, 15}
	runSemFrame(scope, semFrameInput{mouse: header, action: MouseClick}, view)
	runSemFrame(scope, semFrameInput{mouse: header, action: MouseRelease}, view)
	runSemFrame(scope, semFrameInput{mouse: offscreen}, view)
	if !(rowY(rows[1]) < rowY(rows[0])) {
		t.Fatalf("clicking Name header did not re-sort (alpha=%v zeta=%v)",
			rowY(rows[1]), rowY(rows[0]))
	}

	// hide the table behind another view, then come back: the sort hook is
	// GC'd while unused, so the table is back to its default sort
	showTable = false
	for range 3 {
		runSemFrame(scope, semFrameInput{mouse: offscreen}, view)
	}
	showTable = true
	for range 3 {
		runSemFrame(scope, semFrameInput{mouse: offscreen}, view)
	}
	if !(rowY(rows[0]) < rowY(rows[1])) {
		t.Errorf("expected sort to reset to default across view toggle (today's per-frame hook GC); zeta=%v alpha=%v",
			rowY(rows[0]), rowY(rows[1]))
	}
}

// TestTypingAccumulates: focus a text input by clicking it, then deliver
// text over several frames — the buffer and the focus must both persist
// frame to frame.
func TestTypingAccumulates(t *testing.T) {
	initFontsOnce.Do(shirei.InitFontSubsystem)

	scope := new(int)
	buf := ""
	view := func() {
		Container(Attrs(Pad(20)), func() {
			TextInput(&buf)
		})
	}

	for range 3 {
		runSemFrame(scope, semFrameInput{mouse: offscreen}, view)
	}
	inside := Vec2{80, 40}
	runSemFrame(scope, semFrameInput{mouse: inside, action: MouseClick}, view)
	runSemFrame(scope, semFrameInput{mouse: inside, action: MouseRelease}, view)

	runSemFrame(scope, semFrameInput{mouse: offscreen, text: "ab"}, view)
	runSemFrame(scope, semFrameInput{mouse: offscreen, text: "c"}, view)
	runSemFrame(scope, semFrameInput{mouse: offscreen}, view)

	if buf != "abc" {
		t.Errorf("typed text did not accumulate: buf = %q, want %q", buf, "abc")
	}
}

// TestTypeScopedOrdinalIdentity: under the identity tree's (component
// type, per-type ordinal) reconciliation, inserting a sibling of a
// DIFFERENT type in front of a run of anonymous same-type siblings must
// not shift their hook state. (Was skipped before stage 3 — the old flat
// auto-ordinals shifted everything after the insertion.)
func TestTypeScopedOrdinalIdentity(t *testing.T) {
	scope := new(int)
	got := make([]int, 4)
	view := func(withDivider bool) func() {
		return func() {
			if withDivider {
				Container(Attrs(FixHeight(10), Expand), func() {})
			}
			for i := 0; i < 4; i++ {
				Container(Attrs(FixHeight(20), Expand), func() {
					slot := shirei.Use[int]("slot")
					if shirei.FirstRender() {
						*slot = i + 100
					}
					got[i] = *slot
				})
			}
		}
	}

	for range 3 {
		runSemFrame(scope, semFrameInput{mouse: offscreen}, view(false))
	}
	for range 2 {
		runSemFrame(scope, semFrameInput{mouse: offscreen}, view(true))
	}
	for i := range got {
		if got[i] != i+100 {
			t.Errorf("item %d lost its state after different-type insertion: slot=%d, want %d",
				i, got[i], i+100)
		}
	}
}
