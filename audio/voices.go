package audio

import (
	"math"
	"math/rand/v2"
	"sync/atomic"
)

// SampleVoice plays a precomputed buffer once; Release is a no-op (the
// sound rings out on its own). The natural carrier for one-shot synthesis
// like plucks: generate the samples, wrap, Add.
type SampleVoice struct {
	Samples []float32
	pos     int
}

func (v *SampleVoice) Render(out []float32) bool {
	n := min(len(out), len(v.Samples)-v.pos)
	for i := 0; i < n; i++ {
		out[i] += v.Samples[v.pos+i]
	}
	v.pos += n
	return v.pos < len(v.Samples)
}

// Release is note-off for a one-shot: a no-op — the sound rings out on
// its own. Provided so app-side Note interfaces hold every voice kind.
func (v *SampleVoice) Release() {}

// ToneVoice is a sustained additive voice: phase-locked harmonics with an
// optional breath-noise layer and eased-in vibrato, shaped by a one-pole
// attack/decay envelope. It sounds while held and fades out after Release.
// Construct with the exported fields set (Rate and Freq are required); the
// zero values of the rest mean "off".
type ToneVoice struct {
	Rate      float64   // sample rate, e.g. 44100
	Freq      float64   // fundamental, Hz
	Harmonics []float64 // amplitude per harmonic, fundamental first
	Amp       float64   // overall amplitude
	Attack    float64   // seconds (one-pole time constant)
	Decay     float64   // seconds after release (one-pole time constant)
	Breath    float64   // low-passed noise mix (0 = none)
	Vibrato   bool      // 5Hz, ±0.6%, easing in over [0.25s, 0.75s]

	released atomic.Bool

	// render-side state (only touched inside Render, under the mixer lock)
	t        int
	phase    float64
	vibPhase float64
	env      float64
	noise    uint64
	noiseLP  float64
}

func (v *ToneVoice) Release() { v.released.Store(true) }

func (v *ToneVoice) Render(out []float32) bool {
	attackK := 1 - math.Exp(-1/(v.Attack*v.Rate))
	decayK := 1 - math.Exp(-1/(v.Decay*v.Rate))
	released := v.released.Load()

	const vibRate = 5.0
	const vibDepth = 0.006

	if v.noise == 0 {
		v.noise = rand.Uint64() | 1 // lazy xorshift seed
	}

	for i := range out {
		if released {
			v.env -= v.env * decayK
			if v.env < 0.0008 {
				return false
			}
		} else {
			v.env += (1 - v.env) * attackK
		}

		f := v.Freq
		if v.Vibrato {
			ramp := (float64(v.t)/v.Rate - 0.25) / 0.5
			ramp = min(1, max(0, ramp))
			f *= 1 + vibDepth*ramp*math.Sin(v.vibPhase)
			v.vibPhase += 2 * math.Pi * vibRate / v.Rate
		}
		v.phase += 2 * math.Pi * f / v.Rate
		if v.phase > 2*math.Pi {
			v.phase -= 2 * math.Pi
		}

		s := 0.0
		for h, a := range v.Harmonics {
			s += a * math.Sin(v.phase*float64(h+1))
		}
		if v.Breath > 0 {
			v.noise ^= v.noise << 13
			v.noise ^= v.noise >> 7
			v.noise ^= v.noise << 17
			white := float64(int64(v.noise)) / float64(math.MaxInt64)
			v.noiseLP += (white - v.noiseLP) * 0.09
			s += v.noiseLP * v.Breath
		}

		out[i] += float32(v.Amp * v.env * s)
		v.t++
	}
	return true
}
