package widgets

// Continuous wheel must reach the true content end on a variable-height
// VirtualList. Top-N average TotalHeight alone undershoots when lower rows
// are taller; without learning measured extent while scrolling, the list
// clamps at a FALSE BOTTOM (at maxScroll but last rows never rendered).

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"

	"go.hasen.dev/shirei"

	. "go.hasen.dev/shirei"
)

func TestVirtualListContinuousWheelReachesTrueBottom(t *testing.T) {
	initFontsOnce.Do(shirei.InitFontSubsystem)
	ResetInputSession()

	// Same corpus shape as behavior_test/vlist-wheel-to-bottom (seed 1).
	rng := rand.New(rand.NewSource(1))
	words := []string{
		"a", "bb", "ccc", "dddd", "word", "longerword", "wrapping", "line", "text",
		"virtual", "list", "scroll", "anchor", "viewport", "height", "bottom",
	}
	type item struct {
		id   int64
		text string
	}
	var items []item
	for id := int64(1); id <= 500; id++ {
		n := 3 + rng.Intn(50)
		var b strings.Builder
		fmt.Fprintf(&b, "#%d ", id)
		for i := 0; i < n; i++ {
			if i > 0 {
				b.WriteByte(' ')
			}
			b.WriteString(words[rng.Intn(len(words))])
		}
		items = append(items, item{id, b.String()})
	}

	scope := new(int)
	listKey := new(int)
	const fontSize, vpad, wheel f32 = 14, 4, 40
	var scrollY, maxScroll f32
	var lastID int64

	frame := func(dy f32) {
		lastID = 0
		shirei.GetHost().WindowSize = Vec2{960, 720}
		shirei.GetInputState().MousePoint = Vec2{480, 360}
		shirei.GetFrameInput().Mouse = 0
		shirei.GetFrameInput().Scroll = Vec2{0, dy}
		shirei.GetFrameInput().Motion = Vec2{}
		shirei.GetFrameInput().Key = 0
		shirei.GetFrameInput().Text = ""
		shirei.RunFrameFn(func() {
			shirei.ModAttrs(func(a *shirei.AttrSet) { a.Animations = 0 })
			shirei.ContainerWithKey(scope, Attrs(Viewport), func() {
				VirtualListViewExt(listKey, VirtualListAttrs{
					ItemCount: len(items),
					ItemKey:   func(i int) any { return items[i].id },
					ItemHeight: func(i int, w f32) f32 {
						a := TextStyle(FontSize(fontSize))
						sh := ShapeTextMax(items[i].text, a, w)
						var h f32
						for _, ln := range sh.Lines {
							h += ln.Height
						}
						return max(h, fontSize) + vpad*2
					},
					ItemView:           func(i int, w f32) { lastID = items[i].id },
					OutScrollOffset:    &scrollY,
					OutMaxScrollOffset: &maxScroll,
				})
			})
		})
	}

	for range 5 {
		frame(0)
	}
	idle, prev := 0, scrollY
	for i := 0; i < 6000; i++ {
		frame(wheel)
		if scrollY > prev+0.5 {
			idle = 0
			prev = scrollY
		} else {
			idle++
		}
		fb := maxScroll - scrollY
		lastItem := items[len(items)-1].id
		if fb <= 2 && lastID == lastItem {
			t.Logf("reached true bottom in %d wheel frames (scroll=%.1f max=%.1f)", i+1, scrollY, maxScroll)
			return
		}
		if idle > 15 {
			t.Fatalf("stuck after %d frames: scroll=%.1f max=%.1f fromBottom=%.1f lastVisible=#%d want #%d",
				i+1, scrollY, maxScroll, fb, lastID, lastItem)
		}
	}
	t.Fatalf("budget exhausted: scroll=%.1f max=%.1f lastVisible=#%d", scrollY, maxScroll, lastID)
}
