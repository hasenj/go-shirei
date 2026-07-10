package main

import (
	"sort"
	"strings"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

const (
	whiteW   = 56
	whiteH   = 210
	blackW   = 34
	blackH   = 128
	keyGap   = 2
	framePad = 4
)

const (
	winW = 720
	winH = 430
)

func RootView() {

	handleKeyboard()
	mixer.SetVolume(appData.volume)

	Container(Attrs(Viewport, Background(220, 15, 96, 1)), func() {
		Header()
		Toolbar()
		Container(Attrs(Grow(1), Expand, Center), func() {
			Keyboard()
		})
		StatusBar()
	})
}

func Header() {
	Container(Attrs(Row, CrossMid, Expand, Gap(10), Pad2(10, 14), Background(220, 30, 18, 1)), func() {
		Label("Shirei Piano", FontSize(16), FontWeight(WeightBold), TextColor(0, 0, 100, 1))
	})
}

func Toolbar() {
	Container(Attrs(Row, CrossMid, Expand, Gap(12), Pad2(8, 14), Background(220, 18, 91, 1)), func() {
		Label("Synth Voice:", FontSize(12), TextColor(0, 0, 30, 1))
		SegmentedControl(&appData.voice,
			Cell("Strings", VoiceString), Cell("Flute", VoiceFlute), Cell("Sine", VoiceSine))
		Filler(1)
		Label("Volume:", FontSize(11), TextColor(0, 0, 30, 1))
		Slider(&appData.volume, SliderAttrs{Min: 0, Max: 1, Width: 130})
	})
}

func Keyboard() {
	nw := f32(len(whiteKeys))
	kbW := nw*whiteW + (nw-1)*keyGap + framePad*2
	kbH := f32(whiteH) + framePad*2

	Container(Attrs(Row, FixSize(kbW, kbH), Pad(framePad), Gap(keyGap), Background(220, 20, 20, 1), Corners(8), BoxShadow(6), NoAnimate), func() {
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
func keyInteraction(k *PianoKey) bool {
	if IsClicked() {
		noteOn(k, true)
	}
	PressAction()
	holding := IsActive()
	if h := appData.held[k]; h != nil && h.mouse && !holding {
		noteOff(k, true)
	}
	return holding || appData.kbDown[k.Code]
}

func PianoKeyView(k *PianoKey) {
	attrs := Attrs(FixSize(whiteW, whiteH), Corners4(0, 0, 4, 4), CrossAlign(AlignMiddle),
		Background(0, 0, 100, 1), Grad(0, 0, -7, 0), NoAnimate)
	if k.IsBlack {
		x := framePad + f32(k.Slot+1)*(whiteW+keyGap) - keyGap/2 - blackW/2
		attrs = Attrs(Float(x, framePad), InFront, FixSize(blackW, blackH),
			Corners4(0, 0, 3, 3), CrossAlign(AlignMiddle),
			Background(220, 15, 13, 1), Grad(0, 0, 8, 0), BoxShadow(3), NoAnimate)
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
		KeycapChip(k.Phys, k.IsBlack, pressed)
		Spacer(5)
		if k.IsBlack {
			Label(k.Name, FontSize(8), TextColor(220, 10, 75, 1))
			Spacer(8)
		} else {
			Label(k.Name, FontSize(9), TextColor(220, 10, 45, 1))
			Spacer(10)
		}
	})
}

// KeycapChip shows which computer key plays this note, styled like a keycap.
func KeycapChip(phys string, onBlack bool, pressed bool) {
	a := Attrs(FixSize(20, 20), Center, Corners(4), BorderWidth(1), NoAnimate)
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

func StatusBar() {
	// fixed height: the ♪ readout comes and goes (and its glyph rides a
	// fallback font with taller metrics), which otherwise resizes the bar
	// and nudges the whole layout on every key press
	Container(Attrs(Row, CrossMid, Expand, FixHeight(30), Gap(8), Pad2(0, 14), Background(220, 15, 88, 1)), func() {
		if appData.audioErr != nil {
			Label("audio unavailable: "+appData.audioErr.Error(), FontSize(11), TextColor(5, 70, 40, 1))
			return
		}
		Label("Esc releases stuck notes", FontSize(10), TextColor(220, 8, 55, 1))
		Filler(1)
		if len(appData.held) > 0 {
			sounding := make([]*PianoKey, 0, len(appData.held))
			for k := range appData.held {
				sounding = append(sounding, k)
			}
			sort.Slice(sounding, func(i, j int) bool { return sounding[i].Freq < sounding[j].Freq })
			names := make([]string, len(sounding))
			for i, k := range sounding {
				names[i] = k.Name
			}
			Label("♪ "+strings.Join(names, " "), FontSize(11), TextColor(220, 40, 35, 1))
		}
	})
}
