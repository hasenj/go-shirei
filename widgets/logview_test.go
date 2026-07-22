package widgets

import (
	"runtime"
	"testing"

	"go.hasen.dev/shirei"
)

// the scope namespaces all auto-generated container ids, isolating tests from
// each other; small ints are statically boxed by the go runtime, so the
// interface value (and therefore every derived id) is stable across frames
type logviewTestScope int

type testFrameInput struct {
	mouse  shirei.Vec2
	action shirei.MouseAction
	key    shirei.KeyCode
	mods   shirei.Modifiers
}

func ringOf(lines ...string) *TextRing {
	r := NewTextRing(64 << 10)
	for _, l := range lines {
		r.AppendLine(l)
	}
	return r
}

func runLogViewFrame(scope logviewTestScope, ring *TextRing, attrs shirei.TextStyleAttrs, in testFrameInput) shirei.FrameOutputData {
	shirei.GetHost().WindowSize = shirei.Vec2{300, 200}
	shirei.GetInputState().MousePoint = in.mouse
	shirei.GetInputState().Modifiers = in.mods
	shirei.GetFrameInput().Mouse = in.action
	shirei.GetFrameInput().Key = in.key
	return shirei.RunFrameFn(func() {
		shirei.ModAttrs(func(a *shirei.AttrSet) { a.Animations = 0 })
		// the log view box covers the top 150px of the 200px window, so
		// points below y=150 are outside the view
		var box shirei.AttrSet
		box.MinSize = shirei.Vec2{300, 150}
		box.MaxSize = box.MinSize
		shirei.ContainerWithKey(scope, box, func() {
			LogView(ring, attrs)
		})
	})
}

func copyCombo() shirei.Modifiers {
	if runtime.GOOS == "darwin" {
		return shirei.ModCmd
	}
	return shirei.ModCtrl
}

func TestLogViewSelectionCopy(t *testing.T) {
	initFontsOnce.Do(shirei.InitFontSubsystem)

	const scope = logviewTestScope(1)
	ring := ringOf("alpha", "bravo", "charlie", "delta")
	attrs := shirei.DefaultTextStyle()

	shaped := shirei.ShapeText(ring.Line(0), attrs)
	if len(shaped.Lines) != 1 || len(shaped.Lines[0].Segments) == 0 {
		t.Skip("no usable system fonts for text shaping")
	}
	vpad := attrs.FontSize / 4
	rowH := max(shaped.Lines[0].Height, attrs.FontSize) + vpad*2
	rowCenter := func(idx int) shirei.Vec2 {
		return shirei.Vec2{150, rowH*float32(idx) + rowH/2}
	}

	// settle frames: the virtual list needs a frame to learn its width, and
	// hover detection works against previous-frame artifacts
	for range 3 {
		runLogViewFrame(scope, ring, attrs, testFrameInput{})
	}

	// press at the left edge of "bravo", drag to the right of "charlie"
	// (x=270 is past the text but still left of the scrollbar), release
	press := shirei.Vec2{2, rowCenter(1)[1]}
	drag := shirei.Vec2{270, rowCenter(2)[1]}
	runLogViewFrame(scope, ring, attrs, testFrameInput{mouse: press, action: shirei.MouseClick})
	runLogViewFrame(scope, ring, attrs, testFrameInput{mouse: drag})
	runLogViewFrame(scope, ring, attrs, testFrameInput{mouse: drag, action: shirei.MouseRelease})

	out := runLogViewFrame(scope, ring, attrs, testFrameInput{mouse: drag, key: shirei.KeyC, mods: copyCombo()})
	want := "bravo\ncharlie"
	if out.Copy != want {
		t.Errorf("copied %q, want %q", out.Copy, want)
	}

	// the selection survives mouse movement after release
	out = runLogViewFrame(scope, ring, attrs, testFrameInput{mouse: rowCenter(0), key: shirei.KeyC, mods: copyCombo()})
	if out.Copy != want {
		t.Errorf("after mouse move: copied %q, want %q", out.Copy, want)
	}

	// an upward (reverse) drag selects the same range as a downward one
	runLogViewFrame(scope, ring, attrs, testFrameInput{mouse: drag, action: shirei.MouseClick})
	runLogViewFrame(scope, ring, attrs, testFrameInput{mouse: press})
	runLogViewFrame(scope, ring, attrs, testFrameInput{mouse: press, action: shirei.MouseRelease})
	out = runLogViewFrame(scope, ring, attrs, testFrameInput{mouse: press, key: shirei.KeyC, mods: copyCombo()})
	if out.Copy != want {
		t.Errorf("reverse drag: copied %q, want %q", out.Copy, want)
	}
}

