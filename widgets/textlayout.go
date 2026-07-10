package widgets

// textLayout is one frame's frozen map between document rune indices,
// display rune indices (document + IME composition splice), and pixels.
// Hit-testing, caret drawing, underlines, and vertical motion all go
// through it so the shell does not keep parallel shaped/display copies
// that can drift. Geometry-dependent intents (Up/Down, soft-wrap Home/End)
// are resolved to document MoveTo commands here before Apply.
// See notes/textinput-architecture.md.

import (
	"unicode/utf8"

	. "go.hasen.dev/shirei"
)

type textLayout struct {
	shaped     ShapedText
	bounds     []int
	lineStarts []int

	displayShaped ShapedText
	displayCursor int

	composing          bool
	compAt             int // document index where composition is spliced
	compLen            int
	compositionFrom    int // display indices
	compositionTo      int
	compositionSelFrom int
	compositionSelTo   int
}

// makeTextLayout shapes the committed buffer and, when composition is
// non-empty, a display string with the preedit spliced at docCursor.
func makeTextLayout(buf string, docCursor int, composition string, compositionSel [2]int, textAttrs TextAttrSet, masked bool) textLayout {
	docLen := utf8.RuneCountInString(buf)
	docCursor = min(max(docCursor, 0), docLen)

	shaped := textInputShapedText(buf, textAttrs, masked)
	tl := textLayout{
		shaped:        shaped,
		bounds:        clusterBounds(shaped),
		lineStarts:    lineStarts(shaped),
		displayShaped: shaped,
		displayCursor: docCursor,
	}
	if composition == "" {
		return tl
	}

	compLen := utf8.RuneCountInString(composition)
	caretOffset := compositionCaretOffset(compositionSel, compLen)
	selFrom, selTo := normalizedCompositionRange(compositionSel, compLen)

	tl.composing = true
	tl.compAt = docCursor
	tl.compLen = compLen
	tl.compositionFrom = docCursor
	tl.compositionTo = docCursor + compLen
	tl.compositionSelFrom = docCursor + selFrom
	tl.compositionSelTo = docCursor + selTo
	tl.displayCursor = docCursor + caretOffset
	tl.displayShaped = ShapeText(textInputDisplayString(buf, docCursor, composition, masked), textAttrs)
	return tl
}

// DocToDisplay maps a committed-buffer index into display-string space.
func (tl textLayout) DocToDisplay(i int) int {
	if !tl.composing || i <= tl.compAt {
		return i
	}
	return i + tl.compLen
}

// DisplayToDoc maps a display-string index back to the committed buffer.
// Indices inside the composition splice collapse to the splice point
// (the document caret); the model never sees preedit indices.
func (tl textLayout) DisplayToDoc(i int) int {
	if !tl.composing {
		return i
	}
	if i <= tl.compAt {
		return i
	}
	if i >= tl.compAt+tl.compLen {
		return i - tl.compLen
	}
	return tl.compAt
}

func (tl textLayout) CaretPos(displayIndex int, affinity caretAffinity) Vec2 {
	return computeCursorPosWithAffinity(displayIndex, tl.displayShaped, affinity)
}

func (tl textLayout) DocCaretPos(docIndex int, affinity caretAffinity) Vec2 {
	return computeCursorPosWithAffinity(docIndex, tl.shaped, affinity)
}

func (tl textLayout) IndexAt(contentRect Rect, mouse, scroll Vec2) int {
	return ComputeCursorIndex(contentRect, mouse, scroll, tl.displayShaped)
}

func (tl textLayout) VerticalTarget(docCursor int, op _EditOp, goalX float32) int {
	return verticalMoveTarget(docCursor, op, goalX, tl.shaped)
}

func (tl textLayout) ContentSize(fallbackLineHeight float32) Vec2 {
	return shapedContentSize(tl.displayShaped, fallbackLineHeight)
}

func (tl textLayout) IsSoftWrapStart(docIndex int) bool {
	return isSoftWrapStart(docIndex, tl.shaped)
}

func (tl textLayout) PreviousLineStartForSoftWrapStart(docIndex int) (int, bool) {
	return previousLineStartForSoftWrapStart(docIndex, tl.shaped)
}

// resolveEditCommand turns geometry-dependent intents into document
// MoveTo commands before Apply. Up/Down use the sticky column; soft-wrap
// Home/End honor preferPrevLineCaret. Other ops pass through unchanged.
// hasGoal/goalX are the vertical sticky-column run; non-vertical motions
// clear hasGoal.
func resolveEditCommand(cmd _EditCommand, cursor int, tl textLayout, preferPrev bool, goalX float32, hasGoal bool) (resolved _EditCommand, nextGoalX float32, nextHasGoal bool) {
	switch cmd.Op {
	case _EditMoveUp, _EditMoveDown:
		affinity := caretAffinityDefault
		if preferPrev {
			affinity = caretAffinityPreviousLine
		}
		if !hasGoal {
			goalX = tl.DocCaretPos(cursor, affinity)[0]
			hasGoal = true
		}
		return _EditCommand{
			Op:     _EditMoveTo,
			Pos:    tl.VerticalTarget(cursor, cmd.Op, goalX),
			Extend: cmd.Extend,
		}, goalX, true
	case _EditMoveLineStart:
		if preferPrev {
			if pos, ok := tl.PreviousLineStartForSoftWrapStart(cursor); ok {
				return _EditCommand{Op: _EditMoveTo, Pos: pos, Extend: cmd.Extend}, goalX, false
			}
		}
	case _EditMoveLineEnd:
		if preferPrev && tl.IsSoftWrapStart(cursor) {
			return _EditCommand{Op: _EditMoveTo, Pos: cursor, Extend: cmd.Extend}, goalX, false
		}
	}
	return cmd, goalX, false
}

// drawCaretAffinity is the affinity used only for caret pixels. Preferring
// the previous visual line is meaningful solely while the document caret
// sits on a soft-wrap start; elsewhere a stale preferPrev flag is ignored.
func (tl textLayout) drawCaretAffinity(docIndex int, preferPrev bool) caretAffinity {
	if preferPrev && tl.IsSoftWrapStart(docIndex) {
		return caretAffinityPreviousLine
	}
	return caretAffinityDefault
}
