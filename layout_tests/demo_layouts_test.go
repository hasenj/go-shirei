package layout_tests

// Layouts adapted from the demo programs (demo1, demo4, demo5, demo6,
// demo10), with text replaced by fixed-size boxes so the snapshots don't
// depend on system fonts.

import (
	"testing"

	. "go.hasen.dev/shirei"
)

// from demo1: every combination of orientation, main alignment, and cross
// alignment, in one grid
func TestAlignmentGrid(t *testing.T) {
	aligns := [3]Alignment{AlignStart, AlignMiddle, AlignEnd}
	cell := func(row bool, ma Alignment, ca Alignment) {
		Container(AttrSet{
			Row: row, MainAlign: ma, CrossAlign: ca,
			MinSize: Vec2{120, 80}, MaxSize: Vec2{120, 80},
			Padding: N4(4), Gap: 4,
			Background: gray,
			Border:     Border{BorderColor: dark, BorderWidth: 1},
		}, func() {
			Element(box(red, 14, 14))
			Element(box(green, 14, 24))
			Element(box(blue, 14, 14))
		})
	}
	layoutSnapshot(t, "testdata/alignment_grid.png", 560, 460, func() {
		Container(AttrSet{Row: true, Wrap: true, Gap: 8, Padding: N4(8), MaxSize: WindowSize}, func() {
			for _, row := range [2]bool{true, false} {
				for _, ma := range aligns {
					for _, ca := range aligns {
						cell(row, ma, ca)
					}
				}
			}
		})
	})
}

// from demo6: app shell with an intrinsic-width sidebar that expands
// vertically, and a main content area that grows and centers its content
func TestAppShell(t *testing.T) {
	layoutSnapshot(t, "testdata/app_shell.png", 500, 300, func() {
		Container(AttrSet{Row: true, Gap: 10, Padding: N4(10), MinSize: WindowSize, MaxSize: WindowSize, Background: gray}, func() {
			// sidebar: file list with a selected row
			Container(AttrSet{
				ExpandAcross: true, Gap: 2, Padding: N4(2),
				Background: Vec4{0, 0, 100, 1},
				Border:     Border{BorderColor: dark, BorderWidth: 1},
			}, func() {
				for i := range 6 {
					attrs := box(gray, 100, 16)
					if i == 2 {
						attrs.Background = red // selected item
					}
					Element(attrs)
				}
			})
			// main content area
			Container(AttrSet{
				ExtrinsicSize: true, Grow: 1, ExpandAcross: true,
				MainAlign: AlignMiddle, CrossAlign: AlignMiddle,
				Background: dark,
			}, func() {
				Element(box(blue, 120, 80))
			})
		})
	})
}

// from demo10: kanban board with fixed-width lanes; each lane has a title
// bar, a growing card column, and a 1px divider above a footer strip
func TestKanbanBoard(t *testing.T) {
	layoutSnapshot(t, "testdata/kanban_board.png", 520, 300, func() {
		cardCounts := [3]int{3, 1, 2}
		Container(AttrSet{Row: true, Gap: 10, Padding: N4(10), Clip: true, MinSize: WindowSize, MaxSize: WindowSize}, func() {
			for _, n := range cardCounts {
				Container(AttrSet{ExpandAcross: true, Gap: 8}, func() {
					// lane title bar
					Element(AttrSet{ExpandAcross: true, MinSize: Vec2{0, 24}, Background: dark})
					// card column
					Container(AttrSet{
						Grow: 1, MinSize: Vec2{150, 0}, MaxSize: Vec2{150, 0},
						Padding: N4(8), Gap: 8, Background: gray,
					}, func() {
						for i := range n {
							colors := [3]Vec4{blue, green, red}
							Element(AttrSet{ExpandAcross: true, MinSize: Vec2{0, 40}, Background: colors[i]})
						}
						Element(AttrSet{Grow: 1}) // push footer to the bottom
						Element(AttrSet{ExpandAcross: true, MinSize: Vec2{0, 1}, Background: dark}) // divider
						Element(AttrSet{ExpandAcross: true, MinSize: Vec2{0, 20}, Background: Vec4{0, 0, 100, 1}})
					})
				})
			}
		})
	})
}

