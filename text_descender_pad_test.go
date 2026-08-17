package shirei

import "testing"

func TestLastLineDescenderPadFromFontMetrics(t *testing.T) {
	InitFontSubsystem()
	st := DefaultTextStyle()
	shaped := ShapeText("gypq", st)
	if len(shaped.Lines) == 0 {
		t.Skip("no lines / no fonts")
	}
	pad := lastLineDescenderPad(&shaped.Lines[0], st, nil)
	// Default face at 12: depth ≈ 3.5, reserved ≈ 2.2 → pad ≈ 1.3
	if pad <= 0 {
		t.Fatalf("expected positive pad from real descender, got %v", pad)
	}
	if pad > st.FontSize {
		t.Fatalf("pad %v implausibly large for size %v", pad, st.FontSize)
	}

	// Larger inline span on the last line must increase pad (deeper descender).
	base := DefaultTextStyle()
	base.FontSize = 12
	text := "aagy"
	shaped2 := ShapeText(text, base, Span(2, 4, FontSize(24)))
	if len(shaped2.Lines) == 0 {
		t.Fatal("no lines for mixed size")
	}
	resolved := resolveTextSpans(base, []TextSpan{Span(2, 4, FontSize(24))})
	resolved = effectiveSpans(base, resolved, len([]rune(text)))
	padBig := lastLineDescenderPad(&shaped2.Lines[0], base, resolved)
	if padBig <= pad {
		t.Fatalf("larger inline size should need more pad: base %v mixed %v", pad, padBig)
	}
}

func TestCaretHeightForStyleIncludesDescender(t *testing.T) {
	InitFontSubsystem()
	st := DefaultTextStyle()
	h := CaretHeightForStyle(st)
	if h <= st.FontSize {
		t.Fatalf("caret height %v should exceed em %v", h, st.FontSize)
	}
	// baseline + descender, not a second em
	if h > st.FontSize*1.5 {
		t.Fatalf("caret height %v implausibly large for size %v", h, st.FontSize)
	}
}
