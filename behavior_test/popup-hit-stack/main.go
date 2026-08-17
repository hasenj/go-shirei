// Behavior test: pointer hit-testing across stacked layers (content → panel → modal).
//
// Layout:
//   - Main card (screen center): a "base" switch under an "Open panel" button.
//   - Popup panel: opens below that button so it covers the base switch; inert
//     labels over the covered area; its own "panel" switch; "Open modal".
//   - Modal: fullscreen scrim over both; no switches on the card.
//
// Drive injects mouse like a user while the window shows each step (hover,
// click, open panel, blocked click, panel switch, modal, blocked again).
//
//	go run ./behavior_test/popup-hit-stack
//	go run ./behavior_test/popup-hit-stack --close
//	go run ./behavior_test/popup-hit-stack --manual
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"go.hasen.dev/shirei/app"
	"go.hasen.dev/shirei/behavior_test/btmode"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

const winW, winH float32 = 640, 480

// Visible pause between script beats when a window is open (~0.3s at 60Hz).
const windowHoldFrames = 18

var (
	mode *btmode.Mode

	verdictDone   bool
	verdictOK     bool
	verdictDetail string

	// UI state
	showPanel bool
	showModal bool
	baseOn    bool
	panelOn   bool

	baseHovered  bool
	panelHovered bool
	baseId       ContainerId
	panelId      ContainerId
	panelBtnId   ContainerId
	modalBtnId   ContainerId

	// drive script
	phase      = "settle"
	holdLeft   int
	beat       int // micro-step inside a phase (hover settle / click / release)
	status     = "settling"
	holdN      = windowHoldFrames
	savedBase  bool
	savedPanel bool
	parkNext   bool // park in driveBefore — never before PopupsHost same frame
)

func main() {
	mode = btmode.RegisterFlags(nil)
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: go run ./behavior_test/popup-hit-stack [flags]\n\n%s", btmode.FlagHelp())
	}
	flag.Parse()
	mode.AfterParse()
	if err := mode.Validate(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	fmt.Println("=== behavior_test: popup-hit-stack ===")

	if mode.Drive {
		phase = "settle"
		holdLeft = holdN
		status = "settle: initial state"
	} else {
		phase = "manual"
		status = "manual — open panel, then modal"
	}
	app.SetupWindow("behavior_test: popup-hit-stack", int(winW), int(winH))
	app.Run(frameFn)
}

