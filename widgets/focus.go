package widgets

import . "go.hasen.dev/shirei"

// widgetFocusOutline uses the space around a small face for its focus cue.
func widgetFocusOutline(size Vec2, radius float32, color Vec4) {
	const outset float32 = 2
	size = Vec2Add(size, Vec2{2 * outset, 2 * outset})
	Container(Attrs(Float(-outset, -outset), FixSizeVec(size), NoClip, ClickThrough, NoAnimate), func() {
		widgetFocusBorder(size, radius+outset, color)
	})
}

// widgetFocusBorder paints a two-logical-point edge at half the supplied opacity.
// The stroke stays inside its bounds so it also fits clipped trays.
func widgetFocusBorder(size Vec2, radius float32, color Vec4) {
	if color[3] <= 0 || size[0] <= 0 || size[1] <= 0 {
		return
	}
	color[3] *= .5
	Element(Attrs(Float(0, 0), FixSizeVec(size), Corners(radius),
		BorderWidth(2), BorderColorVec(color), ClickThrough, NoAnimate))
}
