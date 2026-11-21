package main

import (
	"math"

	"go.hasen.dev/shirei/audio"
)

const chimeSampleRate = 44100

// ChimeEngine owns the audio mixer and pre-rendered soft chimes for each category.
type ChimeEngine struct {
	Mixer   *audio.Mixer
	samples map[Category][]float32
}

func newChimeEngine() *ChimeEngine {
	m := audio.NewMixer()
	m.SetVolume(0.75)
	return &ChimeEngine{
		Mixer: m,
		samples: map[Category][]float32{
			CatTea:    SynthesizeChime(CatTea, chimeSampleRate),
			CatCoffee: SynthesizeChime(CatCoffee, chimeSampleRate),
			CatEyes:   SynthesizeChime(CatEyes, chimeSampleRate),
		},
	}
}

// Play queues a gentle, non-blocking chime for cat.
func (ce *ChimeEngine) Play(cat Category) {
	buf, ok := ce.samples[cat]
	if !ok || len(buf) == 0 {
		return
	}
	ce.Mixer.Add(&audio.SampleVoice{Samples: buf})
}

type chimeNote struct {
	startSec float64
	freqHz   float64
	amp      float64
	decaySec float64
	detuneHz float64 // subtle singing-bowl shimmer beat
}

// SynthesizeChime renders a soft, warm harmonic bell/singing-bowl chime tailored
// to each category: gentle attack (zero click), warm overtones, smooth decay.
func SynthesizeChime(cat Category, rate int) []float32 {
	var notes []chimeNote
	var totalSec float64

	switch cat {
	case CatTea:
		// Warm ceramic singing-bowl fifth: D5 -> A5
		notes = []chimeNote{
			{startSec: 0.00, freqHz: 587.33, amp: 0.28, decaySec: 1.65, detuneHz: 1.1},
			{startSec: 0.34, freqHz: 880.00, amp: 0.22, decaySec: 1.90, detuneHz: 1.4},
		}
		totalSec = 3.2
	case CatCoffee:
		// Mellow warm triad bloom: C5 -> G5 -> E5
		notes = []chimeNote{
			{startSec: 0.00, freqHz: 523.25, amp: 0.26, decaySec: 1.40, detuneHz: 0.8},
			{startSec: 0.26, freqHz: 783.99, amp: 0.20, decaySec: 1.55, detuneHz: 1.0},
			{startSec: 0.56, freqHz: 659.25, amp: 0.22, decaySec: 1.85, detuneHz: 0.9},
		}
		totalSec = 3.4
	default: // CatEyes
		// Airy crystal shimmer: E5 -> B5
		notes = []chimeNote{
			{startSec: 0.00, freqHz: 659.25, amp: 0.24, decaySec: 1.50, detuneHz: 1.5},
			{startSec: 0.30, freqHz: 987.77, amp: 0.19, decaySec: 1.85, detuneHz: 1.8},
		}
		totalSec = 3.0
	}

	nSamples := int(math.Ceil(totalSec * float64(rate)))
	buf := make([]float32, nSamples)

	const attackSec = 0.022 // soft felt-mallet onset
	const fadeOutSec = 0.08 // ensure exact zero tail

	for _, n := range notes {
		startIdx := int(n.startSec * float64(rate))
		for i := startIdx; i < nSamples; i++ {
			t := float64(i-startIdx) / float64(rate)

			// Smooth raised-cosine attack + natural exponential ring decay.
			env := math.Exp(-t / n.decaySec)
			if t < attackSec {
				env *= 0.5 * (1 - math.Cos(math.Pi*t/attackSec))
			} else if env < 1e-5 {
				break
			}

			// Fundamental + subtle detuned pair + gentle 2nd & 2.756x bell overtones
			// whose upper harmonics fade faster like a real bronze/ceramic chime.
			w := 2 * math.Pi * n.freqHz * t
			wDet := 2 * math.Pi * (n.freqHz + n.detuneHz) * t
			overEnv2 := math.Exp(-t / (n.decaySec * 0.45))
			overEnv3 := math.Exp(-t / (n.decaySec * 0.22))

			s := 0.72*math.Sin(w) +
				0.28*math.Sin(wDet) +
				0.18*overEnv2*math.Sin(2.0*w) +
				0.07*overEnv3*math.Sin(2.756*w)

			buf[i] += float32(n.amp * env * s)
		}
	}

	// Smooth tail fade so the buffer ends cleanly at zero.
	fadeSamples := int(fadeOutSec * float64(rate))
	if fadeSamples > nSamples {
		fadeSamples = nSamples
	}
	for i := 0; i < fadeSamples; i++ {
		idx := nSamples - 1 - i
		gain := float32(i) / float32(fadeSamples)
		buf[idx] *= gain
	}

	return buf
}

// RenderChimeWAV renders a category chime offline through the audio mixer,
// writes a mono WAV file, and returns peak and RMS levels for verification.
func RenderChimeWAV(path string, cat Category) (peak float64, rms float64, err error) {
	ce := newChimeEngine()
	ce.Play(cat)

	n := len(ce.samples[cat])
	out := make([]float32, n)
	for pos := 0; pos < len(out); pos += 512 {
		end := pos + 512
		if end > len(out) {
			end = len(out)
		}
		ce.Mixer.Fill(out[pos:end])
	}

	var sumSq float64
	for _, s := range out {
		a := math.Abs(float64(s))
		if a > peak {
			peak = a
		}
		sumSq += float64(s) * float64(s)
	}
	if len(out) > 0 {
		rms = math.Sqrt(sumSq / float64(len(out)))
	}

	err = audio.WriteWAV(path, chimeSampleRate, out)
	return peak, rms, err
}
