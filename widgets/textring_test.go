package widgets

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestTextRingAppendAndEvict(t *testing.T) {
	r := NewTextRingSize(64, 32)
	r.AppendLine("one")
	r.AppendLine("two")
	r.AppendLine("three")
	if r.Len() != 3 {
		t.Fatalf("Len=%d want 3", r.Len())
	}
	if r.Line(0) != "one" || r.Line(1) != "two" || r.Line(2) != "three" {
		t.Fatalf("lines=%q %q %q", r.Line(0), r.Line(1), r.Line(2))
	}

	for i := 0; i < 40; i++ {
		r.AppendLine(fmt.Sprintf("line-%02d-xxxxxxxx", i))
	}
	if r.Bytes() > int64(r.Cap()) {
		t.Fatalf("Bytes=%d > Cap=%d", r.Bytes(), r.Cap())
	}
	if r.DroppedLines() == 0 {
		t.Fatal("expected some dropped lines")
	}
	for i := 0; i < r.Len(); i++ {
		s := r.Line(i)
		if !strings.HasPrefix(s, "line-") {
			t.Fatalf("line %d = %q", i, s)
		}
	}
}

func TestTextRingLineIDStableAcrossEvict(t *testing.T) {
	r := NewTextRingSize(48, 32)
	r.AppendLine("a")
	r.AppendLine("b")
	idB := r.LineID(1)
	for i := 0; i < 30; i++ {
		r.AppendLine(fmt.Sprintf("x%02dxxxxxx", i))
	}
	for i := 0; i < r.Len(); i++ {
		if r.Line(i) == "b" {
			if r.LineID(i) != idB {
				t.Fatalf("LineID for b changed: %d → %d", idB, r.LineID(i))
			}
		}
	}
}

func TestTextRingAppendRaw(t *testing.T) {
	r := NewTextRingSize(256, 32)
	r.Append([]byte("hello"))
	if r.Len() != 1 || r.Line(0) != "hello" {
		t.Fatalf("open fragment: Len=%d Line0=%q", r.Len(), r.Line(0))
	}
	r.Append([]byte(" world\nmore\n"))
	if r.Len() != 2 {
		t.Fatalf("Len=%d want 2 (hello world, more)", r.Len())
	}
	if r.Line(0) != "hello world" || r.Line(1) != "more" {
		t.Fatalf("%q / %q", r.Line(0), r.Line(1))
	}
}

func TestTextRingEmptyLines(t *testing.T) {
	r := NewTextRingSize(64, 16)
	r.AppendLine("")
	r.AppendLine("x")
	r.AppendLine("")
	if r.Len() != 3 {
		t.Fatalf("Len=%d", r.Len())
	}
	if r.Line(0) != "" || r.Line(1) != "x" || r.Line(2) != "" {
		t.Fatalf("%q %q %q", r.Line(0), r.Line(1), r.Line(2))
	}
}

func TestTextRingWrapPhysical(t *testing.T) {
	r := NewTextRingSize(32, 16)
	for i := 0; i < 20; i++ {
		r.AppendLine(fmt.Sprintf("%02d-aaaa", i)) // 8 bytes + nl = 9
	}
	if r.Len() == 0 {
		t.Fatal("empty after fills")
	}
	var total int
	for i := 0; i < r.Len(); i++ {
		total += len(r.Line(i))
	}
	if total == 0 {
		t.Fatal("all empty lines")
	}
}

func TestTextRingMaxLinesIndependentOfBytes(t *testing.T) {
	// Plenty of byte room; line ring of 4 forces forgetting boundaries.
	r := NewTextRingSize(1<<20, 4)
	for i := 0; i < 10; i++ {
		r.AppendLine(fmt.Sprintf("L%d", i))
	}
	if r.Len() != 4 {
		t.Fatalf("Len=%d want 4", r.Len())
	}
	if r.Line(0) != "L6" || r.Line(3) != "L9" {
		t.Fatalf("got %q .. %q", r.Line(0), r.Line(3))
	}
	if r.LineID(0) != 6 {
		t.Fatalf("first LineID=%d want 6", r.LineID(0))
	}
	// bytes for forgotten lines may still sit in the ring until Cap binds
	if r.Bytes() == 0 {
		t.Fatal("expected retained bytes")
	}
}

