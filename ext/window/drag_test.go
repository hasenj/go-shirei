package window

import (
	"testing"

	"go.hasen.dev/shirei"
)

// Replay the synchronous native-position path through real UI input frames and
// the extension's deferred placement queue, including layout settling passes.
func TestDragMoveLocalPointerAndRelease(t *testing.T) {
	restore := shirei.EnterUI(shirei.NewUI())
	defer restore()
	position := shirei.Vec2{40, 80}
	shirei.GetHost().WindowSize = shirei.Vec2{300, 200}
	frame := func() {
		shirei.Container(shirei.Attrs(shirei.FixSize(200, 80)), func() {
			DragMove(&position)
			shirei.Label("Drag handle")
		})
	}
	run := func(mouse shirei.MouseAction, point, motion shirei.Vec2) {
		t.Helper()
		shirei.GetInputState().MouseButton = shirei.MousePrimary
		shirei.GetInputState().MousePoint = point
		shirei.GetFrameInput().Mouse = mouse
		shirei.GetFrameInput().Motion = motion
		shirei.RunFrameFnLocked(frame)
	}
	run(0, shirei.Vec2{60, 30}, shirei.Vec2{})
	run(shirei.MouseClick, shirei.Vec2{60, 30}, shirei.Vec2{})
	run(0, shirei.Vec2{80, 40}, shirei.Vec2{20, 10})
	if position != (shirei.Vec2{60, 90}) {
		t.Fatalf("drag or settling applies displacement incorrectly: %v", position)
	}
	// Native repositioning puts the grabbed point back under the cursor.
	run(0, shirei.Vec2{60, 30}, shirei.Vec2{-20, -10})
	if position != (shirei.Vec2{60, 90}) {
		t.Fatal("the window's own move becomes another drag step")
	}
	run(0, shirei.Vec2{67, 34}, shirei.Vec2{7, 4})
	if position != (shirei.Vec2{67, 94}) {
		t.Fatalf("second pointer step is lost: %v", position)
	}
	run(shirei.MouseRelease, shirei.Vec2{90, 50}, shirei.Vec2{23, 16})
	run(0, shirei.Vec2{110, 60}, shirei.Vec2{20, 10})
	if position != (shirei.Vec2{67, 94}) {
		t.Fatal("released pointer keeps moving the window")
	}
	c := config()
	if c.pendingPlacement != placeAt || c.pendingX != 67 || c.pendingY != 94 {
		t.Fatalf("drag does not queue the final native position: %v %d,%d", c.pendingPlacement, c.pendingX, c.pendingY)
	}
}
