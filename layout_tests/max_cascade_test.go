package layout_tests

// Snapshot for cascading a parent's cross-axis MaxSize into children that
// did not set their own. The synthetic case is a column with MaxWidth whose
// children (and nested "text-like" wide content) omit MaxWidth: without the
// cascade they overflow the column; with it they clamp to available width
// after parent padding.

import (
	"testing"

	. "go.hasen.dev/shirei"
)

// Column with an explicit MaxWidth. Nested boxes do not set MaxWidth of their
// own. Wide content wants 200px; the column only allows 120.
//
// Without cascade: each row sizes to its content and spills past the column
// (parent max only clamps the parent box; children stay wide).
// With cascade: each child inherits max width = parent max − horizontal pad,
// so nested content clamps the same way (padding peeled at each level).
func TestMaxWidthCascadeColumn(t *testing.T) {
	layoutSnapshot(t, "testdata/max_width_cascade_column.png", 280, 220, func() {
		// Outer pad so the overflow is visible against the canvas.
		Container(AttrSet{Padding: N4(20), Background: Vec4{0, 0, 95, 1}}, func() {
			// Column: max width 120, pad 10 → available 100 for children.
			Container(AttrSet{
				Gap:        6,
				Padding:    N4(10),
				MaxSize:    Vec2{120, 0},
				Background: gray,
				Border:     Border{BorderColor: dark, BorderWidth: 1},
			}, func() {
				// Content-sized header row (short): no overflow either way.
				Element(box(blue, 40, 16))

				// Nested box with its own pad 8. Wide "text" content (200×14).
				// Cascaded max on this box: 100. Inner content max: 100−16=84.
				Container(AttrSet{
					Padding:    N4(8),
					Background: green,
					Border:     Border{BorderColor: dark, BorderWidth: 1},
				}, func() {
					Element(box(red, 200, 14))
					Element(box(red, 200, 14))
				})

				// Sibling wide leaf with no padding of its own.
				Element(box(red, 200, 20))
			})
		})
	})
}

// Row with MaxHeight: cross-axis cascade should clamp child heights, not
// widths. Wide buttons stay content-width; tall content is capped.
func TestMaxHeightCascadeRow(t *testing.T) {
	layoutSnapshot(t, "testdata/max_height_cascade_row.png", 320, 160, func() {
		Container(AttrSet{Padding: N4(20), Background: Vec4{0, 0, 95, 1}}, func() {
			// Row: max height 50, pad 8 → available 34 for children.
			Container(AttrSet{
				Row:        true,
				Gap:        8,
				Padding:    N4(8),
				MaxSize:    Vec2{0, 50},
				Background: gray,
				Border:     Border{BorderColor: dark, BorderWidth: 1},
			}, func() {
				Element(box(blue, 40, 20))  // short: unaffected
				Element(box(red, 40, 80))   // tall: should clamp with cascade
				Element(box(green, 60, 20)) // wide+short: width free, height free
			})
		})
	})
}

// Explicit child MaxSize wins over the cascade (child already set → no write).
func TestMaxCascadeChildOverride(t *testing.T) {
	layoutSnapshot(t, "testdata/max_cascade_child_override.png", 280, 160, func() {
		Container(AttrSet{Padding: N4(20)}, func() {
			Container(AttrSet{
				Gap:        6,
				Padding:    N4(10),
				MaxSize:    Vec2{120, 0},
				Background: gray,
				Border:     Border{BorderColor: dark, BorderWidth: 1},
			}, func() {
				// Would cascade to 100; explicit MaxWidth 60 is smaller and kept.
				Element(AttrSet{
					MinSize:    Vec2{200, 20},
					MaxSize:    Vec2{60, 0},
					Background: blue,
				})
				// Cascaded clamp at 100.
				Element(box(red, 200, 20))
			})
		})
	})
}

// UnsetMaxCross opts out of a cascaded constraint. Attrs(UnsetMaxCross)
// sticks (maxCrossUnset), like YesAnimate under Viewport — no ModAttrs needed.
func TestMaxCascadeUnset(t *testing.T) {
	layoutSnapshot(t, "testdata/max_cascade_unset.png", 280, 160, func() {
		Container(AttrSet{Padding: N4(20)}, func() {
			Container(AttrSet{
				Gap:        6,
				Padding:    N4(10),
				MaxSize:    Vec2{120, 0},
				Background: gray,
				Border:     Border{BorderColor: dark, BorderWidth: 1},
			}, func() {
				// Drop the cascaded max so the wide child may overflow.
				// UnsetMaxCross in Attrs survives cascade (maxCrossUnset).
				Container(AttrsWith(AttrSet{Background: green}, UnsetMaxCross), func() {
					Element(box(red, 200, 20))
				})
				// Still cascaded.
				Element(box(blue, 200, 20))
			})
		})
	})
}
