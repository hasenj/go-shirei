// synthpad: a small grid of tone pads for exercising app.StartAudio.
//
// Click/hold a pad to play a ToneVoice; release to fade out. Works on
// desktop and in the browser (Web Audio resumes after the first click).
//
//	go run ./demos/synthpad
//	go run ./cmd/shirei_web -run ./demos/synthpad
package main

import (
	"fmt"
	"os"

	"go.hasen.dev/shirei/app"
	"go.hasen.dev/shirei/audio"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

const sampleRate = 48000 // matches common Web Audio hardware rates

var (
	mixer   = audio.NewMixer()
	audioOK bool
)

type pad struct {
	Label string
	Freq  float64
	Hue   float32
	voice *audio.ToneVoice
}

var pads = []pad{
	{Label: "C4", Freq: 261.63, Hue: 0},
	{Label: "D4", Freq: 293.66, Hue: 30},
	{Label: "E4", Freq: 329.63, Hue: 55},
	{Label: "F4", Freq: 349.23, Hue: 90},
	{Label: "G4", Freq: 392.00, Hue: 140},
	{Label: "A4", Freq: 440.00, Hue: 200},
	{Label: "B4", Freq: 493.88, Hue: 260},
	{Label: "C5", Freq: 523.25, Hue: 300},
}

const winW, winH = 640, 360

func main() {
	if len(os.Args) >= 3 && os.Args[1] == "--png" {
		if err := RenderToPNG(os.Args[2], winW, winH, root); err != nil {
			fmt.Fprintln(os.Stderr, "render to png failed:", err)
			os.Exit(1)
		}
		return
	}
	if err := app.StartAudio(sampleRate, mixer.Fill); err != nil {
		fmt.Println("audio:", err)
	} else {
		audioOK = true
	}
	app.SetupWindow("Synth Pad", winW, winH)
	app.Run(root)
}

func root() {
	ModAttrs(Viewport, Background(230, 12, 14, 1), Pad(20), Gap(16))

	Label("Synth pad", FontWeight(WeightBold), FontSize(20), TextColor(0, 0, 95, 1))
	if audioOK {
		Label("Hold a pad to play · release to stop  ·  click once if silent (browser autoplay)",
			FontSize(12), TextColor(0, 0, 70, 1))
	} else {
		Label("Audio failed to start — UI still works, silent",
			FontSize(12), TextColor(10, 80, 70, 1))
	}

	Container(Attrs(Row, Gap(10), Expand), func() {
		for i := range pads {
			padCell(&pads[i])
		}
	})
}

func padCell(p *pad) {
	Container(Attrs(Expand, MinSize(0, 120), Corners(10), Clip), func() {
		st := ProcessButtonEvents(false)
		was := Use[bool]("was-active")

		// Note on: transition into Active. Note off: transition out.
		if st.Active && !*was && audioOK {
			if p.voice != nil {
				p.voice.Release()
			}
			v := &audio.ToneVoice{
				Rate:      sampleRate,
				Freq:      p.Freq,
				Harmonics: []float64{1, 0.35, 0.12},
				Amp:       0.22,
				Attack:    0.01,
				Decay:     0.18,
			}
			p.voice = v
			mixer.Add(v)
		}
		if !st.Active && *was && p.voice != nil {
			p.voice.Release()
			p.voice = nil
		}
		*was = st.Active

		sat := float32(55)
		light := float32(42)
		if st.Active {
			sat, light = 70, 55
		} else if st.Hovered {
			light = 48
		}
		ModAttrs(Background(p.Hue, sat, light, 1))

		Container(Attrs(Expand, MainAlign(AlignMiddle), CrossAlign(AlignMiddle)), func() {
			Label(p.Label, FontWeight(WeightBold), FontSize(18), TextColor(0, 0, 100, 1))
		})
	})
}
