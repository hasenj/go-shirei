// The piano's voices, on top of shirei/audio's mixer: awtar's Karplus-Strong
// oud pluck (the app's signature sound — framework-generic voices live in
// the audio package, signature sounds live in apps) and two ToneVoice
// presets.
package main

import (
	"math"
	"math/rand/v2"

	"go.hasen.dev/shirei/audio"
)

const SampleRate = 44100

var mixer = audio.NewMixer()

func init() {
	mixer.SetVolume(0.6)
}

// ---- Oud: Karplus-Strong pluck, ported from awtar src/synth.ts ----

type wavePoint struct{ x, y float64 }

// the oud pluck shape from awtar (with the implied (0,0) and (1,0) endpoints)
var oudShape = []wavePoint{
	{0, 0}, {0.1, 0.1}, {0.16, 0.22}, {0.26, -0.26}, {0.4, -0.22},
	{0.5, 0.1}, {0.6, 0.34}, {0.7, 0.24}, {0.84, 0}, {0.91, -0.04}, {1, 0},
}

// waveShapeToSample rasterizes a piecewise-linear shape into n samples.
func waveShapeToSample(shape []wavePoint, n int) []float32 {
	out := make([]float32, n)
	seg := 0
	for i := range out {
		x := float64(i) / float64(n)
		for x > shape[seg+1].x && seg+2 < len(shape) {
			seg++
		}
		p, q := shape[seg], shape[seg+1]
		t := (x - p.x) / (q.x - p.x)
		out[i] = float32(p.y + (q.y-p.y)*t)
	}
	return out
}

func ksNoise(v float32) float32 {
	if rand.IntN(2) == 0 {
		return v
	}
	return -v
}

func NewStringVoice(freq float64) Note {
	const duration = 1.5
	const gain = 1.0
	n := int(duration * SampleRate)
	tableLen := int(math.Round(SampleRate / freq))
	table := waveShapeToSample(oudShape, tableLen)
	for i := range table {
		table[i] += ksNoise(0.16)
	}
	samples := make([]float32, n)
	for i := 0; i < n; i++ {
		p := i % tableLen
		adj := (tableLen + i - 1) % tableLen
		table[p] = (table[p] + table[adj]) / 2
		// awtar's dampness curve: e^(-2x) - 0.3, floored at 0 — it reaches 0
		// at x≈0.6, so the tail is silence; cut the buffer there instead
		x := float64(i) / float64(n)
		damp := math.Exp(-2*x) - 0.3
		if damp <= 0 {
			samples = samples[:i]
			break
		}
		samples[i] = table[p] * float32(damp*gain)
	}
	return &audio.SampleVoice{Samples: samples}
}

// ---- sustained presets ----

func NewFluteVoice(freq float64) Note {
	return &audio.ToneVoice{
		Rate:      SampleRate,
		Freq:      freq,
		Harmonics: []float64{1, 0.38, 0.14, 0.05},
		Amp:       0.26,
		Attack:    0.05,
		Decay:     0.16,
		Breath:    0.5,
		Vibrato:   true,
	}
}

func NewSineVoice(freq float64) Note {
	return &audio.ToneVoice{
		Rate:      SampleRate,
		Freq:      freq,
		Harmonics: []float64{1},
		Amp:       0.28,
		Attack:    0.01,
		Decay:     0.12,
	}
}
