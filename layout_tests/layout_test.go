package layout_tests

import (
	"testing"

	. "go.hasen.dev/shirei"
)

var red = Vec4{0, 70, 50, 1}
var green = Vec4{120, 70, 40, 1}
var blue = Vec4{220, 70, 50, 1}
var gray = Vec4{0, 0, 85, 1}
var dark = Vec4{0, 0, 20, 1}

func box(color Vec4, w float32, h float32) AttrSet {
	return AttrSet{MinSize: Vec2{w, h}, Background: color}
}

func TestRowLayout(t *testing.T) {
	layoutSnapshot(t, "testdata/row_layout.png", 400, 200, func() {
		Container(AttrSet{Row: true, Gap: 10, Padding: N4(20), Background: gray}, func() {
			Element(box(red, 40, 40))
			Element(box(green, 40, 60))
			Element(box(blue, 40, 40))
		})
	})
}

func TestGrowAndAlign(t *testing.T) {
	layoutSnapshot(t, "testdata/grow_and_align.png", 400, 200, func() {
		Container(AttrSet{Row: true, Gap: 8, Padding: N4(8), MinSize: WindowSize, CrossAlign: AlignMiddle, Background: gray}, func() {
			Element(box(red, 40, 40))
			ElementWithKey("grower", AttrSet{Grow: 1, MinSize: Vec2{0, 60}, Background: green})
			Container(AttrSet{SelfAlign: AlignEnd, Background: blue, Padding: N4(10)}, func() {
				Element(box(dark, 20, 20))
			})
		})
	})
}

func TestCenteredWithBorder(t *testing.T) {
	layoutSnapshot(t, "testdata/centered_with_border.png", 300, 300, func() {
		Container(AttrSet{MinSize: WindowSize, MainAlign: AlignMiddle, CrossAlign: AlignMiddle}, func() {
			Container(AttrSet{
				Padding:    N4(16),
				Gap:        6,
				Background: gray,
				Border:     Border{BorderColor: dark, BorderWidth: 2},
			}, func() {
				Element(box(red, 80, 20))
				Element(box(green, 80, 20))
				Element(box(blue, 80, 20))
			})
		})
	})
}

func TestWrapping(t *testing.T) {
	layoutSnapshot(t, "testdata/wrapping.png", 300, 200, func() {
		Container(AttrSet{Row: true, Wrap: true, Gap: 6, Padding: N4(10), MaxSize: Vec2{150, 0}, Background: gray}, func() {
			for range 9 {
				Element(box(blue, 30, 30))
			}
		})
	})
}

func TestClippingAndFloat(t *testing.T) {
	layoutSnapshot(t, "testdata/clipping_and_float.png", 300, 200, func() {
		Container(AttrSet{Row: true, Gap: 20, Padding: N4(20)}, func() {
			// the floating child leaks outside the unclipped container
			Container(AttrSet{MinSize: Vec2{80, 80}, MaxSize: Vec2{80, 80}, Background: gray}, func() {
				Element(AttrSet{Floats: true, Float: Vec2{60, 60}, MinSize: Vec2{40, 40}, Background: red})
			})
			// same thing clipped
			Container(AttrSet{MinSize: Vec2{80, 80}, MaxSize: Vec2{80, 80}, Background: gray, Clip: true}, func() {
				Element(AttrSet{Floats: true, Float: Vec2{60, 60}, MinSize: Vec2{40, 40}, Background: red})
			})
		})
	})
}

func TestTransparency(t *testing.T) {
	layoutSnapshot(t, "testdata/transparency.png", 200, 200, func() {
		Container(AttrSet{Padding: N4(20)}, func() {
			Container(AttrSet{Transparency: 0.5, Background: blue, Padding: N4(20)}, func() {
				Element(box(red, 60, 60))
			})
		})
	})
}
