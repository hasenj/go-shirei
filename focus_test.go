package shirei

import "testing"

func TestFocusIndicatorRequests(t *testing.T) {
	ResetInputSession()
	defer ResetInputSession()
	var first, second ContainerId
	var visibleInside bool
	var request func()
	var deferredFocus bool
	view := func() {
		if request != nil {
			request()
			request = nil
		}
		first = Container(Attrs(Focusable, FixSize(40, 30)), func() {
			FocusOnClick()
			if deferredFocus {
				Focus()
				deferredFocus = false
			}
			visibleInside = HasVisibleFocus()
		})
		second = Container(Attrs(Focusable, FixSize(40, 30)), func() {
			FocusOnClick()
		})
	}
	for range 3 {
		focusTestFrame(view)
	}
	check := func(id ContainerId, visible bool) {
		t.Helper()
		focusTestFrame(view)
		if !IdHasFocus(id) || IdHasVisibleFocus(id) != visible {
			t.Fatalf("focus=%v visible=%v, want focused with visible=%v", IdHasFocus(id), IdHasVisibleFocus(id), visible)
		}
		if visibleInside != IdHasVisibleFocus(first) {
			t.Fatal("current-container and ID visibility queries disagree")
		}
	}
	request = func() { FocusImmediateOn(first) }
	check(first, false)
	focusTestTab(view, false)
	check(second, true)
	focusTestTab(view, true)
	check(first, true)

	// Clicking the same focused control suppresses the cue without blurring it.
	ui.Host.Input.MousePoint = Vec2{10, 10}
	ui.Host.FrameInput.Mouse = MouseClick
	check(first, false)
	ui.Host.FrameInput.Mouse = MouseRelease
	check(first, false)
	ui.Host.FrameInput.Key = KeyA
	check(first, false) // unrelated keys do not decide how focus is presented
	ShowFocusIndicator()
	check(first, true)
	ui.Host.FrameInput.Motion = Vec2{10, 0}
	ui.Host.FrameInput.Scroll = Vec2{0, 5}
	check(first, true)

	request = func() { FocusImmediateOn(second) }
	check(second, false)
	request = func() {
		FocusImmediateOn(first)
		ShowFocusIndicator()
	}
	check(first, true)
	request = func() { FocusImmediateOn(first) }
	check(first, false) // every explicit request clears the flag, even for this target
	TabFrom(first)
	check(second, true)
	deferredFocus = true
	focusTestFrame(view)
	check(first, false)
	ResetInputSession()
	if ui.focusVisible {
		t.Fatal("focus visibility leaks into a fresh input session")
	}
}

func TestRunFrameTabWithNoFocusableControls(t *testing.T) {
	tests := []struct {
		name      string
		modifiers Modifiers
	}{
		{name: "Tab"},
		{name: "Shift+Tab", modifiers: ModShift},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ResetInputSession()
			RunFrameFn(func() {}) // establish an empty previous frame

			ui.Host.Input.Modifiers = tt.modifiers
			ui.Host.FrameInput.Key = KeyTab
			RunFrameFn(func() {})

			if ui.nextFocused != nil {
				t.Fatal("Tab with no focusable controls scheduled focus")
			}
		})
	}
}

func focusTestFrame(view FrameFn) {
	ui.Host.WindowSize = Vec2{400, 300}
	RunFrameFn(view)
}

func focusTestTab(view FrameFn, shift bool) {
	if shift {
		ui.Host.Input.Modifiers = ModShift
	}
	ui.Host.FrameInput.Key = KeyTab
	focusTestFrame(view)
	ui.Host.Input.Modifiers = 0
}

func TestTabCyclesFocusableInSourceOrder(t *testing.T) {
	ResetInputSession()
	var a, b, c ContainerId
	view := func() {
		a = Container(Attrs(Focusable, FixSize(10, 10)), func() {})
		b = Container(Attrs(Focusable, FixSize(10, 10)), func() {})
		c = Container(Attrs(Focusable, FixSize(10, 10)), func() {})
	}
	focusTestFrame(view)

	focusTestTab(view, false)
	focusTestFrame(view)
	if !IdHasFocus(a) {
		t.Fatal("Tab from nothing should land on the first focusable")
	}

	focusTestTab(view, false)
	focusTestFrame(view)
	if !IdHasFocus(b) {
		t.Fatal("Tab should move to the second focusable")
	}

	focusTestTab(view, false)
	focusTestFrame(view)
	if !IdHasFocus(c) {
		t.Fatal("Tab should move to the third focusable")
	}

	focusTestTab(view, false)
	focusTestFrame(view)
	if !IdHasFocus(a) {
		t.Fatal("Tab should wrap to the first focusable")
	}

	focusTestTab(view, true)
	focusTestFrame(view)
	if !IdHasFocus(c) {
		t.Fatal("Shift+Tab should wrap to the last focusable")
	}
}

func TestTabOrderIgnoresZ(t *testing.T) {
	ResetInputSession()
	var first, second ContainerId
	view := func() {
		first = Container(Attrs(Focusable, InFront, FixSize(10, 10)), func() {})
		second = Container(Attrs(Focusable, FixSize(10, 10)), func() {})
	}
	focusTestFrame(view)
	focusTestTab(view, false)
	focusTestFrame(view)
	if !IdHasFocus(first) {
		t.Fatal("Tab order follows source order, not InFront paint order")
	}
	focusTestTab(view, false)
	focusTestFrame(view)
	if !IdHasFocus(second) {
		t.Fatal("second source-order sibling should be next, even if first paints in front")
	}
}

