package shirei

import "testing"

func TestTouchingListIsTouched(t *testing.T) {
	ResetInputSession()
	ui.Host.WindowSize = Vec2{200, 200}
	scope := new(int)
	box := Attrs(FixSize(100, 100))

	// Contact over the top-left box.
	ui.Host.Input.Touches[0] = TouchInfo{Active: true, Id: 7, Pos: Vec2{40, 40}}
	ui.Host.Input.MousePoint = Vec2{40, 40}

	var touched, direct, hovered bool
	var ids []uint32
	// Same builder closure both frames so ContainerWithKey keeps identity
	// (a new func literal each frame remounts and breaks hover/touch lists).
	body := func() {
		hovered = IsHovered()
		touched = IsTouched()
		direct = IsTouchedDirectly()
		ids = TouchingIds(nil)
	}
	frame := func() {
		Container(Attrs(Viewport), func() {
			ContainerWithKey(scope, box, body)
		})
	}

	// First frame: produce ui.hoverables (ui.touchingList uses previous ui.hoverables).
	RunFrameFn(frame)
	if len(ui.hoverables) == 0 {
		t.Fatal("expected ui.hoverables after first frame")
	}

	RunFrameFn(frame)
	if !hovered {
		t.Fatal("expected IsHovered at (40,40)")
	}
	if !touched {
		t.Fatalf("expected IsTouched under ui.active contact; ui.touchingList=%d", len(ui.touchingList))
	}
	if !direct {
		t.Fatal("expected IsTouchedDirectly on the leaf box")
	}
	if len(ids) != 1 || ids[0] != 7 {
		t.Fatalf("TouchingIds = %v, want [7]", ids)
	}

	// Far away: not touched.
	ui.Host.Input.Touches[0].Pos = Vec2{180, 180}
	touched = true
	hovered = true
	RunFrameFn(frame)
	if touched {
		t.Fatal("expected not IsTouched when contact is outside the box")
	}
}

func TestTouchById(t *testing.T) {
	ResetInputSession()
	ui.Host.Input.Touches[2] = TouchInfo{Active: true, Id: 99, Pos: Vec2{1, 2}}
	ti, ok := TouchById(99)
	if !ok || ti.Pos != (Vec2{1, 2}) {
		t.Fatalf("TouchById(99) = %+v %v", ti, ok)
	}
	if _, ok := TouchById(1); ok {
		t.Fatal("expected miss for unknown id")
	}
}
