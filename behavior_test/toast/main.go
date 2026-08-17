// Behavior test: toast notifications appear on the next frame after a click,
// pin the dismiss control near the card’s right edge, and auto-dismiss when
// their lifetime elapses.
//
//	go run ./behavior_test/toast
//	go run ./behavior_test/toast --close
//	go run ./behavior_test/toast --manual
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"go.hasen.dev/shirei/app"
	"go.hasen.dev/shirei/behavior_test/btmode"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

const winW, winH float32 = 640, 420

// Visible pause between script beats when a window is open (~0.3s at 60Hz).
const windowHoldFrames = 18

// Short lifetime for the auto-dismiss case (wall clock).
const autoDismissDur = 120 * time.Millisecond

var (
	mode *btmode.Mode

	verdictDone   bool
	verdictOK     bool
	verdictDetail string

	msgBtnId  ContainerId
	autoBtnId ContainerId

	phase     = "settle"
	holdLeft  int
	beat      int
	status    = "settling"
	holdN     = windowHoldFrames
	parkNext  bool
	waitUntil time.Time
)

func main() {
	mode = btmode.RegisterFlags(nil)
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: go run ./behavior_test/toast [flags]\n\n%s", btmode.FlagHelp())
	}
	flag.Parse()
	mode.AfterParse()
	if err := mode.Validate(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	fmt.Println("=== behavior_test: toast ===")

	if mode.Drive {
		phase = "settle"
		holdLeft = holdN
		status = "settle: initial state"
	} else {
		phase = "manual"
		status = "manual — click Message / Auto-dismiss"
	}
	app.SetupWindow("behavior_test: toast", int(winW), int(winH))
	app.Run(frameFn)
}

func frameFn() {
	ModAttrs(func(a *AttrSet) { a.Animations = 0 })

	if mode.Drive && !verdictDone {
		driveBeforeUI()
	}

	Container(Attrs(Viewport, Background(220, 12, 96, 1), Pad(28), Gap(16)), func() {
		Label("behavior_test: toast", FontWeight(WeightBold), FontSize(16))
		Label(status, FontSize(12), TextColor(0, 0, 40, 1))
		Label(fmt.Sprintf("toasts=%d layouts=%d phase=%s", ToastCount(), len(ActiveToastLayouts()), phase),
			FontSize(12), TextColor(0, 0, 45, 1))

		Container(Attrs(Row, Wrap, Gap(10)), func() {
			if Button(NoIcon, "Message") {
				ToastMessage("Saved “notes.md”")
			}
			msgBtnId = GetLastId()

			if Button(NoIcon, "Auto-dismiss") {
				ToastExt(ToastAttrs{
					Body:     "This toast expires quickly.",
					Duration: autoDismissDur,
				})
			}
			autoBtnId = GetLastId()

			if !mode.Drive {
				if ButtonExt("Dismiss all", ButtonAttrs{}, DefaultCtrlButtonLook()) {
					DismissAllToasts()
				}
			}
		})
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
		return
	}

	switch phase {
	case "click_message", "click_auto":
		id := msgBtnId
		if phase == "click_auto" {
			id = autoBtnId
		}
		if c, ok := centerOf(id); ok {
			GetInputState().MousePoint = c
		}
		switch beat {
		case 1:
			GetFrameInput().Mouse = MouseClick
		case 2:
			GetFrameInput().Mouse = MouseRelease
		}
	default:
		parkMouse()
	}
}

func fail(detail string) {
	DismissAllToasts()
	parkMouse()
	verdictDone = true
	verdictOK = false
	verdictDetail = detail
	status = "FAIL: " + detail
	fmt.Printf("FAIL %s\n", detail)
}

func passAll() {
	DismissAllToasts()
	parkMouse()
	verdictDone = true
	verdictOK = true
	verdictDetail = "all cases passed"
	status = "PASS: all cases"
}

func advance(next, msg string) {
	phase = next
	beat = 0
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
		DismissAllToasts()
		if ToastCount() != 0 {
			fail("toast stack not empty at start")
			return
		}
		advance("click_message", "click Message")
		beat = 0

	case "click_message":
		if beat == 0 {
			beat = 1
			return
		}
		if beat == 1 {
			beat = 2
			return
		}
		if beat == 2 {
			parkNext = true
			// Click frame queues + paints the toast; geometry is previous-frame
			// data — assert visibility on the following frame.
			advance("appear_next", "assert toast geometry next frame")
			holdLeft = 1
			return
		}

	case "appear_next":
		if ToastCount() < 1 {
			fail("toast did not appear after Message click")
			return
		}
		layouts := ActiveToastLayouts()
		if len(layouts) < 1 || layouts[0].CardId == nil {
			fail("toast card layout missing after Message click")
			return
		}
		card := GetResolvedRectOf(layouts[0].CardId)
		if card.Size[0] < 100 || card.Size[1] < 20 {
			fail(fmt.Sprintf("toast card not visible next frame: size=%v", card.Size))
			return
		}
		fmt.Println("PASS appear-next-frame")
		advance("assert_edge", "assert dismiss near right edge")
		holdLeft = 1

	case "assert_edge":
		layouts := ActiveToastLayouts()
		if len(layouts) < 1 || layouts[0].DismissId == nil {
			fail("dismiss control layout missing")
			return
		}
		card := GetResolvedRectOf(layouts[0].CardId)
		dismiss := GetResolvedRectOf(layouts[0].DismissId)
		if dismiss.Size[0] <= 0 || dismiss.Size[1] <= 0 {
			fail(fmt.Sprintf("dismiss rect empty: %v", dismiss))
			return
		}
		cardRight := card.Origin[0] + card.Size[0]
		dismissRight := dismiss.Origin[0] + dismiss.Size[0]
		rightGap := cardRight - dismissRight
		// Card pad is 14; allow a little layout slack.
		if rightGap < 0 || rightGap > 28 {
			fail(fmt.Sprintf("dismiss not near right edge: rightGap=%.1f card=%v dismiss=%v",
				rightGap, card, dismiss))
			return
		}
		mid := card.Origin[0] + card.Size[0]/2
		if dismiss.Origin[0] < mid {
			fail(fmt.Sprintf("dismiss not in right half: mid=%.1f dismissX=%.1f", mid, dismiss.Origin[0]))
			return
		}
		fmt.Println("PASS dismiss-near-right-edge")
		DismissAllToasts()
		advance("click_auto", "click Auto-dismiss")
		beat = 0
		holdLeft = holdN

	case "click_auto":
		if beat == 0 {
			beat = 1
			return
		}
		if beat == 1 {
			beat = 2
			return
		}
		if beat == 2 {
			parkNext = true
			waitUntil = time.Now().Add(autoDismissDur + 40*time.Millisecond)
			advance("wait_expire", "wait for auto-dismiss")
			holdLeft = 0
			return
		}

	case "wait_expire":
		if time.Now().Before(waitUntil) {
			holdLeft = 0
			return
		}
		advance("assert_gone", "assert toast auto-dismissed")
		holdLeft = 1

	case "assert_gone":
		if ToastCount() != 0 {
			fail(fmt.Sprintf("toast still present after lifetime (%d)", ToastCount()))
			return
		}
		if len(ActiveToastLayouts()) != 0 {
			fail("toast layouts still present after auto-dismiss")
			return
		}
		fmt.Println("PASS auto-dismiss")
		passAll()
	}
}
