// piano: a one-row piano keyboard played with the computer keyboard,
// multi-touch, or mouse, with a small Go port of awtar's Karplus-Strong
// string synth.
//
// The home row plays the white keys and the QWERTY row plays the black keys,
// at the positions where a real piano has them:
//
//	W E   T Y U   O P        ← C#4 D#4  F#4 G#4 A#4  C#5 D#5
//	A S D F G H J K L ; '    ← C4 D4 E4 F4 G4 A4 B4 C5 D5 E5 F5
//
// Multi-touch: each finger can hold a different key (glissando / chords) via
// IsTouchedDirectly, in parallel with mouse/keyboard.
package main

import (
	"fmt"
	"math"
	"os"

	app "go.hasen.dev/shirei/app"
	"go.hasen.dev/shirei/audio"

	. "go.hasen.dev/shirei"
)

type f32 = float32

// ---- the keyboard model ----

type PianoKey struct {
	Name    string  // note name, e.g. "C4" or "F#4"
	Phys    string  // computer-key label, e.g. "A" or "W"
	Code    KeyCode // the computer key that plays this note
	Freq    float64
	IsBlack bool
	Slot    int // white keys: own index; black keys: index of the white key to the left
}

var (
	whitePhys    = "ASDFGHJKL;'"
	boundaryPhys = "WERTYUIOP[" // physical key above the boundary between white i and i+1
	whiteOffsets = []int{0, 2, 4, 5, 7, 9, 11, 12, 14, 16, 17}
	baseMidi     = 60 // C4 on the leftmost white key
)

var whiteKeys, blackKeys, allKeys []*PianoKey

func noteFreq(midi int) float64 {
	return 440 * math.Pow(2, float64(midi-69)/12)
}

var noteNames = [12]string{"C", "C#", "D", "D#", "E", "F", "F#", "G", "G#", "A", "A#", "B"}

func noteName(midi int) string {
	return fmt.Sprintf("%s%d", noteNames[midi%12], midi/12-1)
}

func init() {
	for i, off := range whiteOffsets {
		midi := baseMidi + off
		whiteKeys = append(whiteKeys, &PianoKey{
			Name: noteName(midi), Phys: string(whitePhys[i]),
			Code: KeyCode(whitePhys[i]), Freq: noteFreq(midi), Slot: i,
		})
		// a black key exists between two whites a whole tone apart
		if i+1 < len(whiteOffsets) && whiteOffsets[i+1]-off == 2 {
			midi++
			blackKeys = append(blackKeys, &PianoKey{
				Name: noteName(midi), Phys: string(boundaryPhys[i]),
				Code: KeyCode(boundaryPhys[i]), Freq: noteFreq(midi),
				IsBlack: true, Slot: i,
			})
		}
	}
	allKeys = append(append(allKeys, whiteKeys...), blackKeys...)
}

// ---- app state ----

type VoiceKind int

const (
	VoiceString VoiceKind = iota
	VoiceFlute
	VoiceSine
)

// Note is what this app holds between note-on and note-off: a mixable
// voice that also understands release. audio.Voice is deliberately just
// the mixable unit; note-off is our concept, and every shipped voice
// satisfies this structurally.
type Note interface {
	audio.Voice
	Release()
}

// heldNote tracks one sounding key. byKB and byTouch are independent so a
// computer key and a finger can both "own" the note; release only when both drop.
type heldNote struct {
	voice   Note
	byKB    bool
	byTouch bool // finger (IsTouched) or mouse hold (IsActive) — mutually preferred
}

type PianoApp struct {
	voice  VoiceKind
	volume f32

	held     map[*PianoKey]*heldNote
	kbDown   map[KeyCode]bool // computer keys down as of this frame
	titleH   f32              // measured title-bar height (for keyboard fallback sizing)
	audioErr error
}

var appData = &PianoApp{
	volume: 0.6,
	held:   map[*PianoKey]*heldNote{},
	kbDown: map[KeyCode]bool{},
}

func makeVoice(kind VoiceKind, freq float64) Note {
	switch kind {
	case VoiceFlute:
		return NewFluteVoice(freq)
	case VoiceSine:
		return NewSineVoice(freq)
	default:
		return NewStringVoice(freq)
	}
}

// syncKey starts or stops a note so that it sounds iff byKB || byTouch.
// Returns whether the key should render as pressed.
func syncKey(k *PianoKey, byKB, byTouch bool) bool {
	h := appData.held[k]
	if byKB || byTouch {
		if h == nil {
			v := makeVoice(appData.voice, k.Freq)
			mixer.Add(v)
			h = &heldNote{voice: v}
			appData.held[k] = h
		}
		h.byKB = byKB
		h.byTouch = byTouch
		return true
	}
	if h != nil {
		h.voice.Release()
		delete(appData.held, k)
	}
	return false
}

func releaseAll() {
	for k, h := range appData.held {
		h.voice.Release()
		delete(appData.held, k)
	}
}

// handleKeyboard updates kbDown from computer keys. Note on/off is applied
// later per key in keyInteraction (together with multi-touch).
func handleKeyboard() {
	if GetFrameInput().Key == KeyEscape {
		releaseAll()
	}
	downNow := map[KeyCode]bool{}
	// while a command shortcut is held, keys are not piano input
	if GetInputState().Modifiers&(ModCmd|ModCtrl) == 0 {
		for _, kc := range GetInputState().DownKeys {
			downNow[kc] = true
		}
	}
	for _, k := range allKeys {
		appData.kbDown[k.Code] = downNow[k.Code]
	}
}

func main() {
	if len(os.Args) >= 3 && os.Args[1] == "--png" {
		// `piano --png out.png` renders one settled frame headlessly and exits
		if err := RenderToPNG(os.Args[2], winW, winH, RootView); err != nil {
			fmt.Println("render failed:", err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) >= 2 && os.Args[1] == "--play" {
		// `piano --play [string|flute|sine]` plays the demo scale through the
		// speakers using the same AudioQueue path as the GUI, then exits
		kind := VoiceString
		if len(os.Args) >= 3 {
			switch os.Args[2] {
			case "flute":
				kind = VoiceFlute
			case "sine":
				kind = VoiceSine
			}
		}
		if err := playDemo(kind); err != nil {
			fmt.Println("audio failed:", err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) >= 3 && os.Args[1] == "--wav" {
		// `piano --wav out.wav [string|flute|sine]` renders a C major scale
		// offline through the same mixer and reports signal stats
		kind := VoiceString
		if len(os.Args) >= 4 {
			switch os.Args[3] {
			case "flute":
				kind = VoiceFlute
			case "sine":
				kind = VoiceSine
			}
		}
		if err := writeDemoWAV(os.Args[2], kind); err != nil {
			fmt.Println("wav failed:", err)
			os.Exit(1)
		}
		return
	}

	appData.audioErr = app.StartAudio(SampleRate, mixer.Fill)
	app.SetupIconBytes(iconPNG)
	app.SetupWindow("Shirei Piano", winW, winH)
	// Phone portrait still shows a landscape piano (OS locks interface orientation).
	GetHost().PreferredOrientation = OrientationLandscape
	app.Run(RootView)
}
