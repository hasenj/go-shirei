package main

// image-diff-2: wipe / before-after image compare via widgets.ImageWipe.
//
// Demo controls: labels, outline, accents, and diff-highlight color (alpha =
// opacity).
//
//	go run ./demos/image-diff-2

import (
	"bytes"
	_ "embed"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"log"

	"go.hasen.dev/shirei/app"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

//go:embed a.png
var aPNG []byte

//go:embed b.png
var bPNG []byte

func main() {
	var err error
	imgA, err = decodeRGBA(aPNG)
	if err != nil {
		log.Fatal("a.png: ", err)
	}
	imgB, err = decodeRGBA(bPNG)
	if err != nil {
		log.Fatal("b.png: ", err)
	}
	app.SetupWindow("Image Diff 2 — wipe split", 1100, 900)
	app.Run(root)
}

var (
	imgA, imgB *image.RGBA
	// splitT: 0 = all right (A/old), 1 = all left (B/new).
	splitT float32 = 0.5

	leftLabel          = "B new"
	rightLabel         = "A old"
	outlineTh  float32 = 6

	leftAccent  = ImageWipeLeftAccent
	rightAccent = ImageWipeRightAccent

	// Diff highlight: RGB from preset, alpha from opacity slider.
	diffHLRGB         = ImageWipeDiffHighlight // alpha ignored; see diffHL
	diffHL    float32 = 0.4
)

type colorPreset struct {
	Name  string
	Color Vec4
}

var colorPresets = []colorPreset{
	{"Green", Vec4{130, 55, 42, 1}},
	{"Red", Vec4{8, 70, 48, 1}},
	{"Magenta", Vec4{350, 80, 52, 1}},
	{"Blue", Vec4{210, 70, 48, 1}},
	{"Orange", Vec4{28, 85, 52, 1}},
	{"Purple", Vec4{280, 50, 48, 1}},
	{"Gray", Vec4{0, 0, 40, 1}},
}

func decodeRGBA(data []byte) (*image.RGBA, error) {
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if r, ok := img.(*image.RGBA); ok && r.Bounds().Min == (image.Point{}) {
		return r, nil
	}
	b := img.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(dst, dst.Bounds(), img, b.Min, draw.Src)
	return dst, nil
}

func root() {
	ModAttrs(Viewport, Background(220, 8, 96, 1), Pad(20), Gap(12))
	ScrollOnInput()

	Label("Wipe image compare", FontWeight(WeightBold), FontSize(18))
	Label("Left = B/new (revealed by split). Right = A/old (base). Diff highlight marks disagreements on both sides.",
		FontSize(13), TextColor(0, 0, 40, 1))

	// Options panel
	Container(Attrs(Gap(10), Pad(12), Corners(8),
		Background(0, 0, 100, 1),
		BorderWidth(1), BorderColor(0, 0, 85, 1),
	), func() {
		Label("Options", FontSize(13), FontWeight(WeightBold), TextColor(0, 0, 30, 1))

		Container(Attrs(Row, Wrap, Gap(16), CrossAlign(AlignStart)), func() {
			Container(Attrs(Gap(4), MinWidth(180)), func() {
				Label("Left label", FontSize(11), TextColor(0, 0, 45, 1))
				TextInput(&leftLabel)
			})
			Container(Attrs(Gap(4), MinWidth(180)), func() {
				Label("Right label", FontSize(11), TextColor(0, 0, 45, 1))
				TextInput(&rightLabel)
			})

			Container(Attrs(Gap(4), MinWidth(200)), func() {
				Label(fmt.Sprintf("Outline thickness: %.0f", outlineTh), FontSize(11), TextColor(0, 0, 45, 1))
				Slider(&outlineTh, SliderAttrs{Min: 0, Max: 16, Step: 1, Width: 180})
			})

			Container(Attrs(Gap(4)), func() {
				Label("Left accent", FontSize(11), TextColor(0, 0, 45, 1))
				colorMenu(&leftAccent)
			})
			Container(Attrs(Gap(4)), func() {
				Label("Right accent", FontSize(11), TextColor(0, 0, 45, 1))
				colorMenu(&rightAccent)
			})
		})

		// Diff highlight: one color attr; alpha = opacity
		Container(Attrs(Row, Wrap, Gap(16), CrossMid), func() {
			Container(Attrs(Gap(4), MinWidth(240)), func() {
				Label(fmt.Sprintf("Diff highlight opacity (color α): %.0f%%", diffHL*100),
					FontSize(11), TextColor(0, 0, 45, 1))
				Slider(&diffHL, SliderAttrs{Min: 0, Max: 1, Width: 220})
			})
			Container(Attrs(Gap(4)), func() {
				Label("Highlight color", FontSize(11), TextColor(0, 0, 45, 1))
				colorMenu(&diffHLRGB)
			})
			if ButtonExt("No highlight", ButtonAttrs{}, DefaultButtonLook()) {
				diffHL = 0
			}
			if ButtonExt("Strong (70%)", ButtonAttrs{}, DefaultButtonLook()) {
				diffHL = 0.7
			}
			if ButtonExt("Subtle (25%)", ButtonAttrs{}, DefaultButtonLook()) {
				diffHL = 0.25
			}
		})

		Container(Attrs(Row, CrossMid, Gap(8)), func() {
			if ButtonExt("Defaults", ButtonAttrs{}, DefaultButtonLook()) {
				leftLabel = "B new"
				rightLabel = "A old"
				outlineTh = 6
				leftAccent = ImageWipeLeftAccent
				rightAccent = ImageWipeRightAccent
				diffHLRGB = ImageWipeDiffHighlight
				diffHL = 0.4
			}
			if ButtonExt("Clear labels", ButtonAttrs{}, DefaultButtonLook()) {
				leftLabel = ""
				rightLabel = ""
			}
			if ButtonExt("Git style", ButtonAttrs{Accent: AccentMeadow}, DefaultButtonLook()) {
				leftLabel = "added"
				rightLabel = "removed"
				outlineTh = 6
				leftAccent = colorPresets[0].Color  // green
				rightAccent = colorPresets[1].Color // red
				diffHLRGB = colorPresets[5].Color   // purple
				diffHL = 0.5
			}
		})
	})

	Label(fmt.Sprintf("A/B %d×%d · reveal left: %.0f%% · highlight α: %.0f%%",
		imgA.Bounds().Dx(), imgA.Bounds().Dy(), splitT*100, diffHL*100),
		FontSize(12), TextColor(0, 0, 45, 1))

	Container(Attrs(Row, CrossMid, Gap(8)), func() {
		if ButtonExt("All right (A)", ButtonAttrs{}, DefaultButtonLook()) {
			splitT = 0
		}
		if ButtonExt("50%", ButtonAttrs{}, DefaultButtonLook()) {
			splitT = 0.5
		}
		if ButtonExt("All left (B)", ButtonAttrs{}, DefaultButtonLook()) {
			splitT = 1
		}
	})

	th := outlineTh
	if th == 0 {
		th = -1
	}
	hl := diffHLRGB
	hl[3] = diffHL

	idA := UseImage("image-diff-2/a", imgA)
	idB := UseImage("image-diff-2/b", imgB)
	ImageWipe(ImageWipeAttrs{
		LeftImage:          idB,
		RightImage:         idA,
		OutSlider:          &splitT,
		LeftAccentColor:    leftAccent,
		RightAccentColor:   rightAccent,
		OutlineThickness:   th,
		LeftLabel:          leftLabel,
		RightLabel:         rightLabel,
		DiffHighlightColor: hl,
		MaxSize:            Vec2{900, 520},
	})

	ScrollBars()
}

func colorMenu(dest *Vec4) {
	// Menu compares RGB only; preserve caller's alpha when picking a preset.
	Container(Attrs(Row, CrossMid, Gap(6)), func() {
		swatch := *dest
		swatch[3] = 1
		Element(Attrs(
			FixSize(22, 22), Corners(4),
			BackgroundVec(swatch),
			BorderWidth(1), BorderColor(0, 0, 70, 1),
		))
		MenuButton(MenuIcon, presetName(*dest), func() {
			for _, p := range colorPresets {
				p := p
				if MenuItem(NoIcon, p.Name) {
					a := dest[3]
					*dest = p.Color
					if a > 0 {
						dest[3] = a
					}
				}
			}
		})
	})
}

func presetName(c Vec4) string {
	for _, p := range colorPresets {
		if p.Color[0] == c[0] && p.Color[1] == c[1] && p.Color[2] == c[2] {
			return p.Name
		}
	}
	return "Custom"
}
