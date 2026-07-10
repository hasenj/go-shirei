package widgets

import (
	"testing"

	"go.hasen.dev/shirei"

	. "go.hasen.dev/shirei"
)

// TestSegmentedControlClick drives a full press on another segment and
// checks the bound value follows, the change is reported exactly once, and
// clicking the already-selected segment reports no change.
func TestSegmentedControlClick(t *testing.T) {
	initFontsOnce.Do(shirei.InitFontSubsystem)

	scope := new(int)
	mode := "a"
	var changes int
	view := func() {
		if SegmentedControl(&mode, Cell("A", "a"), Cell("B", "b"), Cell("C", "c")) {
			changes++
		}
	}

	// segments are 56 wide minimum inside a 1px frame pad: segment i spans
	// roughly x ∈ [1+57i, 57+57i]; click mid-height at the segment center
	segCenter := func(i int) Vec2 { return Vec2{1 + 57*f32(i) + 28, 13} }

	runSemFrame(scope, semFrameInput{mouse: offscreen}, view) // settle geometry

	// full press on segment B: down inside, up inside
	runSemFrame(scope, semFrameInput{mouse: segCenter(1), action: MouseClick}, view)
	runSemFrame(scope, semFrameInput{mouse: segCenter(1), action: MouseRelease}, view)
	if mode != "b" {
		t.Fatalf("mode = %q after clicking B, want b", mode)
	}
	if changes != 1 {
		t.Fatalf("changes = %d, want 1", changes)
	}

	// clicking the segment that's already selected reports no change
	runSemFrame(scope, semFrameInput{mouse: segCenter(1), action: MouseClick}, view)
	runSemFrame(scope, semFrameInput{mouse: segCenter(1), action: MouseRelease}, view)
	if mode != "b" || changes != 1 {
		t.Fatalf("re-click changed state: mode=%q changes=%d", mode, changes)
	}
}
