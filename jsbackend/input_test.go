package jsbackend

import (
	"slices"
	"testing"

	"go.hasen.dev/shirei"
)

func clearFrameInput() {
	*shirei.GetFrameInput() = shirei.FrameInputData{}
}

// TestWheelPixelModePositiveIsScrollDown: browser WheelEvent positive deltaY
// means scroll down; that must map to positive FrameInput.Scroll.Y (same as
// X11 wheel-down). Negating to "match Cocoa" inverts trackpad/page feel.
func TestWheelPixelModePositiveIsScrollDown(t *testing.T) {
	shirei.ResetInputSession()
	a := newInputAccum(false)

	a.wheel(0, 40, 0) // deltaMode pixel
	a.sample()
	if got := shirei.GetFrameInput().Scroll; got != (shirei.Vec2{0, 40}) {
		t.Fatalf("Scroll = %v, want {0, 40}", got)
	}

	clearFrameInput()
	a.wheel(-12, -8, 0)
	a.sample()
	if got := shirei.GetFrameInput().Scroll; got != (shirei.Vec2{-12, -8}) {
		t.Fatalf("Scroll = %v, want {-12, -8}", got)
	}
}

// TestWheelLineModeScales: deltaMode 1 (line) multiplies by 16 before sample.
func TestWheelLineModeScales(t *testing.T) {
	shirei.ResetInputSession()
	a := newInputAccum(false)

	a.wheel(0, 3, 1) // 3 lines → 48 px
	a.sample()
	if got := shirei.GetFrameInput().Scroll; got != (shirei.Vec2{0, 48}) {
		t.Fatalf("Scroll = %v, want {0, 48}", got)
	}
}

// TestWheelAccumulatesAcrossEvents: multiple wheel events before sample sum.
func TestWheelAccumulatesAcrossEvents(t *testing.T) {
	shirei.ResetInputSession()
	a := newInputAccum(false)

	a.wheel(0, 10, 0)
	a.wheel(0, 15, 0)
	a.sample()
	if got := shirei.GetFrameInput().Scroll; got != (shirei.Vec2{0, 25}) {
		t.Fatalf("Scroll = %v, want {0, 25}", got)
	}

	clearFrameInput()
	a.sample()
	if got := shirei.GetFrameInput().Scroll; got != (shirei.Vec2{}) {
		t.Fatalf("second sample Scroll = %v, want zero", got)
	}
}

// TestMotionAccumulatedFromSamplePoints pins FrameInput.Motion: each sample
// after the first reports the delta from the previous sample point. Drag-drop
// and other Motion consumers rely on this; the old shell never set Motion.
func TestMotionAccumulatedFromSamplePoints(t *testing.T) {
	shirei.ResetInputSession()
	a := newInputAccum(false)

	a.setPoint(10, 20)
	a.sample()
	if m := shirei.GetFrameInput().Motion; m != (shirei.Vec2{}) {
		t.Fatalf("first sample Motion = %v, want zero", m)
	}
	if p := shirei.GetInputState().MousePoint; p != (shirei.Vec2{10, 20}) {
		t.Fatalf("MousePoint = %v, want {10,20}", p)
	}

	clearFrameInput()
	a.setPoint(40, 50)
	a.sample()
	want := shirei.Vec2{30, 30}
	if m := shirei.GetFrameInput().Motion; m != want {
		t.Fatalf("Motion = %v, want %v", m, want)
	}
	if p := shirei.GetInputState().MousePoint; p != (shirei.Vec2{40, 50}) {
		t.Fatalf("MousePoint = %v, want {40,50}", p)
	}
}

// TestPrimaryReleaseOutsideCanvasStillDeliversRelease: pointer capture on the
// DOM is what ensures primaryUp arrives after a drag outside the canvas; the
// accumulator must not drop that release based on coordinates (regression for
// stuck IsActive / drag-drop when the shell forgets to feed up).
func TestPrimaryReleaseOutsideCanvasStillDeliversRelease(t *testing.T) {
	shirei.ResetInputSession()
	a := newInputAccum(false)

	a.setPoint(100, 100)
	a.primaryDown()
	a.sample()
	if shirei.GetFrameInput().Mouse != shirei.MouseClick {
		t.Fatalf("down: Mouse = %v, want MouseClick", shirei.GetFrameInput().Mouse)
	}

	clearFrameInput()
	// Pointer moved outside the logical canvas; up still arrives (capture).
	a.setPoint(-20, 500)
	a.primaryUp()
	a.sample()
	if shirei.GetFrameInput().Mouse != shirei.MouseRelease {
		t.Fatalf("up outside: Mouse = %v, want MouseRelease", shirei.GetFrameInput().Mouse)
	}
	if p := shirei.GetInputState().MousePoint; p != (shirei.Vec2{-20, 500}) {
		t.Fatalf("MousePoint after outside up = %v", p)
	}
}

