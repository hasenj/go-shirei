package main

import (
	"time"

	"go.hasen.dev/generic"
	app "go.hasen.dev/shirei/giobackend"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/tw"
	. "go.hasen.dev/shirei/widgets"
)

func main() {
	app.SetupWindow("Kanban Drag&Drop Demo", 600, 500)
	app.Run(appView)
}

type Board struct {
	Lanes []BoardLane
	Items []BoardItem
}

type BoardLane struct {
	Id    uint64
	Title string
}

type BoardItem struct {
	Title   string
	Summary string
	LaneId  uint64
	Changes []LaneChange
}

type LaneChange struct {
	LaneId     uint64
	AssignedAt time.Time
}

var board Board

func init() {
	board.Lanes = []BoardLane{
		{Id: 10, Title: "TODO"},
		{Id: 20, Title: "Design"},
		{Id: 30, Title: "Implementation"},
		{Id: 40, Title: "Review"},
		{Id: 50, Title: "Approved"},
	}

	board.Items = []BoardItem{
		{LaneId: 40, Title: "Context Menu", Summary: "Demo for anchored context menu"},
		{LaneId: 20, Title: "Kanban Board", Summary: "Demo for drag and drop, using kanban board as a context"},
		{LaneId: 10, Title: "Text Editing", Summary: "Add more text editing controls"},
		{LaneId: 10, Title: "Checkbox", Summary: "Add a checkbox control"},
		{LaneId: 10, Title: "Radio Button", Summary: "Radio button is like a checkbox, but tied to a value instead of a boolean"},
		{LaneId: 10, Title: "Drop Menu", Summary: "Menu with a set of choices to select from"},
	}
}

var nextItem string

func appView() {
	ModAttrs(Pad(10), Gap(10))

	// for drag and drop
	type LaneIdColumn uint64
	type ItemIndexCard int

	var clsItemCard = TW(Pad(10), Gap(10), MaxHeight(100), MinHeight(100), BR(4), BG(0, 0, 90, 1), Expand)

	Layout(TW(Row, Gap(10), Clip, Grow(1), Expand, Extrinsic), func() {
		for laneIdx := range board.Lanes {
			lane := &board.Lanes[laneIdx]
			Layout(TW(Expand, Gap(10)), func() { // expands vertically to fill space
				// title box
				Layout(TW(Pad(10), BG(0, 0, 90, 1), BR(4), Expand), func() {
					Label(lane.Title, Sz(20), FontWeight(WeightBold))
				})

				// item space
				LayoutId(LaneIdColumn(lane.Id), TW(Grow(1), BR(4), MaxWidth(250), MinWidth(250), BG(0, 0, 70, 1)), func() {
					// if something is being dragged over us, highlight our column!
					if CanDropHere[ItemIndexCard]() {
						ModAttrs(BG(0, 0, 74, 1))
					}
					Layout(TW(Viewport, Spacing(10)), func() {
						ScrollOnInput()
						for itemIdx := range board.Items {
							item := &board.Items[itemIdx]
							if item.LaneId != lane.Id {
								continue
							}

							LayoutId(ItemIndexCard(itemIdx), clsItemCard, func() {
								if IsHovered() {
									ModAttrs(BG(0, 0, 94, 1))
								}
								if IsDragging() {
									ModAttrs(BG(240, 50, 94, 1))
								}
								// returns true when dropping this item on a valid target
								if DragAndDrop() {
									laneId := uint64(GetDropTarget[LaneIdColumn]())
									item.LaneId = laneId
									item.Changes = append(item.Changes, LaneChange{
										LaneId:     laneId,
										AssignedAt: time.Now(),
									})
								}
								sz := GetAvailableSize()
								Label(item.Title, Sz(20), TextWidth(sz[0]))
								Label(item.Summary, Sz(10), TextWidth(sz[0]))
							})
						}
					})

					// button to add items at the end of the lane!
					type AddingTicket struct {
						Adding bool
						Title  string
					}
					var adding = Use[AddingTicket]("adding")
					Element(TW(Expand, BG(0, 0, 40, 1), MinHeight(1)))
					Element(TW(Expand, BG(0, 0, 80, 1), MinHeight(1)))
					Layout(TW(Expand, Spacing(10)), func() {
						if adding.Adding {
							TextInput(&adding.Title)
							Layout(TW(Row, Spacing(4)), func() {
								if CtrlButton(0, "Cancel", true) {
									generic.Reset(adding)
								}
								if CtrlButton(0, "Create", true) {
									generic.Append(&board.Items, BoardItem{
										Title:  adding.Title,
										LaneId: lane.Id,
									})
									generic.Reset(adding)
								}
							})
						} else {
							if Button(SymPlus, "Add Ticket") {
								adding.Adding = true
							}
						}
					})
				})
			})
		}
	})

	var draggingItemIdx, ok = GetDraggingItem[ItemIndexCard]()
	if ok {
		// "ghost" item card
		item := &board.Items[int(draggingItemIdx)]
		rect := GetDraggingItemRect()
		LayoutId("dnd-ghost", clsItemCard, func() {
			ModAttrs(NoAnimate, FloatV(rect.Origin), FixSizeV(rect.Size), ClickThrough, Trans(0.5))
			sz := GetAvailableSize()
			Label(item.Title, Sz(20), TextWidth(sz[0]))
			Label(item.Summary, Sz(10), TextWidth(sz[0]))
		})
	}
}
