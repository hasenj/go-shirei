package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	. "go.hasen.dev/shirei"
)

func waitScannerTerminal(t *testing.T, s *Scanner, timeout time.Duration) State {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		var st State
		var cancelled bool
		WithFrameLock(func() {
			st = s.state
			cancelled = s.cancelled.Load()
		})
		if st == Done || st == Stopped || cancelled {
			// Give in-flight publish a moment to settle after cancel.
			if st == Done || st == Stopped {
				return st
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("scanner did not reach terminal state (state=%v cancelled=%v)", s.state, s.cancelled.Load())
	return s.state
}

// TestHardLinkCountedOnce: two directory entries hard-linked to the same
// inode contribute size only once, even when scanned by parallel workers.
func TestHardLinkCountedOnce(t *testing.T) {
	if runtime.GOOS == "windows" {
		// Link() works on NTFS but CI images vary; still run when possible.
	}
	root := t.TempDir()
	a := filepath.Join(root, "a")
	b := filepath.Join(root, "b")
	if err := os.Mkdir(a, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(b, 0o755); err != nil {
		t.Fatal(err)
	}
	payload := bytes.Repeat([]byte("h"), 32*1024)
	fa := filepath.Join(a, "file.bin")
	fb := filepath.Join(b, "file.bin")
	if err := os.WriteFile(fa, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(fa, fb); err != nil {
		t.Skipf("hard link not supported: %v", err)
	}

	info, err := os.Lstat(fa)
	if err != nil {
		t.Fatal(err)
	}
	one := int(PhysicalSize(fa, info))
	if one <= 0 {
		t.Fatalf("expected positive physical size, got %d", one)
	}

	s := newScanner()
	startScan(s, root)
	st := waitScannerTerminal(t, s, 10*time.Second)
	if st != Done {
		t.Fatalf("want Done, got %v", st)
	}

	var total int
	WithFrameLock(func() {
		total = s.root.Size
	})
	// Root size is sum of children; skipped hard-link name contributes 0.
	if total != one {
		t.Fatalf("hard link double-count: root size %d, want single file %d", total, one)
	}
}

// TestCancelDoesNotBecomeDone: closing/cancelling a scan must not be
// overwritten to Done by in-flight job completions.
func TestCancelDoesNotBecomeDone(t *testing.T) {
	root := t.TempDir()
	// Wide tree so workers are still in flight when we cancel.
	for i := 0; i < 40; i++ {
		d := filepath.Join(root, "d"+itoa(i))
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
		for j := 0; j < 20; j++ {
			p := filepath.Join(d, "f"+itoa(j)+".dat")
			if err := os.WriteFile(p, bytes.Repeat([]byte{byte(i)}, 4096), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}

	s := newScanner()
	startScan(s, root)
	time.Sleep(15 * time.Millisecond)

	WithFrameLock(func() {
		cancelScan(s)
	})

	// Wait for queue to drain far past a normal small-tree scan.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var st State
		WithFrameLock(func() { st = s.state })
		if st == Done {
			t.Fatal("cancelled scan promoted to Done")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !s.cancelled.Load() {
		t.Fatal("cancelled flag cleared")
	}
	if s.state != Stopped {
		t.Fatalf("want Stopped, got %v", s.state)
	}
}

// TestSymlinkDirCycleTerminates: a↔b directory symlinks must not unbounded-scan.
func TestSymlinkDirCycleTerminates(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "a")
	b := filepath.Join(root, "b")
	if err := os.Mkdir(a, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(b, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(a, "x"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(b, filepath.Join(a, "to_b")); err != nil {
		t.Skipf("symlink: %v", err)
	}
	if err := os.Symlink(a, filepath.Join(b, "to_a")); err != nil {
		t.Skipf("symlink: %v", err)
	}

	s := newScanner()
	startScan(s, root)
	st := waitScannerTerminal(t, s, 5*time.Second)
	if st != Done {
		t.Fatalf("cycle scan did not finish: %v", st)
	}
	// If we followed the cycle, submitted would explode; keep a sane bound.
	if s.submitted > 50 {
		t.Fatalf("suspected cycle: submitted=%d", s.submitted)
	}
}

func TestFlatListTotalAndProportion(t *testing.T) {
	a := &ScanEntry{Size: 10}
	b := &ScanEntry{Size: 30}
	c := &ScanEntry{Size: 10}
	entries := []*ScanEntry{a, b, c}
	total := flatListTotal(entries)
	if total != 50 {
		t.Fatalf("flatListTotal=%d want 50", total)
	}
	// Old bug: parentSize started as entry.Size then added every item → 10+50=60.
	if d := proportionDenominator(a, true, total); d != 50 {
		t.Fatalf("flat denom=%d want 50 (not self+total)", d)
	}
	parent := &ScanEntry{Size: 100}
	child := &ScanEntry{Size: 25, Parent: parent}
	if d := proportionDenominator(child, false, 0); d != 100 {
		t.Fatalf("tree denom=%d want parent 100", d)
	}
}

// tiny itoa without strconv import clutter
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [16]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
