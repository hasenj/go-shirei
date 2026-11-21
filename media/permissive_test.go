//go:build (darwin && !ios) || (linux && !android) || windows

package media

import (
	"fmt"
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
			shirei.WithFrameLock(func() {
				entry := shirei.LookupImage(id)
				if entry != nil && len(entry.RGBA.Pix) > 0 {
					for i := 0; i < len(entry.RGBA.Pix); i += 4 {
						if entry.RGBA.Pix[i] > 16 || entry.RGBA.Pix[i+1] > 16 || entry.RGBA.Pix[i+2] > 16 {
							nonZero = true
							break
						}
					}
				}
			})
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

var _ = fmt.Sprintf
