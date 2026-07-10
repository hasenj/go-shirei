package main

import (
	"testing"

	"go.hasen.dev/shirei"

	. "go.hasen.dev/shirei"
)

// TestSearchBoxCaretEditing guards that editing the query works: type, move the
// caret back with Left, and insert — the search box must stay focused and
// responsive (searching is explicit now, so nothing runs under the caret).
func TestSearchBoxCaretEditing(t *testing.T) {
	shirei.InitFontSubsystem()
	shirei.ResetInputSession()

	appData = &App{}
	scope := new(int)

	frame := func(text string, key KeyCode) {
		WindowSize = Vec2{800, 600}
		InputState.MousePoint = Vec2{-1000, -1000}
		FrameInput.Mouse = 0
		FrameInput.Scroll = Vec2{}
		FrameInput.Motion = Vec2{}
		FrameInput.Key = key
		FrameInput.Text = text
		RunFrameFn(func() {
			ModAttrs(func(a *AttrSet) { a.NoAnimate = true })
			ContainerWithKey(scope, Attrs(Viewport), RootView)
		})
	}

	for range 3 { // settle + let the search box grab focus
		frame("", 0)
	}
	for _, c := range "abcd" {
		frame(string(c), 0)
	}
	frame("", KeyLeft) // caret between c and d
	frame("X", 0)      // insert at the caret

	if appData.query != "abcXd" {
		t.Fatalf("query = %q, want %q — Left arrow did not move the caret", appData.query, "abcXd")
	}
}

// TestCloseTabViaClick guards the mutate-while-iterating crash: clicking a
// tab's × removes it from appData.searches, and doing that mid-range would
// hand the next SearchTab a nil element. Drives a real click on the × and
// checks the tab is gone without a panic.
