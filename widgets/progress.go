package widgets

import (
	"go.hasen.dev/generic"

	. "go.hasen.dev/shirei"
)

// ProgressBarAttrs configures ProgressBarExt.
type ProgressBarAttrs struct {
	Width  f32  // track width; zero → 140
	Height f32  // track height; zero → 8
	Fill   Vec4 // completed portion; zero → DefaultAccent
	Track  Vec4 // remaining portion; zero → muted surface
	Label  string
}

// ProgressBar paints a determinate horizontal bar for frac in [0, 1].
func ProgressBar(frac f32) {
	ProgressBarExt(frac, ProgressBarAttrs{})
}

// ProgressBarExt paints a determinate bar with size/color/label overrides.
// frac is clamped to [0, 1]. Pair with Busy* for indeterminate activity.
func ProgressBarExt(frac f32, attrs ProgressBarAttrs) {
	generic.Clamp(0, &frac, 1)

	w := attrs.Width
	if w == 0 {
		w = 140
	}
	h := attrs.Height
	if h == 0 {
		h = 8
	}
	// Height × comfort (bar thickness); width stays layout.
	h = comfort(h)

	fill := AccentOrFallback(attrs.Fill, DefaultAccent)
	track := attrs.Track
	if track == (Vec4{}) {
		track = Vec4{220, 15, 84, 1}
	}
	corners := h * 0.5

	Container(Attrs(Row, CrossMid, Gap(6)), func() {
		Container(Attrs(FixWidth(w), FixHeight(h), Corners(corners), BackgroundVec(track), NoAnimate, Clip), func() {
			Element(Attrs(FixWidth(w*frac), FixHeight(h), BackgroundVec(fill), NoAnimate))
		})
		if attrs.Label != "" {
			Label(attrs.Label, FontSize(9), TextColor(0, 0, 45, 1))
		}
	})
}
