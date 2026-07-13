package widgets

// Regression: pin-to-bottom (ScrollToEnd re-posted each frame) must show the
// last row at margin 0 and hold a non-zero margin across append/prepend.
//
// A prior bug set scroll from measured contentEnd while seeding the last-item
// anchor from a larger average-height TotalHeight, so fromBottom read 0 while
// the last rows stayed below the viewport.

import (
	"math/rand"
	"testing"

	"go.hasen.dev/shirei"

	. "go.hasen.dev/shirei"
)

func TestVirtualListPinBottomMutations(t *testing.T) {
	initFontsOnce.Do(shirei.InitFontSubsystem)
	ResetInputSession()

	scope := new(int)
	listKey := new(int)
	rng := rand.New(rand.NewSource(7))

	type item struct {
		id int
		h  f32
	}
	items := make([]item, 0, 500)
	nextID := 1
	add := func(n int, atTop bool) {
		batch := make([]item, n)
		for i := range batch {
			id := nextID
			nextID++
			// heights 15..45 so top-N average ≠ true average / tail
			batch[i] = item{id: id, h: 15 + f32(rng.Intn(31))}
		}
		if atTop {
			items = append(batch, items...)
		} else {
			items = append(items, batch...)
		}
	}
	add(300, false)

	var scrollY, maxScroll f32
	pinMargin := f32(0)
	var lastIdx int

	frame := func() {
		lastIdx = -1
		shirei.WindowSize = Vec2{400, 300}
		shirei.InputState.MousePoint = Vec2{-1000, -1000}
		shirei.FrameInput.Mouse = 0
		shirei.FrameInput.Scroll = Vec2{}
		shirei.FrameInput.Motion = Vec2{}
		shirei.FrameInput.Key = 0
		shirei.FrameInput.Text = ""
		shirei.RunFrameFn(func() {
			ModAttrs(func(a *AttrSet) { a.NoAnimate = true })
			ContainerWithKey(scope, Attrs(Viewport), func() {
				VirtualListView_ScrollToEnd(listKey, pinMargin)
				VirtualListViewExt(listKey, VirtualListAttrs{
					ItemCount: len(items),
					ItemKey:   func(i int) any { return items[i].id },
					ItemHeight: func(i int, w f32) f32 {
						return items[i].h
					},
					ItemView: func(i int, w f32) {
						if i > lastIdx {
							lastIdx = i
						}
					},
					OutScrollOffset:    &scrollY,
					OutMaxScrollOffset: &maxScroll,
				})
			})
		})
	}

	for range 6 {
		frame()
	}
	if lastIdx != len(items)-1 {
		t.Fatalf("pin margin 0 should show last index %d, got %d (scroll=%.1f max=%.1f)",
			len(items)-1, lastIdx, scrollY, maxScroll)
	}
	if maxScroll-scrollY > 1 {
		t.Errorf("pin margin 0: fromBottom=%.1f want 0", maxScroll-scrollY)
	}

	add(40, false)
	for range 6 {
		frame()
	}
	if lastIdx != len(items)-1 {
		t.Fatalf("after append should show last index %d, got %d", len(items)-1, lastIdx)
	}

	pinMargin = 50
	for range 6 {
		frame()
	}
	fb := maxScroll - scrollY
	if fb < 40 || fb > 60 {
		t.Errorf("margin 50: fromBottom=%.1f want ~50", fb)
	}

	add(30, true)
	for range 6 {
		frame()
	}
	fb = maxScroll - scrollY
	if fb < 40 || fb > 60 {
		t.Errorf("after prepend, margin should hold: fromBottom=%.1f want ~50", fb)
	}

	// replace corpus while pinned to end
	items = items[:0]
	nextID = 1
	add(200, false)
	pinMargin = 0
	for range 8 {
		frame()
	}
	if lastIdx != len(items)-1 {
		t.Fatalf("after replace pin0 should show last %d got %d", len(items)-1, lastIdx)
	}
}
