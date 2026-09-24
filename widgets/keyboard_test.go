package widgets

import (
	"testing"

	"go.hasen.dev/shirei"

	. "go.hasen.dev/shirei"
)

func kbFrame(scope any, key KeyCode, fn func()) {
	shirei.GetHost().WindowSize = Vec2{600, 400}
	shirei.GetInputState().MousePoint = offscreen
	shirei.GetInputState().Modifiers = 0
	shirei.GetFrameInput().Mouse = 0
	shirei.GetFrameInput().Scroll = Vec2{}
	shirei.GetFrameInput().Motion = Vec2{}
	shirei.GetFrameInput().Key = key
	shirei.GetFrameInput().Text = ""
	if key == 0 {
		shirei.GetInputState().DownKeys = nil
	} else {
		shirei.GetInputState().DownKeys = []KeyCode{key}
	}
	shirei.RunFrameFn(func() {
		shirei.ModAttrs(NoAnimate)
		shirei.ContainerWithKey(scope, Attrs(Viewport, Pad(12), Gap(8)), fn)
	})
}

func kbFocusFirst(scope any, fn func()) {
	kbFrame(scope, 0, fn)
	kbFrame(scope, KeyTab, fn)
	kbFrame(scope, 0, fn)
}

func kbTap(scope any, key KeyCode, fn func()) {
	kbFrame(scope, key, fn)
	kbFrame(scope, 0, fn)
}

func TestButtonSpaceAndEnterClick(t *testing.T) {
	initFontsOnce.Do(shirei.InitFontSubsystem)
	shirei.ResetInputSession()

	scope := new(int)
	var clicks int
	view := func() {
		if Button(NoIcon, "Go") {
			clicks++
		}
	}

	kbFocusFirst(scope, view)
	kbFrame(scope, KeySpace, view)
	if clicks != 0 {
		t.Fatalf("Space press should not click yet: clicks=%d", clicks)
	}
	kbFrame(scope, 0, view)
	if clicks != 1 {
		t.Fatalf("Space release on focused Button: clicks=%d, want 1", clicks)
	}
	kbTap(scope, KeyEnter, view)
	if clicks != 2 {
		t.Fatalf("Enter tap on focused Button: clicks=%d, want 2", clicks)
	}
}

func TestCheckBoxAndToggleSpace(t *testing.T) {
	initFontsOnce.Do(shirei.InitFontSubsystem)
	shirei.ResetInputSession()

	scope := new(int)
	on := false
	sw := false
	viewBox := func() { CheckBox(&on, "box") }
	viewSwitch := func() { ToggleSwitch(&sw) }

	kbFocusFirst(scope, viewBox)
	kbTap(scope, KeySpace, viewBox)
	if !on {
		t.Fatal("Space on focused CheckBox should turn it on")
	}
	kbTap(scope, KeySpace, viewBox)
	if on {
		t.Fatal("second Space on CheckBox should turn it off")
	}

	shirei.ResetInputSession()
	kbFocusFirst(scope, viewSwitch)
	kbTap(scope, KeySpace, viewSwitch)
	if !sw {
		t.Fatal("Space on focused ToggleSwitch should turn it on")
	}
}

func TestOptionButtonSpaceAssigns(t *testing.T) {
	initFontsOnce.Do(shirei.InitFontSubsystem)
	shirei.ResetInputSession()

	scope := new(int)
	choice := "a"
	view := func() {
		OptionGroup(&choice, func() {
			OptionButton("A", "a")
			OptionButton("B", "b")
		})
	}

	kbFrame(scope, 0, view)
	kbFrame(scope, KeyTab, view)
	kbFrame(scope, 0, view) // first radio
	kbFrame(scope, KeyTab, view)
	kbFrame(scope, 0, view) // second radio
	kbTap(scope, KeySpace, view)
	if choice != "b" {
		t.Fatalf("Space on second OptionButton: choice=%q, want b", choice)
	}
}

