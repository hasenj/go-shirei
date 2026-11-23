//go:build (darwin && !ios) || (linux && !android) || windows

package media

import (
	"context"
	"testing"
	"time"

	"go.hasen.dev/shirei"
)

func runPermissivePlaybackTest(t *testing.T, clipPath string) {
	t.Helper()

	p, err := Open(clipPath)
	if err != nil {
		t.Fatalf("Open(%s): %v", clipPath, err)
	}
	defer p.Close()

	p.SetMuted(true)
	p.Play()

	deadline := time.Now().Add(5 * time.Second)
	var gotFrame bool
	for time.Now().Before(deadline) {
		if err := p.Err(); err != nil {
			t.Fatalf("player error: %v", err)
		}
		id := p.ImageId()
		if id != 0 && p.Position() > 200*time.Millisecond {
			var nonZero bool
			if entry := shirei.LookupImage(id); entry != nil && len(entry.RGBA.Pix) > 0 {
				for i := 0; i < len(entry.RGBA.Pix); i += 4 {
					if entry.RGBA.Pix[i] > 16 || entry.RGBA.Pix[i+1] > 16 || entry.RGBA.Pix[i+2] > 16 {
						nonZero = true
						break
					}
				}
			}
			if nonZero {
				gotFrame = true
				break
			}
		}
		time.Sleep(15 * time.Millisecond)
	}
	if !gotFrame {
		w, h := p.Size()
		t.Fatalf("timed out waiting for decoded frame (pos=%s dur=%s size=%dx%d id=%d err=%v)",
			p.Position(), p.Duration(), w, h, p.ImageId(), p.Err())
	}

	w, h := p.Size()
	if w != 160 || h != 90 {
		t.Fatalf("size = %dx%d, want 160x90", w, h)
	}
	if dur := p.Duration(); dur < 1500*time.Millisecond || dur > 2600*time.Millisecond {
		t.Fatalf("duration = %s, want ~2s", dur)
	}

	// Test seek while playing.
	p.Seek(1.2)
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if p.Position() >= 1000*time.Millisecond {
			break
		}
		time.Sleep(15 * time.Millisecond)
	}
	if pos := p.Position(); pos < 1000*time.Millisecond {
		t.Fatalf("post-seek position = %s, expected >= 1s", pos)
	}

	p.Pause()
	if p.Playing() {
		t.Fatalf("expected Playing() == false after Pause()")
	}
	// Verify second Close is idempotent.
	p.Close()
}

func TestPermissiveAV1Opus(t *testing.T) {
	if !dav1dAvailable() {
		t.Skip("libdav1d not installed")
	}
	if !opusAvailable() {
		t.Skip("libopus not installed")
	}

	cases := []struct {
		name   string
		file   string
		ffArgs []string
	}{
		{
			name:   "webm-av1-opus",
			file:   "av1.webm",
			ffArgs: []string{"-c:v", "libsvtav1", "-preset", "12", "-g", "25", "-c:a", "libopus", "-b:a", "64k"},
		},
		{
			name:   "mp4-av1-opus",
			file:   "av1.mp4",
			ffArgs: []string{"-c:v", "libsvtav1", "-preset", "12", "-g", "25", "-c:a", "libopus", "-b:a", "64k", "-movflags", "+faststart"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := makeEncodedClip(t, tc.file, tc.ffArgs...)
			runPermissivePlaybackTest(t, path)
		})
	}
}

