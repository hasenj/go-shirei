package widgets

// VirtualListView_ScrollToEnd: end-relative scroll with real tail measure.
// Pin policy stays in the caller; these tests pin the list primitive only.

import (
	"testing"

	"go.hasen.dev/shirei"

	. "go.hasen.dev/shirei"
)

func TestVirtualListScrollToEndFlush(t *testing.T) {
	initFontsOnce.Do(shirei.InitFontSubsystem)
	ResetInputSession()

	scope := new(int)
	listKey := new(int)
	const itemCount = 80
	const rowH f32 = 20 // content 1600 in a 200-tall viewport → max scroll 1400
	rendered := map[int]bool{}
	var scrollY, maxScroll f32

	frame := func() {
		rendered = map[int]bool{}
		shirei.GetHost().WindowSize = Vec2{400, 200}
		shirei.GetInputState().MousePoint = Vec2{-1000, -1000}
		shirei.GetFrameInput().Mouse = 0
		shirei.GetFrameInput().Scroll = Vec2{}
		shirei.GetFrameInput().Motion = Vec2{}
		shirei.GetFrameInput().Key = 0
		shirei.GetFrameInput().Text = ""
		shirei.RunFrameFn(func() {
			shirei.ModAttrs(func(a *shirei.AttrSet) { a.Animations = 0 })
			shirei.ContainerWithKey(scope, Attrs(Viewport), func() {
				VirtualListViewExt(listKey, VirtualListAttrs{
					ItemCount: itemCount,
					ItemKey:   func(i int) any { return i },
					ItemHeight: func(i int, w f32) f32 {
						return rowH
					},
					ItemView: func(i int, w f32) {
						rendered[i] = true
					},
					OutScrollOffset:    &scrollY,
					OutMaxScrollOffset: &maxScroll,
				})
			})
		})
	}

	frame() // learn width
	VirtualListView_ScrollToEnd(listKey, 0)
	for range 6 {
		frame()
	}

	if !rendered[itemCount-1] {
		t.Fatalf("ScrollToEnd(0) should show last row: rendered=%v scrollY=%.1f max=%.1f",
			rendered, scrollY, maxScroll)
	}
	if scrollY < maxScroll-1 {
		t.Errorf("ScrollToEnd(0) should sit at maxScroll: scrollY=%.1f max=%.1f", scrollY, maxScroll)
	}
	// first row must not still be on screen
	if rendered[0] {
		t.Errorf("ScrollToEnd(0) still showing top row: %v", rendered)
	}
}

func TestVirtualListScrollToEndMargin(t *testing.T) {
	initFontsOnce.Do(shirei.InitFontSubsystem)
	ResetInputSession()

	scope := new(int)
	listKey := new(int)
	const itemCount = 80
	const rowH f32 = 20
	const margin f32 = 40 // two rows above flush end
	var scrollY, maxScroll f32

	frame := func() {
		shirei.GetHost().WindowSize = Vec2{400, 200}
		shirei.GetInputState().MousePoint = Vec2{-1000, -1000}
		shirei.GetFrameInput().Mouse = 0
		shirei.GetFrameInput().Scroll = Vec2{}
		shirei.GetFrameInput().Motion = Vec2{}
		shirei.GetFrameInput().Key = 0
		shirei.GetFrameInput().Text = ""
		shirei.RunFrameFn(func() {
			shirei.ModAttrs(func(a *shirei.AttrSet) { a.Animations = 0 })
			shirei.ContainerWithKey(scope, Attrs(Viewport), func() {
				VirtualListViewExt(listKey, VirtualListAttrs{
					ItemCount:          itemCount,
					ItemKey:            func(i int) any { return i },
					ItemHeight:         func(i int, w f32) f32 { return rowH },
					ItemView:           func(i int, w f32) {},
					OutScrollOffset:    &scrollY,
					OutMaxScrollOffset: &maxScroll,
				})
			})
		})
	}

	frame()
	VirtualListView_ScrollToEnd(listKey, margin)
	for range 6 {
		frame()
	}

	fromBottom := maxScroll - scrollY
	if fromBottom < margin-2 || fromBottom > margin+2 {
		t.Errorf("ScrollToEnd(%.0f): from-bottom=%.1f want ~%.0f (scrollY=%.1f max=%.1f)",
			margin, fromBottom, margin, scrollY, maxScroll)
	}
}

func TestVirtualListScrollToEndVariableHeights(t *testing.T) {
	// Tail rows taller than the head average: average-height TotalHeight alone
	// would undershoot the real end; ScrollToEnd must still reach the last row.
	initFontsOnce.Do(shirei.InitFontSubsystem)
	ResetInputSession()

	scope := new(int)
	listKey := new(int)
	const itemCount = 100
	rendered := map[int]bool{}
	var scrollY, maxScroll f32

	heightAt := func(i int) f32 {
		if i >= itemCount-10 {
			return 40 // tall tail
		}
		return 10
	}

	frame := func() {
		rendered = map[int]bool{}
		shirei.GetHost().WindowSize = Vec2{400, 200}
		shirei.GetInputState().MousePoint = Vec2{-1000, -1000}
		shirei.GetFrameInput().Mouse = 0
		shirei.GetFrameInput().Scroll = Vec2{}
		shirei.GetFrameInput().Motion = Vec2{}
		shirei.GetFrameInput().Key = 0
		shirei.GetFrameInput().Text = ""
		shirei.RunFrameFn(func() {
			shirei.ModAttrs(func(a *shirei.AttrSet) { a.Animations = 0 })
			shirei.ContainerWithKey(scope, Attrs(Viewport), func() {
				VirtualListViewExt(listKey, VirtualListAttrs{
					ItemCount: itemCount,
					ItemKey:   func(i int) any { return i },
					ItemHeight: func(i int, w f32) f32 {
						return heightAt(i)
					},
					ItemView: func(i int, w f32) {
						rendered[i] = true
					},
					OutScrollOffset:    &scrollY,
					OutMaxScrollOffset: &maxScroll,
				})
			})
		})
	}

	frame()
	VirtualListView_ScrollToEnd(listKey, 0)
	for range 8 {
		frame()
	}

	if !rendered[itemCount-1] {
		t.Fatalf("variable-height ScrollToEnd(0) missed last row: rendered=%v scrollY=%.1f max=%.1f",
			rendered, scrollY, maxScroll)
	}
}

