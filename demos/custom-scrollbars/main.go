package main

// custom-scrollbars demos ScrollBarExt skins and the app-wide DefaultScrollBar.
// The package default is a modern overlay; this gallery shows that look next
// to classic and themed skins. Interaction stays in widgets.ScrollBarExt.
//
//	go run ./demos/custom-scrollbars

import (
	"os"
	"fmt"

	"go.hasen.dev/shirei/app"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

func main() {
	if len(os.Args) >= 3 && os.Args[1] == "--png" {
		if err := RenderToPNG(os.Args[2], 960, 640, root); err != nil {
			fmt.Println("render to png failed:", err)
			os.Exit(1)
		}
		return
	}
	// Outer ScrollBars() at the bottom of root uses the package default
	// (modern). Panels call ScrollBarExt / styles directly for side-by-side
	// comparison.
	app.SetupWindow("Custom Scrollbars", 960, 640)
	app.Run(root)
}

func root() {
	ModAttrs(Viewport, Background(220, 10, 96, 1), Pad(24), Gap(16))
	ScrollOnInput()

	Label("Custom scrollbars via ScrollBarExt", FontWeight(WeightBold), FontSize(18))
	Label("Package default is the modern overlay (neutral gray; darker on hover/drag). Skins below call ScrollBarExt directly; the window ScrollBars() at the bottom uses DefaultScrollBarStyle.",
		FontSize(13), TextColor(0, 0, 40, 1))

	Container(Attrs(Row, Expand, Grow(1), Gap(14)), func() {
		scrollPanel("Package default", "DefaultScrollBarStyle() — modern overlay", DefaultScrollBarStyle)
		scrollPanel("Classic", "Former default: white track, accent pill + grip", classicBar)
	})
	Container(Attrs(Row, Expand, Grow(1), Gap(14)), func() {
		scrollPanel("Windows 98", "Silver track, raised 3D thumb", win98Bar)
		scrollPanel("Cool blue", "Pale trough, powder-blue thumb with white grip (XP-ish)", coolBlueBar)
	})

	// Uses DefaultScrollBar → DefaultScrollBarStyle (modern)
	ScrollBars()
}

func sectionTitle(title, sub string) {
	Container(Attrs(Gap(2)), func() {
		Label(title, FontWeight(WeightBold), FontSize(13), TextColor(0, 0, 20, 1))
		Label(sub, FontSize(11), TextColor(0, 0, 45, 1))
	})
}

// scrollPanel is a titled card with a Viewport body and a custom bar builder.
func scrollPanel(title, sub string, bar func() ContainerId) {
	Container(Attrs(Grow(1), Expand, Gap(8)), func() {
		sectionTitle(title, sub)
		Container(Attrs(Grow(1), Expand, Corners(6),
			Background(0, 0, 100, 1),
			BorderWidth(1), BorderColor(0, 0, 0, 0.1),
			Clip,
		), func() {
			Container(Attrs(Viewport, Pad(12), Gap(6)), func() {
				ScrollOnInput()
				bar()
				for i := 0; i < 40; i++ {
					Container(Attrs(Expand, Pad2(6, 8), Corners(3),
						Background(220, 8, 94+float32(i%3), 1)), func() {
						Label(fmt.Sprintf("Row %02d — scroll me", i+1), FontSize(13))
					})
				}
			})
		})
	})
}

// ---------------------------------------------------------------------------
// Classic — former package default (white track, accent pill + grip icon)
// ---------------------------------------------------------------------------

func classicBar() ContainerId {
	return ScrollBarExt(ScrollBarAttrs{
		TrackWidth:     16,
		TrackBG:        Vec4{0, 0, 100, 1},
		TrackPad:       1,
		ThumbMinHeight: 30,
		Thumb:          classicThumb,
	})
}

func classicThumb(size Vec2) {
	accent := AccentOrFallback(Vec4{}, DefaultAccent)
	border := Vec4{accent[0], accent[1], accent[2] - 15, accent[3]}
	Container(Attrs(
		FixSizeVec(size),
		Corners(size[0]/2),
		BackgroundVec(accent),
		BorderColorVec(border),
		BorderWidth(1),
		Center,
	), func() {
		Icon(TypArrowUnsorted, FontSize(12), TextColor(0, 0, 100, 0.6))
	})
}

// ---------------------------------------------------------------------------
// Windows 98 — classic raised thumb on a silver trough
// ---------------------------------------------------------------------------

func win98Bar() ContainerId {
	return ScrollBarExt(ScrollBarAttrs{
		TrackWidth:     16,
		TrackBG:        Vec4{0, 0, 82, 1}, // silver face
		TrackPad:       1,
		ThumbMinHeight: 28,
		Thumb:          win98Thumb,
	})
}

func win98Thumb(size Vec2) {
	// Raised bevel: light top-left, dark bottom-right. Every layer fills `size`
	// so Center can place the grip in the middle of the thumb (Expand alone
	// only grows the cross-axis, so grips were stuck at the top).
	hi := Vec4{0, 0, 100, 1}
	lo := Vec4{0, 0, 40, 1}
	face := Vec4{0, 0, 82, 1}
	w, h := size[0], size[1]
	Container(Attrs(FixSize(w, h), BackgroundVec(lo), Pad4(0, 1, 1, 0)), func() {
		Container(Attrs(FixSize(w-1, h-1), BackgroundVec(hi), Pad4(1, 0, 0, 1)), func() {
			Container(Attrs(FixSize(w-2, h-2), BackgroundVec(face), Center), func() {
				Container(Attrs(Gap(2)), func() {
					ridgeW := w * 0.45
					if ridgeW < 4 {
						ridgeW = 4
					}
					for i := 0; i < 3; i++ {
						Element(Attrs(FixSize(ridgeW, 1), Background(0, 0, 50, 0.55)))
					}
				})
			})
		})
	})
}

// ---------------------------------------------------------------------------
// Cool blue — soft powder-blue thumb on a pale trough (XP-ish reference:
// light face, slightly darker rim, white grip ticks). Uses package Accent for
// the face so retheming the app retints the bar.
// ---------------------------------------------------------------------------

func coolBlueBar() ContainerId {
	return ScrollBarExt(ScrollBarAttrs{
		TrackWidth:     16,
		TrackBG:        Vec4{210, 8, 94, 1},
		TrackPad:       2,
		ThumbMinHeight: 28,
		Thumb:          coolBlueThumb,
	})
}

func coolBlueThumb(size Vec2) {
	// Face/rim from package accent (fallback powder-blue if unset).
	accent := AccentOrFallback(Vec4{}, DefaultAccent)
	face := accent
	rimL := accent[2] - 12
	if rimL < 0 {
		rimL = 0
	}
	rim := Vec4{accent[0], accent[1], rimL, accent[3]}
	w, h := size[0], size[1]
	Container(Attrs(FixSize(w, h), Corners(3), BackgroundVec(rim), Pad(1)), func() {
		Container(Attrs(FixSize(w-2, h-2), Corners(2), BackgroundVec(face), Center), func() {
			Container(Attrs(Gap(2)), func() {
				tickW := w * 0.45
				if tickW < 4 {
					tickW = 4
				}
				for i := 0; i < 3; i++ {
					Element(Attrs(FixSize(tickW, 1.5), Corners(0.5),
						Background(0, 0, 100, 0.85)))
				}
			})
		})
	})
}
