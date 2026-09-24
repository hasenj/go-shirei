package widgets

import (
	"fmt"

	"go.hasen.dev/generic"

	. "go.hasen.dev/shirei"
)

// ProgressBarAttrs configures ProgressBarExt.
type ProgressBarAttrs struct {
	Width  f32  // track width; zero → 140
	Height f32  // track height; zero → 8
	Fill   Vec4 // completed portion; zero → scheme fill
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
	style := CurrentColorScheme.Progress
	if attrs.Fill != (Vec4{}) {
		style.Fill = attrs.Fill
	}
	if attrs.Track != (Vec4{}) {
		style.Track = attrs.Track
	}
	ProgressBarStyled(frac, attrs, style)
}

// ProgressBarStyled uses literal paint; Fill and Track overrides in attrs are ignored.
// The optional label inherits its container's text color.
func ProgressBarStyled(frac f32, attrs ProgressBarAttrs, style ProgressStyle) {
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

	corners := h * 0.5

	Container(Attrs(Row, CrossMid, Gap(6)), func() {
		NextAccessRole("progressbar")
		NextAccessValue(fmt.Sprintf("%g", frac))
		AssignAccess()
		Container(Attrs(FixWidth(w), FixHeight(h), Corners(corners), BackgroundVec(style.Track), NoAnimate, Clip), func() {
			Element(Attrs(FixWidth(w*frac), FixHeight(h), BackgroundVec(style.Fill), NoAnimate))
		})
		if attrs.Label != "" {
			Label(attrs.Label, FontSize(9))
		}
	})
}
