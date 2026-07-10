# Sound in Shirei Applications

How to make a shirei app produce sound: beeps, notes, sound effects, and
streamed audio. Companion to [tutorial.md](tutorial.md) (the GUI tutorial); like it,
this is written to be readable by humans while being practical enough for
AI coding sessions to follow.

The reference program, referenced throughout: `examples/piano` — a playable
instrument with three voices, keyboard/mouse note handling, and the
headless verification flags (`--wav`, `--play`).

This tutorial was written by Claude Fable 5.

---

# Part I — The model

## 1. Two layers, one line between them

Sound support is split the same way windows are:

1. **`app.StartAudio(sampleRate, fill)`** — the platform boundary. It opens
   the default output device and starts calling `fill(out []float32)` on
   the platform's audio thread, forever. Mono float32, nothing else. Per
   OS it's AudioQueue (macOS), ALSA (Linux), or waveOut (Windows), but an
   app never sees that.
2. **`shirei/audio`** — the pure-Go mixing layer you actually program
   against: a `Mixer` of `Voice`s. The mixer's `Fill` method has exactly
   the fill function's shape.

So the entire setup is:

```go
import (
    app "go.hasen.dev/shirei/app"
    "go.hasen.dev/shirei/audio"
)

const SampleRate = 44100

var mixer = audio.NewMixer()

func main() {
    audioErr := app.StartAudio(SampleRate, mixer.Fill)
    // show audioErr somewhere in the UI if not nil; the app still works,
    // just silently
    app.SetupWindow("My App", 800, 600)
    app.Run(RootView)
}
```

Call `StartAudio` once, at startup. There is no `StopAudio` — audio lives
for the process the way `Run` owns the window — and a second call is an
error. On failure nothing is started and the app simply runs silent;
`examples/piano` shows the error in its status bar.

Everything below is about what to put *into* the mixer.

## 2. A Voice is the mixable unit

```go
type Voice interface {
    Render(out []float32) bool // add into out; false = finished
}
```

Every sound — a beep, a note, a laser zap, a music stream — is a `Voice`,
and that one method is the whole interface: it's the additive twin of
the mixer's own `Fill` shape, which is what makes voices (and, in
principle, whole sub-mixes) composable. The contract:

- `Render` **adds** its samples into `out` (it never overwrites — that's
  what lets voices overlap) and returns whether it still has sound left.
  A voice that returns false is dropped by the mixer.

The shipped voices all carry a `Release()` method on top — it starts the
fade on sustained voices and is a no-op on one-shots, which end by
ringing out. `examples/piano` builds its note handling on that with one
declared type:

```go
type Note interface {
    audio.Voice
    Release()
}
```

It holds each sounding key as a `Note`, adds it to the mixer on
key-down, and calls `Release` on key-up — without caring whether the
voice is a fading flute or a ring-out pluck.

```go
v := makeSomeNote()
mixer.Add(v)     // sounding now
...
v.Release()      // later, e.g. on key-up
```

Keep the value if you need to release it later (sustained sounds);
fire-and-forget one-shots don't need keeping. The mixer holds at
most `audio.MaxVoices` (64) — beyond that the oldest is dropped — and
applies a master volume with clamping, so overlapping sounds saturate
instead of distorting:

```go
mixer.SetVolume(appData.volume)   // e.g. from a Slider, once per frame
```

## 3. The threading rules (short version)

- `Render` runs on the **audio thread**, under the mixer's lock, every
  ~6ms. Keep it quick and allocation-free: math and copies, no I/O, no
  frame lock, nothing that blocks. All the built-in voices follow this.
- `Add`, `SetVolume`, and the shipped voices' `Release` methods are safe
  from anywhere (the UI thread, a background goroutine).
- A voice's exported configuration is set **before** `Add` and not
  touched after; its internal state is only touched inside `Render`. If
  UI code must signal a running voice, do it like `Release` does — an
  atomic flag the next `Render` observes. (`ToneVoice.released` is the
  reference pattern.)
- Audio does not interact with the frame system at all. No
  `WithFrameLock`, no `RequestNextFrame` — unless a sound event should
  *also* change the UI, in which case that part follows the normal rules
  from the GUI tutorial §11.

---

# Part II — The built-in voices

## 4. ToneVoice: beeps and sustained notes

An oscillator with an envelope — the general-purpose "make a tone"
primitive. Construct it with fields; `Rate` and `Freq` are required, zero
values elsewhere mean "off":

```go
// an alert beep, no assets needed (fire and forget):
mixer.Add(&audio.ToneVoice{
    Rate: SampleRate, Freq: 880,
    Harmonics: []float64{1},         // pure sine
    Amp: 0.2, Attack: 0.005, Decay: 0.15,
})
```

For a *held* tone, keep the voice and release it on key-up. The envelope
is one-pole: `Attack` is the time constant while held, `Decay` after
release; the voice finishes by itself once the fade is inaudible.

The remaining fields add character:

- `Harmonics` — amplitude per harmonic, fundamental first. `{1}` is a
  sine; `{1, 0.38, 0.14, 0.05}` is the piano's flute.
- `Breath` — low-passed noise mixed in (0.5 gives the flute its air).
- `Vibrato` — 5Hz, ±0.6%, easing in after a quarter second.

`examples/piano/synth.go` holds two ready presets (flute, sine) built
exactly this way.

## 5. SampleVoice: one-shots

Plays a `[]float32` once; `Release` is a no-op (it rings out). This is
the sound-effects workhorse — and also the carrier for one-shot
*synthesis*: generate samples, wrap, add.