func TestModalTrapExcludesBackground(t *testing.T) {
	ResetInputSession()
	var bg, inner ContainerId
	open := true
	view := func() {
		bg = Container(Attrs(Focusable, FixSize(20, 20)), func() {})
		if open {
			Modal(200, func() { open = false }, func() {
				inner = Container(Attrs(Focusable, FixSize(20, 20)), func() {})
			})
		}
	}

	focusTestFrame(view) // trap mounts, steals, first-stop schedules inner
	focusTestFrame(view) // first-stop lands
	if IdHasFocus(bg) {
		t.Fatal("background must not keep focus while the modal is open")
	}
	if !IdHasFocus(inner) {
		t.Fatal("newly mounted modal should move focus to its first focusable")
	}

	focusTestTab(view, false)
	focusTestFrame(view)
	if IdHasFocus(bg) {
		t.Fatal("Tab must not leave the modal trap for the background")
	}
	if !IdHasFocus(inner) {
		t.Fatal("sole trap focusable should keep focus across Tab")
	}

	open = false
	focusTestFrame(view)
	focusTestTab(view, false)
	focusTestFrame(view)
	if !IdHasFocus(bg) {
		t.Fatal("after dismiss, Tab should reach the background control")
	}
}

func TestTabAfterSplicesSubtreeAfterTarget(t *testing.T) {
	ResetInputSession()
	var a, b, c, d ContainerId
	view := func() {
		a = Container(Attrs(Focusable, FixSize(10, 10)), func() {})
		b = Container(Attrs(Focusable, FixSize(10, 10)), func() {})
		Container(Attrs(TabAfter(a), Gap(2)), func() {
			c = Container(Attrs(Focusable, FixSize(10, 10)), func() {})
			d = Container(Attrs(Focusable, FixSize(10, 10)), func() {})
		})
	}
	focusTestFrame(view)

	focusTestTab(view, false)
	focusTestFrame(view)
	if !IdHasFocus(a) {
		t.Fatal("Tab from nothing should land on a")
	}
	focusTestTab(view, false)
	focusTestFrame(view)
	if !IdHasFocus(c) {
		t.Fatal("TabAfter(a) should put c immediately after a, not b")
	}
	focusTestTab(view, false)
	focusTestFrame(view)
	if !IdHasFocus(d) {
		t.Fatal("Tab should continue through the TabAfter subtree")
	}
	focusTestTab(view, false)
	focusTestFrame(view)
	if !IdHasFocus(b) {
		t.Fatal("after the TabAfter run, Tab should reach b")
	}
}

func TestTabAfterFirstStopOnMount(t *testing.T) {
	ResetInputSession()
	var trigger, inner, page ContainerId
	open := false
	view := func() {
		trigger = Container(Attrs(Focusable, FixSize(10, 10)), func() {
			if FirstRender() {
				Focus()
			}
		})
		page = Container(Attrs(Focusable, FixSize(10, 10)), func() {})
		if open {
			Container(Attrs(TabAfter(trigger)), func() {
				inner = Container(Attrs(Focusable, FixSize(10, 10)), func() {})
			})
		}
	}
	focusTestFrame(view)
	focusTestFrame(view)
	if !IdHasFocus(trigger) {
		t.Fatal("setup: trigger should have focus")
	}
	open = true
	focusTestFrame(view) // TabAfter mounts, first-stop schedules inner
	focusTestFrame(view)
	if !IdHasFocus(inner) {
		t.Fatal("newly mounted TabAfter subtree should take focus from the trigger")
	}
	if IdHasFocus(page) {
		t.Fatal("first-stop should not land on the page control")
	}
}

func TestTabAfterInheritsFocusTrap(t *testing.T) {
	ResetInputSession()
	var bg, trigger, inner ContainerId
	open := true
	panel := false
	view := func() {
		bg = Container(Attrs(Focusable, FixSize(20, 20)), func() {})
		if open {
			Modal(200, func() { open = false }, func() {
				trigger = Container(Attrs(Focusable, FixSize(20, 20)), func() {})
				if panel {
					Popup(func() {
						Container(Attrs(TabAfter(trigger)), func() {
							inner = Container(Attrs(Focusable, FixSize(20, 20)), func() {})
						})
					})
				}
			})
		}
	}
	focusTestFrame(view)
	focusTestFrame(view)
	if IdHasFocus(bg) {
		t.Fatal("setup: modal should trap focus")
	}
	panel = true
	focusTestFrame(view)
	focusTestFrame(view)
	if inner == nil {
		t.Fatal("setup: panel inner was not built")
	}
	if !IdHasFocus(inner) && !IdHasFocus(trigger) {
		t.Fatal("panel inner must be allowed in the modal trap (TabAfter inherits trap owner)")
	}
	// From trigger, Tab should reach inner, not wrap to trigger-only or leak to bg.
	if IdHasFocus(trigger) {
		focusTestTab(view, false)
		focusTestFrame(view)
	}
	if IdHasFocus(bg) {
		t.Fatal("Tab must not leak to the background through a TabAfter popup")
	}
	if !IdHasFocus(inner) && !IdHasFocus(trigger) {
		t.Fatal("focus should stay on trigger or inner, not leave the trap")
	}
}

func TestIdHasFocusWithin(t *testing.T) {
	ResetInputSession()
	var root, child ContainerId
	view := func() {
		root = Container(Attrs(Pad(4)), func() {
			child = Container(Attrs(Focusable, FixSize(10, 10)), func() {
				if FirstRender() {
					Focus()
				}
			})
		})
	}
	focusTestFrame(view)
	focusTestFrame(view)
	if !IdHasFocus(child) {
		t.Fatal("setup: child should have focus")
	}
	if !IdHasFocusWithin(root) {
		t.Fatal("IdHasFocusWithin(root) when a descendant is focused")
	}
	if !IdHasFocusWithin(child) {
		t.Fatal("IdHasFocusWithin includes the node itself")
	}
}
