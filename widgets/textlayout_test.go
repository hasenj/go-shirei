package widgets

import (
	"testing"

	. "go.hasen.dev/shirei"
)

func TestTextLayoutDisplayDocMapping(t *testing.T) {
	InitFontSubsystem()
	attrs := DefaultTextAttrs()
	attrs.Size = DefaultTextSize

	tl := makeTextLayout("ab", 1, "かな", [2]int{2, 2}, attrs, false)
	if !tl.composing || tl.compLen != 2 {
		t.Fatalf("expected composing layout with compLen=2, got composing=%v compLen=%d", tl.composing, tl.compLen)
	}
	if got, want := tl.displayCursor, 3; got != want {
		t.Fatalf("displayCursor = %d, want %d", got, want)
	}
	if got, want := tl.compositionFrom, 1; got != want {
		t.Fatalf("compositionFrom = %d, want %d", got, want)
	}
	if got, want := tl.compositionTo, 3; got != want {
		t.Fatalf("compositionTo = %d, want %d", got, want)
	}

	cases := []struct {
		display, doc int
	}{
		{0, 0},
		{1, 1}, // splice point
		{2, 1}, // inside composition → doc caret
		{3, 1}, // end of composition → doc caret
		{4, 2}, // after composition → trailing doc rune
	}
	for _, c := range cases {
		if got := tl.DisplayToDoc(c.display); got != c.doc {
			t.Errorf("DisplayToDoc(%d) = %d, want %d", c.display, got, c.doc)
		}
	}
	if got, want := tl.DocToDisplay(0), 0; got != want {
		t.Errorf("DocToDisplay(0) = %d, want %d", got, want)
	}
	if got, want := tl.DocToDisplay(1), 1; got != want {
		t.Errorf("DocToDisplay(1) = %d, want %d", got, want)
	}
	if got, want := tl.DocToDisplay(2), 4; got != want {
		t.Errorf("DocToDisplay(2) = %d, want %d", got, want)
	}

	plain := makeTextLayout("ab", 1, "", [2]int{}, attrs, false)
	if plain.composing {
		t.Fatal("empty composition should not mark composing")
	}
	if got, want := plain.DisplayToDoc(2), 2; got != want {
		t.Errorf("plain DisplayToDoc(2) = %d, want %d", got, want)
	}
}
