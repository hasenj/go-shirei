package widgets

import (
	"sort"

	. "go.hasen.dev/shirei"
)

// TableColumn describes one column of a Table: how wide it is, how to
// render a cell, and (optionally) how to compare two rows for sorting by
// this column. A nil Less means the column can't be clicked to sort.
type TableColumn[T any] struct {
	Label       string
	Width       f32 // 0 = flexible (Grow(1)); otherwise a fixed pixel width
	Render      func(row T)
	Less        func(a, b T) bool
	DefaultDesc bool // sort direction the first time this column is clicked
}

// TableSortState is the table's sort state: which column it's currently
// sorted by, and in which direction. By default it lives as the table's
// own hooked state (self-contained via UseWithInit, scoped to the table's
// LayoutWithKey); a caller that needs to read or drive the sort threads its own
// instance in through TableAttrs.SortState — the established
// pointer-threading way, not cross-container hook sharing.
type TableSortState struct {
	Column int
	Desc   bool
}

// TableAttrs is the extended configuration for TableExt. The zero value
// plus RowHeight reproduces plain Table behavior.
type TableAttrs[T any] struct {
	RowHeight f32

	// DefaultSortColumn picks the starting sort column (and, via its
	// DefaultDesc, the direction) when the table owns its sort state.
	// Ignored when SortState is provided — the caller initialized it.
	DefaultSortColumn int

	// SortState, when non-nil, is the caller's sort state instead of
	// table-private hooked state — for callers that need to read or drive
	// the sort from outside (e.g. ordering tree siblings by the sorted
	// column).
	SortState *TableSortState

	// OrderRows, when set, replaces the built-in flat sort: it receives the
	// rows plus the active sort column and direction and returns them in
	// display order. A tree view returns its depth-first flattening; a
	// caller that already ordered the rows returns them as-is.
	OrderRows func(rows []T, column int, desc bool) []T

	// OnRow, when set, runs inside each row's container before its cells —
	// the hook for row-level styling and interactivity (zebra striping,
	// hover highlight, click-to-select) via ModAttrs / IsHovered /
	// PressAction.
	OnRow func(index int, row T)
}

const tableHeaderHeight = 30

// Table is a generic, sortable, virtualized table. id is forwarded to
// LayoutWithKey (nil = auto id, matching the rest of shirei). rowId must return
// a stable, unique identity per row (e.g. the row's own pointer) — it's
// used both for LayoutWithKey identity within a row and for VirtualListView's
// scroll/animation bookkeeping. defaultSortColumn picks which column (and,
// via its DefaultDesc) which direction the table starts sorted by, before
// any header has been clicked.
func Table[T any](key any, rowHeight f32, columns []TableColumn[T], rows []T, rowKey func(T) any, defaultSortColumn int) {
	TableExt(key, TableAttrs[T]{RowHeight: rowHeight, DefaultSortColumn: defaultSortColumn}, columns, rows, rowKey)
}

