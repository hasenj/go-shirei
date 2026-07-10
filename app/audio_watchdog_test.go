//go:build darwin || windows

package app

import (
	"os"
	"sync/atomic"
	"testing"
	"time"
)

// TestAudioWatchdogRevival opens the real output device, pauses the stream
// to simulate it dying (the post-sleep symptom: pulls silently stop), and
// asserts the watchdog rebuilds it and pulls resume. Gated behind an env
// var because it touches audio hardware (it only ever renders silence):
//
//	SHIREI_AUDIO_DEVICE_TESTS=1 go test ./app -run AudioWatchdog -v
//
// On windows, run it under wine with a cross-compiled test binary:
//
//	GOOS=windows GOARCH=amd64 go test -c -o /tmp/apptest.exe ./app
//	SHIREI_AUDIO_DEVICE_TESTS=1 wine /tmp/apptest.exe -test.run AudioWatchdog -test.v
func TestAudioWatchdogRevival(t *testing.T) {
	if os.Getenv("SHIREI_AUDIO_DEVICE_TESTS") == "" {
		t.Skip("set SHIREI_AUDIO_DEVICE_TESTS=1 to run (opens the audio device)")
	}

	var fills atomic.Int64
	err := StartAudio(44100, func(out []float32) {
		fills.Add(1)
		clear(out) // silence
	})
	if err != nil {
		t.Fatalf("StartAudio: %v", err)
	}

	waitFills := func(label string) {
		start := fills.Load()
		deadline := time.Now().Add(3 * time.Second)
		for fills.Load() < start+10 {
			if time.Now().After(deadline) {
				t.Fatalf("%s: callbacks not flowing (%d new fills)", label, fills.Load()-start)
			}
			time.Sleep(50 * time.Millisecond)
		}
	}

	waitFills("initial start")

	if err := audioPause(); err != nil {
		t.Fatalf("pause: %v", err)
	}
	// confirm the queue is actually dead before trusting the revival
	time.Sleep(500 * time.Millisecond)
	base := fills.Load()
	time.Sleep(500 * time.Millisecond)
	if got := fills.Load() - base; got != 0 {
		t.Fatalf("queue still filling after pause (%d fills) — test setup is wrong", got)
	}

	// the watchdog checks every 1s and declares death after 2.5s; allow
	// two full cycles of slack
	deadline := time.Now().Add(8 * time.Second)
	for fills.Load() == base {
		if time.Now().After(deadline) {
			t.Fatal("watchdog did not revive the paused queue within 8s")
		}
		time.Sleep(100 * time.Millisecond)
	}
	waitFills("after revival")
	t.Logf("queue revived; callbacks flowing again (%d total fills)", fills.Load())
}
