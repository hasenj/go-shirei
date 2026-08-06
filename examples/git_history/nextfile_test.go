package main

import "testing"

func TestLastFileHeaderInRange(t *testing.T) {
	headers := []int{0, 10, 50, 100}
	if got := lastFileHeaderInRange(headers, 5, 60); got != 50 {
		t.Fatalf("in view got %d want 50", got)
	}
	if got := lastFileHeaderInRange(headers, 70, 90); got != 50 {
		t.Fatalf("mid-file got %d want 50", got)
	}
	if got := lastFileHeaderInRange(headers, 0, 5); got != 0 {
		t.Fatalf("top got %d want 0", got)
	}
	if got := lastFileHeaderInRange(nil, 0, 10); got != -1 {
		t.Fatalf("empty headers got %d", got)
	}
	if got := lastFileHeaderInRange(headers, -1, -1); got != -1 {
		t.Fatalf("invalid range got %d", got)
	}
}

func TestPrevFileHeaderBefore(t *testing.T) {
	headers := []int{0, 10, 50}
	// Mid-file after header 10: firstVis=20 → header 10 (start of current).
	if got := prevFileHeaderBefore(headers, 20); got != 10 {
		t.Fatalf("mid-file got %d want 10", got)
	}
	// Already at header 10: firstVis=10 → header 0 (previous file).
	if got := prevFileHeaderBefore(headers, 10); got != 0 {
		t.Fatalf("at header got %d want 0", got)
	}
	// At top of first file: firstVis=0 → none.
	if got := prevFileHeaderBefore(headers, 0); got != -1 {
		t.Fatalf("top got %d want -1", got)
	}
	// One line into first file: firstVis=1 → header 0.
	if got := prevFileHeaderBefore(headers, 1); got != 0 {
		t.Fatalf("into first got %d want 0", got)
	}
}

func TestNextFileHeaderAfter(t *testing.T) {
	headers := []int{0, 10, 50}
	if got := nextFileHeaderAfter(headers, 0); got != 10 {
		t.Fatalf("got %d", got)
	}
	if got := nextFileHeaderAfter(headers, 50); got != -1 {
		t.Fatalf("after last got %d", got)
	}
	if got := nextFileHeaderAfter(headers, -1); got != 0 {
		t.Fatalf("from none got %d", got)
	}
}


