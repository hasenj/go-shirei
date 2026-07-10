package widgets

// Frame benchmarks establishing the baseline for the identity-tree
// refactor (notes/identity-tree-plan.md). The interesting numbers are
// ns/frame and allocs/frame: identity machinery (scope hashing, hook and
// renderData map traffic) is per-container overhead, so most variants use
// bare containers without text; Gallery and VirtualTable include real
// widgets and text shaping for a realistic composite.
//
// Baseline snapshots live in notes/bench/ — compare with:
//   go test -bench BenchmarkFrame -benchmem -count=6 -run '^$' ./widgets/
//   benchstat notes/bench/identity-baseline.txt <new output>

import (
	"fmt"
	"testing"

	"go.hasen.dev/shirei"

	. "go.hasen.dev/shirei"
)

func benchFrame(b *testing.B, fn func()) {
	shirei.InitFontSubsystem()
	shirei.ResetInputSession()
	shirei.WindowSize = Vec2{1200, 800}

	scope := new(int)
	frame := func() {
		shirei.RunFrameFn(func() {
			shirei.ModAttrs(func(a *shirei.AttrSet) { a.NoAnimate = true })
			shirei.ContainerWithKey(scope, Attrs(Viewport), fn)
		})
	}
	for range 3 { // settle: virtual lists need previous-frame sizes
		frame()
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		frame()
	}
}

// 1000 anonymous fixed-size elements under one parent: auto-id generation
// and per-container map traffic, no text.
func BenchmarkFrameWideShallow(b *testing.B) {
	benchFrame(b, func() {
		Container(Attrs(Viewport), func() {
			for i := 0; i < 1000; i++ {
				Element(Attrs(FixSize(50, 20)))
			}
		})
	})
}

// 200 nested containers: scope-chain depth.
func BenchmarkFrameDeep(b *testing.B) {
	var deep func(n int)
	deep = func(n int) {
		if n == 0 {
			return
		}
		Container(Attrs(Pad(1), Expand), func() { deep(n - 1) })
	}
	benchFrame(b, func() {
		Container(Attrs(Viewport), func() { deep(200) })
	})
}

var benchIds = func() []*int {
	out := make([]*int, 500)
	for i := range out {
		out[i] = new(int)
	}
	return out
}()

// 500 explicit pointer-id containers: the contract-conformant explicit-id
// path (scopeIdFrom hashing + renderData lookups by value).
func BenchmarkFrameExplicitIds(b *testing.B) {
	benchFrame(b, func() {
		Container(Attrs(Viewport), func() {
			for _, id := range benchIds {
				shirei.ContainerWithKey(id, Attrs(FixSize(50, 20)), func() {})
			}
		})
	})
}

type benchRow struct {
	Name string
	Val  int64
}

var benchRows = func() []*benchRow {
	out := make([]*benchRow, 10_000)
	for i := range out {
		out[i] = &benchRow{Name: fmt.Sprintf("pkg.func%04d", i), Val: int64(i * 37)}
	}
	return out
}()

// A 10k-row virtualized table: per-frame sort copy, virtualization, and
// text shaping for the ~visible rows.
func BenchmarkFrameVirtualTable(b *testing.B) {
	cols := []TableColumn[*benchRow]{
		{
			Label:  "Name",
			Render: func(r *benchRow) { Label(r.Name) },
			Less:   func(a, b *benchRow) bool { return a.Name < b.Name },
		},
		{
			Label: "Val", Width: 90, DefaultDesc: true,
			Render: func(r *benchRow) { Label(fmt.Sprintf("%d", r.Val)) },
			Less:   func(a, b *benchRow) bool { return a.Val < b.Val },
		},
	}
	benchFrame(b, func() {
		Table(nil, 24, cols, benchRows, func(r *benchRow) any { return r }, 1)
	})
}

// The snapshot-test widget gallery: a realistic mixed composition
// (buttons, checkboxes, slider, focused text input, sortable table).
func BenchmarkFrameGallery(b *testing.B) {
	benchFrame(b, widgetGallery)
}