```go
mixer.Add(&audio.SampleVoice{Samples: zapSamples})
```

The piano's Karplus-Strong pluck is the worked example: `NewOudVoice` in
`examples/piano/synth.go` computes ~1.5s of samples (a wave-shaped table
run through the KS feedback loop with a damping curve) and returns it as
a `SampleVoice`. That's the intended division of labor: **the framework
provides the console and generic voices; signature sounds live in apps.**

## 6. StreamVoice: audio that arrives incrementally

For sound that is *produced over time* — a decoder, a network source, a
live synth — `StreamVoice` is the transport between your producer and
the audio thread. It's a `Voice` that is also a blocking writer:

```go
stream := audio.NewStreamVoice(SampleRate / 2) // ring: 0.5s of jitter budget
mixer.Add(stream)

go func() {
    defer stream.Close()
    buf := make([]float32, 4096)
    for {
        n, err := producer.Read(buf) // your decoder — formats are the app's business
        if n > 0 {
            if _, werr := stream.Write(buf[:n]); werr != nil {
                return // closed or released underneath us
            }
        }
        if err != nil {
            return
        }
    }
}()
```

What the transport guarantees:

- **`Write` blocks while the ring is full.** That's the producer's
  pacing — a decode loop self-regulates with no timers.
- **`Render` never blocks and never clicks.** If the producer falls
  behind (underrun), the last sample decays to zero instead of jumping,
  the voice stays alive, and when data returns it ramps back in over a
  couple of milliseconds. The very first samples ramp too.
- **`Close` is end-of-stream**: buffered audio plays out, the tail
  fades, the voice finishes. **`Release` stops promptly** (a short fade,
  not a pop). Both unblock a waiting `Write` with `audio.ErrStreamDone`.
- `Buffered()` and `Underruns()` are the diagnostics: producers that
  prefer decode-ahead over blocking watch the former; a rising underrun
  count means the ring is too small or the producer too slow.

A pleasant accident of the underrun handling: **pause is free**. A
producer that stops writing gets a declicked fade to silence; writing
again resumes with the ramp. No pause API needed.

Current limits, so nobody discovers them the hard way: output is mono
(stereo is a platform-boundary change, not a mixer one), there is no
`Flush` yet (after a seek, whatever was already buffered plays out
first), and decoding formats is deliberately out of scope.

## 7. Writing your own Voice

Implement `Render` and honor three rules: *add* into `out`, return false
only when truly done (the mixer drops you immediately), and touch
internal state only inside `Render`. A minimal template:

```go
type Noise struct {
    Amp      float64
    released atomic.Bool
    rng      uint64
}

func (n *Noise) Release() { n.released.Store(true) } // for the app's Note interface

func (n *Noise) Render(out []float32) bool {
    if n.released.Load() {
        return false // (a real voice fades first — see ToneVoice)
    }
    if n.rng == 0 {
        n.rng = 1
    }
    for i := range out {
        n.rng ^= n.rng << 13
        n.rng ^= n.rng >> 7
        n.rng ^= n.rng << 17
        out[i] += float32(n.Amp * (float64(int64(n.rng))/float64(math.MaxInt64)))
    }
    return true
}
```

`Render` alone makes it a `Voice`; the `Release` is there for your own
`Note` interface (§2), using the same atomic-flag pattern the shipped
voices use. Ending abruptly like this clicks; real voices end through a
fade — steal the one-pole envelope from `ToneVoice` or the declick tail
from `StreamVoice`.

---

# Part III — Verification

## 8. Headless sound, like headless rendering

The GUI tutorial's `--png` habit (§3 there) has an audio twin, because
`Mixer.Fill` doesn't care whether a device is calling it:

```go
// render a short demo offline, no device involved
m := audio.NewMixer()
m.SetVolume(0.85)
out := make([]float32, 5*SampleRate)
const chunk = 512
for pos := 0; pos < len(out); pos += chunk {
    // add/release voices at the right positions here (a tiny sequencer)
    m.Fill(out[pos:min(pos+chunk, len(out))])
}
audio.WriteWAV("demo.wav", SampleRate, out)
```

`examples/piano` wires this into two flags worth copying into any
sounding app:

- `--wav out.wav [voice]` — render a scale offline, print peak/RMS, write
  the file. A coding session can check levels numerically (no clipping,
  not silent) and even verify pitches from the file (the piano's
  frequencies were confirmed by zero-crossing analysis; low-pass first if
  the voice has noise in it).
- `--play [voice]` — the same scale through the real device path; the
  end-to-end smoke test for the platform layer.

The habit worth forming, same as snapshots: when a sound reaches a state
you like, freeze the numbers — a peak/RMS assertion is the audio
equivalent of a golden PNG, cheap and refactor-proof.

## 9. Platform behavior you get for free

Things the boundary handles that apps should *not* re-implement:

- **Latency** is tuned per OS (~17ms macOS, ~30ms Linux, ~46ms Windows) —
  responsive enough for live playing.
- **Sleep recovery**: a laptop waking from deep sleep can silently kill
  the OS audio stream; the macOS and Windows backends detect the stall
  and rebuild the device path within a few seconds. Linux recovers
  in-loop via ALSA. Apps do nothing.
- **The audio thread always pulls** — silence is rendered, never skipped —
  so an idle mixer costs a memset every ~6ms and nothing else. Don't
  gate `StartAudio` behind "only when something plays."
