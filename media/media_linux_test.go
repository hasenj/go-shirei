//go:build linux && !android

package media

import (
	"context"
	"encoding/binary"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"go.hasen.dev/shirei"
)

func TestOutputSize(t *testing.T) {
	cases := []struct{ inW, inH, w, h int }{
		{320, 180, 320, 180},
		{3840, 2160, 1920, 1080},
		{1919, 1080, 1918, 1080},
		{100, 1, 100, 2},
	}
	for _, c := range cases {
		w, h := outputSize(c.inW, c.inH)
		if w != c.w || h != c.h {
			t.Fatalf("outputSize(%d,%d) = %d,%d want %d,%d", c.inW, c.inH, w, h, c.w, c.h)
		}
	}
}

func TestPlayback(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	path := filepath.Join(t.TempDir(), "clip.mp4")
	cmd := exec.Command("ffmpeg", "-y",
		"-f", "lavfi", "-i", "testsrc=size=320x180:rate=30",
		"-f", "lavfi", "-i", "sine=frequency=440:sample_rate=44100",
		"-t", "2",
		"-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p", "-g", "15",
		"-c:a", "aac",
		path,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("make clip: %v\n%s", err, out)
	}

	p, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	if w, h := p.Size(); w != 320 || h != 180 {
		t.Fatalf("size %d×%d", w, h)
	}
	if d := p.Duration(); d < 1500*time.Millisecond || d > 2500*time.Millisecond {
		t.Fatalf("duration %s", d)
	}

	p.SetMuted(true)
	p.Play()
	waitUntil(t, 4*time.Second, func() bool {
		return p.Position() > 300*time.Millisecond && p.Err() == nil && frameReady(p)
	})
	if err := p.Err(); err != nil {
		t.Fatal(err)
	}
	if !frameReady(p) {
		t.Fatal("no frame")
	}
	if !p.Playing() {
		t.Fatal("expected playing")
	}

	p.Pause()
	held := p.Position()
	time.Sleep(250 * time.Millisecond)
	if delta := p.Position() - held; delta < 0 || delta > 120*time.Millisecond {
		t.Fatalf("position moved while paused: %s -> %s", held, p.Position())
	}

	p.Play()
	p.Seek(1.2)
	waitUntil(t, 3*time.Second, func() bool {
		return p.Position() >= 1100*time.Millisecond && p.Err() == nil
	})
	if err := p.Err(); err != nil {
		t.Fatal(err)
	}
	if pos := p.Position(); pos < 1100*time.Millisecond {
		t.Fatalf("seek position %s", pos)
	}
}

func frameReady(p *Player) bool {
	p.mu.Lock()
	id := p.id
	p.mu.Unlock()
	img := shirei.LookupImage(id)
	if img == nil || len(img.Pix) < 4 {
		return false
	}
	for i := 0; i+3 < len(img.Pix); i += 64 {
		if img.Pix[i] != 0 || img.Pix[i+1] != 0 || img.Pix[i+2] != 0 {
			return true
		}
	}
	return false
}

func waitUntil(t *testing.T, d time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out after %s", d)
}

func TestOpenMissing(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	_, err := Open(filepath.Join(t.TempDir(), "no-such-file.mp4"))
	if err == nil {
		t.Fatal("expected error")
	}
	if !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

type chunkReader struct {
	data   []byte
	chunks []int
	off    int
	idx    int
}

func (c *chunkReader) Read(p []byte) (int, error) {
	if c.off >= len(c.data) {
		return 0, io.EOF
	}
	n := len(p)
	if c.idx < len(c.chunks) {
		if c.chunks[c.idx] < n {
			n = c.chunks[c.idx]
		}
		c.idx++
	}
	if rem := len(c.data) - c.off; n > rem {
		n = rem
	}
	copy(p, c.data[c.off:c.off+n])
	c.off += n
	return n, nil
}

func TestReadAudioMisalignedChunks(t *testing.T) {
	// Interleaved stereo float32 frames (2 samples = 8 bytes per frame).
	want := []float32{0.25, -0.5, 0.75, -1.0, 0.125, 0.5}
	raw := make([]byte, len(want)*4)
	for i, s := range want {
		binary.LittleEndian.PutUint32(raw[i*4:], math.Float32bits(s))
	}
	p := &streamPlayer{audio: newPCMRing(64), playCh: make(chan struct{})}
	r := &chunkReader{data: raw, chunks: []int{3, 2, 1, 7, 3, 4, 4}}
	if err := readAudioPipe(context.Background(), p, r); err != io.EOF {
		t.Fatalf("readAudioPipe err = %v, want io.EOF", err)
	}
	got := make([]float32, len(want))
	p.audio.mu.Lock()
	n := p.audio.readLocked(got)
	p.audio.mu.Unlock()
	if n != len(want) {
		t.Fatalf("read %d samples, want %d", n, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sample[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestAudioMixMultiplePlayers(t *testing.T) {
	p1 := &streamPlayer{audio: newPCMRing(64)}
	p2 := &streamPlayer{audio: newPCMRing(64)}
	p1.playingA.Store(true)
	p2.playingA.Store(true)
	p1.volumeBits.Store(math.Float32bits(0.5))
	p2.volumeBits.Store(math.Float32bits(1.0))
	p1.audio.write([]float32{0.6, 0.8, 0.4, 0.2})
	p2.audio.write([]float32{0.2, 0.9, 0.1, 0.3})

	out := make([]float32, 4)
	p1.mixInto(out, true)
	p2.mixInto(out, true)
	if math.Abs(float64(out[0]-0.5)) > 1e-5 {
		t.Fatalf("out[0] = %v, want 0.5", out[0])
	}
	if out[1] != 1.0 {
		t.Fatalf("out[1] = %v, want clamped 1.0", out[1])
	}
	if p1.played.Load() != 2 || p2.played.Load() != 2 {
		t.Fatalf("played frames = %d, %d; want 2, 2", p1.played.Load(), p2.played.Load())
	}
}
