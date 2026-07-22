package widgets

import (
	"fmt"
	"testing"

	"go.hasen.dev/shirei"

	. "go.hasen.dev/shirei"
)

// TestMenuCapsHeightAndScrolls: a dropdown with far more items than the
// window fits must cap its height to the window and scroll inside, instead
// of extending past the screen (the androidrun device-menu bug).
func TestMenuCapsHeightAndScrolls(t *testing.T) {
	initFontsOnce.Do(shirei.InitFontSubsystem)

	scope := new(int)
	var menuId shirei.ContainerId
	var menuScrollY f32
	view := func() {
		MenuButton("Options", func() {
			// fn runs inside the action-menu container scope.
			menuId = shirei.CurrentId()
			menuScrollY = shirei.GetScrollOffset()[1]
			for i := 0; i < 50; i++ {
				MenuItem(0, fmt.Sprintf("Item %d", i))
			}
		})
	}

	runSemFrame(scope, semFrameInput{mouse: offscreen}, view) // settle geometry

	// Open the menu: full press on the trigger button (top-left of the scope).
	btn := Vec2{30, 14}
	runSemFrame(scope, semFrameInput{mouse: btn, action: MouseClick}, view)
	runSemFrame(scope, semFrameInput{mouse: btn, action: MouseRelease}, view)
	// One more frame so the popup's geometry resolves.
	runSemFrame(scope, semFrameInput{mouse: offscreen}, view)

	rect := shirei.GetResolvedRectOf(menuId)
	winH := f32(400) // runSemFrame fixes WindowSize at 600x400
	if rect.Size[1] <= 0 {
		t.Fatalf("menu did not open (size %v)", rect.Size)
	}
	if rect.Size[1] > winH {
		t.Fatalf("menu height %v exceeds window height %v", rect.Size[1], winH)
	}
	// 50 items at ~20+px each dwarf 400px — the cap must be engaged, i.e.
	// the menu should be using most of the window, not truncating to a few items.
	if rect.Size[1] < winH*0.7 {
		t.Fatalf("menu height %v suspiciously small for 50 items (cap not engaged?)", rect.Size[1])
	}
	if rect.Origin[1]+rect.Size[1] > winH+1 {
		t.Fatalf("menu bottom %v extends past window %v", rect.Origin[1]+rect.Size[1], winH)
	}

	// Scroll inside the menu: wheel input while hovering it must move the offset.
	inMenu := Vec2{rect.Origin[0] + 40, rect.Origin[1] + rect.Size[1]/2}
	runSemFrame(scope, semFrameInput{mouse: inMenu, scroll: Vec2{0, 120}}, view)
	runSemFrame(scope, semFrameInput{mouse: inMenu}, view)
	if menuScrollY <= 0 {
		t.Fatalf("menu scroll offset = %v after wheel input, want > 0", menuScrollY)
	}
}