func TestPermissiveVPXAndVorbis(t *testing.T) {
	if !vpxVP9Available() || !vpxVP8Available() {
		t.Skip("libvpx not installed")
	}

	t.Run("webm-vp9-opus", func(t *testing.T) {
		if !opusAvailable() {
			t.Skip("libopus not installed")
		}
		path := makeEncodedClip(t, "vp9.webm",
			"-c:v", "libvpx-vp9", "-deadline", "realtime", "-cpu-used", "8", "-g", "25",
			"-c:a", "libopus", "-b:a", "64k")
		runPermissivePlaybackTest(t, path)
	})

	t.Run("mp4-vp9-opus", func(t *testing.T) {
		if !opusAvailable() {
			t.Skip("libopus not installed")
		}
		path := makeEncodedClip(t, "vp9.mp4",
			"-c:v", "libvpx-vp9", "-deadline", "realtime", "-cpu-used", "8", "-g", "25",
			"-c:a", "libopus", "-b:a", "64k", "-movflags", "+faststart")
		runPermissivePlaybackTest(t, path)
	})

	t.Run("webm-vp8-vorbis", func(t *testing.T) {
		if !vorbisAvailable() {
			t.Skip("libvorbis not installed")
		}
		path := makeEncodedClip(t, "vp8.webm",
			"-c:v", "libvpx", "-deadline", "realtime", "-cpu-used", "8", "-g", "25",
			"-c:a", "libvorbis", "-q:a", "3")
		runPermissivePlaybackTest(t, path)
	})
}

type noopStreamEngine struct{}

func (noopStreamEngine) info() probeInfo     { return probeInfo{width: 320, height: 180, fps: 30} }
func (noopStreamEngine) run(p *streamPlayer) { <-p.stop }
func (noopStreamEngine) close()              {}

func TestStreamPlayerPlayWakesBothAudioAndVideo(t *testing.T) {
	p := newStreamPlayer(noopStreamEngine{})
	defer p.Close()

	// Fill the PCM ring completely so writeAudio blocks while paused.
	fill := make([]float32, len(p.audio.buf))
	if n := p.audio.write(fill); n != len(fill) {
		t.Fatalf("prefill ring: wrote %d, want %d", n, len(fill))
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	videoDone := make(chan bool, 1)
	audioDone := make(chan bool, 1)

	go func() {
		videoDone <- p.waitUntil(ctx, 0)
	}()
	go func() {
		audioDone <- p.writeAudio(ctx, []float32{0.1, 0.2})
	}()

	// Give both goroutines time to block on the paused state.
	time.Sleep(40 * time.Millisecond)

	// Drain space in the ring and resume with a single Play() call.
	// Both video and audio waiters must wake up together.
	p.Play()
	p.audio.mu.Lock()
	p.audio.resetLocked()
	p.audio.mu.Unlock()

	deadline := time.After(500 * time.Millisecond)
	var vOK, aOK bool
	for !vOK || !aOK {
		select {
		case ok := <-videoDone:
			if !ok {
				t.Fatal("video waitUntil returned false")
			}
			vOK = true
		case ok := <-audioDone:
			if !ok {
				t.Fatal("audio writeAudio returned false")
			}
			aOK = true
		case <-deadline:
			t.Fatalf("Play() failed to wake both waiters: videoWoke=%v audioWoke=%v", vOK, aOK)
		}
	}
}

func TestStreamPlayerAlignClockAndAudioStallFallback(t *testing.T) {
	p := newStreamPlayer(noopStreamEngine{})
	defer p.Close()

	p.mu.Lock()
	p.audioOn = true
	p.playing = true
	p.base = 30.0
	p.mu.Unlock()
	p.playingA.Store(true)
	// Simulate 0.5s of audio already consumed before first video frame arrives at PTS 28.0s.
	p.played.Store(audioRate / 2)
	p.lastAudioNs.Store(time.Now().UnixNano())

	p.alignClock(28.0)
	pos := p.positionSeconds()
	if pos < 27.9 || pos > 28.1 {
		t.Fatalf("positionSeconds after alignClock(28.0) = %.3f, want ~28.0", pos)
	}

	// Simulate audio stalling for 1s: positionSeconds should advance past stallGrace.
	p.lastAudioNs.Store(time.Now().Add(-1 * time.Second).UnixNano())
	stalledPos := p.positionSeconds()
	if stalledPos < 28.5 {
		t.Fatalf("positionSeconds during audio stall = %.3f, want > 28.5", stalledPos)
	}
}
