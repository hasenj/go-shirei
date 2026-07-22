package widgets

import (
	"testing"

	. "go.hasen.dev/shirei"
)

func TestCaretAtDirBoundary(t *testing.T) {
	InitFontSubsystem()
	attrs := DefaultTextStyle()
	attrs.FontSize = DefaultTextSize

	shaped := ShapeText("hey عربي world", attrs)
	if len(shaped.Lines) == 0 {
		t.Skip("no usable fonts")
	}

	// Walk every caret-legal stop; marks should fire only where the
	// nearest non-space visual neighbors have different directions.
	bounds := clusterBounds(shaped)
	var atBoundary, notBoundary int
	for _, cursor := range bounds {
		caret := computeCursorPos(cursor, shaped)
		if caretAtDirBoundary(shaped, caret, attrs.FontSize) {
			atBoundary++
			left, right, ok := visualStrongNeighborsAtCaret(shaped, caret, attrs.FontSize)
			if !ok || left.Dir == right.Dir {
				t.Fatalf("cursor %d reported boundary but neighbors ok=%v dirs=%v/%v",
					cursor, ok, left.Dir, right.Dir)
			}
		} else {
			notBoundary++
		}
	}
	if atBoundary == 0 {
		t.Fatal("expected at least one LTR↔RTL caret boundary in mixed string")
	}
	if notBoundary == 0 {
		t.Fatal("expected some interior stops not at a dir boundary")
	}

	// Pure LTR: never a boundary.
	ltr := ShapeText("hello world", attrs)
	for _, cursor := range clusterBounds(ltr) {
		if caretAtDirBoundary(ltr, computeCursorPos(cursor, ltr), attrs.FontSize) {
			t.Fatalf("pure LTR cursor %d should not be a dir boundary", cursor)
		}
	}
}