func TestSliderArrows(t *testing.T) {
	shirei.ResetInputSession()

	scope := new(int)
	v := float32(50)
	view := func() {
		Slider(&v, SliderAttrs{Min: 0, Max: 100, Step: 10, Width: 200})
	}

	kbFocusFirst(scope, view)
	kbFrame(scope, KeyRight, view)
	if v != 60 {
		t.Fatalf("Right: v=%v, want 60", v)
	}
	kbFrame(scope, KeyLeft, view)
	if v != 50 {
		t.Fatalf("Left: v=%v, want 50", v)
	}
	kbFrame(scope, KeyHome, view)
	if v != 0 {
		t.Fatalf("Home: v=%v, want 0", v)
	}
	kbFrame(scope, KeyEnd, view)
	if v != 100 {
		t.Fatalf("End: v=%v, want 100", v)
	}
}

func TestDisabledButtonSkipsTabRing(t *testing.T) {
	initFontsOnce.Do(shirei.InitFontSubsystem)
	shirei.ResetInputSession()

	scope := new(int)
	var clicks int
	var other ContainerId
	view := func() {
		if ButtonExt("No", ButtonAttrs{Disabled: true}, DefaultButtonLook()) {
			clicks++
		}
		other = Container(Attrs(Focusable, FixSize(20, 20)), func() {})
	}

	kbFocusFirst(scope, view)
	if !IdHasFocus(other) {
		t.Fatal("Tab should skip a disabled Button and land on the next Focusable")
	}
	kbFrame(scope, KeySpace, view)
	if clicks != 0 {
		t.Fatalf("disabled Button activated from keyboard: clicks=%d", clicks)
	}
}

func TestSegmentedControlArrows(t *testing.T) {
	initFontsOnce.Do(shirei.InitFontSubsystem)
	shirei.ResetInputSession()

	scope := new(int)
	mode := "a"
	var changes int
	view := func() {
		if SegmentedControl(&mode, func() {
			SegmentedCell("A", "a")
			SegmentedCell("B", "b")
			SegmentedCell("C", "c")
		}) {
			changes++
		}
	}

	kbFocusFirst(scope, view) // first cell
	kbFrame(scope, KeyRight, view)
	if mode != "b" {
		t.Fatalf("Right: mode=%q, want b", mode)
	}
	if changes != 1 {
		t.Fatalf("Right: changes=%d, want 1", changes)
	}
	kbFrame(scope, KeyRight, view)
	if mode != "c" {
		t.Fatalf("Right again: mode=%q, want c", mode)
	}
	kbFrame(scope, KeyRight, view)
	if mode != "a" {
		t.Fatalf("Right wrap: mode=%q, want a", mode)
	}
	kbFrame(scope, KeyLeft, view)
	if mode != "c" {
		t.Fatalf("Left wrap: mode=%q, want c", mode)
	}
}

func TestMenuKeyboardSelectsAndActivates(t *testing.T) {
	initFontsOnce.Do(shirei.InitFontSubsystem)
	shirei.ResetInputSession()

	scope := new(int)
	picked := ""
	view := func() {
		MenuButton(NoIcon, "File", func() {
			if MenuItem(NoIcon, "Open") {
				picked = "open"
			}
			if MenuItem(NoIcon, "Save") {
				picked = "save"
			}
		})
	}

	kbFocusFirst(scope, view)
	trigger := FocusedId()
	kbFrame(scope, KeyDown, view) // open, highlight first
	kbFrame(scope, KeyEnter, view)
	if picked != "open" {
		t.Fatalf("Down+Enter: picked=%q, want open", picked)
	}
	kbFrame(scope, 0, view)
	if !IdHasVisibleFocus(trigger) {
		t.Fatal("keyboard menu activation must return visible focus to its trigger")
	}

	picked = ""
	shirei.ResetInputSession()
	kbFocusFirst(scope, view)
	kbTap(scope, KeySpace, view) // open, nothing highlighted
	kbFrame(scope, KeyDown, view)
	kbFrame(scope, KeyDown, view)
	kbFrame(scope, KeyEnter, view)
	if picked != "save" {
		t.Fatalf("Space+Down+Down+Enter: picked=%q, want save", picked)
	}
}

