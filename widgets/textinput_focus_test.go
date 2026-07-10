package widgets

import (
	"testing"

	"go.hasen.dev/shirei"

	. "go.hasen.dev/shirei"
)

// TestTextInputFocusedRender: an input must survive gaining focus and being
// typed into. Regression for the ModAttrs-after-children panic: the input
// draws decoration child elements, and the focused-restyle ModAttrs must
// stay ahead of them — which no headless snapshot verifies, because nothing
// ever focuses an input in a snapshot.
func TestTextInputFocusedRender(t *testing.T) {
	initFontsOnce.Do(shirei.InitFontSubsystem)
	shirei.ResetInputSession()

	var buf string
	attrs := DefaultTextInputAttrs()
	attrs.NoAutoFocus = true
	view := func() {
		Container(Attrs(Pad(20)), func() {
			TextInputExt(&buf, attrs)
		})
	}

	scope := new(int)
	inside := Vec2{60, 40} // well within the input's min size + padding
	runSemFrame(scope, semFrameInput{mouse: offscreen}, view)
	runSemFrame(scope, semFrameInput{mouse: inside, action: MouseClick}, view)
	runSemFrame(scope, semFrameInput{mouse: inside, action: MouseRelease}, view)
	// the frame after focus is where the ModAttrs ordering bug lived
	runSemFrame(scope, semFrameInput{mouse: inside}, view)
	runSemFrame(scope, semFrameInput{mouse: inside, text: "hello"}, view)
	if buf != "hello" {
		t.Fatalf("typed text did not land in the buffer: %q", buf)
	}
}
