package main

// custom-segmented is a proof of concept for ProcessSegmentEvents: default
// SegmentedControl for comparison, plus two Apple skins (modern sliding
// white pill, and iOS 7 blue outline / filled selection).

import (
	"os"
	"fmt"

	"go.hasen.dev/shirei/app"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

func main() {
	if len(os.Args) >= 3 && os.Args[1] == "--png" {
		if err := RenderToPNG(os.Args[2], 720, 560, root); err != nil {
			fmt.Println("render to png failed:", err)
			os.Exit(1)
		}
		return
	}
	app.SetupWindow("Custom Segmented", 720, 560)
	app.Run(root)
}

var (
	defaultView = "list"
	appleView = "day"
	appleSize = "m"
	ios7Kind  = "movie"
	ios7Filter = "all"
	actionLog = "pick a segment…"
)

func root() {
	ModAttrs(Viewport, Background(220, 12, 96, 1), Pad(28), Gap(24))
	ScrollOnInput()

	Label("Custom segmented via ProcessSegmentEvents", FontWeight(WeightBold), FontSize(18))
	Label("Per-cell process returns Selected / BecameSelected / Prev / Local / SelectedAt. Demo skins stay out of the catalogue.",
		FontSize(13), TextColor(0, 0, 40, 1))

	// --- default ----------------------------------------------------------------
	section("Default SegmentedControl")
	if SegmentedControl(&defaultView,
		Cell("List", "list"),
		Cell("Grid", "grid"),
		Cell("Gallery", "gallery"),
	) {
		note(fmt.Sprintf("default → %s", defaultView))
	}
	Label(fmt.Sprintf("value: %s", defaultView), FontSize(12), TextColor(0, 0, 45, 1))

	// --- Apple ----------------------------------------------------------------
	section("Apple-style (sliding white pill)")
	Label("The pill is one ContainerWithKey floated under the cells at the selected index.",
		FontSize(12), TextColor(0, 0, 45, 1))

	AppleSegmented(&appleView,
		Cell("Day", "day"),
		Cell("Week", "week"),
		Cell("Month", "month"),
		Cell("Year", "year"),
	)
	Label(fmt.Sprintf("value: %s", appleView), FontSize(12), TextColor(0, 0, 45, 1))

	AppleSegmented(&appleSize,
		Cell("S", "s"),
		Cell("M", "m"),
		Cell("L", "l"),
	)
	Label(fmt.Sprintf("size: %s", appleSize), FontSize(12), TextColor(0, 0, 45, 1))

	// --- iOS 7 ----------------------------------------------------------------
	section("iOS 7 style (blue outline / filled selection)")
	Label("Classic UISegmentedControl: blue hairline frame and dividers, selected cell is solid tint with white label.",
		FontSize(12), TextColor(0, 0, 45, 1))

	IOS7Segmented(&ios7Kind,
		Cell("Movie", "movie"),
		Cell("TV Show", "tv"),
		Cell("Cartoons", "cartoons"),
	)
	Label(fmt.Sprintf("kind: %s", ios7Kind), FontSize(12), TextColor(0, 0, 45, 1))

	IOS7Segmented(&ios7Filter,
		Cell("All", "all"),
		Cell("Unread", "unread"),
		Cell("Flagged", "flagged"),
	)
	Label(fmt.Sprintf("filter: %s", ios7Filter), FontSize(12), TextColor(0, 0, 45, 1))

	// --- log ------------------------------------------------------------------
	Container(Attrs(Pad2(10, 14), Background(0, 0, 100, 1), Corners(4),
		BorderWidth(1), BorderColor(0, 0, 0, 0.08)), func() {
		Label("Last action", FontSize(11), TextColor(0, 0, 45, 1))
		Label(actionLog, FontSize(14), FontWeight(WeightBold))
	})
	ScrollBars()
}

func section(title string) {
	Label(title, FontWeight(WeightBold), FontSize(14), TextColor(0, 0, 30, 1))
}

func note(s string) {
	actionLog = s
}

// ---------------------------------------------------------------------------
// AppleSegmented — gray track, equal-width cells, white pill under the
// selected cell (same ContainerWithKey identity; Float X jumps with selection).
// ---------------------------------------------------------------------------

// AppleSegmented is an iOS-style segmented control. Demo only.
// Tuned toward common iOS HIG references: modest corner radius (not a full
// stadium), a few px of track inset so the white pill has air above/below,
// and a light drop shadow rather than BoxShadow's default heavy alpha.
func AppleSegmented[T comparable](target *T, cells ...SegmentedCell[T]) {
	if len(cells) == 0 {
		return
	}

	const (
		height  float32 = 36
		pad     float32 = 3 // track inset; pill must Float to (pad, pad), not y=0
		cellW   float32 = 84
		cellH   float32 = height - pad*2
		// Modest rounding — reference controls are softly rounded, not capsules.
		trackR  float32 = 9
		pillR   float32 = 7
		labelSz float32 = 13
	)

	// Selected index for pill placement.
	sel := 0
	for i, c := range cells {
		if *target == c.Value {
			sel = i
			break
		}
	}
	// Float is relative to the track's outer top-left (padding is not
	// pre-applied to Float), so both axes include pad.
	pillX := pad + float32(sel)*cellW
	pillY := pad

	// Track. No Clip — lets the light pill shadow sit on the track without
	// being shaved at the edges.
	Container(Attrs(Row, FixHeight(height),
		Corners(trackR),
		Background(0, 0, 91, 1),
		Pad(pad),
	), func() {
		// Sliding selection pill — stable key so layout anim can move it.
		// Behind the labels; ClickThrough so presses hit the cell on top.
		ContainerWithKey("apple-pill", Attrs(
			Float(pillX, pillY),
			FixSize(cellW, cellH),
			Corners(pillR),
			Background(0, 0, 100, 1),
			// Soft shadow: BoxShadow defaults to alpha 0.5, which is too heavy.
			func(a *AttrSet) {
				a.Shadow.Alpha = 0.14
				a.Shadow.Blur = 3
				a.Shadow.Offset[1] = 1
			},
			Behind,
			ClickThrough,
		), func() {})

		for _, c := range cells {
			ContainerWithKey(c.Value, Attrs(
				FixSize(cellW, cellH),
				Corners(pillR),
				Row, CrossMid,
			), func() {
				st := ProcessSegmentEvents(target, c.Value, false)
				if st.BecameSelected {
					note(fmt.Sprintf("apple %v → %v  (prev %v, local %.0f,%.0f)",
						c.Label, c.Value, st.Prev, st.Local[0], st.Local[1]))
				}

				// Hover fill on unselected cells (reference "Hover" row).
				if st.Hovered && !st.Selected {
					ModAttrs(Background(0, 0, 86, 1))
				}

				clr := Vec4{0, 0, 28, 1}
				weight := WeightNormal
				if st.Selected {
					clr = Vec4{0, 0, 8, 1}
					weight = WeightSemibold
				} else if st.Hovered {
					clr = Vec4{0, 0, 18, 1}
				}
				if st.Active && !st.Selected {
					clr = Vec4{0, 0, 40, 1}
				}

				Filler(1)
				Label(c.Label, FontSize(labelSz), FontWeight(weight), TextColorVec(clr))
				Filler(1)
			})
		}
	})
}

// ---------------------------------------------------------------------------
// IOS7Segmented — classic iOS 7 UISegmentedControl chrome.
//
// Blue outline frame, blue vertical dividers, white unselected cells with
// blue labels, solid blue fill + white label when selected. No sliding pill;
// selection is a filled segment (edge-to-edge within the frame).
// Reference: flat blue #007AFF outline control with tinted selection.
// ---------------------------------------------------------------------------

// ios7Blue is system blue as used by iOS 7 segmented controls (~#007AFF).
var ios7Blue = Vec4{211, 100, 50, 1}

// IOS7Segmented is an iOS 7–style segmented control. Demo only.
func IOS7Segmented[T comparable](target *T, cells ...SegmentedCell[T]) {
	if len(cells) == 0 {
		return
	}

	const (
		height  float32 = 29 // classic control height was ~29pt
		cellW   float32 = 100
		border  float32 = 1
		frameR  float32 = 5 // soft outer rounding (not a stadium)
		labelSz float32 = 13
	)

	accent := ios7Blue

	Container(Attrs(Row,
		Corners(frameR),
		Background(0, 0, 100, 1),
		BorderWidth(border),
		BorderColorVec(accent),
		Clip,
	), func() {
		for i, c := range cells {
			var rl, rr float32
			if i == 0 {
				rl = frameR - 1
			}
			if i == len(cells)-1 {
				rr = frameR - 1
			}

			ContainerWithKey(c.Value, Attrs(
				FixHeight(height),
				FixWidth(cellW),
				Row, CrossMid,
				Corners4(rl, rr, rr, rl),
			), func() {
				st := ProcessSegmentEvents(target, c.Value, false)
				if st.BecameSelected {
					note(fmt.Sprintf("iOS7 %v → %v  (prev %v)", c.Label, c.Value, st.Prev))
				}

				bg := Vec4{0, 0, 100, 1}
				text := accent
				weight := WeightNormal
				if st.Selected {
					bg = accent
					text = Vec4{0, 0, 100, 1}
					weight = WeightMedium
				} else if st.Hovered {
					// Light blue wash on hover (readable affordance).
					bg = Vec4{accent[0], accent[1] * 0.15, 97, 1}
				}
				if st.Active && !st.Selected {
					bg = Vec4{accent[0], accent[1] * 0.25, 94, 1}
				}
				ModAttrs(BackgroundVec(bg))

				Filler(1)
				Label(c.Label, FontSize(labelSz), FontWeight(weight), TextColorVec(text))
				Filler(1)
			})

			if i < len(cells)-1 {
				// Blue hairline divider between segments.
				Element(Attrs(FixWidth(border), FixHeight(height), BackgroundVec(accent)))
			}
		}
	})
}