func TestTextRingMonotonicIDsPastWrap(t *testing.T) {
	r := NewTextRingSize(64, 8)
	var lastID int64 = -1
	for i := 0; i < 200; i++ {
		r.AppendLine(fmt.Sprintf("n%03d-xxxx", i))
		id := r.LineID(r.Len() - 1)
		if id <= lastID {
			t.Fatalf("LineID not monotonic: %d after %d", id, lastID)
		}
		lastID = id
	}
	if r.LineID(0) < 100 {
		t.Fatalf("expected first retained id well past wrap, got %d", r.LineID(0))
	}
	// every retained line readable and matches its id suffix pattern
	for i := 0; i < r.Len(); i++ {
		want := fmt.Sprintf("n%03d-xxxx", r.LineID(i))
		if got := r.Line(i); got != want {
			t.Fatalf("line %d id=%d: %q want %q", i, r.LineID(i), got, want)
		}
	}
}

func TestTextRingChunkBoundaries(t *testing.T) {
	r := NewTextRingSize(256, 64)
	// split lines across Append calls
	r.Append([]byte("aa"))
	r.Append([]byte("bb\ncc"))
	r.Append([]byte("dd\ne"))
	r.Append([]byte("e\n"))
	want := []string{"aabb", "ccdd", "ee"}
	if r.Len() != len(want) {
		t.Fatalf("Len=%d want %d", r.Len(), len(want))
	}
	for i, w := range want {
		if g := r.Line(i); g != w {
			t.Fatalf("Line(%d)=%q want %q", i, g, w)
		}
	}
}

// TestTextRingFloodAppendCost fills past capacity and checks that append cost
// stays flat (no O(n) starts-slice copies). Run with -v to see the timings:
//
//	go test ./experimental_widgets -run TestTextRingFloodAppendCost -v
func TestTextRingFloodAppendCost(t *testing.T) {
	const (
		byteCap  = 1 << 20 // 1 MiB — fast in CI, still exercises wrap
		maxLines = 64 << 10
		chunkN   = 256 // bytes per Append, with newlines
	)
	r := NewTextRingSize(byteCap, maxLines)

	// build a chunk of ~chunkN bytes of short lines
	var chunk []byte
	for len(chunk) < chunkN {
		chunk = fmt.Appendf(chunk, "flood-%d-xxxxxxxx\n", len(chunk))
	}

	timeN := func(n int) time.Duration {
		t0 := time.Now()
		for i := 0; i < n; i++ {
			r.Append(chunk)
		}
		return time.Since(t0)
	}

	// warm / fill to capacity
	fill := timeN( (byteCap/len(chunk))*2 )
	if r.Bytes() > int64(byteCap) {
		t.Fatalf("Bytes=%d > Cap=%d", r.Bytes(), byteCap)
	}
	if r.DroppedLines() == 0 && r.Bytes() < int64(byteCap)/2 {
		t.Fatalf("expected to be near/full after fill; Bytes=%d dropped=%d", r.Bytes(), r.DroppedLines())
	}

	const rounds = 4000
	warm := timeN(200) // steady-state warm-up (discard)
	_ = warm
	steady := timeN(rounds)

	per := steady / rounds
	t.Logf("fill %d chunks in %v; steady %d chunks in %v (%v/chunk); Bytes=%d Len=%d droppedLines=%d",
		(byteCap/len(chunk))*2, fill, rounds, steady, per, r.Bytes(), r.Len(), r.DroppedLines())

	// Before the ring-starts fix this blew past milliseconds/chunk once full.
	// A healthy append of 256B should be well under 50µs even on a slow CI box.
	const budget = 50 * time.Microsecond
	if per > budget {
		t.Fatalf("steady append too slow: %v/chunk (budget %v) — starts ring likely copying", per, budget)
	}

	// spot-check boundaries after flood: every line non-empty-ish and LineID contiguous
	n := r.Len()
	if n == 0 {
		t.Fatal("no lines after flood")
	}
	for i := 0; i < n; i++ {
		if r.LineID(i) != r.LineID(0)+int64(i) {
			t.Fatalf("LineID gap at %d", i)
		}
		s := r.Line(i)
		if !strings.HasPrefix(s, "flood-") {
			t.Fatalf("Line(%d)=%q", i, s)
		}
	}
}