// TableExt is Table with the full configuration surface; see TableAttrs.
func TableExt[T any](key any, attrs TableAttrs[T], columns []TableColumn[T], rows []T, rowKey func(T) any) {
	ContainerWithKey(key, Attrs(Viewport), func() {
		state := attrs.SortState
		if state == nil {
			state = UseWithInit[TableSortState]("table-sort", func() *TableSortState {
				s := &TableSortState{Column: attrs.DefaultSortColumn}
				if s.Column >= 0 && s.Column < len(columns) {
					s.Desc = columns[s.Column].DefaultDesc
				}
				return s
			})
		}

		// Inter-column spacing lives on each cell (Pad4 here), not as a Row
		// Gap between them: a Gap sits *outside* every child's own box, so
		// the header cell's hover highlight — which is that box's own
		// background — would stop short of the separator, leaving an
		// unhighlighted sliver on each side. Padding is inside the box, so
		// the highlight now reaches the separator exactly. Shared between
		// the header and body cells below so columns still line up.
		//
		// The flexible column is sized from flex (Grow) *only*, never from its
		// content. ExtrinsicSize (bundled in Viewport, together with Clip +
		// ExpandAcross + Grow(1)) tells the cell to ignore its content's width,
		// so a long function name clips to the column instead of expanding the
		// cell and shoving every later column right in that one row. The other
		// pieces of the bundle are load-bearing here: ExpandAcross gives the
		// cell the row's height (ExtrinsicSize would otherwise collapse it
		// vertically and clip the text to nothing), so MainAlign(AlignMiddle) is what
		// re-centers the label in the now-full-height cell. MinWidth is the
		// floor — once the fixed columns + separators + padding exceed the
		// available width there's no room to grow, so without it the cell would
		// shrink toward zero; with it the table overflows (clipped by the outer
		// Viewport) instead of vanishing.
		const columnMinWidth = 120

		columnAttrs := func(col TableColumn[T]) AttrSet {
			if col.Width > 0 {
				// Clip: FixWidth pins the cell's box but not its painting —
				// content wider than the column (a long version string, say)
				// would otherwise bleed into the next column.
				return Attrs(FixWidth(col.Width), Clip, Pad4(0, 6, 0, 6))
			}
			return Attrs(Viewport, MinWidth(columnMinWidth), MainAlign(AlignMiddle), Pad4(0, 6, 0, 6))
		}

		// Header cells additionally Expand (fill the header row's full
		// height, not just their own text height) and carry their own
		// vertical padding (4,6,4,6, overriding columnAttrs' 0,6,0,6): the
		// row itself now has no top/bottom padding, so a cell filling it
		// via Expand reaches the row's true top/bottom edges, and the
		// cell's own padding insets the label text without shrinking the
		// cell's box (and hence its hover-highlight background). Same
		// reasoning as the horizontal fix above, just the other axis: the
		// *row's* padding is outside every cell's box, so it's the row that
		// must not have any left for the highlight to reach.
		// MainAlign(AlignMiddle): the cell is a plain (non-Row) container, so its
		// child — the label+chevron row below — is positioned along the
		// cell's *main* axis, not its cross axis. The fixed body cells don't
		// need this because they aren't Expanded, so their parent row's
		// CrossMid centers them directly; an Expanded cell fully occupies that
		// space itself, so it must center its own child (the flexible body
		// cell is Expanded too, and carries its own MA — see columnAttrs).
		headerCellAttrs := func(col TableColumn[T]) AttrSet {
			return AttrsWith(columnAttrs(col), Expand, Pad4(4, 6, 4, 6), MainAlign(AlignMiddle))
		}

		// forEachColumn is shared by the header row and every body row so
		// they can't drift apart: it inserts a spacer of separatorAttrs
		// before every column after the first, then calls cellFn. The
		// header's spacer is visible (painted with a background); the
		// body's is the same FixWidth(1) but unpainted. Writing the header
		// and body loops independently is exactly what caused this bug —
		// the header's visible separators consumed 4px the body's loop
		// didn't account for, so the one flexible (Grow) column resolved to
		// a different width in each, shifting every fixed column after it.
		forEachColumn := func(separatorAttrs AttrSet, cellFn func(i int, col TableColumn[T])) {
			for i := range columns {
				if i > 0 {
					Element(separatorAttrs)
				}
				cellFn(i, columns[i])
			}
		}

		headerSeparator := Attrs(FixWidth(1), Expand, Background(0, 0, 80, 1))
		bodySeparator := Attrs(FixWidth(1), Expand) // same width, no background: invisible

		// The header must reserve the same right-hand gutter the body rows do.
		// VirtualListView unconditionally lays its rows out in
		// (viewportWidth - SCROLLBAR_WIDTH) — see scroll.go — so its content box
		// is 20px narrower than the header, which spans the full width as a
		// sibling above the list. The flexible column absorbs that 20px, so
		// every fixed column after it ends up shifted left in the body relative
		// to the header. Matching the header's right padding (8 + SCROLLBAR_WIDTH)
		// to the body's usable width lines the two back up; the scrollbar then
		// floats over the reserved gutter rather than over the last column.
		Container(Attrs(Row, Expand, CrossMid, Pad4(0, 8+SCROLLBAR_WIDTH, 0, 8), FixHeight(tableHeaderHeight), Background(0, 0, 92, 1)), func() {
			forEachColumn(headerSeparator, func(colIndex int, col TableColumn[T]) {
				sortable := col.Less != nil

				Container(headerCellAttrs(col), func() {
					if sortable {
						if IsHovered() {
							ModAttrs(Background(0, 0, 87, 1))
						}
						if PressAction() {
							if state.Column == colIndex {
								state.Desc = !state.Desc
							} else {
								state.Column = colIndex
								state.Desc = col.DefaultDesc
							}
						}
					}

					// The label sits inside a small chip that lights up on
					// the active sort column, with the direction arrow
					// beside it — so the sorted column reads as a "pressed"
					// tag rather than a far-away chevron.
					Container(Attrs(Row, Expand, CrossMid), func() {
						active := sortable && state.Column == colIndex
						Container(Attrs(Row, CrossMid, Gap(4), Pad2(2, 5), Corners(4)), func() {
							if active {
								ModAttrs(Background(0, 0, 82, 1))
							}
							Label(col.Label, FontWeight(WeightBold), FontSize(11))
							if active {
								arrow := "↑"
								if state.Desc {
									arrow = "↓"
								}
								Label(arrow, FontWeight(WeightBold), FontSize(11))
							}
						})
					})
				})
			})
		})

		sorted := rows
		if attrs.OrderRows != nil {
			sorted = attrs.OrderRows(rows, state.Column, state.Desc)
		} else if state.Column >= 0 && state.Column < len(columns) && columns[state.Column].Less != nil {
			sorted = append([]T(nil), rows...) // copy: never mutate the caller's slice
			less := columns[state.Column].Less
			desc := state.Desc
			sort.SliceStable(sorted, func(i, j int) bool {
				if desc {
					return less(sorted[j], sorted[i])
				}
				return less(sorted[i], sorted[j])
			})
		}

		itemId := func(i int) any { return rowKey(sorted[i]) }
		itemHeight := func(i int, width f32) f32 { return attrs.RowHeight }
		itemView := func(i int, width f32) {
			row := sorted[i]
			// Pad4 here is 4/8 rather than 4/14 to match the header: each
			// column's own left/right padding (6, in columnAttrs) makes up
			// the difference, so column text lines up between the two.
			Container(Attrs(Row, Expand, CrossMid, Pad4(4, 8, 4, 8)), func() {
				if attrs.OnRow != nil {
					attrs.OnRow(i, row)
				}
				forEachColumn(bodySeparator, func(_ int, col TableColumn[T]) {
					Container(columnAttrs(col), func() {
						col.Render(row)
					})
				})
			})
		}
		VirtualListView(nil, len(sorted), itemId, itemHeight, itemView)
	})
}
