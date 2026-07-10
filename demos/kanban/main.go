package main

import (
	"time"

	"go.hasen.dev/generic"
	"go.hasen.dev/shirei/app"

	. "go.hasen.dev/shirei"
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

// LaneIdColumn is the stable drop-zone target for a lane column.
type LaneIdColumn uint64

// ItemIndexCard is the drag payload and card container key (index into board.Items).
type ItemIndexCard int

var board Board

// Previous-frame card container ids per lane (for mouse-Y → insert index).
var laneCardIds = map[uint64][]ContainerId{}

// Insertion index (0..N, N = append) while a lane is the active drop target.
var dropInsertAt = map[uint64]int{}

// ticketEdit is the draft for the double-click edit panel. Open == false means
// closed; drafts are discarded on cancel / outside click / Escape.
type ticketEdit struct {
	Open    bool
	ItemIdx int
	Title   string
	Summary string
}

var editing ticketEdit

func closeTicketEdit() {
	editing = ticketEdit{}
}

func saveTicketEdit() {
	if editing.ItemIdx >= 0 && editing.ItemIdx < len(board.Items) {
		board.Items[editing.ItemIdx].Title = editing.Title
		board.Items[editing.ItemIdx].Summary = editing.Summary
	}
	closeTicketEdit()
}

func openTicketEdit(itemIdx int) {
	if itemIdx < 0 || itemIdx >= len(board.Items) {
		return
	}
	item := board.Items[itemIdx]
	editing = ticketEdit{
		Open:    true,
		ItemIdx: itemIdx,
		Title:   item.Title,
		Summary: item.Summary,
	}
}

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

// insertIndexForLane returns a lane-local insertion index from mouse Y vs card
// midpoints: 0 = before first, N = append after last.
func insertIndexForLane(cardIds []ContainerId, mouseY float32) int {
	for i, id := range cardIds {
		rd := GetRenderDataOf(id)
		if rd.ResolvedSize[1] <= 0 {
			continue
		}
		mid := rd.ResolvedOrigin[1] + rd.ResolvedSize[1]/2
		if mouseY < mid {
			return i
		}
	}
	return len(cardIds)
}

// moveItemToLaneAt moves the item at fromIdx into laneId at lane-local index at
// (0 = first in lane, count = append). LaneChange is recorded only when the lane changes.
func moveItemToLaneAt(items *[]BoardItem, fromIdx int, laneId uint64, at int) {
	if fromIdx < 0 || fromIdx >= len(*items) {
		return
	}
	if at < 0 {
		at = 0
	}

	oldLane := (*items)[fromIdx].LaneId

	// Lane-local index of the dragged item before removal (if same lane).
	srcAt := -1
	if oldLane == laneId {
		srcAt = 0
		for i := 0; i < fromIdx; i++ {
			if (*items)[i].LaneId == laneId {
				srcAt++
			}
		}
		// Already in this slot, or "insert after self" which is a no-op.
		if at == srcAt || at == srcAt+1 {
			return
		}
		// Removing an earlier same-lane item shifts later indices down.
		if srcAt < at {
			at--
		}
	}

	item := (*items)[fromIdx]
	if oldLane != laneId {
		item.LaneId = laneId
		item.Changes = append(item.Changes, LaneChange{
			LaneId:     laneId,
			AssignedAt: time.Now(),
		})
	}

	generic.RemoveAt(items, fromIdx, 1)

	// Absolute insert index: before the `at`-th remaining item in this lane.
	insertAt := len(*items)
	count := 0
	for i, it := range *items {
		if it.LaneId != laneId {
			continue
		}
		if count == at {
			insertAt = i
			break
		}
		count++
	}
	if at >= count {
		// After last item of this lane (or end of slice if lane empty).
		insertAt = len(*items)
		for i := len(*items) - 1; i >= 0; i-- {
			if (*items)[i].LaneId == laneId {
				insertAt = i + 1
				break
			}
		}
	}

	generic.InsertAt(items, insertAt, item)
}

var nextItem string

func appView() {
	ModAttrs(Pad(10), Gap(10))

	var clsItemCard = Attrs(Pad(10), Gap(10), MaxHeight(100), MinHeight(100), Corners(4), Background(0, 0, 90, 1), Expand)
	const gapH float32 = 10
	const markerH float32 = 4
	dropMarker := func() {
		Element(Attrs(Expand, MinHeight(markerH), MaxHeight(markerH), Background(210, 70, 55, 1), Corners(2), ClickThrough))
	}

	Container(Attrs(Row, Gap(10), Clip, Grow(1), Expand, Extrinsic), func() {
		for laneIdx := range board.Lanes {
			lane := &board.Lanes[laneIdx]
			Container(Attrs(Expand, Gap(10)), func() { // expands vertically to fill space
				// title box
				Container(Attrs(Pad(10), Background(0, 0, 90, 1), Corners(4), Expand), func() {
					Label(lane.Title, FontSize(20), FontWeight(WeightBold))
				})

				// Lane column = single stable drop zone; insert index from mouse Y.
				ContainerWithKey(LaneIdColumn(lane.Id), Attrs(Grow(1), Corners(4), MaxWidth(250), MinWidth(250), Background(0, 0, 70, 1)), func() {
					// Items currently in this lane (board indices).
					var laneItems []int
					for i := range board.Items {
						if board.Items[i].LaneId == lane.Id {
							laneItems = append(laneItems, i)
						}
					}

					insertAt := insertIndexForLane(laneCardIds[lane.Id], InputState.MousePoint[1])
					active := CanDropHere[ItemIndexCard](LaneIdColumn(lane.Id))
					if active {
						dropInsertAt[lane.Id] = insertAt
						ModAttrs(Background(0, 0, 74, 1))
					}

					// Pad matches the old Spacing(10) inset; vertical gaps are
					// explicit below so insertion markers can sit between cards.
					Container(Attrs(Viewport, Pad(10)), func() {
						ScrollOnInput()
						var ids []ContainerId
						for i, itemIdx := range laneItems {
							// Gap before card i — marker sits between blocks.
							Container(Attrs(Expand, MinHeight(gapH), MainAlign(AlignMiddle)), func() {
								if active && insertAt == i {
									dropMarker()
								}
							})

							item := board.Items[itemIdx] // value copy for labels
							ContainerWithKey(ItemIndexCard(itemIdx), clsItemCard, func() {
								ids = append(ids, CurrentId())
								if IsHovered() {
									ModAttrs(Background(0, 0, 94, 1))
								}
								if IsDragging() {
									ModAttrs(Background(240, 50, 94, 1))
								}
								if IsDoubleClicked() {
									openTicketEdit(itemIdx)
								}
								// Skip applying a drop while the edit panel is open
								// (double-click release can otherwise nudge order).
								if DragAndDrop(ItemIndexCard(itemIdx)) && !editing.Open {
									laneId := uint64(GetDropTarget[LaneIdColumn]())
									at := dropInsertAt[laneId]
									moveItemToLaneAt(&board.Items, itemIdx, laneId, at)
								}
								sz := GetAvailableSize()
								Label(item.Title, FontSize(20), TextWidth(sz[0]))
								Label(item.Summary, FontSize(10), TextWidth(sz[0]))
							})
						}

						// Append slot: same 10px gap as between cards (marker centered),
						// then a growing hit area for “drop at end” below the last card.
						Container(Attrs(Expand, MinHeight(gapH), MainAlign(AlignMiddle)), func() {
							if active && insertAt == len(laneItems) {
								dropMarker()
							}
						})
						Container(Attrs(Expand, Grow(1)), func() {})

						laneCardIds[lane.Id] = ids
					})

					// button to add items at the end of the lane!
					type AddingTicket struct {
						Adding bool
						Title  string
					}
					var adding = Use[AddingTicket]("adding")
					Element(Attrs(Expand, Background(0, 0, 40, 1), MinHeight(1)))
					Element(Attrs(Expand, Background(0, 0, 80, 1), MinHeight(1)))
					Container(Attrs(Expand, Spacing(10)), func() {
						if adding.Adding {
							TextInput(&adding.Title)
							Container(Attrs(Row, Spacing(4)), func() {
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
		ContainerWithKey("dnd-ghost", clsItemCard, func() {
			ModAttrs(NoAnimate, FloatVec(rect.Origin), FixSizeVec(rect.Size), ClickThrough, Trans(0.5))
			sz := GetAvailableSize()
			Label(item.Title, FontSize(20), TextWidth(sz[0]))
			Label(item.Summary, FontSize(10), TextWidth(sz[0]))
		})
	}

	// Double-click edit panel: draft fields; Save writes back, Cancel/outside/Esc drops.
	if editing.Open {
		Modal(360, closeTicketEdit, func() {
			Label("Edit ticket", FontSize(18), FontWeight(WeightBold))

			Label("Title", FontSize(12), TextColor(0, 0, 40, 1))
			TextInput(&editing.Title)

			Label("Description", FontSize(12), TextColor(0, 0, 40, 1))
			descAttrs := DefaultMultilineTextInputAttrs()
			descAttrs.MinWidth = 320
			descAttrs.Rows = 4
			TextInputExt(&editing.Summary, descAttrs)

			Container(Attrs(Row, Gap(8)), func() {
				if CtrlButton(0, "Cancel", true) {
					closeTicketEdit()
				}
				if CtrlButton(0, "Save", true) {
					saveTicketEdit()
				}
			})
		})
	}
}
