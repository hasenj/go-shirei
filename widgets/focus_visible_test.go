package widgets

import (
	"testing"

	. "go.hasen.dev/shirei"
)

// Exercise the real control paint and input paths across multiple frames.
func TestFocusIndicatorPaint(t *testing.T) {
	initFontsOnce.Do(InitFontSubsystem)
	oldScheme := CurrentColorScheme
	defer func() { CurrentColorScheme = oldScheme; ResetInputSession() }()
	CurrentColorScheme = LightColorScheme()
	ring := CurrentColorScheme.FocusRing
	checked := false
	value := float32(50)
	choice := "one"
	text := "edit"
	for _, tc := range []struct {
		name    string
		view    func()
		key     KeyCode
		editing bool
	}{
		{"button", func() { Button(NoIcon, "Button") }, KeySpace, false},
		{"checkbox", func() { CheckBox(&checked, "Check") }, KeySpace, false},
		{"slider", func() { Slider(&value, SliderAttrs{Min: 0, Max: 100, Step: 1}) }, KeyRight, false},
		{"segment", func() {
			SegmentedControl(&choice, func() {
				NextAccessName("control")
				SegmentedCell("One", "one")
				SegmentedCell("Two", "two")
			})
		}, KeyRight, false},
		{"text", func() { TextInput(&text) }, KeyRight, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ResetInputSession()
			GetHost().WindowSize = Vec2{400, 150}
			scope := new(int)
			view := func() {
				ModAttrs(NoAnimate)
				ContainerWithKey(scope, Attrs(Pad(12)), func() {
					if tc.name != "segment" {
						NextAccessName("control")
					}
					tc.view()
				})
			}
			var out FrameOutputData
			frame := func() { out = RunFrameFn(view) }
			for range 4 {
				frame()
			}
			n, ok := QueryContainer("control")
			if !ok {
				t.Fatal("missing control")
			}
			bounds := n.Bounds
			checkPaint := func(want bool) {
				t.Helper()
				edge := ring
				if !tc.editing {
					edge[3] *= .5
				}
				found := false
				for _, s := range out.Surfaces {
					if s.Stroke > 0 && s.Color1 == edge {
						found = true
						if !tc.editing && s.Stroke != 2 {
							t.Fatal("focus edge is not two logical points")
						}
					}
				}
				if found != want {
					t.Fatalf("focus paint=%v, want %v", found, want)
				}
				if after, _ := QueryContainer("control"); after.Bounds != bounds {
					t.Fatal("focus indicator changes control geometry")
				}
			}
			click := func() {
				GetInputState().MousePoint = Vec2Add(bounds.Origin, Vec2Mul(bounds.Size, .5))
				GetFrameInput().Mouse = MouseClick
				frame()
				GetFrameInput().Mouse = MouseRelease
				frame()
				frame()
			}
			click()
			if !IdHasFocus(n.Container) {
				t.Fatal("click loses actual keyboard focus")
			}
			checkPaint(tc.editing)
			GetFrameInput().Key = tc.key
			frame()
			frame()
			checkPaint(true)
			click()
			checkPaint(tc.editing)
			GetFrameInput().Key = KeyTab
			frame()
			frame()
			checkPaint(true)
			GetFrameInput().Motion = Vec2{1, 0}
			frame()
			checkPaint(true)
			click()
			GetFrameInput().AccessAction = AccessAction{ID: n.ID, Kind: AccessFocus}
			frame()
			frame()
			checkPaint(true)
		})
	}
}
