package app

import (
	"errors"
	"fmt"
	"os"
	"sync/atomic"
	"time"
)

// AudioFillFn fills out with mono float32 samples at the sample rate passed
// to StartAudio. It is called on the platform's audio thread — keep it quick
// and allocation-free (a mixer draining its voices, not a place to do work).
type AudioFillFn func(out []float32)

// audioFill is both the started-guard and the dispatch target: platform
// callbacks pull samples through it. It is written before the platform
// starts its audio thread (thread creation orders the write) and only
// cleared on a failed start, before any audio thread exists — so the audio
// thread reads it without locking. Like SetupWindow, StartAudio is a
// startup-time call, not something to race from multiple goroutines.
var audioFill AudioFillFn

// StartAudio opens the default output device and begins pulling mono float32
// samples from fill on a dedicated audio thread. Call it once, at startup;
// there is no StopAudio — audio lives for the process, the way Run owns the
// window. A second call is an error (two consumers would each steal every
// other buffer from the first). On error nothing is started and a later
// retry is allowed.
//
// Buffering targets interactive use: ~17ms on darwin, ~30ms on linux,
// ~46ms on windows (see the per-platform audio_*.go for the choices).
//
// The darwin and windows backends self-heal: if the device path silently
// dies (waking from a long sleep can restart the audio server and orphan
// the stream), a watchdog rebuilds it within a few seconds. On linux, ALSA
// suspend/resume recovery is handled in the feeder loop via snd_pcm_recover.
func StartAudio(sampleRate int, fill AudioFillFn) error {
	if fill == nil {
		return errors.New("StartAudio: nil fill function")
	}
	if audioFill != nil {
		return errors.New("StartAudio: audio already started")
	}
	audioFill = fill
	if err := audioStart(sampleRate); err != nil {
		audioFill = nil
		return err
	}
	return nil
}

// audioLastFill is the wall-clock time (unix nanos) of the most recent pull
// through audioFill. Backends stamp it via audioNoteFill on every buffer
// they fill. Audio pulls continuously (~6-12ms) even when the app is silent
// — silence is rendered, never skipped — so a stall of a few seconds means
// the device path is dead, not idle.
var audioLastFill atomic.Int64

func audioNoteFill() {
	audioLastFill.Store(time.Now().UnixNano())
}

// audioWatchdog revives audio when the platform stream silently dies. When
// nothing has pulled samples for a while, restart rebuilds the stream from
// scratch; a failed rebuild (e.g. mid-wake, device not back yet) simply
// retries on the next tick. Started by backends that need it (darwin,
// windows) after a successful audioStart.
func audioWatchdog(restart func() error) {
	const checkEvery = 1 * time.Second
	const deadAfter = 2500 * time.Millisecond
	for {
		time.Sleep(checkEvery)
		last := time.Unix(0, audioLastFill.Load())
		if time.Since(last) < deadAfter {
			continue
		}
		err := restart()
		// stamp regardless of outcome so failures retry at deadAfter pace
		// instead of every tick
		audioNoteFill()
		if err != nil {
			fmt.Fprintf(os.Stderr, "audio: restart after stall failed: %v\n", err)
		}
	}
}
