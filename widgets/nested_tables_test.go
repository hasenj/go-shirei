package widgets

// Regression test born from a see_pprof bug (2026-07-02): Tables whose id —
// or whose row ids — are strings render headers but never any rows.
// addChildScope hashes the raw interface bytes of an id, and a string boxed
// into `any` carries a fresh data pointer every frame, so every derived
// scope id changes each frame and previous-frame lookups (GetResolvedSize,
// scroll, hover) miss forever; VirtualListView never learns its size and
// bails before rendering a row. See also the scope comment in
// experimental_widgets/logview_test.go. The working pattern asserted here: nil
// table id (auto, hashed from stable ints) and pointer row ids (pointer bytes
// are stable).

import (
	"testing"

	"go.hasen.dev/shirei"
)

// nestedTablesTestScope namespaces container ids for this test file.
type nestedTablesTestScope int

type peekEdge struct {
	Name  string
	Value int64
}

func TestNestedTablesRenderRows(t *testing.T) {
	initFontsOnce.Do(shirei.InitFontSubsystem)

	callers := []*peekEdge{
		{"runtime.growslice", 180},
		{"runtime.mallocgcSmallScanNoHeader", 70},
		{"runtime.(*mspan).initHeapBits", 40},
		{"runtime.mallocgcSmallNoscan", 30},
		{"runtime.mallocgcSmallScanHeader", 30},
	}
	callees := []*peekEdge{{"(self)", 350}}

	rowRects := map[string]shirei.Rect{}

	cols := func() []TableColumn[*peekEdge] {
		return []TableColumn[*peekEdge]{
			{
				Label: "Function",
				Render: func(e *peekEdge) {
					rowRects[e.Name] = shirei.GetScreenRect()
					shirei.Label(e.Name)
				},
				Less: func(a, b *peekEdge) bool { return a.Name < b.Name },
			},
			{
				Label: "Value", Width: 90, DefaultDesc: true,
				Render: func(e *peekEdge) { shirei.Label("v") },
				Less:   func(a, b *peekEdge) bool { return a.Value < b.Value },
			},
		}
	}

	section := func(edges []*peekEdge) {
		shirei.Container(shirei.Attrs(shirei.Grow(1), shirei.Expand, shirei.Clip), func() {
			shirei.Container(shirei.Attrs(shirei.Expand), func() {
				shirei.Label("heading")
			})
			Table(nil, 30, cols(), edges, func(e *peekEdge) any { return e }, 1)
		})
	}

	const scope = nestedTablesTestScope(77)

	frame := func() {
		shirei.GetHost().WindowSize = shirei.Vec2{900, 700}
		shirei.RunFrameFn(func() {
			shirei.ModAttrs(func(a *shirei.AttrSet) { a.Animations = 0 })
			box := shirei.Attrs(shirei.FixHeight(500), shirei.Expand, shirei.Clip)
			shirei.ContainerWithKey(scope, box, func() {
				shirei.Container(shirei.Attrs(shirei.Row, shirei.Expand), func() {
					shirei.Label("strip")
				})
				section(callers)
				section(callees)
			})
		})
	}

	// a couple of settle frames: VirtualListView needs a frame to learn its
	// size via previous-frame renderData
	for range 3 {
		clear(rowRects)
		frame()
	}

	want := len(callers) + len(callees)
	if len(rowRects) != want {
		t.Fatalf("rendered %d rows, want %d (rects: %v)", len(rowRects), want, rowRects)
	}
	for name, r := range rowRects {
		if r.Size[0] <= 0 || r.Size[1] <= 0 {
			t.Errorf("row %q has degenerate rect %+v", name, r)
		}
	}
	// the two tables split the section vertically; (self) belongs to the
	// second one, so it must sit below every caller row
	selfY := rowRects["(self)"].Origin[1]
	for _, e := range callers {
		if rowRects[e.Name].Origin[1] >= selfY {
			t.Errorf("caller row %q (y=%v) not above callee row (y=%v)",
				e.Name, rowRects[e.Name].Origin[1], selfY)
		}
	}
}
