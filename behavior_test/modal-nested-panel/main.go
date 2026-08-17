// Behavior test: PopupPanel opened from inside a Modal runs in the same
// PopupsHost pass (drain-until-empty) and stacks above the modal.
//
// Observables:
//
//  1. panel builder body executes (sets a flag). With a fixed-length range over
//     ui.popups, a panel registered during the modal's Popup callback would be
//     queued and then discarded without running.
//
//  2. floating roots pick up ui.popupZ from the drain index (panel > modal).
//
// Drive shows each step in the window (modal open → panel open → assert).
//
//	go run ./behavior_test/modal-nested-panel
//	go run ./behavior_test/modal-nested-panel --close
//	go run ./behavior_test/modal-nested-panel --manual
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

const winW, winH float32 = 640, 420

// Visible pause between script beats when a window is open (~0.7s at 60Hz).
const windowHoldFrames = 42

var (
	mode *btmode.Mode

	verdictDone   bool
	verdictOK     bool
	verdictDetail string

	showModal bool
	showPanel bool
	panelRan  bool // set in panel builder (PopupsHost); sticky until cleared
	panelId   ContainerId
	panelZ    float32

	phase    = "settle"
	holdLeft int
	status   = "settling"
	holdN    = windowHoldFrames
)

func main() {
	mode = btmode.RegisterFlags(nil)
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: go run ./behavior_test/modal-nested-panel [flags]\n\n%s", btmode.FlagHelp())
	}
	flag.Parse()
	mode.AfterParse()
	if err := mode.Validate(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	fmt.Println("=== behavior_test: modal-nested-panel ===")

	if mode.Drive {
		phase = "settle"
		holdLeft = holdN
		status = "settle: initial state"
	} else {
		phase = "manual"
		status = "manual — open modal, then open panel from inside it"
	}
	app.SetupWindow("behavior_test: modal-nested-panel", int(winW), int(winH))
	app.Run(frameFn)
}

func frameFn() {
	ModAttrs(func(a *AttrSet) { a.Animations = 0 })

	if mode.Drive && !verdictDone {
		driveBeforeUI()
	}

	Container(Attrs(Viewport, Background(220, 12, 96, 1), Pad(20), Gap(12)), func() {
		Label("behavior_test: modal-nested-panel", FontWeight(WeightBold), FontSize(16))
		Label(status, FontSize(12), TextColor(0, 0, 40, 1))
		Label(fmt.Sprintf("modal=%v panel=%v panelRan=%v panelZ=%.0f", showModal, showPanel, panelRan, panelZ),
			FontSize(12), TextColor(0, 0, 45, 1))

		if !showModal {
			if Button(NoIcon, "Open modal") {
				showModal = true
			}
		}

		if showModal {
			dismiss := func() { showModal = false }
			if mode.Drive && phase != "manual" {
				dismiss = nil // avoid Escape/scrim closing mid-drive
			}
			Modal(400, dismiss, func() {
				Label("Modal card", FontSize(14), FontWeight(WeightBold))
				if Button(SymMenu, "Open panel") {
					showPanel = !showPanel
				}
				anchor := GetLastId()
				if showPanel {
					on := true
					PopupPanel(&on, anchor, Attrs(Spacing(8), Pad(10), Corners(4), MinWidth(200)), func() {
						panelRan = true
						panelId = CurrentId()
						panelZ = GetAttrs().Z
						Label("Panel from modal", FontSize(13), FontWeight(WeightBold))
						Label("Nested popup drained correctly.", FontSize(12))
					})
				}
			})
		}
	})

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

func driveBeforeUI() {
	GetFrameInput().Scroll = Vec2{}
	GetFrameInput().Motion = Vec2{}
	GetFrameInput().Key = 0
	GetFrameInput().Text = ""
	GetFrameInput().Mouse = 0

	switch phase {
	case "hover_panel":
		if c, ok := centerOf(panelId); ok {
			GetInputState().MousePoint = c
		}
	default:
		GetInputState().MousePoint = Vec2{-1000, -1000}
	}

	switch phase {
	case "same_pass":
		showModal = true
		showPanel = true
	case "panel_closed":
		showModal = true
		showPanel = false
		panelRan = false
		panelId = nil
		panelZ = 0
	case "panel_reopen", "hover_panel":
		showModal = true
		showPanel = true
	}
}

func centerOf(id ContainerId) (Vec2, bool) {
	if id == nil {
		return Vec2{}, false
	}
	r := GetResolvedRectOf(id)
	if r.Size[0] <= 0 || r.Size[1] <= 0 {
		return Vec2{}, false
	}
	return Vec2{r.Origin[0] + r.Size[0]/2, r.Origin[1] + r.Size[1]/2}, true
}

func panelAboveModal() bool {
	// Modal is drain index 1 (Z=1). The nested panel must pick up a later
	// drain index so it paints and hits above the scrim.
	return panelZ > 1
}

func fail(detail string) {
	verdictDone = true
	verdictOK = false
	verdictDetail = detail
	status = "FAIL: " + detail
	fmt.Printf("FAIL %s\n", detail)
}

func passAll() {
	verdictDone = true
	verdictOK = true
	verdictDetail = "all cases passed"
	status = "PASS: all cases"
	fmt.Println("PASS panel-runs-same-pass")
	fmt.Println("PASS panel-runs-after-toggle")
	fmt.Println("PASS panel-stacks-above-modal")
}

func advance(next, msg string) {
	phase = next
	holdLeft = holdN
	status = msg
}

func driveAfterUI() {
	if holdLeft > 0 {
		holdLeft--
		return
	}

	switch phase {
	case "settle":
		// Open modal+panel together: panel must run in the same host drain.
		advance("same_pass", "same-pass: modal + panel together")

	case "same_pass":
		// panelRan is set in PopupsHost after this frameFn returns; wait one
		// more frame before asserting (holdLeft=0 → next entry has it set).
		if holdLeft == 0 && !panelRan {
			// First visit after advance hold: schedule one extra frame.
			holdLeft = 1
			return
		}
		if !panelRan {
			fail("panel builder did not run in the same PopupsHost pass as the modal")
			return
		}
		if !panelAboveModal() {
			fail(fmt.Sprintf("panel Z=%.0f; expected >1 so it paints above the modal", panelZ))
			return
		}
		advance("panel_closed", "toggle: close panel, keep modal")

	case "panel_closed":
		if panelRan {
			fail("panel ran while toggle was false")
			return
		}
		advance("panel_reopen", "toggle: reopen panel from modal")

	case "panel_reopen":
		if holdLeft == 0 && !panelRan {
			holdLeft = 1
			return
		}
		if !panelRan {
			fail("panel builder did not run after toggle opened")
			return
		}
		if !showModal || !showPanel {
			fail("modal or panel not visible after toggle open")
			return
		}
		if !panelAboveModal() {
			fail(fmt.Sprintf("panel Z=%.0f after toggle; expected >1 so it paints above the modal", panelZ))
			return
		}
		advance("hover_panel", "hover: panel must be hit above the modal")

	case "hover_panel":
		// After the advance hold the pointer has sat on the panel for holdN
		// frames; hoverList is last-frame geometry, so that is enough.
		if _, ok := centerOf(panelId); !ok {
			fail("panel has no resolved rect to hover")
			return
		}
		if !IdIsHovered(panelId) {
			fail("panel is not hovered — it is stacked under the modal")
			return
		}
		passAll()
	}
}
