//go:build darwin && !ios

package media

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/ebitengine/purego/objc"

	"go.hasen.dev/shirei"
)

func TestMain(m *testing.M) {
	runtime.LockOSThread()
	if err := bind(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	done := make(chan int, 1)
	go func() {
		done <- m.Run()
	}()
	for {
		select {
		case code := <-done:
			os.Exit(code)
		default:
			pumpMainRunLoop(10 * time.Millisecond)
		}
	}
}

func pumpMainRunLoop(d time.Duration) {
	pool := objc.Send[objc.ID](cls("NSAutoreleasePool"), sel("new"))
	defer objc.Send[objc.ID](pool, sel("drain"))
	rl := objc.Send[objc.ID](cls("NSRunLoop"), sel("mainRunLoop"))
	until := objc.Send[objc.ID](cls("NSDate"), sel("dateWithTimeIntervalSinceNow:"), d.Seconds())
	rl.Send(sel("runUntilDate:"), until)
}

func makeTestClip(t *testing.T, seconds int) string {
	t.Helper()
	ffmpeg := findFFmpeg(t)
	path := filepath.Join(t.TempDir(), "clip.mp4")
	cmd := exec.Command(ffmpeg, "-y",
		"-f", "lavfi", "-i", "testsrc=size=320x180:rate=30",
		"-f", "lavfi", "-i", "sine=frequency=440:sample_rate=44100",
		"-t", strconv.Itoa(seconds),
		"-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p", "-g", "15",
		"-c:a", "aac",
		path,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("make clip: %v\n%s", err, out)
	}
	return path
}

func findFFmpeg(t *testing.T) string {
	t.Helper()
	if p, err := exec.LookPath("ffmpeg"); err == nil {
		return p
	}
	for _, p := range []string{"/opt/homebrew/bin/ffmpeg", "/usr/local/bin/ffmpeg"} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	t.Skip("ffmpeg not installed")
	return ""
}

func TestPlaybackDarwin(t *testing.T) {
	path := makeTestClip(t, 2)

	p, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	p.SetMuted(true)
	p.SetVolume(0.5)
	if !p.Muted() || p.Volume() != 0.5 {
		t.Fatalf("muted=%v vol=%v", p.Muted(), p.Volume())
	}

	p.Play()
	waitUntil(t, 5*time.Second, func() bool {
		return p.Position() > 250*time.Millisecond && p.Err() == nil && frameReadyRGBA(p)
	}, func() string {
		w, h := p.Size()
		return fmt.Sprintf("pos=%s dur=%s size=%dx%d id=%d playing=%v ended=%v err=%v",
			p.Position(), p.Duration(), w, h, p.ImageId(), p.Playing(), p.Ended(), p.Err())
	})
	if err := p.Err(); err != nil {
		t.Fatal(err)
	}
	w, h := p.Size()
	if w != 320 || h != 180 {
		t.Fatalf("size = %d×%d, want 320×180", w, h)
	}
	if d := p.Duration(); d < 1500*time.Millisecond || d > 2500*time.Millisecond {
		t.Fatalf("duration = %s", d)
	}

	p.Pause()
	held := p.Position()
	time.Sleep(200 * time.Millisecond)
	if delta := p.Position() - held; delta < -50*time.Millisecond || delta > 150*time.Millisecond {
		t.Fatalf("position moved while paused: %s -> %s", held, p.Position())
	}

	p.Seek(1.2)
	p.Play()
	waitUntil(t, 4*time.Second, func() bool {
		return p.Position() >= 1100*time.Millisecond && p.Err() == nil
	})
	if err := p.Err(); err != nil {
		t.Fatal(err)
	}

	// Wait for end-of-stream (no loop).
	waitUntil(t, 4*time.Second, func() bool {
		return p.Ended() && !p.Playing()
	})

	id := p.ImageId()
	p.Close()
	// Calling controls after Close must be safe no-ops.
	p.Pause()
	p.Play()
	p.Seek(0.5)
	p.SetMuted(false)
	p.SetVolume(1)

	if id != 0 && shirei.LookupImage(id) != nil {
		t.Fatal("Close should release the registered image immediately")
	}
}

func TestTwoPlayersConcurrentCloseDarwin(t *testing.T) {
	path := makeTestClip(t, 4)

	p1, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	p2, err := Open(path)
	if err != nil {
		p1.Close()
		t.Fatal(err)
	}
	defer p1.Close()
	defer p2.Close()

	p1.SetMuted(true)
	p2.SetMuted(true)
	p1.Play()
	p2.Play()

	waitUntil(t, 5*time.Second, func() bool {
		return frameReadyRGBA(p1) && frameReadyRGBA(p2) &&
			p1.Position() > 150*time.Millisecond && p2.Position() > 150*time.Millisecond
	}, func() string {
		w1, h1 := p1.Size()
		w2, h2 := p2.Size()
		return fmt.Sprintf("p1(pos=%s dur=%s size=%dx%d id=%d ready=%v playing=%v ended=%v err=%v) p2(pos=%s dur=%s size=%dx%d id=%d ready=%v playing=%v ended=%v err=%v)",
			p1.Position(), p1.Duration(), w1, h1, p1.ImageId(), frameReadyRGBA(p1), p1.Playing(), p1.Ended(), p1.Err(),
			p2.Position(), p2.Duration(), w2, h2, p2.ImageId(), frameReadyRGBA(p2), p2.Playing(), p2.Ended(), p2.Err())
	})

	// Close p1 while playing; p2 must keep running cleanly.
	p1.Close()
	before := p2.Position()
	waitUntil(t, 3*time.Second, func() bool {
		return p2.Position() > before+150*time.Millisecond
	}, func() string {
		return fmt.Sprintf("p2(before=%s pos=%s dur=%s playing=%v ended=%v err=%v)",
			before, p2.Position(), p2.Duration(), p2.Playing(), p2.Ended(), p2.Err())
	})
	if err := p2.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestOpenMissingDarwin(t *testing.T) {
	_, err := Open(filepath.Join(t.TempDir(), "no-such-file.mp4"))
	if err == nil {
		t.Fatal("expected error")
	}
	if !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func frameReadyRGBA(p *Player) bool {
	id := p.ImageId()
	if id == 0 {
		return false
	}
	img := shirei.LookupImage(id)
	if img == nil || len(img.RGBA.Pix) < 4 {
		return false
	}
	for i := 0; i+3 < len(img.RGBA.Pix); i += 64 {
		if img.RGBA.Pix[i] != 0 || img.RGBA.Pix[i+1] != 0 || img.RGBA.Pix[i+2] != 0 {
			return true
		}
	}
	return false
}

func waitUntil(t *testing.T, d time.Duration, cond func() bool, diag ...func() string) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(diag) > 0 && diag[0] != nil {
		t.Fatalf("timed out after %s (%s)", d, diag[0]())
	}
	t.Fatalf("timed out after %s", d)
}