func frameFn() {
	ModAttrs(func(a *AttrSet) { a.Animations = 0 })

	if mode.Drive && !verdictDone {
		driveBeforeUI()
	}

	Container(Attrs(Viewport, Background(220, 12, 96, 1), Pad(20), Gap(10)), func() {
		Label("behavior_test: popup-hit-stack", FontWeight(WeightBold), FontSize(16))
		Label(status, FontSize(12), TextColor(0, 0, 40, 1))
		// Hover from last frame's hoverables (panel lives in PopupsHost after this builder).
		baseHovered = IdIsHovered(baseId)
		panelHovered = IdIsHovered(panelId)
		Label(fmt.Sprintf("base on=%v hover=%v | panel on=%v hover=%v | panelOpen=%v modal=%v",
			baseOn, baseHovered, panelOn, panelHovered, showPanel, showModal),
			FontSize(12), TextColor(0, 0, 45, 1))

		layerCard()
	})

	if mode.Drive {
		if !verdictDone {
			// Re-sample after ids updated this frame; hoverList is still last-frame.
			baseHovered = IdIsHovered(baseId)
			panelHovered = IdIsHovered(panelId)
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

func layerCard() {
	Container(Attrs(Expand, Center), func() {
		Container(Attrs(Gap(12), Pad(20), MinWidth(280), Background(0, 0, 100, 1), Corners(10), BoxShadow(16)), func() {
			Label("Main content", FontSize(14), FontWeight(WeightBold))

			if Button(SymMenu, "Open panel") {
				showPanel = !showPanel
			}
			panelBtnId = GetLastId()
			anchor := panelBtnId

			baseId = switchProbe("base", "Base switch", &baseOn)

			// Clicks on a higher popup (modal) look "outside" to PopupPanel and
			// would clear showPanel; keep the panel mounted while the modal is up
			// so the stack stays visible for hit-testing.
			panelOpen := showPanel || showModal
			if panelOpen {
				PopupPanel(&panelOpen, anchor, Attrs(Spacing(10), Pad(14), Corners(6), MinWidth(260), MinHeight(200)), func() {
					Label("Panel cover", FontSize(14), FontWeight(WeightBold))
					Label("Inert text over the base switch area — clicks here must not reach base.", FontSize(12), TextColor(0, 0, 40, 1))
					panelId = switchProbe("panel", "Panel switch", &panelOn)
					if Button(NoIcon, "Open modal") {
						showModal = true
					}
					modalBtnId = GetLastId()
				})
				if !showModal {
					showPanel = panelOpen
				}
			}

			// Modal is a sibling popup (not nested inside the panel callback) so it
			// stays registered even if the panel toggle flickers.
			if showModal {
				Modal(360, func() { showModal = false }, func() {
					Label("Modal", FontSize(16), FontWeight(WeightBold))
					Label("No switches here. Scrim covers base + panel.", FontSize(12), TextColor(0, 0, 40, 1))
					Label(fmt.Sprintf("base=%v panel=%v", baseOn, panelOn), FontSize(12))
					Label("Click outside (scrim) or Escape to dismiss.", FontSize(12), TextColor(0, 0, 45, 1))
				})
			}
		})
	})
}

func switchProbe(key, title string, on *bool) ContainerId {
	var switchId ContainerId
	ContainerWithKey(key+"-row", Attrs(Row, CrossMid, Gap(10), Pad2(8, 12), Corners(6),
		Background(220, 20, 94, 1), MinWidth(200)), func() {
		Label(title, FontSize(13), FontWeight(WeightBold))
		switchId = ContainerWithKey(key, Attrs(Row, CrossMid), func() {
			if IsHovered() {
				ModAttrs(Background(220, 40, 88, 0.35))
			}
			ToggleSwitch(on)
		})
	})
	return switchId
}

// ── drive ────────────────────────────────────────────────────────────────

func parkMouse() {
	GetInputState().MousePoint = Vec2{-1000, -1000}
	GetFrameInput().Mouse = 0
	GetFrameInput().Scroll = Vec2{}
	GetFrameInput().Motion = Vec2{}
	GetFrameInput().Key = 0
	GetFrameInput().Text = ""
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

func driveBeforeUI() {
	GetFrameInput().Scroll = Vec2{}
	GetFrameInput().Motion = Vec2{}
	GetFrameInput().Key = 0
	GetFrameInput().Text = ""
	GetFrameInput().Mouse = 0

	if parkNext {
		parkMouse()
		parkNext = false
	}

	switch phase {
	case "settle", "hold", "manual":
		parkMouse()
	case "hover_base", "click_base":
		if c, ok := centerOf(baseId); ok {
			GetInputState().MousePoint = c
		}
	case "open_panel":
		if c, ok := centerOf(panelBtnId); ok {
			GetInputState().MousePoint = c
		}
	case "blocked_base_hover", "blocked_base_click":
		if c, ok := centerOf(baseId); ok {
			GetInputState().MousePoint = c
		}
	case "hover_panel", "click_panel":
		if c, ok := centerOf(panelId); ok {
			GetInputState().MousePoint = c
		}
	case "open_modal":
		if c, ok := centerOf(modalBtnId); ok {
			GetInputState().MousePoint = c
		}
	case "modal_block_base", "modal_block_panel":
		id := baseId
		if phase == "modal_block_panel" {
			id = panelId
		}
		if c, ok := centerOf(id); ok {
			GetInputState().MousePoint = c
		}
	case "dismiss_modal":
		// Corner of the window: scrim, outside the centered card.
		GetInputState().MousePoint = Vec2{12, 12}
	}

	// Click / release beats.
	switch phase {
	case "click_base", "open_panel", "blocked_base_click", "click_panel", "open_modal",
		"dismiss_modal":
		switch beat {
		case 1:
			GetFrameInput().Mouse = MouseClick
		case 2:
			GetFrameInput().Mouse = MouseRelease
		}
	}
}

func clearOverlays() {
	showModal = false
	showPanel = false
	parkMouse()
}

func fail(detail string) {
	clearOverlays()
	verdictDone = true
	verdictOK = false
	verdictDetail = detail
	status = "FAIL: " + detail
	fmt.Printf("FAIL %s\n", detail)
}

func passAll() {
	clearOverlays()
	verdictDone = true
	verdictOK = true
	verdictDetail = "all steps passed"
	status = "PASS: all steps"
	fmt.Println("PASS all steps")
}

func advance(next, msg string) {
	phase = next
	beat = 0
	holdLeft = holdN
	status = msg
	if next == "settle" || next == "manual" || strings.HasPrefix(next, "hold") {
		parkMouse()
	}
}

func driveAfterUI() {
	if holdLeft > 0 {
		holdLeft--
		return
	}

	switch phase {
	case "settle":
		advance("hover_base", "hover base switch")
		beat = 0
		holdLeft = 2 // need 2 frames for hoverables → IsHovered

	case "hover_base":
		// first entry: mouse already on base from driveBefore; wait 2 frames then assert
		if beat < 2 {
			beat++
			return
		}
		if !baseHovered {
			fail("base not hovered when uncovered")
			return
		}
		savedBase = baseOn
		advance("click_base", "click base switch")
		beat = 0

	case "click_base":
		// beat 0: hover settle, 1: click, 2: release, 3: assert after park
		if beat == 0 {
			beat = 1
			return
		}
		if beat == 1 {
			beat = 2
			return
		}
		if beat == 2 {
			beat = 3
			parkNext = true
			holdLeft = 1
			return
		}
		if baseOn == savedBase {
			fail(fmt.Sprintf("base did not toggle (still %v)", baseOn))
			return
		}
		advance("open_panel", "click Open panel")
		beat = 0

	case "open_panel":
		if beat == 0 {
			beat = 1
			return
		}
		if beat == 1 {
			beat = 2
			return
		}
		if beat == 2 {
			beat = 3
			parkNext = true
			holdLeft = holdN
			return
		}
		if !showPanel {
			fail("panel did not open")
			return
		}
		advance("blocked_base_hover", "hover base location under panel")
		beat = 0
		holdLeft = 2

	case "blocked_base_hover":
		if beat < 2 {
			beat++
			return
		}
		if baseHovered {
			fail("base still hovered under panel")
			return
		}
		savedBase = baseOn
		advance("blocked_base_click", "click base location under panel (must not toggle)")
		beat = 0

	case "blocked_base_click":
		if beat == 0 {
			beat = 1
			return
		}
		if beat == 1 {
			beat = 2
			return
		}
		if beat == 2 {
			beat = 3
			parkNext = true
			holdLeft = 1
			return
		}
		if baseOn != savedBase {
			fail(fmt.Sprintf("base toggled under panel → %v", baseOn))
			return
		}
		if !showPanel {
			fail("panel closed while clicking its cover")
			return
		}
		advance("hover_panel", "hover panel switch")
		beat = 0
		holdLeft = 2

	case "hover_panel":
		if beat < 2 {
			beat++
			return
		}
		if !panelHovered {
			fail("panel switch not hovered")
			return
		}
		if baseHovered {
			fail("base hovered while on panel switch")
			return
		}
		savedPanel = panelOn
		advance("click_panel", "click panel switch")
		beat = 0

	case "click_panel":
		if beat == 0 {
			beat = 1
			return
		}
		if beat == 1 {
			beat = 2
			return
		}
		if beat == 2 {
			beat = 3
			parkNext = true
			holdLeft = 1
			return
		}
		if panelOn == savedPanel {
			fail(fmt.Sprintf("panel switch did not toggle (still %v)", panelOn))
			return
		}
		advance("open_modal", "click Open modal")
		beat = 0

	case "open_modal":
		if beat == 0 {
			beat = 1
			return
		}
		if beat == 1 {
			beat = 2
			return
		}
		if beat == 2 {
			beat = 3
			parkNext = true
			holdLeft = holdN
			return
		}
		if !showModal {
			fail("modal did not open")
			return
		}
		savedBase, savedPanel = baseOn, panelOn
		// Hover-only under the modal: a scrim click would dismiss (by design).
		advance("modal_block_base", "hover base location under modal")
		beat = 0
		holdLeft = 2

	case "modal_block_base":
		if beat < 2 {
			beat++
			return
		}
		if baseHovered {
			fail("base hovered under modal")
			return
		}
		if baseOn != savedBase {
			fail("base changed under modal without a click")
			return
		}
		advance("modal_block_panel", "hover panel location under modal")
		beat = 0
		holdLeft = 2

	case "modal_block_panel":
		if beat < 2 {
			beat++
			return
		}
		if panelHovered {
			fail("panel hovered under modal")
			return
		}
		if panelOn != savedPanel || baseOn != savedBase {
			fail("switch flipped under modal without a click")
			return
		}
		advance("dismiss_modal", "click scrim to dismiss modal")
		beat = 0

	case "dismiss_modal":
		if beat == 0 {
			beat = 1
			return
		}
		if beat == 1 {
			beat = 2
			return
		}
		if beat == 2 {
			beat = 3
			parkNext = true
			holdLeft = 1
			return
		}
		if showModal {
			fail("modal did not dismiss on scrim click")
			return
		}
		passAll()
	}
}
