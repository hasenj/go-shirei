package shirei

import (
	"testing"
	"time"
)

// TestClickStreakDetection pins the core double-click rule: a click within
// DoubleClickInterval and DoubleClickSlop of the previous one continues the
// streak; breaking either resets to 1. Test frames run back to back (well
// inside the interval), so time-expiry is exercised by shrinking the
// tunable rather than sleeping.
func TestClickStreakDetection(t *testing.T) {
	ResetInputSession()
	WindowSize = Vec2{400, 300}
	scope := new(int)

	click := func(at Vec2) int {
		InputState.MousePoint = at
		FrameInput.Mouse = MouseClick
		var count int
		RunFrameFn(func() {
			ContainerWithKey(scope, AttrSet{}, func() {
				count = FrameInput.ClickCount
			})
		})
		return count
	}

	p := Vec2{100, 100}
	if got := click(p); got != 1 {
		t.Errorf("first click: ClickCount = %d, want 1", got)
	}
	if got := click(p); got != 2 {
		t.Errorf("second click same spot: ClickCount = %d, want 2", got)
	}
	if got := click(p); got != 3 {
		t.Errorf("third click same spot: ClickCount = %d, want 3", got)
	}

	// moving beyond the slop breaks the streak
	if got := click(Vec2{200, 100}); got != 1 {
		t.Errorf("click far away: ClickCount = %d, want 1", got)
	}
	// a nudge within the slop continues it
	if got := click(Vec2{200 + DoubleClickSlop - 1, 100}); got != 2 {
		t.Errorf("click within slop: ClickCount = %d, want 2", got)
	}

	// expiring the interval breaks the streak
	saved := DoubleClickInterval
	DoubleClickInterval = time.Nanosecond
	defer func() { DoubleClickInterval = saved }()
	if got := click(Vec2{200 + DoubleClickSlop - 1, 100}); got != 1 {
		t.Errorf("click after interval expiry: ClickCount = %d, want 1", got)
	}
}
