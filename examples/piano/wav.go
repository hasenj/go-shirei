// Offline rendering: drive the same Mixer/Voice machinery without an audio
// device, write the result as a 16-bit mono WAV, and print signal stats.
// This is the audio equivalent of the --png flag: a way to verify the synth
// headlessly (and to listen with `afplay`).
package main

import (
	"fmt"
	"math"
	"time"

	app "go.hasen.dev/shirei/app"
	"go.hasen.dev/shirei/audio"
)

// playDemo plays the demo scale live on the global mixer — the same device
// path the GUI uses (app.StartAudio, whatever that means on this platform).
func playDemo(kind VoiceKind) error {
	if err := app.StartAudio(SampleRate, mixer.Fill); err != nil {
		return err
	}
	mixer.SetVolume(0.85)
	notes := []int{60, 62, 64, 65, 67, 69, 71, 72}
	for _, n := range notes {
		v := makeVoice(kind, noteFreq(n))
		mixer.Add(v)
		time.Sleep(240 * time.Millisecond)
		v.Release()
		time.Sleep(60 * time.Millisecond)
	}
	time.Sleep(1500 * time.Millisecond) // let the tail ring out
	return nil
}

func writeDemoWAV(path string, kind VoiceKind) error {
	samples := renderDemo(kind)

	var peak, sumSq float64
	for _, s := range samples {
		if a := math.Abs(float64(s)); a > peak {
			peak = a
		}
		sumSq += float64(s) * float64(s)
	}
	rms := math.Sqrt(sumSq / float64(len(samples)))
	fmt.Printf("%s: %.2fs, peak %.3f, rms %.3f\n",
		path, float64(len(samples))/SampleRate, peak, rms)

	return audio.WriteWAV(path, SampleRate, samples)
}

// renderDemo plays a C major scale up one octave and lets the tail ring out.
func renderDemo(kind VoiceKind) []float32 {
	m := audio.NewMixer()
	m.SetVolume(0.85)
	step := int(0.30 * SampleRate) // note spacing
	hold := int(0.24 * SampleRate) // held portion (sustained voices)
	notes := []int{60, 62, 64, 65, 67, 69, 71, 72}

	total := len(notes)*step + 2*SampleRate
	out := make([]float32, total)
	voices := map[int]Note{}

	const chunk = 512
	for pos := 0; pos < total; pos += chunk {
		for i, n := range notes {
			on := i * step
			if on >= pos && on < pos+chunk {
				v := makeVoice(kind, noteFreq(n))
				voices[n] = v
				m.Add(v)
			}
			if off := on + hold; off >= pos && off < pos+chunk {
				if v := voices[n]; v != nil {
					v.Release()
				}
			}
		}
		m.Fill(out[pos:min(pos+chunk, total)])
	}
	return out
}
