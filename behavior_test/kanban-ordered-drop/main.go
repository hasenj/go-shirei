// Behavior test: ordered kanban drop inserts at the highlighted index, not
// always at lane end.
//
// Three lanes, cards only (no add-ticket UI). One drop zone per lane; insert
// index from mouse Y vs card midpoints. Drive drags Alpha into the gap between
// Gamma and Delta on the middle lane and asserts order.
//
//	go run ./behavior_test/kanban-ordered-drop
//	go run ./behavior_test/kanban-ordered-drop --window --drive --close
//	go run ./behavior_test/kanban-ordered-drop --window --drive
//	go run ./behavior_test/kanban-ordered-drop --window
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"go.hasen.dev/generic"
	"go.hasen.dev/shirei/app"
	"go.hasen.dev/shirei/behavior_test/btmode"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

const winW, winH float32 = 720, 420

const windowHoldFrames = 18

type laneIdColumn uint64
type itemIndexCard int

type boardItem struct {
	Title  string
	LaneId uint64
}

var (
	mode *btmode.Mode

	verdictDone   bool
	verdictOK     bool
	verdictDetail string

	// Left: Alpha, Beta | Mid: Gamma, Delta | Right: empty
	items = []boardItem{
		{Title: "Alpha", LaneId: 10},
		{Title: "Beta", LaneId: 10},
		{Title: "Gamma", LaneId: 20},
		{Title: "Delta", LaneId: 20},
	}

	lanes = []struct {
		Id    uint64
		Title string
	}{
		{10, "Left"},
		{20, "Mid"},
		{30, "Right"},
	}

	laneCardIds  = map[uint64][]ContainerId{}
	dropInsertAt = map[uint64]int{}
	cardIds      = map[string]ContainerId{}

	phase    = "settle"
	holdLeft int
	beat     int
	status   = "settling"
	holdN    = 2

	dragFrom Vec2
	dragTo   Vec2
)

func main() {
	mode = btmode.RegisterFlags(nil)
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: go run ./behavior_test/kanban-ordered-drop [flags]\n\n%s", btmode.FlagHelp())
	}
	flag.Parse()
	mode.AfterParse()
	if err := mode.Validate(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	fmt.Println("=== behavior_test: kanban-ordered-drop ===")

	if mode.Window {
		holdN = windowHoldFrames
		if mode.Drive {
			phase = "settle"
			holdLeft = holdN
			status = "settle: initial board"
		} else {
			phase = "manual"
			status = "manual — drag Alpha between Gamma and Delta"
		}
		app.SetupWindow("behavior_test: kanban-ordered-drop", int(winW), int(winH))
		app.Run(frameFn)
		return
	}

	ResetInputSession()
	GetHost().WindowSize = Vec2{winW, winH}
	phase = "settle"
	holdLeft = holdN
	status = "settle: initial board"
	for !verdictDone {
		RunFrameFn(frameFn)
	}
	if verdictOK {
		fmt.Println("RESULT: all cases passed")
	} else {
		fmt.Printf("RESULT: FAIL %s\n", verdictDetail)
	}
	os.Exit(btmode.ExitCode(verdictOK))
}