func TestVirtualListScrollToEndLargeListNoFullWalk(t *testing.T) {
	// Pin-end on 10k+ items must not measure every row from the top.
	initFontsOnce.Do(shirei.InitFontSubsystem)
	ResetInputSession()

	scope := new(int)
	listKey := new(int)
	const itemCount = 12_000
	const rowH f32 = 20
	heightCalls := make([]int, itemCount)
	var scrollY, maxScroll f32

	frame := func() {
		for i := range heightCalls {
			heightCalls[i] = 0
		}
		shirei.GetHost().WindowSize = Vec2{400, 200}
		shirei.GetInputState().MousePoint = Vec2{-1000, -1000}
		shirei.GetFrameInput().Mouse = 0
		shirei.GetFrameInput().Scroll = Vec2{}
		shirei.GetFrameInput().Motion = Vec2{}
		shirei.GetFrameInput().Key = 0
		shirei.GetFrameInput().Text = ""
		shirei.RunFrameFn(func() {
			shirei.ModAttrs(func(a *shirei.AttrSet) { a.Animations = 0 })
			shirei.ContainerWithKey(scope, Attrs(Viewport), func() {
				VirtualListViewExt(listKey, VirtualListAttrs{
					ItemCount: itemCount,
					ItemKey:   func(i int) any { return i },
					ItemHeight: func(i int, w f32) f32 {
						heightCalls[i]++
						return rowH
					},
					ItemView:           func(i int, w f32) {},
					OutScrollOffset:    &scrollY,
					OutMaxScrollOffset: &maxScroll,
				})
			})
		})
	}

	frame() // settle width (top sample only)
	VirtualListView_ScrollToEnd(listKey, 0)
	frame() // apply ScrollToEnd

	// Head (away from top sample N=50 and far from tail) must not be walked.
	midTouched := 0
	for i := 200; i < itemCount-200; i++ {
		if heightCalls[i] > 0 {
			midTouched++
		}
	}
	if midTouched > 0 {
		t.Errorf("ScrollToEnd on large list measured %d mid-list rows (want 0 full walk)", midTouched)
	}

	// Tail measure should touch the last rows.
	if heightCalls[itemCount-1] == 0 {
		t.Errorf("ScrollToEnd should measure the last row")
	}
	if scrollY < maxScroll-1 {
		t.Errorf("large-list ScrollToEnd(0): scrollY=%.1f max=%.1f", scrollY, maxScroll)
	}
}

func TestVirtualListScrollToEndThenScrollToIndex(t *testing.T) {
	// ScrollToIndex after ScrollToEnd must win (top restore for pin-to-top).
	initFontsOnce.Do(shirei.InitFontSubsystem)
	ResetInputSession()

	scope := new(int)
	listKey := new(int)
	const itemCount = 60
	const rowH f32 = 20
	rendered := map[int]bool{}
	var scrollY f32

	frame := func() {
		rendered = map[int]bool{}
		shirei.GetHost().WindowSize = Vec2{400, 200}
		shirei.GetInputState().MousePoint = Vec2{-1000, -1000}
		shirei.GetFrameInput().Mouse = 0
		shirei.GetFrameInput().Scroll = Vec2{}
		shirei.GetFrameInput().Motion = Vec2{}
		shirei.GetFrameInput().Key = 0
		shirei.GetFrameInput().Text = ""
		shirei.RunFrameFn(func() {
			shirei.ModAttrs(func(a *shirei.AttrSet) { a.Animations = 0 })
			shirei.ContainerWithKey(scope, Attrs(Viewport), func() {
				VirtualListViewExt(listKey, VirtualListAttrs{
					ItemCount:          itemCount,
					ItemKey:            func(i int) any { return i },
					ItemHeight:         func(i int, w f32) f32 { return rowH },
					ItemView:           func(i int, w f32) { rendered[i] = true },
					OutScrollOffset:    &scrollY,
					OutMaxScrollOffset: new(f32),
				})
			})
		})
	}

	frame()
	VirtualListView_ScrollToEnd(listKey, 0)
	for range 4 {
		frame()
	}
	if !rendered[itemCount-1] {
		t.Fatalf("setup: expected at end, got %v", rendered)
	}

	VirtualListView_ScrollToIndex(listKey, 0)
	for range 4 {
		frame()
	}
	if !rendered[0] || rendered[itemCount-1] {
		t.Fatalf("ScrollToIndex(0) after ScrollToEnd should show top: %v scrollY=%.1f", rendered, scrollY)
	}
}
