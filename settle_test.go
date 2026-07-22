package shirei

import "testing"

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