func frameFn() {
	ModAttrs(func(a *AttrSet) { a.Animations = 0 })

	if mode.Drive && !verdictDone {
		driveBeforeUI()
	}

	Container(Attrs(Viewport, Background(220, 12, 96, 1), Pad(12), Gap(8)), func() {
		Label("behavior_test: kanban-ordered-drop", FontWeight(WeightBold), FontSize(16))
		Label(status, FontSize(12), TextColor(0, 0, 40, 1))
		Label(fmt.Sprintf("Left=%v  Mid=%v  Right=%v",
			laneTitles(10), laneTitles(20), laneTitles(30)),
			FontSize(12), TextColor(0, 0, 45, 1))
		boardUI()
	})

	if draggingIdx, ok := GetDraggingItem[itemIndexCard](); ok {
		item := items[int(draggingIdx)]
		rect := GetDraggingItemRect()
		origin := Vec2Sub(rect.Origin, GetRenderData().ResolvedOrigin)
		cls := Attrs(Pad(10), MinHeight(48), MaxHeight(48), Corners(4), Background(0, 0, 90, 1), Expand)
		ContainerWithKey("dnd-ghost", cls, func() {
			ModAttrs(NoAnimate, FloatVec(origin), FixSizeVec(rect.Size), ClickThrough, Trans(0.5))
			Label(item.Title, FontSize(16))
		})
	}

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

func boardUI() {
	const gapH float32 = 10
	const markerH float32 = 4
	clsCard := Attrs(Pad(10), MinHeight(48), MaxHeight(48), Corners(4), Background(0, 0, 90, 1), Expand)
	dropMarker := func() {
		Element(Attrs(Expand, MinHeight(markerH), MaxHeight(markerH),
			Background(210, 70, 55, 1), Corners(2), ClickThrough))
	}

	Container(Attrs(Row, Gap(10), Grow(1), Expand, Extrinsic), func() {
		for _, lane := range lanes {
			lane := lane
			Container(Attrs(Expand, Gap(6)), func() {
				Container(Attrs(Pad(8), Background(0, 0, 90, 1), Corners(4), Expand), func() {
					Label(lane.Title, FontSize(16), FontWeight(WeightBold))
				})

				ContainerWithKey(laneIdColumn(lane.Id), Attrs(Grow(1), Corners(4),
					MaxWidth(200), MinWidth(200), Background(0, 0, 70, 1)), func() {
					var laneItems []int
					for i := range items {
						if items[i].LaneId == lane.Id {
							laneItems = append(laneItems, i)
						}
					}

					insertAt := insertIndexForLane(laneCardIds[lane.Id], GetInputState().MousePoint[1])
					active := CanDropHere[itemIndexCard](laneIdColumn(lane.Id))
					if active {
						dropInsertAt[lane.Id] = insertAt
						ModAttrs(Background(0, 0, 74, 1))
					}

					Container(Attrs(Viewport, Pad(8)), func() {
						var ids []ContainerId
						for i, itemIdx := range laneItems {
							Container(Attrs(Expand, MinHeight(gapH), MainAlign(AlignMiddle)), func() {
								if active && insertAt == i {
									dropMarker()
								}
							})

							item := items[itemIdx]
							ContainerWithKey(itemIndexCard(itemIdx), clsCard, func() {
								ids = append(ids, CurrentId())
								cardIds[item.Title] = CurrentId()
								if IsHovered() {
									ModAttrs(Background(0, 0, 94, 1))
								}
								if IsDragging() {
									ModAttrs(Background(240, 50, 94, 1))
								}
								if DragAndDrop(itemIndexCard(itemIdx)) {
									target := uint64(GetDropTarget[laneIdColumn]())
									at := dropInsertAt[target]
									moveItemToLaneAt(&items, itemIdx, target, at)
								}
								Label(item.Title, FontSize(16))
							})
						}
						Container(Attrs(Expand, MinHeight(gapH), MainAlign(AlignMiddle)), func() {
							if active && insertAt == len(laneItems) {
								dropMarker()
							}
						})
						Container(Attrs(Expand, Grow(1)), func() {})
						laneCardIds[lane.Id] = ids
					})
				})
			})
		}
	})
}

func insertIndexForLane(ids []ContainerId, mouseY float32) int {
	for i, id := range ids {
		rd := GetRenderDataOf(id)
		if rd.ResolvedSize[1] <= 0 {
			continue
		}
		mid := rd.ResolvedOrigin[1] + rd.ResolvedSize[1]/2
		if mouseY < mid {
			return i
		}
	}
	return len(ids)
}

func moveItemToLaneAt(list *[]boardItem, fromIdx int, laneId uint64, at int) {
	if fromIdx < 0 || fromIdx >= len(*list) {
		return
	}
	if at < 0 {
		at = 0
	}

	oldLane := (*list)[fromIdx].LaneId
	if oldLane == laneId {
		srcAt := 0
		for i := 0; i < fromIdx; i++ {
			if (*list)[i].LaneId == laneId {
				srcAt++
			}
		}
		if at == srcAt || at == srcAt+1 {
			return
		}
		if srcAt < at {
			at--
		}
	}

	item := (*list)[fromIdx]
	item.LaneId = laneId
	generic.RemoveAt(list, fromIdx, 1)

	insertAt := len(*list)
	count := 0
	found := false
	for i, it := range *list {
		if it.LaneId != laneId {
			continue
		}
		if count == at {
			insertAt = i
			found = true
			break
		}
		count++
	}
	if !found {
		insertAt = len(*list)
		for i := len(*list) - 1; i >= 0; i-- {
			if (*list)[i].LaneId == laneId {
				insertAt = i + 1
				break
			}
		}
	}
	generic.InsertAt(list, insertAt, item)
}

func laneTitles(laneId uint64) []string {
	var out []string
	for _, it := range items {
		if it.LaneId == laneId {
			out = append(out, it.Title)
		}
	}
	return out
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

func gapBetween(above, below ContainerId) (Vec2, bool) {
	ra := GetResolvedRectOf(above)
	rb := GetResolvedRectOf(below)
	if ra.Size[1] <= 0 || rb.Size[1] <= 0 {
		return Vec2{}, false
	}
	x := ra.Origin[0] + ra.Size[0]/2
	y := (ra.Origin[1] + ra.Size[1] + rb.Origin[1]) / 2
	return Vec2{x, y}, true
}

func driveBeforeUI() {
	GetFrameInput().Scroll = Vec2{}
	GetFrameInput().Motion = Vec2{}
	GetFrameInput().Key = 0
	GetFrameInput().Text = ""
	GetFrameInput().Mouse = 0

	switch phase {
	case "settle", "hold", "manual", "assert":
		parkMouse()
	case "hover_alpha":
		if c, ok := centerOf(cardIds["Alpha"]); ok {
			GetInputState().MousePoint = c
		}
	case "press_alpha":
		if c, ok := centerOf(cardIds["Alpha"]); ok {
			dragFrom = c
			GetInputState().MousePoint = c
		}
		if beat == 1 {
			GetFrameInput().Mouse = MouseClick
		}
	case "drag_threshold":
		next := Vec2{dragFrom[0] + 10, dragFrom[1]}
		GetFrameInput().Motion = Vec2Sub(next, GetInputState().MousePoint)
		GetInputState().MousePoint = next
	case "drag_to_gap":
		prev := GetInputState().MousePoint
		GetInputState().MousePoint = dragTo
		GetFrameInput().Motion = Vec2Sub(dragTo, prev)
	case "drop":
		GetInputState().MousePoint = dragTo
		if beat == 1 {
			GetFrameInput().Mouse = MouseRelease
		}
	}
}

func fail(detail string) {
	parkMouse()
	verdictDone = true
	verdictOK = false
	verdictDetail = detail
	status = "FAIL: " + detail
	fmt.Printf("FAIL %s\n", detail)
}

func passAll() {
	parkMouse()
	verdictDone = true
	verdictOK = true
	verdictDetail = "mid-lane insert ok"
	status = "PASS: mid-lane insert"
	fmt.Println("PASS mid-lane insert")
}

func advance(next, msg string) {
	phase = next
	beat = 0
	holdLeft = holdN
	status = msg
	if next == "settle" || next == "manual" || next == "assert" || strings.HasPrefix(next, "hold") {
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
		if _, ok := centerOf(cardIds["Alpha"]); !ok {
			fail("Alpha card not laid out")
			return
		}
		if _, ok := centerOf(cardIds["Gamma"]); !ok {
			fail("Gamma card not laid out")
			return
		}
		advance("hover_alpha", "hover Alpha")
		holdLeft = 2

	case "hover_alpha":
		if beat < 2 {
			beat++
			return
		}
		advance("press_alpha", "press Alpha")

	case "press_alpha":
		if beat == 0 {
			beat = 1
			return
		}
		advance("drag_threshold", "cross drag threshold")
		holdLeft = 1

	case "drag_threshold":
		gap, ok := gapBetween(cardIds["Gamma"], cardIds["Delta"])
		if !ok {
			fail("could not resolve gap between Gamma and Delta")
			return
		}
		dragTo = gap
		advance("drag_to_gap", "drag to gap between Gamma and Delta")
		holdLeft = 3

	case "drag_to_gap":
		if beat < 2 {
			beat++
			return
		}
		advance("drop", "release to drop")

	case "drop":
		if beat == 0 {
			beat = 1
			return
		}
		advance("assert", "assert Mid order")
		holdLeft = 2

	case "assert":
		got := laneTitles(20)
		want := []string{"Gamma", "Alpha", "Delta"}
		if len(got) != len(want) {
			fail(fmt.Sprintf("Mid lane %v, want %v", got, want))
			return
		}
		for i := range want {
			if got[i] != want[i] {
				fail(fmt.Sprintf("Mid lane %v, want %v", got, want))
				return
			}
		}
		left := laneTitles(10)
		if len(left) != 1 || left[0] != "Beta" {
			fail(fmt.Sprintf("Left lane %v, want [Beta]", left))
			return
		}
		passAll()
	}
}
