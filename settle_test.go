package shirei

import (
	"testing"
	"time"
)

// TestSettlePassResolvesFreshGeometry pins RunFrameFn's settle loop: a
// build that sizes content from GetResolvedSize — which reads the previous
// frame — used to produce a wrong first frame (the query answers zero until
// the container has rendered once). The settle loop must catch the missed
// query and re-run the build within the same RunFrameFn call, so even the
// very first output is laid out from resolved geometry.
func TestSettlePassResolvesFreshGeometry(t *testing.T) {
	ResetInputSession()
	ui.Host.WindowSize = Vec2{400, 300}
	scope := new(int)

	build := func() {
		ContainerWithKey(scope, AttrSet{MinSize: Vec2{200, 100}, MaxSize: Vec2{200, 100}}, func() {
			sz := GetResolvedSize()
			Element(AttrSet{
				MinSize:    Vec2{sz[0] / 2, sz[1] / 2},
				MaxSize:    Vec2{sz[0] / 2, sz[1] / 2},
				Background: Vec4{0, 0, 50, 1},
			})
		})
	}

	hasHalfSizeSurface := func(out FrameOutputData) bool {
		for _, s := range out.Surfaces {
			if s.Rect.Size == (Vec2{100, 50}) {
				return true
			}
		}
		return false
	}

	before := ui.FrameNumber
	out := RunFrameFn(build)
	if !hasHalfSizeSurface(out) {
		t.Errorf("first output missing the 100x50 surface: the settle pass did not re-run the incomplete frame")
	}
	if got := ui.FrameNumber - before; got != 2 {
		t.Errorf("first call ran %d passes, want 2 (build + settle)", got)
	}

	// once geometry resolves, the query hits and no settle pass runs
	before = ui.FrameNumber
	out = RunFrameFn(build)
	if !hasHalfSizeSurface(out) {
		t.Errorf("second output missing the 100x50 surface")
	}
	if got := ui.FrameNumber - before; got != 1 {
		t.Errorf("settled call ran %d passes, want 1", got)
	}

	// a query that can never resolve (unregistered id) must not loop or
	// keep the app awake: the pass cap bounds the call, and identical
	// output means no change-driven wake either
	var deadId ContainerId
	dead := func() {
		ContainerWithKey(scope, AttrSet{}, func() {
			_ = GetResolvedRectOf(deadId)
		})
	}
	RunFrameFn(dead) // settles content change from the previous build
	before = ui.FrameNumber
	out = RunFrameFn(dead)
	if got := ui.FrameNumber - before; got != 2 {
		t.Errorf("dead-id call ran %d passes, want 2 (capped)", got)
	}
	if out.NextFrameRequested {
		t.Errorf("dead-id frame requested a follow-up: stable-but-unresolvable content should idle")
	}
}

// TestSettlePassOnQueriedSizeChange: a child sized from GetAvailableSize must
// match the parent's new width on the same RunFrameFn output after a window
// resize — not one presented frame late.
func TestSettlePassOnQueriedSizeChange(t *testing.T) {
	ResetInputSession()
	ui.Host.WindowSize = Vec2{400, 300}
	scope := new(int)

	var childW float32
	build := func() {
		// Fill the window; query available width; pin a child to that width.
		// NoAnimate so the settle lands on the true new size (AnimSize easing
		// is covered separately — it must not force a settle every frame).
		ContainerWithKey(scope, Attrs(NoAnimate, Grow(1), Expand), func() {
			w := GetAvailableSize()[0]
			childW = w
			Element(AttrSet{
				MinSize:    Vec2{w, 20},
				MaxSize:    Vec2{w, 20},
				Background: Vec4{0, 0, 40, 1},
			})
		})
	}

	RunFrameFn(build) // miss settle
	RunFrameFn(build) // steady at 400

	ui.Host.WindowSize = Vec2{520, 300}
	before := ui.FrameNumber
	out := RunFrameFn(build)
	if got := ui.FrameNumber - before; got != 2 {
		t.Errorf("resize call ran %d passes, want 2 (stale hit → settle)", got)
	}
	if childW != 520 {
		t.Errorf("after resize settle, queried available width = %.1f, want 520", childW)
	}
	found := false
	for _, s := range out.Surfaces {
		if s.Rect.Size[0] == 520 && s.Rect.Size[1] == 20 {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("presented surfaces missing 520x20 child; still sized from previous window?")
	}

	// steady at new size: one pass
	before = ui.FrameNumber
	RunFrameFn(build)
	if got := ui.FrameNumber - before; got != 1 {
		t.Errorf("steady after resize ran %d passes, want 1", got)
	}
}

// TestSettlePassIgnoresAnimSizeEase: once the layout target is stable,
// querying an AnimSize-easing node must not force a settle every frame.
func TestSettlePassIgnoresAnimSizeEase(t *testing.T) {
	ResetInputSession()
	ui.Host.WindowSize = Vec2{400, 300}

	target := float32(100)
	var id ContainerId
	build := func() {
		Container(Attrs(AnimateOnly(AnimSize), FixSize(target, 40), Background(0, 0, 50, 1)), func() {
			id = CurrentId()
			_ = GetResolvedSize() // mark geometry queried
		})
	}

	RunFrameFn(build)
	RunFrameFn(build)

	target = 200
	// Force a real clock so AnimSize eases instead of snapping.
	ui.frameStart = time.Now().Add(-50 * time.Millisecond)
	RunFrameFn(build) // target change; may settle once

	for i := 0; i < 4; i++ {
		ui.frameStart = time.Now().Add(-50 * time.Millisecond)
		before := ui.FrameNumber
		RunFrameFn(build)
		if got := ui.FrameNumber - before; got != 1 {
			t.Fatalf("anim ease frame %d ran %d passes, want 1 (layout target unchanged)", i, got)
		}
		_ = GetRenderDataOf(id).ResolvedSize[0]
	}
}
