package layout_tests

// Tests for the interaction of ExpandAcross with wrapping and cross
// alignment. These started as replications of two known issues (an expanding
// item in a wrapping row was given the cross size of the whole container
// instead of its wrap line, and the cross alignment offset pushed expanded
// items out of their container); the snapshots now pin down the corrected
// behavior.

import (
	"testing"

	. "go.hasen.dev/shirei"
)

// A wrapping row where one item sets ExpandAcross: the red item stretches to
// the height of its own wrap line, matching its siblings — not to the height
// of the whole container.
func TestWrapWithExpandingItem(t *testing.T) {
	layoutSnapshot(t, "testdata/wrap_with_expanding_item.png", 280, 160, func() {
		Container(AttrSet{Padding: N4(20)}, func() {
			Container(AttrSet{
				Row: true, Wrap: true, Gap: 6, Padding: N4(10),
				MinSize: Vec2{200, 0}, MaxSize: Vec2{200, 0},
				Background: gray,
				Border:     Border{BorderColor: dark, BorderWidth: 1},
			}, func() {
				for i := range 6 {
					attrs := box(blue, 50, 30)
					if i == 4 {
						attrs.Background = red
						attrs.ExpandAcross = true
					}
					Element(attrs)
				}
			})
		})
	})
}

// A wrapping row where a taller sibling gives the expanding item room to
// stretch within its wrap line: the red item grows to match the green item's
// height (the line's cross size), while the blue items keep their intrinsic
// height. The first line is unaffected.
func TestWrapExpandWithinLine(t *testing.T) {
	layoutSnapshot(t, "testdata/wrap_expand_within_line.png", 280, 160, func() {
		Container(AttrSet{Padding: N4(20)}, func() {
			Container(AttrSet{
				Row: true, Wrap: true, Gap: 6, Padding: N4(10),
				MinSize: Vec2{200, 0}, MaxSize: Vec2{200, 0},
				Background: gray,
				Border:     Border{BorderColor: dark, BorderWidth: 1},
			}, func() {
				// line 1: uniform height
				Element(box(blue, 50, 30))
				Element(box(blue, 50, 30))
				Element(box(blue, 50, 30))
				// line 2: the taller green sibling sets the line height
				Element(box(blue, 50, 30))
				expander := box(red, 50, 0)
				expander.ExpandAcross = true
				Element(expander)
				Element(box(green, 50, 50))
			})
		})
	})
}

// Wrap, expansion, and cross alignment combined: the wrap lines as a group
// are centered in the taller container, the red item fills its own line's
// height and moves with its line, and the shorter blue item in that line is
// individually centered within the line.
func TestWrapExpandWithCrossAlign(t *testing.T) {
	layoutSnapshot(t, "testdata/wrap_expand_with_cross_align.png", 280, 220, func() {
		Container(AttrSet{Padding: N4(20)}, func() {
			Container(AttrSet{
				Row: true, Wrap: true, Gap: 6, Padding: N4(10),
				MinSize: Vec2{200, 160}, MaxSize: Vec2{200, 160},
				CrossAlign: AlignMiddle,
				Background: gray,
				Border:     Border{BorderColor: dark, BorderWidth: 1},
			}, func() {
				Element(box(blue, 50, 30))
				Element(box(blue, 50, 30))
				Element(box(blue, 50, 30))
				Element(box(blue, 50, 30))
				expander := box(red, 50, 0)
				expander.ExpandAcross = true
				Element(expander)
				Element(box(green, 50, 50))
			})
		})
	})
}

// Rows with CrossAlign middle/end containing an item that sets ExpandAcross:
// the red item fills the full inner height of its container, while the blue
// items are aligned per the container's CrossAlign.
func TestCrossAlignWithExpandingItem(t *testing.T) {
	row := func(crossAlign Alignment) {
		Container(AttrSet{
			Row: true, Gap: 8, Padding: N4(8),
			MinSize: Vec2{240, 100}, MaxSize: Vec2{240, 100},
			CrossAlign: crossAlign,
			Background: gray,
			Border:     Border{BorderColor: dark, BorderWidth: 1},
		}, func() {
			Element(box(blue, 40, 30))
			expander := box(red, 40, 0)
			expander.ExpandAcross = true
			Element(expander)
			Element(box(blue, 40, 30))
		})
	}
	layoutSnapshot(t, "testdata/cross_align_with_expanding_item.png", 280, 300, func() {
		Container(AttrSet{Padding: N4(12), Gap: 24}, func() {
			row(AlignMiddle)
			row(AlignEnd)
		})
	})
}
