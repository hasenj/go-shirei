package widgets

import "testing"

func TestLineSelectionLineRange(t *testing.T) {
	var s LineSelection
	s.Anchor = LinePos{1, 2}
	s.Head = LinePos{3, 4}

	// outside
	if lo, hi := s.LineRange(0, 10); lo != 0 || hi != 0 {
		t.Fatalf("line 0: got %d,%d", lo, hi)
	}
	// first line of range
	if lo, hi := s.LineRange(1, 10); lo != 2 || hi != 10 {
		t.Fatalf("line 1: got %d,%d want 2,10", lo, hi)
	}
	// middle
	if lo, hi := s.LineRange(2, 10); lo != 0 || hi != 10 {
		t.Fatalf("line 2: got %d,%d want 0,10", lo, hi)
	}
	// last
	if lo, hi := s.LineRange(3, 10); lo != 0 || hi != 4 {
		t.Fatalf("line 3: got %d,%d want 0,4", lo, hi)
	}
}

func TestLineSelectionCopy(t *testing.T) {
	lines := []string{"alpha", "bravo", "charlie"}
	line := func(i int) string { return lines[i] }

	var s LineSelection
	s.Anchor = LinePos{0, 1} // "lpha"
	s.Head = LinePos{2, 3}   // "cha"

	got, ok := s.Copy(len(lines), line)
	if !ok {
		t.Fatal("expected copy ok")
	}
	want := "lpha\nbravo\ncha"
	if got != want {
		t.Fatalf("Copy = %q, want %q", got, want)
	}

	s.Clear()
	if _, ok := s.Copy(len(lines), line); ok {
		t.Fatal("empty selection should not copy")
	}
}

func TestLineSelectionOrdered(t *testing.T) {
	var s LineSelection
	s.Anchor = LinePos{5, 0}
	s.Head = LinePos{2, 3}
	from, to := s.Ordered()
	if from != (LinePos{2, 3}) || to != (LinePos{5, 0}) {
		t.Fatalf("Ordered = %v,%v", from, to)
	}
}