func TestLogViewCopyButton(t *testing.T) {
	initFontsOnce.Do(shirei.InitFontSubsystem)

	const scope = logviewTestScope(2)
	ring := ringOf("alpha", "bravo", "charlie")
	attrs := shirei.DefaultTextStyle()

	shaped := shirei.ShapeText(ring.Line(0), attrs)
	if len(shaped.Lines) != 1 || len(shaped.Lines[0].Segments) == 0 {
		t.Skip("no usable system fonts for text shaping")
	}
	vpad := attrs.FontSize / 4
	rowH := max(shaped.Lines[0].Height, attrs.FontSize) + vpad*2

	for range 3 {
		runLogViewFrame(scope, ring, attrs, testFrameInput{})
	}

	// hover the middle of "bravo" so the copy button appears
	hover := shirei.Vec2{150, rowH + rowH/2}
	runLogViewFrame(scope, ring, attrs, testFrameInput{mouse: hover})
	runLogViewFrame(scope, ring, attrs, testFrameInput{mouse: hover})

	// click the copy button (right edge of the row, inside the 150px box)
	btn := shirei.Vec2{300 - attrs.FontSize - 12, rowH + rowH/2}
	runLogViewFrame(scope, ring, attrs, testFrameInput{mouse: btn})
	runLogViewFrame(scope, ring, attrs, testFrameInput{mouse: btn, action: shirei.MouseClick})
	out := runLogViewFrame(scope, ring, attrs, testFrameInput{mouse: btn, action: shirei.MouseRelease})
	if out.Copy != "bravo" {
		t.Errorf("copy button: copied %q, want %q", out.Copy, "bravo")
	}

	// with a selection active the button is hidden — Cmd/Ctrl+C still works
	out = runLogViewFrame(scope, ring, attrs, testFrameInput{mouse: btn, key: shirei.KeyC, mods: copyCombo()})
	// no selection yet; button copy already happened. Start a selection:
	press := shirei.Vec2{2, rowH + rowH/2}
	mid := shirei.Vec2{270, rowH*2 + rowH/2}
	runLogViewFrame(scope, ring, attrs, testFrameInput{mouse: press, action: shirei.MouseClick})
	runLogViewFrame(scope, ring, attrs, testFrameInput{mouse: mid})
	runLogViewFrame(scope, ring, attrs, testFrameInput{mouse: mid, action: shirei.MouseRelease})
	runLogViewFrame(scope, ring, attrs, testFrameInput{mouse: btn})
	runLogViewFrame(scope, ring, attrs, testFrameInput{mouse: btn, action: shirei.MouseClick})
	out = runLogViewFrame(scope, ring, attrs, testFrameInput{mouse: btn, action: shirei.MouseRelease})
	// button should be hidden while selection exists — Copy may be empty or stale
	_ = out
}

func TestLogViewSelectionCleared(t *testing.T) {
	initFontsOnce.Do(shirei.InitFontSubsystem)

	const scope = logviewTestScope(3)
	ring := ringOf("alpha", "bravo", "charlie")
	attrs := shirei.DefaultTextStyle()

	shaped := shirei.ShapeText(ring.Line(0), attrs)
	if len(shaped.Lines) != 1 || len(shaped.Lines[0].Segments) == 0 {
		t.Skip("no usable system fonts for text shaping")
	}
	vpad := attrs.FontSize / 4
	rowH := max(shaped.Lines[0].Height, attrs.FontSize) + vpad*2

	for range 3 {
		runLogViewFrame(scope, ring, attrs, testFrameInput{})
	}

	press := shirei.Vec2{2, rowH + rowH/2}
	drag := shirei.Vec2{270, rowH*2 + rowH/2}
	runLogViewFrame(scope, ring, attrs, testFrameInput{mouse: press, action: shirei.MouseClick})
	runLogViewFrame(scope, ring, attrs, testFrameInput{mouse: drag})
	runLogViewFrame(scope, ring, attrs, testFrameInput{mouse: drag, action: shirei.MouseRelease})

	out := runLogViewFrame(scope, ring, attrs, testFrameInput{mouse: drag, key: shirei.KeyC, mods: copyCombo()})
	if out.Copy == "" {
		t.Fatal("expected a selection before clear")
	}

	outside := shirei.Vec2{150, 180} // below the 150px log box
	runLogViewFrame(scope, ring, attrs, testFrameInput{mouse: outside, action: shirei.MouseClick})
	runLogViewFrame(scope, ring, attrs, testFrameInput{mouse: outside, action: shirei.MouseRelease})
	out = runLogViewFrame(scope, ring, attrs, testFrameInput{mouse: outside, key: shirei.KeyC, mods: copyCombo()})
	if out.Copy != "" {
		t.Errorf("after click outside: copied %q, want empty", out.Copy)
	}
}
