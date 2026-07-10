// Package audio is the pure-Go mixing layer above app.StartAudio: a mixer
// of active voices plus a couple of generic voice types. The platform layer
// stays a dumb device boundary (pull mono float32); this package is what
// apps actually talk to:
//
//	mixer := audio.NewMixer()
//	app.StartAudio(44100, mixer.Fill)
//	...
//	mixer.Add(&audio.ToneVoice{Rate: 44100, Freq: 440, Harmonics: []float64{1}, Amp: 0.3, Attack: 0.01, Decay: 0.1})
//
// Everything here also runs without a device — call Fill yourself to render
// offline (see WriteWAV) — which is how apps verify their sound headlessly.
//
// Grown out of examples/piano and the maqam-gui app, which had identical
// copies of all of this.
package audio

import (
	"sync"
)

// Voice is the mixable unit: a mono sound in progress. Render adds its
// samples into out — the additive twin of the mixer's own Fill shape —
// and reports whether the voice still has sound left; a finished voice is
// dropped by the mixer. Render is only called by the mixer, under its
// lock.
//
// Note-off is not part of the interface. The shipped voices all have a
// Release method (meaningful on sustained ones, a ring-out no-op on
// SampleVoice), so an app that wants uniform note-off declares its own
// interface and every voice satisfies it structurally:
//
//	type Note interface {
//	    audio.Voice
//	    Release()
//	}
type Voice interface {
	Render(out []float32) bool
}

// MaxVoices caps how many voices a mixer holds; adding beyond it drops the
// oldest. Generous enough for hands-on-keyboard playing with long tails.
const MaxVoices = 64

// Mixer sums active voices into the output buffer, applying a master volume
// and clamping to [-1, 1]. Its Fill matches app.AudioFillFn, and is equally
// happy being driven offline. Use NewMixer — the zero value is silent
// (volume 0).
type Mixer struct {
	mu     sync.Mutex
	voices []Voice
	volume float32
}

// NewMixer returns a mixer at full volume.
func NewMixer() *Mixer {
	return &Mixer{volume: 1}
}

// SetVolume sets the master volume (0..1; values outside are legal and
// simply scale).
func (m *Mixer) SetVolume(v float32) {
	m.mu.Lock()
	m.volume = v
	m.mu.Unlock()
}

// Add starts a voice. Beyond MaxVoices the oldest voice is dropped.
func (m *Mixer) Add(v Voice) {
	m.mu.Lock()
	if len(m.voices) >= MaxVoices {
		m.voices = m.voices[1:]
	}
	m.voices = append(m.voices, v)
	m.mu.Unlock()
}

// Fill overwrites out with the mixed master signal. This is the audio
// callback's entry point; it stays allocation-free and quick.
func (m *Mixer) Fill(out []float32) {
	for i := range out {
		out[i] = 0
	}
	m.mu.Lock()
	live := m.voices[:0]
	for _, v := range m.voices {
		if v.Render(out) {
			live = append(live, v)
		}
	}
	clear(m.voices[len(live):])
	m.voices = live
	vol := m.volume
	m.mu.Unlock()

	for i := range out {
		s := out[i] * vol
		if s > 1 {
			s = 1
		} else if s < -1 {
			s = -1
		}
		out[i] = s
	}
}
