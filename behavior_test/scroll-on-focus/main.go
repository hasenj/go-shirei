// Behavior test: Tabbing from a visible Focusable row to one below the
// fold of a ScrollOnInput pane pans the pane so the focused row is on
// screen. Only a few rows are tab stops, so Tab jumps the gap instead of
// walking every line into view.
//
//	go run ./behavior_test/scroll-on-focus
//	go run ./behavior_test/scroll-on-focus --close
//	go run ./behavior_test/scroll-on-focus --manual
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

const (
	winW, winH float32 = 420, 480
	paneH      float32 = 120
	rowH       float32 = 36
	nRows              = 16
	firstRow           = 0
	jumpRow            = 8  // below the fold in a 120px pane (~3 rows visible)
	nearRow            = 14 // pair of adjacent stops further down
	nearRow2           = 15
)

func rowIsTabStop(i int) bool {
	return i == firstRow || i == jumpRow || i == nearRow || i == nearRow2
}

const windowHoldFrames = 16

var (
	mode *btmode.Mode

	verdictDone   bool
	verdictOK     bool
	verdictDetail string

	rowIds [nRows]ContainerId
	paneId ContainerId
	status = "settling"

	phase    = "settle"
	holdLeft int
	holdN    = windowHoldFrames
	tabsLeft int
)

func main() {
	mode = btmode.RegisterFlags(nil)
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: go run ./behavior_test/scroll-on-focus [flags]\n\n%s", btmode.FlagHelp())
	}
	flag.Parse()
	mode.AfterParse()
	if err := mode.Validate(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	fmt.Println("=== behavior_test: scroll-on-focus ===")

	if mode.Drive {
		phase = "settle"
		holdLeft = holdN
		status = "settle"
	} else {
		phase = "manual"
		status = "manual — Tab: 0 → 8 → 14 → 15"
	}
	app.SetupWindow("behavior_test: scroll-on-focus", int(winW), int(winH))
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

func firstPass() bool {
	return InputEpoch() == GetFrameNumber()
}

func frameFn() {
	ModAttrs(NoAnimate)

	if mode.Drive && !verdictDone && firstPass() {
		park()
	}

	Container(Attrs(Viewport, Background(220, 12, 96, 1), Pad(20), Gap(10)), func() {
		Label("behavior_test: scroll-on-focus", FontWeight(WeightBold), FontSize(16))
		Label(status, FontSize(12), TextColor(0, 0, 40, 1))

		paneId = Container(Attrs(Clip, Extrinsic, Expand, FixHeight(paneH),
			Background(0, 0, 100, 1), BorderWidth(1), BorderColor(0, 0, 80, 1)), func() {
			ScrollOnInput()
			ScrollBars()
			for i := 0; i < nRows; i++ {
				i := i
				a := Attrs(Expand, FixHeight(rowH), CrossMid, Pad2(0, 8))
				if rowIsTabStop(i) {
					a = AttrsWith(a, Focusable)
				}
				rowIds[i] = Container(a, func() {
					if HasFocus() {
						ModAttrs(Background(210, 40, 92, 1), BorderWidth(2), BorderColorVec(CurrentColorScheme.FocusRing))
					} else if rowIsTabStop(i) {
						ModAttrs(Background(210, 15, 94, 1))
					} else if i%2 == 0 {
						ModAttrs(Background(0, 0, 96, 1))
					}
					label := fmt.Sprintf("Row %d", i)
					if rowIsTabStop(i) {
						label += "  (tab)"
					}
					Label(label, FontSize(13))
				})
			}
		})
	})

	if mode.Drive {
		if !verdictDone && firstPass() {
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

func fail(detail string) {
	park()
	verdictDone = true
	verdictOK = false
	verdictDetail = detail
	status = "FAIL: " + detail
	fmt.Printf("FAIL %s\n", detail)
}

func pass(detail string) {
	park()
	verdictDone = true
	verdictOK = true
	verdictDetail = detail
	status = "PASS: " + detail
	fmt.Printf("PASS %s\n", detail)
}

func rowFullyVisible(row, pane ContainerId) bool {
	if row == nil || pane == nil {
		return false
	}
	f := GetResolvedRectOf(row)
	p := GetScreenRectOf(pane)
	if f.Size[0] < 1 || f.Size[1] < 1 || p.Size[1] < 1 {
		return false
	}
	top := p.Origin[1]
	bot := p.Origin[1] + p.Size[1]
	const eps float32 = 1
	return f.Origin[1] >= top-eps && f.Origin[1]+f.Size[1] <= bot+eps
}

func driveAfterUI() {
	if holdLeft > 0 {
		holdLeft--
		return
	}
	switch phase {
	case "settle":
		if paneId == nil || rowIds[firstRow] == nil || rowIds[jumpRow] == nil ||
			rowIds[nearRow] == nil || rowIds[nearRow2] == nil {
			fail("row ids not captured")
			return
		}
		// nothing → 0 → 8 (gap) → 14 → 15 (adjacent)
		tabsLeft = 4
		phase = "tabbing"
		status = "tab 0 → 8 → 14 → 15"
		fmt.Println("PASS settle")
	case "tabbing":
		if tabsLeft <= 0 {
			phase = "assert"
			holdLeft = holdN
			status = "assert focused row is on screen"
			return
		}
		Tab()
		tabsLeft--
		status = fmt.Sprintf("tabbing (%d left)", tabsLeft)
		holdLeft = 4
	case "assert":
		if !IdHasFocus(rowIds[nearRow2]) {
			fail(fmt.Sprintf("expected focus on row %d", nearRow2))
			return
		}
		if rowFullyVisible(rowIds[firstRow], paneId) && GetScrollOffsetOf(paneId)[1] < 1 {
			fail("pane did not scroll (row 0 still fully visible and offset=0)")
			return
		}
		if !rowFullyVisible(rowIds[nearRow2], paneId) {
			f := GetResolvedRectOf(rowIds[nearRow2])
			p := GetScreenRectOf(paneId)
			fail(fmt.Sprintf("row %d not fully visible: row=%v pane=%v scroll=%v",
				nearRow2, f, p, GetScrollOffsetOf(paneId)))
			return
		}
		fmt.Println("PASS reveal-below-fold")
		pass("focused row scrolled into view")
	}
}