// from demo5: split panes sized from GetAvailableSize (previous-frame data)
func TestSplitPanes(t *testing.T) {
	layoutSnapshot(t, "testdata/split_panes.png", 400, 240, func() {
		Container(AttrSet{Row: true, Padding: N4(8), MinSize: WindowSize, MaxSize: WindowSize}, func() {
			const splitterSize = 8
			const splitterOffset = 40 // drag offset from the center
			avail := GetAvailableSize()
			w1 := max(0, (avail[0]-splitterSize)/2-splitterOffset)
			w2 := max(0, avail[0]-splitterSize-w1)
			Container(AttrSet{MinSize: Vec2{w1, 0}, MaxSize: Vec2{w1, 0}, ExpandAcross: true, ExtrinsicSize: true, Padding: N4(8), Background: gray}, func() {
				Element(box(red, 60, 30))
			})
			Element(AttrSet{MinSize: Vec2{splitterSize, splitterSize}, ExpandAcross: true, Background: dark})
			Container(AttrSet{MinSize: Vec2{w2, 0}, MaxSize: Vec2{w2, 0}, ExpandAcross: true, ExtrinsicSize: true, Padding: N4(8), Background: gray}, func() {
				Element(box(blue, 60, 30))
			})
		})
	})
}

// from demo4: a panel floated just below an anchor element, positioned via
// GetResolvedRectOf (previous-frame data)
func TestAnchoredPanel(t *testing.T) {
	layoutSnapshot(t, "testdata/anchored_panel.png", 300, 220, func() {
		Container(AttrSet{Padding: N4(16), Gap: 8}, func() {
			Element(box(gray, 80, 24))
			anchorId := ElementWithKey("anchor-btn", box(blue, 80, 24))
			Element(box(gray, 80, 24))

			anchor := GetResolvedRectOf(anchorId)
			pos := anchor.Origin
			pos[0] += 20
			pos[1] += anchor.Size[1] + 4
			Container(AttrSet{
				Floats: true, Float: pos, Z: 10,
				Padding: N4(8), Gap: 4,
				Background: Vec4{0, 0, 100, 1},
				Border:     Border{BorderColor: dark, BorderWidth: 1},
			}, func() {
				Element(box(red, 90, 14))
				Element(box(green, 90, 14))
				Element(box(green, 90, 14))
			})
		})
	})
}

// overlapping floats drawn in Z order, not declaration order
func TestZOrder(t *testing.T) {
	layoutSnapshot(t, "testdata/z_order.png", 200, 200, func() {
		Container(AttrSet{Padding: N4(20)}, func() {
			// declared first but highest Z: drawn on top
			Element(AttrSet{Floats: true, Float: Vec2{40, 40}, Z: 2, MinSize: Vec2{60, 60}, Background: red})
			Element(AttrSet{Floats: true, Float: Vec2{60, 60}, Z: 1, MinSize: Vec2{60, 60}, Background: green})
			Element(AttrSet{Floats: true, Float: Vec2{80, 80}, MinSize: Vec2{60, 60}, Background: blue})
		})
	})
}

// a clipped container scrolled down; the offset is clamped against
// previous-frame content size
func TestScrolledContent(t *testing.T) {
	layoutSnapshot(t, "testdata/scrolled_content.png", 200, 200, func() {
		Container(AttrSet{Padding: N4(20)}, func() {
			Container(AttrSet{MinSize: Vec2{120, 120}, MaxSize: Vec2{120, 120}, Clip: true, Padding: N4(4), Gap: 4, Background: gray}, func() {
				SetScrollOffset(Vec2{0, 50})
				colors := [3]Vec4{red, green, blue}
				for i := range 10 {
					Element(box(colors[i%3], 80, 20))
				}
			})
		})
	})
}
