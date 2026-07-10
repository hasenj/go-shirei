package widgets

// TestVirtualListScrollIntoView pins the scroll-into-view command:
// revealing an off-screen item scrolls minimally (nearest edge), revealing
// a visible item scrolls nothing, and the command consumes on use.

import (
	"testing"

	"go.hasen.dev/shirei"

	. "go.hasen.dev/shirei"
)

func TestVirtualListScrollIntoView(t *testing.T) {
	initFontsOnce.Do(shirei.InitFontSubsystem)

	scope := new(int)
	listKey := new(int)
	const itemCount = 200
	const rowH = 20 // viewport is 200 → exactly 10 rows visible
	ids := make([]*int, itemCount)
	for i := range ids {
		ids[i] = new(int)
	}

	rendered := map[int]bool{}

	// the builder literal deliberately lives INSIDE settle, which is called
	// from several sites below: the compiler inlines settle per call site,
	// cloning the literal into a distinct compiled function each time. Before
	// funcCodePtr canonicalized code pointers to their source position, each
	// new call site changed the component type, remounted the whole list, and
	// silently clamped the scroll to 0 (found the hard way). This test now
	// pins the canonicalization: identity must survive the clones.
	settle := func() {
		for range 6 {
			rendered = map[int]bool{}
			shirei.WindowSize = Vec2{400, 200}
			shirei.InputState.MousePoint = Vec2{-1000, -1000}
			shirei.FrameInput.Mouse = 0
			shirei.FrameInput.Scroll = Vec2{}
			shirei.FrameInput.Motion = Vec2{}
			shirei.FrameInput.Key = 0
			shirei.FrameInput.Text = ""
			shirei.RunFrameFn(func() {
				shirei.ModAttrs(func(a *shirei.AttrSet) { a.NoAnimate = true })
				shirei.ContainerWithKey(scope, Attrs(Viewport), func() {
					VirtualListView(listKey,
						itemCount,
						func(i int) any { return ids[i] },
						func(i int, w f32) f32 { return rowH },
						func(i int, w f32) { rendered[i] = true },
					)
				})
			})
		}
	}

	settle()
	if !rendered[0] || rendered[50] {
		t.Fatalf("baseline should show the top of the list: %v", rendered)
	}

	// far below the viewport → bottom-aligned: scroll 150*20+20-200 = 2820,
	// first fully visible row = 141
	VirtualListScrollIntoView(listKey, ids[150])
	settle()
	if !rendered[150] || !rendered[141] || rendered[140] {
		t.Fatalf("reveal 150 should bottom-align (rows 141..150): %v", rendered)
	}

	// above the viewport → top-aligned at 100*20 = 2000
	VirtualListScrollIntoView(listKey, ids[100])
	settle()
	if !rendered[100] || rendered[99] || rendered[111] {
		t.Fatalf("reveal 100 should top-align (rows 100..110): %v", rendered)
	}

	// already visible → no scroll: 100 must still be the first row
	VirtualListScrollIntoView(listKey, ids[105])
	settle()
	if !rendered[100] || !rendered[105] {
		t.Fatalf("revealing a visible item must not scroll: %v", rendered)
	}

	// last one wins when two requests land before a render
	VirtualListScrollIntoView(listKey, ids[190])
	VirtualListScrollIntoView(listKey, ids[0])
	settle()
	if !rendered[0] || rendered[190] {
		t.Fatalf("last request should win: %v", rendered)
	}
}