func TestMenuTabClosesAndMovesOn(t *testing.T) {
	initFontsOnce.Do(shirei.InitFontSubsystem)
	shirei.ResetInputSession()

	scope := new(int)
	var other ContainerId
	view := func() {
		MenuButton(NoIcon, "File", func() {
			MenuItem(NoIcon, "Open")
			MenuItem(NoIcon, "Save")
		})
		other = Container(Attrs(Focusable, FixSize(20, 20)), func() {})
	}

	kbFocusFirst(scope, view)
	kbTap(scope, KeySpace, view)
	kbFrame(scope, KeyTab, view)
	kbFrame(scope, 0, view)
	if !IdHasFocus(other) {
		t.Fatal("Tab from an open menu should close it and land on the next Focusable")
	}
}

func TestMenuEscapeReturnsToTrigger(t *testing.T) {
	initFontsOnce.Do(shirei.InitFontSubsystem)
	shirei.ResetInputSession()

	scope := new(int)
	var other ContainerId
	view := func() {
		MenuButton(NoIcon, "File", func() {
			MenuItem(NoIcon, "Open")
		})
		other = Container(Attrs(Focusable, FixSize(20, 20)), func() {})
	}

	kbFocusFirst(scope, view)
	kbTap(scope, KeySpace, view)
	kbFrame(scope, KeyEscape, view)
	kbFrame(scope, 0, view)
	if IdHasFocus(other) {
		t.Fatal("Escape should close the menu without Tabbing to the next control")
	}
	if !IdHasVisibleFocus(FocusedId()) {
		t.Fatal("Escape must restore a visible focus indicator")
	}
}

func TestPopupPanelEscapeCloses(t *testing.T) {
	initFontsOnce.Do(shirei.InitFontSubsystem)
	shirei.ResetInputSession()

	scope := new(int)
	open := true
	var anchor ContainerId
	view := func() {
		anchor = Container(Attrs(Focusable, FixSize(40, 20)), func() {})
		if open {
			PopupPanel(&open, anchor, Attrs(Pad(8), MinWidth(80)), func() {
				Label("panel")
			})
		}
	}

	kbFrame(scope, 0, view)
	kbFrame(scope, 0, view)
	if !open {
		t.Fatal("setup: panel should stay open")
	}
	kbFrame(scope, KeyEscape, view)
	kbFrame(scope, 0, view)
	if open {
		t.Fatal("Escape should close PopupPanel")
	}
	if !IdHasVisibleFocus(anchor) {
		t.Fatal("Escape must restore the anchor's focus indicator")
	}
}

func TestTableHeaderKeyboardSort(t *testing.T) {
	initFontsOnce.Do(shirei.InitFontSubsystem)
	shirei.ResetInputSession()

	scope := new(int)
	rows := []*semRow{
		{"zeta", 9},
		{"alpha", 1},
		{"mid", 5},
	}
	rowIds := map[*semRow]shirei.ContainerId{}
	cols := []TableColumn[*semRow]{
		{
			Label: "Name",
			Cell:  func(r *semRow) { rowIds[r] = CurrentId(); Label(r.Name) },
			Less:  func(a, b *semRow) bool { return a.Name < b.Name },
		},
		{
			Label: "Val", Width: 80, DefaultDesc: true,
			Cell: func(r *semRow) { Label("v") },
			Less: func(a, b *semRow) bool { return a.Val < b.Val },
		},
	}
	view := func() {
		Table(nil, 24, cols, rows, func(r *semRow) any { return r }, 1)
	}
	rowY := func(r *semRow) float32 { return GetScreenRectOf(rowIds[r]).Origin[1] }

	kbFrame(scope, 0, view)
	kbFrame(scope, 0, view)
	if !(rowY(rows[0]) < rowY(rows[1])) {
		t.Fatalf("setup: default Val-desc (zeta above alpha); zeta=%v alpha=%v",
			rowY(rows[0]), rowY(rows[1]))
	}

	kbFocusFirst(scope, view) // Name header
	kbFrame(scope, KeySpace, view)
	kbFrame(scope, 0, view)
	if !(rowY(rows[1]) < rowY(rows[0])) {
		t.Fatalf("Space on Name header should sort alpha above zeta; alpha=%v zeta=%v",
			rowY(rows[1]), rowY(rows[0]))
	}
}

