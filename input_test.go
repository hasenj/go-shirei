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
	ui.Host.WindowSize = Vec2{400, 300}
	scope := new(int)

	click := func(at Vec2) int {
		ui.Host.Input.MousePoint = at
		ui.Host.FrameInput.Mouse = MouseClick
		var count int
		RunFrameFn(func() {
			ContainerWithKey(scope, AttrSet{}, func() {
				count = ui.Host.FrameInput.ClickCount
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

// TestRequestOpenURLMirroredToOutput: Host.OpenURL is copied into FrameOutputData
// and cleared after the pass (same as Copy/Paste).
func TestRequestOpenURLMirroredToOutput(t *testing.T) {
	out := RunFrameFn(func() {
		RequestOpenURL("https://example.com/a")
		RequestOpenURL("https://example.com/b") // last wins
	})
	if out.OpenURL != "https://example.com/b" {
		t.Fatalf("OpenURL=%q want last write", out.OpenURL)
	}
	if GetHost().OpenURL != "" {
		t.Fatal("Host.OpenURL should be cleared after frame")
	}
	out = RunFrameFn(func() {})
	if out.OpenURL != "" {
		t.Fatalf("stale OpenURL leaked: %q", out.OpenURL)
	}
}

// TestWantsKeyboardEarnsKeep: flag is cleared each frame pass and only stays
// true if something called WantKeyboard during that pass. Backends read it
// after RunFrameFn.
func TestWantsKeyboardEarnsKeep(t *testing.T) {
	ResetInputSession()
	ui.Host.WindowSize = Vec2{200, 100}
	scope := new(int)

	RunFrameFn(func() {
		ContainerWithKey(scope, AttrSet{}, func() {
			WantKeyboard()
		})
	})
	if !ui.Host.WantsKeyboard {
		t.Fatal("WantKeyboard during frame should leave ui.Host.WantsKeyboard true after RunFrameFn")
	}

	RunFrameFn(func() {
		ContainerWithKey(scope, AttrSet{}, func() {
			// intentionally not WantKeyboard
		})
	})
	if ui.Host.WantsKeyboard {
		t.Fatal("next frame without WantKeyboard must clear the flag")
	}

	ui.Host.WantsKeyboard = true // stale external set
	RunFrameFn(func() {
		ContainerWithKey(scope, AttrSet{}, func() {})
	})
	if ui.Host.WantsKeyboard {
		t.Fatal("pass start must clear a stale ui.Host.WantsKeyboard")
	}
}
