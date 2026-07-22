package main

import (
	"bytes"
	"image"
	"image/draw"
	_ "image/png"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

// Base proportions for black keys relative to white (from a typical one-row board).
const (
	blackWRatio = 34.0 / 56.0
	blackHRatio = 128.0 / 210.0
	keyGap      = f32(1)
)

const (
	winW = 720
	winH = 360
)

// Per-frame key layout: white keys fill the area under the title bar.
var (
	layWhiteW f32 = 56
	layWhiteH f32 = 210
	layBlackW f32 = 34
	layBlackH f32 = 128
)

// appIcon is the embedded dock icon as RGBA for in-UI ImageView.
var appIcon *image.RGBA

func init() {
	img, _, err := image.Decode(bytes.NewReader(iconPNG))
	if err != nil {
		return
	}
	b := img.Bounds()
	appIcon = image.NewRGBA(b)
	draw.Draw(appIcon, b, img, b.Min, draw.Src)
}

func RootView() {

	handleKeyboard()
	mixer.SetVolume(appData.volume)

	ModAttrs(Background(220, 20, 20, 1))
	TitleBar()
	// Extrinsic: size comes only from the flex slot under the title bar.
	// Without it, GetResolvedSize can disagree with the visible box (especially
	// on mobile) and key math goes wrong.
	Container(Attrs(Viewport), func() {
		Keyboard()
	})
}

// TitleBar is the single chrome row: icon + title, voice picker (left-middle),
// volume slider flush right.
//
//	[icon] Shirei Piano ··· [voice] · [========●== volume]
func TitleBar() {
	// Explicit NoAnimate: pin the mask even if a parent ever re-enables anim.
	Container(Attrs(Row, CrossMid, Expand, Gap(10), Pad2(8, 12), Background(220, 30, 18, 1), NoAnimate), func() {
		if appIcon != nil {
			ImageView(UseImage("piano-app-icon", appIcon), Vec2{22, 22})
		}
		Label("Shirei Piano", FontSize(15), FontWeight(WeightBold), TextColor(0, 0, 100, 1))
		Spacer(16)
		SegmentedControl(&appData.voice,
			Cell("Strings", VoiceString), Cell("Flute", VoiceFlute), Cell("Sine", VoiceSine))
		Spacer(12)
		Filler(1)
		Slider(&appData.volume, SliderAttrs{Min: 0, Max: 1, Width: 120})

		appData.titleH = GetScreenRect().Size[1]
	})
}

// keyboardAvail is the board size: prefer the Extrinsic host's resolved box;
// fall back to WindowSize minus the measured title bar (reliable on first pass
// and if a settle query is still zero).
func keyboardAvail() Vec2 {
	if sz := GetResolvedSize(); sz[0] > 1 && sz[1] > 1 {
		return sz
	}
	ws := GetHost().WindowSize
	th := appData.titleH
	if th < 1 {
		th = 48
	}
	h := ws[1] - th
	if h < 1 {
		h = 1
	}
	w := ws[0]
	if w < 1 {
		w = 1
	}
	return Vec2{w, h}
}

func Keyboard() {
	nw := f32(len(whiteKeys))
	if nw < 1 {
		return
	}
	avail := keyboardAvail()

	gaps := (nw - 1) * keyGap
	layWhiteW = (avail[0] - gaps) / nw
	if layWhiteW < 1 {
		layWhiteW = 1
	}
	layWhiteH = avail[1]
	if layWhiteH < 1 {
		layWhiteH = 1
	}
	layBlackW = layWhiteW * f32(blackWRatio)
	layBlackH = layWhiteH * f32(blackHRatio)
	// Never let black keys reach full white height (ratio must stay short).
	if layBlackH > layWhiteH*0.75 {
		layBlackH = layWhiteH * 0.75
	}
	if layBlackH < 1 {
		layBlackH = 1
	}

	// Match the Extrinsic host exactly — do not Expand (which can re-stretch
	// children on the cross axis past their FixSize).
	Container(Attrs(FixSize(avail[0], avail[1]), Row, Gap(keyGap), Background(220, 20, 20, 1), NoAnimate), func() {
		for _, k := range whiteKeys {
			PianoKeyView(k)
		}
		for _, k := range blackKeys {
			PianoKeyView(k)
		}
	})
}

// keyInteraction wires the current container (a piano key) to the note
// machinery and reports whether the key should render pressed.
//
// Pointer: prefer touch, then real mouse — never a touch-synthesized click.
// While GetInputState().MouseFromTouch is set (finger driving the mouse, including
// the post-tap hold), we ignore PressAction/IsActive so the delayed mouse-up
// cannot re-hold the note after the finger lifts. Desktop mouse has
// MouseFromTouch false, so click/drag still works. Keyboard is separate.
func keyInteraction(k *PianoKey) bool {
	byPointer := IsTouched()
	if !byPointer && !GetInputState().MouseFromTouch {
		PressAction()
		byPointer = IsActive()
	}
	byKB := appData.kbDown[k.Code]
	return syncKey(k, byKB, byPointer)
}

func PianoKeyView(k *PianoKey) {
	// Extrinsic + FixSize: content (Filler, labels) cannot change the key box.
	attrs := Attrs(Extrinsic, FixSize(layWhiteW, layWhiteH), CrossAlign(AlignMiddle),
		Background(0, 0, 100, 1), Grad(0, 0, -7, 0), NoAnimate, Clip)
	if layWhiteW >= 20 {
		attrs = AttrsWith(attrs, Corners4(0, 0, 3, 3))
	}
	if k.IsBlack {
		x := f32(k.Slot+1)*(layWhiteW+keyGap) - keyGap/2 - layBlackW/2
		attrs = Attrs(Float(x, 0), InFront, Extrinsic, FixSize(layBlackW, layBlackH),
			CrossAlign(AlignMiddle),
			Background(220, 15, 13, 1), Grad(0, 0, 8, 0), NoAnimate, Clip)
		if layBlackW >= 12 {
			attrs = AttrsWith(attrs, Corners4(0, 0, 2, 2), BoxShadow(2))
		}
	}

	ContainerWithKey(k, attrs, func() {
		pressed := keyInteraction(k)
		if pressed {
			if k.IsBlack {
				ModAttrs(Background(210, 45, 32, 1), Grad(0, 0, 6, 0))
			} else {
				ModAttrs(Background(210, 55, 88, 1), Grad(0, 0, -4, 0))
			}
		}

		Filler(1)
		// Computer-key hints only when a physical keyboard is attached
		// (Host.HardwareKeyboard). Soft-IME-only phones hide them.
		if GetHost().HardwareKeyboard {
			KeycapChip(k.Phys, k.IsBlack, pressed)
			Spacer(5)
		}
		// Note names: scale font slightly with key size so they stay readable
		// on phones without a hard height cutoff that can hide them wrongly.
		nameSize := f32(9)
		if layWhiteH < 100 {
			nameSize = 7
		}
		if k.IsBlack {
			Label(k.Name, FontSize(nameSize-1), TextColor(220, 10, 75, 1))
			Spacer(6)
		} else {
			Label(k.Name, FontSize(nameSize), TextColor(220, 10, 45, 1))
			Spacer(8)
		}
	})
}

// KeycapChip shows which computer key plays this note, styled like a keycap.
// ClickThrough so the chip is not a separate hit target over the key body.
func KeycapChip(phys string, onBlack bool, pressed bool) {
	a := Attrs(FixSize(20, 20), Center, Corners(4), BorderWidth(1), NoAnimate, ClickThrough)
	textClr := TextColor(220, 15, 35, 1)
	switch {
	case pressed:
		a = AttrsWith(a, Background(210, 80, 92, 1), BorderColor(210, 50, 55, 1))
	case onBlack:
		a = AttrsWith(a, Background(220, 12, 25, 1), BorderColor(220, 10, 45, 1))
		textClr = TextColor(220, 10, 88, 1)
	default:
		a = AttrsWith(a, Background(0, 0, 96, 1), Grad(0, 0, -6, 0), BorderColor(0, 0, 60, 1))
	}
	Container(a, func() {
		Label(phys, FontSize(10), FontWeight(WeightSemibold), textClr)
	})
}