func TestMenuButtonCheckboxesTabIntoPanel(t *testing.T) {
	initFontsOnce.Do(shirei.InitFontSubsystem)
	shirei.ResetInputSession()

	scope := new(int)
	on := false
	var inner, page ContainerId
	view := func() {
		MenuButton(NoIcon, "Opts", func() {
			CheckBox(&on, "Author name")
			inner = GetLastId()
		})
		page = Container(Attrs(Focusable, FixSize(20, 20)), func() {})
	}

	kbFocusFirst(scope, view) // trigger
	kbTap(scope, KeySpace, view)
	kbFrame(scope, 0, view) // first-stop lands
	if !IdHasFocus(inner) {
		t.Fatal("opening a CheckBox menu should move focus to the first checkbox")
	}
	if !IdHasVisibleFocus(inner) {
		t.Fatal("a keyboard-opened popup must preserve the focus indicator")
	}
	kbTap(scope, KeySpace, view)
	if !on {
		t.Fatal("Space on the focused checkbox should toggle it")
	}
	kbFrame(scope, KeyTab, view)
	kbFrame(scope, 0, view)
	if !IdHasFocus(page) {
		t.Fatal("Tab from the last inner stop should reach the next page control")
	}
}

func TestPopupPanelTabEntersContents(t *testing.T) {
	initFontsOnce.Do(shirei.InitFontSubsystem)
	shirei.ResetInputSession()

	scope := new(int)
	open := false
	on := false
	var trigger, inner, page ContainerId
	view := func() {
		trigger = Container(Attrs(), func() {
			st := ProcessButtonEvents(false)
			if st.Clicked {
				open = true
			}
		})
		page = Container(Attrs(Focusable, FixSize(20, 20)), func() {})
		if open {
			PopupPanel(&open, trigger, Attrs(Pad(8), MinWidth(80)), func() {
				CheckBox(&on, "Author name")
				inner = GetLastId()
			})
		}
	}

	kbFocusFirst(scope, view)
	kbTap(scope, KeySpace, view)
	kbFrame(scope, 0, view)
	if !open {
		t.Fatal("setup: panel should be open")
	}
	if !IdHasFocus(inner) {
		t.Fatal("opening PopupPanel should first-stop on the checkbox")
	}
	kbTap(scope, KeySpace, view)
	if !on {
		t.Fatal("Space should toggle the checkbox inside PopupPanel")
	}
	kbFrame(scope, KeyTab, view)
	kbFrame(scope, 0, view)
	if !IdHasFocus(page) {
		t.Fatal("Tab-out should land on the page control")
	}
	if open {
		t.Fatal("Tab-out should close PopupPanel (focus left the panel)")
	}
}

func TestTabReachesFileSelectorThenMovesOn(t *testing.T) {
	initFontsOnce.Do(shirei.InitFontSubsystem)
	shirei.ResetInputSession()

	scope := new(int)
	sel := ""
	cands := []string{"a.go", "b.go"}
	var other ContainerId
	view := func() {
		FileSelector(FileSelectorAttrs{
			Selection:  &sel,
			Candidates: cands,
			Width:      200,
			MaxRows:    4,
		})
		other = Container(Attrs(Focusable, FixSize(20, 20)), func() {})
	}

	kbFrame(scope, 0, view)
	kbFrame(scope, 0, view)
	if IdHasFocus(other) {
		t.Fatal("setup: FileSelector query should AutoFocus, not the trailing control")
	}
	kbFrame(scope, KeyTab, view)
	kbFrame(scope, 0, view)
	if !IdHasFocus(other) {
		t.Fatal("Tab from FileSelector query should land on the next Focusable")
	}
}
