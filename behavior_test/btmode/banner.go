package btmode

import (
	. "go.hasen.dev/shirei"
)

// VerdictBanner paints a large floating SUCCESS / FAIL when done.
// Call every frame from the window UI (no-op while !done).
func VerdictBanner(done, ok bool, detail string) {
	if !done {
		return
	}
	title := "FAIL"
	// HSLA: red-ish fail, green-ish success
	bgH, bgS, bgL := f32(8), f32(70), f32(42)
	fgL := f32(98)
	if ok {
		title = "SUCCESS"
		bgH, bgS, bgL = 140, 55, 38
	}
	sz := GetHost().WindowSize
	// Above popup drain Z (ui.popupZ is 1-based drain index); InFront (1) loses to panels.
	Container(Attrs(
		Float(0, 0), FixSize(sz[0], sz[1]),
		Center, Z(1e6), ClickThrough, NoAnimate,
	), func() {
		Container(Attrs(
			Pad2(22, 48), Gap(10), Corners(14),
			Background(bgH, bgS, bgL, 0.94),
			BorderWidth(1), BorderColor(0, 0, 100, 0.2),
			CrossAlign(AlignMiddle),
		), func() {
			Label(title, FontSize(44), FontWeight(WeightBold), TextColor(0, 0, fgL, 1))
			if detail != "" {
				Label(detail, FontSize(14), TextColor(0, 0, fgL, 0.9))
			}
		})
	})
}

type f32 = float32
