package shirei

import "testing"

func requireTextShaping(t *testing.T) TextStyleAttrs {
	t.Helper()
	InitFontSubsystem()
	attrs := DefaultTextStyle()
	probe := ShapeText("alpha", attrs)
	if len(probe.Lines) != 1 || len(probe.Lines[0].Segments) == 0 {
		t.Skip("no usable system fonts for text shaping")
	}
	return attrs
}

func TestShapeTextNewlineHasNoAdvance(t *testing.T) {
	attrs := requireTextShaping(t)
	bWidth := ShapeText("b", attrs).Lines[0].Width

	shaped := ShapeText("a\n\nb", attrs)
	if len(shaped.Lines) != 3 {
		t.Fatalf("line count = %d, want 3", len(shaped.Lines))
	}
	if shaped.Lines[1].Width != 0 {
		t.Errorf("empty hard line width = %.2f, want 0", shaped.Lines[1].Width)
	}
	if shaped.Lines[1].Height <= 0 {
		t.Errorf("empty hard line height = %.2f, want positive", shaped.Lines[1].Height)
	}
	if shaped.Lines[2].Width != bWidth {
		t.Errorf("line after hard break width = %.2f, want %.2f", shaped.Lines[2].Width, bWidth)
	}

	trailing := ShapeText("ab\n", attrs)
	if len(trailing.Lines) != 2 {
		t.Fatalf("trailing newline line count = %d, want 2", len(trailing.Lines))
	}
	if trailing.Lines[1].Width != 0 {
		t.Errorf("trailing phantom line width = %.2f, want 0", trailing.Lines[1].Width)
	}
	if trailing.Lines[1].Height <= 0 {
		t.Errorf("trailing phantom line height = %.2f, want positive", trailing.Lines[1].Height)
	}
}
