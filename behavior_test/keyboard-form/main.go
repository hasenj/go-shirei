// Behavior test: a stock-widget form is fully operable with keyboard only.
//
// Drive Tabs through the controls, activates with Space/Enter, moves a
// slider and segmented control with arrows, opens a menu and a modal, and
// checks the modal trap does not leak Tab to a background control.
//
//	go -C behavior_test run ./keyboard-form
//	go -C behavior_test run ./keyboard-form --close
//	go -C behavior_test run ./keyboard-form --manual
package main

import (
	"flag"
	"fmt"
	"os"

	"go.hasen.dev/shirei/app"
	"go.hasen.dev/shirei/behavior_test/btmode"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

const winW, winH float32 = 560, 720

// Visible pause between script beats (~0.25s at 60Hz).
const windowHoldFrames = 16

var (
	mode *btmode.Mode

	verdictDone   bool
	verdictOK     bool
	verdictDetail string

	name      string
	checked   bool
	radio             = "a"
	slider    float32 = 50
	seg               = 10
	did       bool
	menuPick  string
	modalOpen bool
	modalOK   bool

	nameId   ContainerId
	checkId  ContainerId
	optAId   ContainerId
	optBId   ContainerId
	sliderId ContainerId
	doId     ContainerId
	openId   ContainerId
	cancelId ContainerId
	okId     ContainerId
	bgId     ContainerId

	status   = "settling"
	stepI    int
	fired    bool
	holdLeft int
	holdN    = windowHoldFrames
)

func main() {
	mode = btmode.RegisterFlags(nil)
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: go -C behavior_test run ./keyboard-form [flags]\n\n%s", btmode.FlagHelp())
	}
	flag.Parse()
	mode.AfterParse()
	if err := mode.Validate(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	fmt.Println("=== behavior_test: keyboard-form ===")

	if mode.Drive {
		status = "settle"
		holdLeft = holdN
	} else {
		status = "manual — Tab / Space / arrows / Esc"
	}
	app.SetupWindow("behavior_test: keyboard-form", int(winW), int(winH))
	app.Run(frameFn)
}

func park() {
	GetInputState().MousePoint = Vec2{-1000, -1000}
	GetInputState().Modifiers = 0
	GetFrameInput().Mouse = 0
	GetFrameInput().Scroll = Vec2{}
	GetFrameInput().Motion = Vec2{}
	GetFrameInput().Key = 0
	GetFrameInput().Text = ""
}

func frameFn() {
	ModAttrs(NoAnimate)

	if mode.Drive && !verdictDone {
		driveBeforeUI()
	}

	formUI()

	if mode.Drive {
		if !verdictDone {
			driveAfterUI()
			RequestNextFrame()
		}
		btmode.VerdictBanner(verdictDone, verdictOK, verdictDetail)
		mode.TickClose(verdictDone, verdictOK)
		if verdictDone && !mode.Close {
			RequestNextFrame()
		}
	}
}

func formUI() {
	Container(Attrs(Viewport, Background(220, 12, 96, 1), Pad(20), Gap(10)), func() {
		Label("behavior_test: keyboard-form", FontWeight(WeightBold), FontSize(16))
		Label(status, FontSize(12), TextColor(0, 0, 40, 1))
		Label(fmt.Sprintf("name=%q check=%v radio=%s slider=%.0f seg=%d did=%v menu=%s modal=%v ok=%v",
			name, checked, radio, slider, seg, did, menuPick, modalOpen, modalOK),
			FontSize(11), TextColor(0, 0, 45, 1))

		attrs := DefaultTextInputAttrs()
		attrs.NoAutoFocus = true
		attrs.MinWidth = 280
		attrs.Placeholder = "name"
		TextInputExt(&name, attrs)
		nameId = GetLastId()

		CheckBox(&checked, "Subscribe")
		checkId = GetLastId()

		OptionGroup(&radio, func() {
			OptionButton("Alpha", "a")
			optAId = GetLastId()
			OptionButton("Beta", "b")
			optBId = GetLastId()
		})

		Slider(&slider, SliderAttrs{Min: 0, Max: 100, Step: 10, Width: 280})
		sliderId = GetLastId()

		SegmentedControl(&seg, func() {
			SegmentedCell("X", 10)
			SegmentedCell("Y", 20)
			SegmentedCell("Z", 30)
		})

		if Button(NoIcon, "Do") {
			did = true
		}
		doId = GetLastId()

		MenuButton(NoIcon, "File", func() {
			if MenuItem(NoIcon, "Open") {
				menuPick = "open"
			}
			if MenuItem(NoIcon, "Save") {
				menuPick = "save"
			}
		})

		if Button(NoIcon, "Open modal") {
			modalOpen = true
			modalOK = false
		}
		openId = GetLastId()

		bgId = Container(Attrs(Focusable, Pad(6), BorderWidth(1), BorderColor(0, 0, 80, 1), Corners(4)), func() {
			Label("background probe", FontSize(12))
		})

		if modalOpen {
			Modal(320, func() { modalOpen = false }, func() {
				Label("Confirm", FontWeight(WeightBold), FontSize(14))
				Label("Tab should stay in this card.", FontSize(12), TextColor(0, 0, 40, 1))
				Container(Attrs(Row, Gap(8)), func() {
					if Button(NoIcon, "Cancel") {
						modalOpen = false
					}
					cancelId = GetLastId()
					if ButtonExt("OK", ButtonAttrs{Accent: AccentMeadow}, DefaultButtonLook()) {
						modalOK = true
						modalOpen = false
					}
					okId = GetLastId()
				})
			})
		}
	})
}

func fail(detail string) {
	park()
	verdictDone = true
	verdictOK = false
	verdictDetail = detail
	status = "FAIL: " + detail
	fmt.Printf("FAIL %s\n", detail)
}

func passAll() {
	park()
	verdictDone = true
	verdictOK = true
	verdictDetail = "all cases passed"
	status = "PASS: all cases"
}

func requireFocus(id ContainerId, what string) error {
	if id == nil {
		return fmt.Errorf("%s id is nil", what)
	}
	if !IdHasFocus(id) {
		return fmt.Errorf("want focus on %s", what)
	}
	return nil
}

func requireNotFocus(id ContainerId, what string) error {
	if id != nil && IdHasFocus(id) {
		return fmt.Errorf("focus leaked to %s", what)
	}
	return nil
}

type step struct {
	name  string
	key   KeyCode // KeyTab → Tab(); 0 → assert only
	check func() error
}

func steps() []step {
	return []step{
		{"settle", 0, func() error {
			if nameId == nil || checkId == nil || optAId == nil || optBId == nil ||
				sliderId == nil || doId == nil || openId == nil || bgId == nil {
				return fmt.Errorf("control ids not captured")
			}
			return nil
		}},
		{"tab-name", KeyTab, func() error { return requireFocus(nameId, "name field") }},
		{"tab-check", KeyTab, func() error { return requireFocus(checkId, "checkbox") }},
		{"space-check", KeySpace, func() error {
			if !checked {
				return fmt.Errorf("Space did not check the box")
			}
			return nil
		}},
		{"tab-radio-a", KeyTab, func() error { return requireFocus(optAId, "radio Alpha") }},
		{"tab-radio-b", KeyTab, func() error { return requireFocus(optBId, "radio Beta") }},
		{"space-radio", KeySpace, func() error {
			if radio != "b" {
				return fmt.Errorf("Space on Beta: radio=%q, want b", radio)
			}
			return nil
		}},
		{"tab-slider", KeyTab, func() error { return requireFocus(sliderId, "slider") }},
		{"slider-right", KeyRight, func() error {
			if slider != 60 {
				return fmt.Errorf("Right on slider: %.0f, want 60", slider)
			}
			return nil
		}},
		{"tab-seg", KeyTab, func() error {
			if IdHasFocus(sliderId) || IdHasFocus(doId) {
				return fmt.Errorf("Tab from slider should land on a segmented cell")
			}
			return nil
		}},
		{"seg-right", KeyRight, func() error {
			if slider != 60 {
				return fmt.Errorf("Right on segmented cell moved the slider")
			}
			if seg != 20 {
				return fmt.Errorf("Right on segmented: seg=%d, want 20", seg)
			}
			return nil
		}},
		{"tab-seg-z", KeyTab, nil},
		{"tab-do", KeyTab, func() error { return requireFocus(doId, "Do button") }},
		{"space-do", KeySpace, func() error {
			if !did {
				return fmt.Errorf("Space did not activate Do")
			}
			return nil
		}},
		{"tab-menu", KeyTab, nil},
		{"space-menu", KeySpace, nil},
		{"menu-down", KeyDown, nil},
		{"menu-down-2", KeyDown, nil},
		{"menu-enter", KeyEnter, func() error {
			if menuPick != "save" {
				return fmt.Errorf("menu pick=%q, want save", menuPick)
			}
			return nil
		}},
		{"tab-open-modal", KeyTab, func() error { return requireFocus(openId, "Open modal") }},
		{"space-open-modal", KeySpace, func() error {
			if !modalOpen {
				return fmt.Errorf("Space did not open the modal")
			}
			return requireNotFocus(bgId, "background probe")
		}},
		{"modal-tab", KeyTab, func() error {
			if !modalOpen {
				return fmt.Errorf("modal closed during Tab")
			}
			if err := requireNotFocus(bgId, "background probe"); err != nil {
				return err
			}
			if IdHasFocus(openId) {
				return fmt.Errorf("Tab leaked to Open modal behind the trap")
			}
			return nil
		}},
		{"modal-esc", KeyEscape, func() error {
			if modalOpen {
				return fmt.Errorf("Escape did not close the modal")
			}
			return nil
		}},
		{"focus-open-again", 0, func() error {
			FocusImmediateOn(openId)
			return nil
		}},
		{"tab-bg", KeyTab, func() error { return requireFocus(bgId, "background probe") }},
	}
}

func driveBeforeUI() {
	park()
	if verdictDone || stepI >= len(steps()) {
		return
	}
	st := steps()[stepI]
	if !fired && st.key != 0 && st.key != KeyTab {
		GetFrameInput().Key = st.key
		fired = true
		holdLeft = holdN
	}
}

func driveAfterUI() {
	if holdLeft > 0 {
		holdLeft--
		return
	}
	all := steps()
	if stepI >= len(all) {
		fmt.Println("PASS all cases")
		passAll()
		return
	}
	st := all[stepI]
	if !fired {
		if st.key == KeyTab {
			Tab()
		}
		fired = true
		holdLeft = holdN
		status = st.name
		return
	}
	if st.check != nil {
		if err := st.check(); err != nil {
			fail(st.name + ": " + err.Error())
			return
		}
	}
	fmt.Printf("PASS %s\n", st.name)
	stepI++
	fired = false
	holdLeft = 2
	if stepI < len(all) {
		status = all[stepI].name
	}
}
