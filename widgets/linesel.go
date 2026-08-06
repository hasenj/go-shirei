package widgets

import (
	"strings"

	. "go.hasen.dev/shirei"
)

// LinePos is a caret position in a line-addressed text surface: rune Rune
// within line Line. Products map Line to their own storage (TextRing index,
// DiffRow index, …); this type does not know that mapping.
type LinePos struct {
	Line int
	Rune int
}

func linePosLess(a, b LinePos) bool {
	return a.Line < b.Line || (a.Line == b.Line && a.Rune < b.Rune)
}

// LineSelection is a multi-line text range over an abstract line list.
// Pure model: no product identity (not LogView-specific, not Diff-specific).
//
// Callers own layout/virtualization. Typical frame:
//
//  1. LineSelectionFrame(sel, viewHovered, n, lineText) — release, clear, copy
//  2. per visible row: shape text, LineSelection.Hit, paint with LineRange
type LineSelection struct {
	Selecting bool // mouse is down and dragging
	Anchor    LinePos
	Head      LinePos
}

// Clear collapses the selection and ends a drag.
func (s *LineSelection) Clear() {
	s.Anchor, s.Head = LinePos{}, LinePos{}
	s.Selecting = false
}

// Empty reports a collapsed (zero-length) selection.
func (s *LineSelection) Empty() bool {
	return s.Anchor == s.Head
}

// Ordered returns the selection endpoints with from ≤ to in document order.
func (s *LineSelection) Ordered() (from, to LinePos) {
	from, to = s.Anchor, s.Head
	if linePosLess(to, from) {
		from, to = to, from
	}
	return
}

// LineRange is the [lo, hi) rune range of line idx covered by the selection.
// runeCount is len([]rune(lineText)) for that line.
func (s *LineSelection) LineRange(idx int, runeCount int) (lo, hi int) {
	from, to := s.Ordered()
	if from == to || idx < from.Line || idx > to.Line {
		return 0, 0
	}
	lo, hi = 0, runeCount
	if idx == from.Line {
		lo = min(from.Rune, runeCount)
	}
	if idx == to.Line {
		hi = min(to.Rune, runeCount)
	}
	return lo, hi
}

// Copy extracts the selected text. line(i) returns the full text of line i;
// n is the line count. Empty selection returns ("", false).
func (s *LineSelection) Copy(n int, line func(i int) string) (string, bool) {
	from, to := s.Ordered()
	if from == to || from.Line >= n {
		return "", false
	}
	to.Line = min(to.Line, n-1)
	var b strings.Builder
	for i := from.Line; i <= to.Line; i++ {
		runes := []rune(line(i))
		lo, hi := s.LineRange(i, len(runes))
		if i > from.Line {
			b.WriteByte('\n')
		}
		if lo < hi {
			b.WriteString(string(runes[lo:hi]))
		}
	}
	return b.String(), true
}

// LineSelectionFrame handles once-per-view input that is independent of rows:
// mouse-up ends a drag; click outside the view clears; Cmd/Ctrl+C copies.
// viewHovered is IsHovered() on the selection's outer container.
func LineSelectionFrame(sel *LineSelection, viewHovered bool, n int, line func(i int) string) {
	if GetFrameInput().Mouse == MouseRelease {
		sel.Selecting = false
	}
	if GetFrameInput().Mouse == MouseClick && !viewHovered {
		sel.Clear()
	}
	if ActiveCombo() == Combo(KeyC, PrimaryMod()) {
		if text, ok := sel.Copy(n, line); ok {
			RequestTextCopy(text)
		}
	}
}

// Hit updates the selection from the mouse against shaped text on line idx.
// Call only when the hit target (row / text box) is hovered. Uses the current
// container's content rect for hit testing.
func (s *LineSelection) Hit(idx int, shaped ShapedText) {
	pos := LinePos{idx, ComputeCursorIndex(GetContentRect(), GetInputState().MousePoint, Vec2{}, shaped)}
	if GetFrameInput().Mouse == MouseClick {
		s.Selecting = true
		s.Anchor, s.Head = pos, pos
	} else if s.Selecting {
		s.Head = pos
		RequestNextFrame()
	}
}

// SelectedPrefixWidth is the advance width of glyphs with rune index < hi on
// the first shaped visual line (LTR). Used to paint inter-row selection gaps
// when lines wrap or have vertical padding.
func SelectedPrefixWidth(shaped ShapedText, hi int) f32 {
	if len(shaped.Lines) == 0 {
		return 0
	}
	var w f32
	for _, seg := range shaped.Lines[0].Segments {
		for _, g := range seg.Glyphs {
			if int(g.Cluster) < hi {
				w += g.XAdvance
			}
		}
	}
	return w
}