// TestCompositionSuppressesFrameKey: during IME composition, Backspace/arrows
// must not set FrameInput.Key (textinput trusts the backend and would delete
// from the document while the IME still owns the preedit).
func TestCompositionSuppressesFrameKey(t *testing.T) {
	shirei.ResetInputSession()
	a := newInputAccum(false)

	a.compositionStart()
	a.compositionUpdate("に")
	a.setMods(false, false, false, false)
	if a.keyDown("Backspace") {
		t.Fatal("Backspace during composition should not preventDefault via key path")
	}
	a.sample()
	if shirei.GetFrameInput().Key != 0 {
		t.Fatalf("Key = %v during composition, want 0", shirei.GetFrameInput().Key)
	}
	if shirei.GetInputState().Composition != "に" {
		t.Fatalf("Composition = %q, want に", shirei.GetInputState().Composition)
	}

	clearFrameInput()
	a.compositionEnd("日")
	a.sample()
	if shirei.GetFrameInput().Key != 0 {
		t.Fatalf("Key after composition end = %v, want 0", shirei.GetFrameInput().Key)
	}
	if shirei.GetFrameInput().Text != "日" {
		t.Fatalf("Text = %q, want 日", shirei.GetFrameInput().Text)
	}
	if shirei.GetInputState().Composition != "" {
		t.Fatalf("Composition still set: %q", shirei.GetInputState().Composition)
	}

	// After composition, Backspace is a normal edit key again.
	clearFrameInput()
	a.keyDown("Backspace")
	a.sample()
	if shirei.GetFrameInput().Key != shirei.KeyDeleteBackward {
		t.Fatalf("Key = %v, want KeyDeleteBackward", shirei.GetFrameInput().Key)
	}
}

// TestSpaceDeliversAsFrameKey: Space must appear on FrameInput.Key for hotkeys
// (games jump, etc.). Typed space still also arrives as Text via beforeinput
// when the hidden field is focused; editdecode ignores KeySpace for insert.
// preventDefault is false so beforeinput can still deliver " " into text fields.
func TestSpaceDeliversAsFrameKey(t *testing.T) {
	shirei.ResetInputSession()
	a := newInputAccum(false)
	a.setMods(false, false, false, false)
	if a.keyDown("Space") {
		t.Fatal("Space must not preventDefault (would block beforeinput space)")
	}
	a.sample()
	if shirei.GetFrameInput().Key != shirei.KeySpace {
		t.Fatalf("Key = %v, want KeySpace", shirei.GetFrameInput().Key)
	}
}

// TestMetaMapsToKeyCommandOnAppleHost: Cocoa uses KeyCommand for ⌘; web-Mac
// must put KeyCommand in DownKeys, not KeySuper.
func TestMetaMapsToKeyCommandOnAppleHost(t *testing.T) {
	if got := mapCode("MetaLeft", true); got != shirei.KeyCommand {
		t.Fatalf("apple MetaLeft = %v, want KeyCommand", got)
	}
	if got := mapCode("MetaRight", true); got != shirei.KeyCommand {
		t.Fatalf("apple MetaRight = %v, want KeyCommand", got)
	}
	if got := mapCode("MetaLeft", false); got != shirei.KeySuper {
		t.Fatalf("non-apple MetaLeft = %v, want KeySuper", got)
	}

	shirei.ResetInputSession()
	a := newInputAccum(true)
	a.setMods(false, false, false, true)
	a.keyDown("MetaLeft")
	a.sample()
	if !slices.Contains(shirei.GetInputState().DownKeys, shirei.KeyCommand) {
		t.Fatalf("DownKeys = %v, want KeyCommand", shirei.GetInputState().DownKeys)
	}
	if shirei.GetInputState().Modifiers&shirei.ModCmd == 0 {
		t.Fatal("mods missing ModCmd on apple meta")
	}
	if shirei.GetInputState().Modifiers&shirei.ModSuper != 0 {
		t.Fatal("mods must not set ModSuper on apple meta")
	}
}

// TestCmdADeliversEditShortcut: primary-mod letter chords set FrameInput.Key
// and request preventDefault so the browser does not select-all the page.
func TestCmdADeliversEditShortcut(t *testing.T) {
	shirei.ResetInputSession()
	a := newInputAccum(true)
	a.setMods(false, false, false, true) // meta → ModCmd
	if !a.keyDown("KeyA") {
		t.Fatal("Cmd+A should preventDefault")
	}
	a.sample()
	if shirei.GetFrameInput().Key != shirei.KeyA {
		t.Fatalf("Key = %v, want KeyA", shirei.GetFrameInput().Key)
	}
	if shirei.GetInputState().Modifiers&shirei.ModCmd == 0 {
		t.Fatal("expected ModCmd")
	}
}
