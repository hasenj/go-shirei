package widgets

// The text input WIDGET SHELL: input analysis (mouse geometry, key
// decoding via editdecode.go) issues _EditCommand values; textlayout.go
// freezes the frame's rune↔pixel map and resolves geometry-dependent
// intents (Up/Down, soft-wrap Home/End) to document MoveTo; the pure
// model (editcore.go) executes them; this file syncs results back to
// the world (buffer, blink, clipboard) and renders. Editing logic does
// not belong here — keep pure model changes in editcore.go and key
// decoding in editdecode.go.

import (
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	g "go.hasen.dev/generic"
	"go.hasen.dev/shirei"

	. "go.hasen.dev/shirei"
)

// focused input state! transient by design: only one input is focused
// at a time, and it resets when focus arrives (see ReceivedFocusNow in
// TextInputExt). The editing operations themselves live on _EditState
// (editcore.go); this only carries the model's cursor/anchor between
// frames plus the caret-blink epoch.
type _TextInputState struct {
	start  time.Time
	cursor int
	anchor int // selection anchor (fixed end); == cursor when nothing is selected

	// clickStreak is the ClickCount of the last press: while a word/all
	// selection from a double/triple click is held, drag frames must not
	// collapse it (word-snap dragging is deferred polish — see plan)
	clickStreak int

	// verticalGoalX is the preferred text-coordinate column for a run of
	// consecutive Up/Down motions. It lets Down through a short line and
	// back onto a long line recover the original column.
	verticalGoalX    float32
	hasVerticalGoalX bool

	// preferPrevLineCaret: after Right/End onto a soft-wrap start, draw
	// the caret at the end of the previous visual line and resolve Home
	// against that visual line. Cleared on other caret motions. Drawing
	// also requires the caret to still sit on a soft-wrap start
	// (see textLayout.drawCaretAffinity) so a stale flag cannot desync.
	preferPrevLineCaret bool
	revealCaret         bool

	// motionArrivalSide remembers whether the caret got here via plain
	// Left or Right. Click / other motions clear it so the bidi ghost
	// preview stays quiet.
	motionArrivalSide caretMotionSide

	composition    string
	compositionSel [2]int
}

var activeInput _TextInputState

type caretAffinity byte

const (
	caretAffinityDefault caretAffinity = iota
	caretAffinityPreviousLine
)

type caretMotionSide byte

const (
	caretMotionNone caretMotionSide = iota
	caretMotionLeft
	caretMotionRight
)

// clusterBounds lists the caret-legal rune indices of shaped text:
// every glyph cluster start plus end-of-text, sorted and deduplicated
// (a cluster shaped into several glyphs — base + mark — claims its
// index once). Motion, deletion, click mapping, and caret drawing all
// share this vocabulary, which is what keeps the caret out of the
// middle of ligatures, combining sequences, and ZWJ emoji.
func clusterBounds(shaped ShapedText) []int {
	bounds := make([]int, 0, len(shaped.Runes)+1)
	for li := range shaped.Lines {
		line := &shaped.Lines[li]
		for si := range line.Segments {
			for gi := range line.Segments[si].Glyphs {
				bounds = append(bounds, int(line.Segments[si].Glyphs[gi].Cluster))
			}
		}
	}
	// Some shapers do not emit a glyph for a hard line break. Keep both
	// sides of every newline as caret stops regardless: otherwise the
	// preceding glyph and newline become one apparent cluster, and
	// Backspace from the next line deletes them together.
	for i, r := range shaped.Runes {
		if r == '\n' {
			bounds = append(bounds, i, i+1)
		}
	}
	bounds = append(bounds, len(shaped.Runes))
	slices.Sort(bounds)
	return slices.Compact(bounds)
}

func lineStarts(shaped ShapedText) []int {
	starts := make([]int, 0, max(1, len(shaped.Lines)))
	if len(shaped.Lines) == 0 {
		return []int{0}
	}

	fallbackStart := 0
	nextHardStart := func(from int) int {
		for i := max(0, from); i < len(shaped.Runes); i++ {
			if shaped.Runes[i] == '\n' {
				return i + 1
			}
		}
		return from
	}

	for li := range shaped.Lines {
		line := &shaped.Lines[li]
		firstText := -1
		for si := range line.Segments {
			for gi := range line.Segments[si].Glyphs {
				cluster := int(line.Segments[si].Glyphs[gi].Cluster)
				if cluster >= 0 && cluster < len(shaped.Runes) && shaped.Runes[cluster] == '\n' {
					continue
				}
				if firstText < 0 || cluster < firstText {
					firstText = cluster
				}
			}
		}

		start := fallbackStart
		if firstText >= 0 {
			start = firstText
		}
		start = min(max(start, 0), len(shaped.Runes))
		starts = append(starts, start)
		fallbackStart = nextHardStart(start)
	}
	return starts
}

// computeCursorPos places the caret at the nearest cluster boundary at
// or before the cursor; end of text always counts as a boundary. A
// cursor that lands inside a multi-rune cluster anyway (stale state,
// external SetCursor) draws at the cluster's start — never the old
// fall-through to end of line.
func computeCursorPos(cursor int, text ShapedText) Vec2 {
	return computeCursorPosWithAffinity(cursor, text, caretAffinityDefault)
}

func computeCursorPosWithAffinity(cursor int, text ShapedText, affinity caretAffinity) Vec2 {
	starts := lineStarts(text)
	if affinity == caretAffinityPreviousLine {
		for i, start := range starts {
			if i > 0 && cursor == start && start <= len(text.Runes) && text.Runes[start-1] != '\n' {
				return Vec2{text.Lines[i-1].Width, lineTop(i-1, text)}
			}
		}
	}

	for i, start := range starts {
		if cursor == start {
			return Vec2{0, lineTop(i, text)}
		}
	}

	if cursor >= 0 && cursor < len(text.Runes) && text.Runes[cursor] == '\n' {
		i := sort.Search(len(starts), func(i int) bool {
			return starts[i] > cursor
		}) - 1
		if i >= 0 && i < len(text.Lines) {
			return Vec2{text.Lines[i].Width, lineTop(i, text)}
		}
	}

	var x, y float32
	var endPos, bestPos Vec2
	best := -1
	for idx := range text.Lines {
		line := &text.Lines[idx]
		x = 0
		for si := range line.Segments {
			segment := &line.Segments[si]
			for gi := range segment.Glyphs {
				g := &segment.Glyphs[gi]
				if c := int(g.Cluster); c <= cursor && c > best {
					best = c
					bestPos = Vec2{x, y}
					if segment.Dir == RTL {
						// the caret sits to the right of an RTL character
						bestPos[0] += g.XAdvance
					}
				}
				x += g.XAdvance
			}
		}
		endPos = Vec2{x, y}
		if idx < len(text.Lines)-1 {
			y += line.Height
		}
	}
	if cursor >= len(text.Runes) {
		return endPos
	}
	if best >= 0 {
		return bestPos
	}
	return Vec2{}
}

func isSoftWrapStart(cursor int, shaped ShapedText) bool {
	if cursor <= 0 || cursor > len(shaped.Runes) || shaped.Runes[cursor-1] == '\n' {
		return false
	}
	for i, start := range lineStarts(shaped) {
		if i > 0 && cursor == start {
			return true
		}
	}
	return false
}

func previousLineStartForSoftWrapStart(cursor int, shaped ShapedText) (int, bool) {
	if !isSoftWrapStart(cursor, shaped) {
		return 0, false
	}
	starts := lineStarts(shaped)
	for i, start := range starts {
		if i > 0 && cursor == start {
			return starts[i-1], true
		}
	}
	return 0, false
}

func lineAtY(y float32, shaped ShapedText) (line *ShapedTextLine, lineTop float32, lineIndex int) {
	if len(shaped.Lines) == 0 {
		return nil, 0, 0
	}
	for i := range shaped.Lines {
		line = &shaped.Lines[i]
		if lineTop+line.Height > y {
			return line, lineTop, i
		}
		lineTop += line.Height
	}
	lineIndex = len(shaped.Lines) - 1
	line = &shaped.Lines[lineIndex]
	lineTop -= line.Height
	return line, lineTop, lineIndex
}

func lineTop(index int, shaped ShapedText) float32 {
	index = min(max(index, 0), len(shaped.Lines)-1)
	var y float32
	for i := 0; i < index; i++ {
		y += shaped.Lines[i].Height
	}
	return y
}

func visualLineIndexForCursor(cursor int, shaped ShapedText) int {
	starts := lineStarts(shaped)
	cursor = min(max(cursor, 0), len(shaped.Runes))
	i := sort.Search(len(starts), func(i int) bool {
		return starts[i] > cursor
	}) - 1
	return min(max(i, 0), len(shaped.Lines)-1)
}

func computeCursorIndexInText(pos Vec2, shaped ShapedText) int {
	if len(shaped.Runes) == 0 {
		return 0
	}

	// pass 1: find the line worth searching
	// it must be the first line we fine whose bottom is below the mouse cursor
	// and if we don't find any, then it's the last line!
	line, y, lineIndex := lineAtY(pos[1], shaped)
	starts := lineStarts(shaped)
	lineStart := 0
	if lineIndex < len(starts) {
		lineStart = starts[lineIndex]
	}

	// clamp to the line itself this time!
	// FIXME I think we also need to consider alignment?
	// if alignment setting pushes the line to the left side, we need to apply the offset to the cursor position to!
	g.Clamp(0, &pos[0], max(float32(0), line.Width-0.1))

	// NOTE the rules here are still wip
	// pass 2: find the glyph
	// use the half point and switch on the segment direction
	//     LTR segment -> cursor in left half of box
	//     RTL segment -> cursor in right half of box (wip)
	//
	// "after this glyph" means the NEXT CLUSTER BOUNDARY, not cluster+1:
	// for a multi-rune cluster (ligature, combining sequence, ZWJ emoji)
	// cluster+1 would be a mid-cluster index no caret should rest on.
	bounds := clusterBounds(shaped)
	after := func(g *Glyph) int {
		i := sort.SearchInts(bounds, int(g.Cluster)+1)
		if i == len(bounds) {
			return len(shaped.Runes)
		}
		return bounds[i]
	}
	var x float32
	var glyph *Glyph
	for segmentIndex := range line.Segments {
		segment := &line.Segments[segmentIndex]
		for glyphIndex := range segment.Glyphs {
			glyph = &segment.Glyphs[glyphIndex]
			if c := int(glyph.Cluster); c >= 0 && c < len(shaped.Runes) && shaped.Runes[c] == '\n' {
				continue
			}
			if x+glyph.XAdvance >= pos[0] {
				// mouse pointer is inside this glyph; let's figure out which side it is
				leftSide := x+(glyph.XAdvance/2) > pos[0]
				switch segment.Dir {
				case LTR:
					if leftSide {
						return int(glyph.Cluster)
					} else {
						return after(glyph)
					}
				case RTL:
					if leftSide {
						return after(glyph)
					} else {
						return int(glyph.Cluster)
					}
				}
			}
			centerX := x + (glyph.XAdvance / 2)
			if centerX > pos[0] && y > pos[1] {
				break
			}
			x += glyph.XAdvance
		}
	}
	return lineStart
}

// ComputeCursorIndex maps a mouse position in a content rect to a rune index
// in shaped text, accounting for scroll offset.
func ComputeCursorIndex(contentRect Rect, pos Vec2, scroll Vec2, shaped ShapedText) int {
	// for now just a linear scan
	pos = Vec2Sub(pos, contentRect.Origin)

	// "clamp" position to the edges of the box if outside so we don't worry
	// about edge cases
	g.Clamp(0, &pos[0], contentRect.Size[0])
	g.Clamp(0, &pos[1], contentRect.Size[1])

	// the box shows the text shifted left by the scroll offset; map the
	// clamped viewport point into text coordinates
	pos = Vec2Add(pos, scroll)

	return computeCursorIndexInText(pos, shaped)
}

func verticalMoveTarget(cursor int, op _EditOp, goalX float32, shaped ShapedText) int {
	if len(shaped.Lines) == 0 {
		return 0
	}
	currentLine := visualLineIndexForCursor(cursor, shaped)

	var targetLine int
	switch op {
	case _EditMoveUp:
		if currentLine == 0 {
			return 0
		}
		targetLine = currentLine - 1
	case _EditMoveDown:
		if currentLine == len(shaped.Lines)-1 {
			return len(shaped.Runes)
		}
		targetLine = currentLine + 1
	default:
		return cursor
	}

	target := Vec2{goalX, lineTop(targetLine, shaped) + shaped.Lines[targetLine].Height/2}
	return computeCursorIndexInText(target, shaped)
}

func textInputShapedText(buf string, attrs TextStyleAttrs, masked bool, maxWidth float32) ShapedText {
	if masked {
		buf = strings.Repeat("•", utf8.RuneCountInString(buf))
	}
	return ShapeTextMax(buf, attrs, maxWidth)
}

// shapedContentSize is the scrollable extent of shaped text — the widest
// line and the stacked line heights. Used to clamp the ti-scroll hook
// during the build, before layout resolves origins, so the caret float
// and ShapedTextLayout agree on the offset.
func shapedContentSize(shaped ShapedText, fallbackLineHeight float32) Vec2 {
	if len(shaped.Lines) == 0 {
		return Vec2{}
	}
	var size Vec2
	for _, line := range shaped.Lines {
		size[0] = max(size[0], line.Width)
		h := line.Height
		if h <= 0 {
			h = fallbackLineHeight
		}
		size[1] += h
	}
	return size
}

func textInputDisplayString(buf string, cursor int, composition string, masked bool) string {
	runes := []rune(buf)
	cursor = min(max(cursor, 0), len(runes))
	before := string(runes[:cursor])
	after := string(runes[cursor:])
	if masked {
		before = strings.Repeat("•", len(runes[:cursor]))
		after = strings.Repeat("•", len(runes[cursor:]))
	}
	return before + composition + after
}

func compositionCaretOffset(sel [2]int, compLen int) int {
	from, to := normalizedCompositionRange(sel, compLen)
	if from != to {
		return to
	}
	if from < 0 || from > compLen {
		return compLen
	}
	return from
}

func normalizedCompositionRange(sel [2]int, compLen int) (int, int) {
	from, to := sel[0], sel[1]
	from = min(max(from, 0), compLen)
	to = min(max(to, 0), compLen)
	if from > to {
		from, to = to, from
	}
	return from, to
}

func textSpanRects(shaped ShapedText, from int, to int, height float32) []Rect {
	from = min(max(from, 0), len(shaped.Runes))
	to = min(max(to, 0), len(shaped.Runes))
	if from > to {
		from, to = to, from
	}
	if from == to || len(shaped.Lines) == 0 {
		return nil
	}

	starts := lineStarts(shaped)
	var rects []Rect
	for i := range shaped.Lines {
		lineStart := 0
		if i < len(starts) {
			lineStart = starts[i]
		}
		lineEnd := len(shaped.Runes)
		if i+1 < len(starts) {
			lineEnd = starts[i+1]
		}

		segFrom := max(from, lineStart)
		segTo := min(to, lineEnd)
		if segFrom >= segTo {
			continue
		}

		startX := computeCursorPos(segFrom, shaped)[0]
		var endX float32
		if i+1 < len(starts) && segTo == lineEnd {
			endX = shaped.Lines[i].Width
		} else {
			endX = computeCursorPos(segTo, shaped)[0]
		}
		if endX < startX {
			startX, endX = endX, startX
		}
		if endX == startX {
			endX = startX + 1
		}
		rects = append(rects, Rect{
			Origin: Vec2{startX, lineTop(i, shaped)},
			Size:   Vec2{endX - startX, height},
		})
	}
	return rects
}

// drawTextInputUnderline paints the IME preedit (or selected-clause) underline
// under display indices [from, to). Uses per-glyph boxes, not caret-to-caret
// spans: at an LTR→RTL boundary (e.g. Japanese composition before Arabic)
// caret-to-caret geometry bridges across the RTL run and underlines text that
// is not part of the composition.
func drawTextInputUnderline(shaped ShapedText, textSize float32, scroll Vec2, from int, to int, height float32) {
	for _, r := range mergeAdjacentRects(glyphBoxesForClusters(shaped, from, to, height)) {
		pos := Vec2{r.Origin[0] - scroll[0], r.Origin[1] + textSize + 1 - scroll[1]}
		Element(Attrs(NoAnimate, FloatVec(pos), FixSize(r.Size[0], height), Background(0, 0, 30, 1)))
	}
}

// mergeAdjacentRects coalesces left-to-right neighbor boxes on the same line
// so a multi-glyph preedit still draws as one continuous underline.
func mergeAdjacentRects(rects []Rect) []Rect {
	if len(rects) == 0 {
		return nil
	}
	out := make([]Rect, 0, len(rects))
	cur := rects[0]
	for _, r := range rects[1:] {
		sameLine := r.Origin[1] == cur.Origin[1]
		// allow a half-pixel gap from rounding; require left-to-right adjacency
		touches := r.Origin[0] <= cur.Origin[0]+cur.Size[0]+0.5
		if sameLine && touches {
			end := max(cur.Origin[0]+cur.Size[0], r.Origin[0]+r.Size[0])
			if r.Origin[0] < cur.Origin[0] {
				cur.Origin[0] = r.Origin[0]
			}
			cur.Size[0] = end - cur.Origin[0]
			if r.Size[1] > cur.Size[1] {
				cur.Size[1] = r.Size[1]
			}
			continue
		}
		out = append(out, cur)
		cur = r
	}
	out = append(out, cur)
	return out
}

type glyphBoxDir struct {
	Rect
	Dir     Direction
	Cluster int
}

// glyphBoxesForClusters returns the on-screen boxes of glyphs whose
// cluster falls in [from, to). Unlike textSpanRects (caret-to-caret),
// this follows visual glyph order — so at LTR/RTL boundaries it paints
// the character being stepped over, not a bridge across the run.
func glyphBoxesForClusters(shaped ShapedText, from, to int, height float32) []Rect {
	from = min(max(from, 0), len(shaped.Runes))
	to = min(max(to, 0), len(shaped.Runes))
	if from >= to || len(shaped.Lines) == 0 {
		return nil
	}
	var rects []Rect
	var y float32
	for li := range shaped.Lines {
		line := &shaped.Lines[li]
		h := height
		if h <= 0 {
			h = line.Height
		}
		var x float32
		for si := range line.Segments {
			seg := &line.Segments[si]
			for gi := range seg.Glyphs {
				g := &seg.Glyphs[gi]
				c := int(g.Cluster)
				if c >= from && c < to {
					rects = append(rects, Rect{
						Origin: Vec2{x, y},
						Size:   Vec2{g.XAdvance, h},
					})
				}
				x += g.XAdvance
			}
		}
		y += line.Height
	}
	return rects
}

func lineGlyphBoxes(shaped ShapedText, line *ShapedTextLine, lineY, height float32) []glyphBoxDir {
	h := height
	if h <= 0 {
		h = line.Height
	}
	var boxes []glyphBoxDir
	var x float32
	for si := range line.Segments {
		seg := &line.Segments[si]
		for gi := range seg.Glyphs {
			g := &seg.Glyphs[gi]
			boxes = append(boxes, glyphBoxDir{
				Rect:    Rect{Origin: Vec2{x, lineY}, Size: Vec2{g.XAdvance, h}},
				Dir:     seg.Dir,
				Cluster: int(g.Cluster),
			})
			x += g.XAdvance
		}
	}
	return boxes
}

func isSpaceCluster(shaped ShapedText, cluster int) bool {
	if cluster < 0 || cluster >= len(shaped.Runes) {
		return true
	}
	r := shaped.Runes[cluster]
	return r == ' ' || r == '\t' || r == '\n' || r == '\u00a0'
}

// visualStrongNeighborsAtCaret finds the nearest non-space glyphs left
// and right of the caret (by glyph center), so a space between "hey" and
// Arabic still counts as an LTR↔RTL boundary. ok is false at line ends.
func visualStrongNeighborsAtCaret(shaped ShapedText, caret Vec2, lineHeight float32) (left, right glyphBoxDir, ok bool) {
	if len(shaped.Lines) == 0 {
		return glyphBoxDir{}, glyphBoxDir{}, false
	}
	line, lineY, _ := lineAtY(caret[1], shaped)
	if line == nil {
		return glyphBoxDir{}, glyphBoxDir{}, false
	}
	boxes := lineGlyphBoxes(shaped, line, lineY, lineHeight)
	if len(boxes) == 0 {
		return glyphBoxDir{}, glyphBoxDir{}, false
	}

	caretX := caret[0]
	leftI, rightI := -1, -1
	for i, b := range boxes {
		mid := b.Origin[0] + b.Size[0]/2
		if mid < caretX {
			leftI = i
		}
		if mid >= caretX && rightI < 0 {
			rightI = i
		}
	}

	strongLeft := -1
	for i := leftI; i >= 0; i-- {
		if !isSpaceCluster(shaped, boxes[i].Cluster) {
			strongLeft = i
			break
		}
	}
	strongRight := -1
	for i := rightI; i >= 0 && i < len(boxes); i++ {
		if !isSpaceCluster(shaped, boxes[i].Cluster) {
			strongRight = i
			break
		}
	}
	if strongLeft < 0 || strongRight < 0 {
		return glyphBoxDir{}, glyphBoxDir{}, false
	}
	return boxes[strongLeft], boxes[strongRight], true
}

// caretAtDirBoundary is true when the nearest non-space glyphs flanking
// the caret belong to different segment directions (LTR vs RTL).
func caretAtDirBoundary(shaped ShapedText, caret Vec2, lineHeight float32) bool {
	left, right, ok := visualStrongNeighborsAtCaret(shaped, caret, lineHeight)
	if !ok {
		return false
	}
	return left.Dir != right.Dir
}

func drawTextInputGlyphHighlight(shaped ShapedText, scroll Vec2, from, to int, height float32, color Vec4) {
	for _, r := range glyphBoxesForClusters(shaped, from, to, height) {
		pos := Vec2{r.Origin[0] - scroll[0], r.Origin[1] - scroll[1]}
		Element(Attrs(NoAnimate, FloatVec(pos), FixSize(r.Size[0], r.Size[1]), BackgroundVec(color)))
	}
}

func drawGhostCaret(pos Vec2, scroll Vec2, height float32, color Vec4) {
	p := Vec2{pos[0] - scroll[0], pos[1] - scroll[1]}
	Element(Attrs(NoAnimate, FloatVec(p), FixSize(1, height), BackgroundVec(color)))
}

// drawCaretMotionPreview shows where the arrival-side arrow would go
// next across an LTR↔RTL edge. Ghost caret matches the real caret
// (same blink phase, ~0.2 alpha); the stepped-over glyph gets a faint
// selection tint (Shift+arrow preview).
//
// Show when the caret is already on a dir boundary OR the next stop in
// the arrival direction would land on one — otherwise the Right that
// exits an RTL run (from one stop inside) never gets a warning.
func drawCaretMotionPreview(tl textLayout, cursor int, scroll Vec2, lineHeight float32, affinity caretAffinity, side caretMotionSide, caretAlpha float32) {
	if side == caretMotionNone || caretAlpha <= 0 {
		return
	}
	cursor = min(max(cursor, 0), len(tl.shaped.Runes))
	es := _EditState{Bounds: tl.bounds}
	prev := max(0, es.prevStop(cursor))
	next := min(es.nextStop(cursor), len(tl.shaped.Runes))

	var dest int
	var from, to int
	switch side {
	case caretMotionLeft:
		if prev >= cursor {
			return
		}
		dest, from, to = prev, prev, cursor
	case caretMotionRight:
		if cursor >= next {
			return
		}
		dest, from, to = next, cursor, next
	default:
		return
	}

	caret := tl.DocCaretPos(cursor, affinity)
	destPos := tl.DocCaretPos(dest, caretAffinityDefault)
	if !caretAtDirBoundary(tl.shaped, caret, lineHeight) &&
		!caretAtDirBoundary(tl.shaped, destPos, lineHeight) {
		return
	}

	ghost := Vec4{0, 0, 30, caretAlpha * 0.2}
	sel := SelectionColor
	sel[3] = 0.2
	drawGhostCaret(destPos, scroll, lineHeight, ghost)
	drawTextInputGlyphHighlight(tl.shaped, scroll, from, to, lineHeight, sel)
}

// EditorSetCursor moves the caret to cursor (collapsing any selection) in the
// text input identified by editorId, but only while that input has focus.
func EditorSetCursor(editorId ContainerId, cursor int) {
	if IdHasFocus(editorId) {
		activeInput.cursor = cursor
		activeInput.anchor = cursor
		activeInput.preferPrevLineCaret = false
		activeInput.motionArrivalSide = caretMotionNone
		activeInput.revealCaret = true
	}
}

// EditorSetSelection selects the rune range from anchor to cursor in the text
// input identified by editorId, but only while that input has focus. Both
// positions must be valid rune offsets in the input's current buffer.
func EditorSetSelection(editorId ContainerId, anchor, cursor int) {
	if IdHasFocus(editorId) {
		activeInput.anchor = anchor
		activeInput.cursor = cursor
		activeInput.preferPrevLineCaret = false
		activeInput.motionArrivalSide = caretMotionNone
		activeInput.revealCaret = true
	}
}

// TextInput renders a single-line text field bound to buf, reading and writing
// it in place.
func TextInput(buf *string) {
	TextInputExt(buf, DefaultTextInputAttrs())
}

// TextArea renders a multi-line, wrapping text field bound to buf.
func TextArea(buf *string) {
	TextInputExt(buf, DefaultMultilineTextInputAttrs())
}

// PasswordInput renders a single-line field bound to buf that masks its content.
func PasswordInput(buf *string) {
	attrs := DefaultTextInputAttrs()
	attrs.Masked = true
	TextInputExt(buf, attrs)
}

// CtrlTextInput renders a compact single-line field sized to sit with
// CtrlButton (smaller type and padding). For filters, find bars, and other
// unobtrusive controls — not a primary form field. To tweak attrs, call
// TextInputExt with CtrlTextInputAttrs() and override fields.
func CtrlTextInput(buf *string) {
	TextInputExt(buf, CtrlTextInputAttrs())
}

// TextInputAttrs configures TextInputExt default chrome + editing. For custom
// field chrome use ProcessTextInput + DrawTextInputPlain with TextInputConfig.
type TextInputAttrs struct {
	FontSize float32 // text size; also drives default padding and row height
	Padding  Vec4    // inner padding around the text
	MinWidth float32 // minimum box width; 0 uses a default (about 10em)
	// MaxWidth caps the box width; 0 means unconstrained (subject to Fill).
	MaxWidth float32

	// FixedWidth pins the box to MinWidth (or the 10em default) and disables
	// Grow/ExpandAcross. When false (default), the field fills leftover
	// main-axis space in a row (and expands across in a column) so a compose
	// bar of [TextInput][Send] needs no manual MinWidth. Height stays capped
	// to the row count so Grow does not stretch a single-line field taller.
	FixedWidth bool

	Masked bool // render each character as a bullet (password fields)
	Wrap   bool // wrap long lines instead of scrolling horizontally

	// MaxLines caps logical lines. 0 means unlimited; 1 is the default
	// single-line field.
	MaxLines int

	// Rows controls the visible height in line heights. 0 picks a
	// default from MaxLines: one row for single-line fields, up to four
	// rows otherwise.
	Rows int

	// NoAutoFocus opts out of grabbing focus when the input first renders —
	// for inputs that sit permanently in a panel rather than appearing in
	// response to a user action.
	NoAutoFocus bool

	// NoUpDownLineEdges frees the Up/Down keys for the app (e.g.
	// for host-owned Up/Down (e.g. suggestion lists); by default they jump the caret
	// to the line edges, the single-line convention.
	NoUpDownLineEdges bool

	// Placeholder is shown, dimmed, while the buffer is empty. Draw-only:
	// it never enters the buffer, hit-testing, or the clipboard.
	Placeholder string

	// Depth scales the stock top-inset shadow (chrome only — not used by
	// ProcessTextInput). 1 is the usual subtle whisper (set by
	// DefaultTextInputAttrs). 0 is flat (no inset). Larger values deepen the
	// inset for a more control-like field without a separate Ctrl flag.
	Depth float32

	// Accent is reserved for default-chrome accent color. Current stock chrome
	// does not use it (focus darkens the border alpha). Not used by
	// ProcessTextInput.
	Accent Vec4
}

// TextInputConfig is the edit/layout/paint configuration for ProcessTextInput
// and DrawTextInputPlain. It is chrome-free: the caller owns the field
// container's look. Padding on the field container must match cfg.Padding
// (v1 contract: field padding = text geometry padding).
type TextInputConfig struct {
	FontSize   float32
	Padding    Vec4
	MinWidth   float32
	MaxWidth   float32
	FixedWidth bool

	Masked   bool
	Wrap     bool
	MaxLines int
	Rows     int

	NoAutoFocus       bool
	NoUpDownLineEdges bool

	// Placeholder is shown, dimmed, while the buffer is empty (and no IME
	// composition is active). Never masked — a password field's placeholder
	// reads as plain text.
	Placeholder string

	// Plain-draw colors; zero uses defaults (black text, package selection,
	// dark caret).
	TextColor      Vec4
	SelectionColor Vec4
	CaretColor     Vec4

	// PlaceholderColor tints the placeholder; zero derives it from TextColor
	// at a reduced alpha.
	PlaceholderColor Vec4
}

// TextInputConfigFromAttrs maps default attrs to a config (drops Accent).
func TextInputConfigFromAttrs(attrs TextInputAttrs) TextInputConfig {
	return TextInputConfig{
		FontSize:          attrs.FontSize,
		Padding:           attrs.Padding,
		MinWidth:          attrs.MinWidth,
		MaxWidth:          attrs.MaxWidth,
		FixedWidth:        attrs.FixedWidth,
		Masked:            attrs.Masked,
		Wrap:              attrs.Wrap,
		MaxLines:          attrs.MaxLines,
		Rows:              attrs.Rows,
		NoAutoFocus:       attrs.NoAutoFocus,
		NoUpDownLineEdges: attrs.NoUpDownLineEdges,
		Placeholder:       attrs.Placeholder,
	}
}

func (c TextInputConfig) withDefaults() TextInputConfig {
	if c.FontSize == 0 {
		c.FontSize = DefaultTextSize
	}
	if c.Padding == (Vec4{}) {
		c.Padding = N4(c.FontSize / 2)
	}
	if c.TextColor == (Vec4{}) {
		c.TextColor = Vec4{0, 0, 0, 1}
	}
	if c.CaretColor == (Vec4{}) {
		c.CaretColor = Vec4{0, 0, 30, 1}
	}
	return c
}

// withComfort applies Host.ComfortScale to font size and padding after
// withDefaults. Safe to call once per frame from ProcessTextInput / sizing
// (always start from unscaled attrs; do not feed the result back into this).
func (c TextInputConfig) withComfort() TextInputConfig {
	c = c.withDefaults()
	s := ComfortScale()
	c.FontSize *= s
	c.Padding[0] *= s
	c.Padding[1] *= s
	c.Padding[2] *= s
	c.Padding[3] *= s
	return c
}

// textInputPlaceholderColor resolves the placeholder ink: explicit
// PlaceholderColor when set, else TextColor at a dimmed alpha (the
// ::placeholder convention of a faded text color).
func textInputPlaceholderColor(cfg TextInputConfig) Vec4 {
	if cfg.PlaceholderColor != (Vec4{}) {
		return cfg.PlaceholderColor
	}
	c := cfg.TextColor
	c[3] *= 0.4
	return c
}

// TextInputState is the data-centric snapshot from ProcessTextInput for
// custom chrome and DrawTextInputPlain. Unexported fields are draw payload
// for the plain paint helpers in this package.
type TextInputState struct {
	HasFocus, ReceivedFocus bool
	Hovered, Active         bool
	Composing               bool
	Local                   Vec2

	Cursor, Anchor int
	SelFrom, SelTo int

	Scroll     Vec2
	BlinkAlpha float32
	FocusedAt  time.Time

	ContentSize Vec2
	CaretPos    Vec2 // text-space caret (before outer pad / scroll)

	CompositionFrom, CompositionTo       int
	CompositionSelFrom, CompositionSelTo int

	// draw payload (same package)
	tl          textLayout
	availW      float32
	availH      float32
	lineHeight  float32
	textAttrs   TextStyleAttrs
	dragging    bool
	wrap        bool
	cfg         TextInputConfig
	contentRect Rect
	// FieldSize is the outer field's resolved size (for chrome floats).
	FieldSize Vec2
}

// DefaultTextInputAttrs returns the attributes for a standard single-line text
// field: one line, default font size and padding. The field fills leftover
// parent space unless FixedWidth is set later.
func DefaultTextInputAttrs() (out TextInputAttrs) {
	out.FontSize = DefaultTextSize
	out.Padding = N4(out.FontSize / 2)
	out.MaxLines = 1
	out.Depth = 1
	return out
}

// CtrlTextInputAttrs returns attributes for a compact single-line field meant
// to sit with CtrlButton: ButtonCtrlSize type, PadScale 0.8 padding, lighter
// inset (Depth 0.5). Total height matches a default CtrlButton face.
func CtrlTextInputAttrs() (out TextInputAttrs) {
	out = DefaultTextInputAttrs()
	out.FontSize = ButtonCtrlSize
	out.Padding = N4(out.FontSize / 2 * 0.8)
	out.Depth = 0.5
	return out
}

// DefaultMultilineTextInputAttrs returns the attributes for a multi-line,
// wrapping text field showing several rows by default.
func DefaultMultilineTextInputAttrs() (out TextInputAttrs) {
	out = DefaultTextInputAttrs()
	out.Wrap = true
	out.MaxLines = 0
	out.Rows = 4
	return out
}

func textInputRows(attrs TextInputAttrs) int {
	return textInputConfigRows(TextInputConfigFromAttrs(attrs))
}

func textInputConfigRows(cfg TextInputConfig) int {
	if cfg.Rows > 0 {
		return cfg.Rows
	}
	if cfg.MaxLines == 0 {
		return 4
	}
	return max(1, min(cfg.MaxLines, 4))
}

func enforceMaxLines(e _EditState, text string, maxLines int) string {
	if maxLines <= 1 {
		return text
	}
	from, to := e.SelRange()
	newlines := 0
	for i, r := range e.Runes {
		if i >= from && i < to {
			continue
		}
		if r == '\n' {
			newlines++
		}
	}

	allowed := maxLines - 1 - newlines
	if allowed < 0 {
		allowed = 0
	}
	if allowed == 0 && text == "\n" {
		return ""
	}

	var b strings.Builder
	b.Grow(len(text))
	for _, r := range text {
		if r == '\n' {
			if allowed > 0 {
				b.WriteRune('\n')
				allowed--
			} else {
				b.WriteRune(' ')
			}
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// TextInputExt renders a default-chrome text field bound to buf. Custom skins
// should open their own field container and call ProcessTextInput +
// DrawTextInputPlain instead.
func TextInputExt(buf *string, attrs TextInputAttrs) {
	// Unscaled attrs for ProcessTextInput (withComfort inside once).
	// Sized chrome uses a comfort copy so the outer box matches the field.
	raw := TextInputConfigFromAttrs(attrs)
	cfg := raw.withComfort()

	// Default field chrome: sizing, fill, border, and background.
	padSize := PadSize(cfg.Padding)
	lineHeight := cfg.FontSize
	rows := textInputConfigRows(cfg)
	minW := padSize[0] + cfg.FontSize*10
	if cfg.MinWidth > 0 {
		minW = cfg.MinWidth
	}
	boxH := float32(rows)*lineHeight + padSize[1]
	maxW := cfg.MaxWidth
	if cfg.FixedWidth {
		maxW = minW
	}
	minSize := Vec2{minW, boxH}
	parent := GetAttrs()
	// Stock chrome: white face, 1px border that darkens on focus,
	// optional top inset scaled by Depth.
	Container(Attrs(
		Focusable,
		Corners(4),
		Background(0, 0, 100, 1),
		PadVec(cfg.Padding),
		MinSizeVec(minSize),
		MaxSizeVec(Vec2{maxW, boxH}),
		Clip,
		BorderWidth(1),
		// Idle border a bit stronger than a pure whisper so the silhouette
		// holds next to filled controls (e.g. CtrlButton) on light panels.
		BorderColor(0, 0, 0, 0.16),
	), func() {
		if !cfg.FixedWidth && cfg.MaxWidth == 0 {
			ModAttrs(Expand)
			if parent.Row {
				ModAttrs(Grow(1))
			}
		}
		st := ProcessTextInput(buf, raw)
		if st.HasFocus {
			ModAttrs(BorderColor(0, 0, 0, 0.30))
		} else {
			ModAttrs(BorderColor(0, 0, 0, 0.16))
		}
		size := st.FieldSize
		if size == (Vec2{}) {
			size = minSize
		}
		// Soft top inset (floats don't scroll with text content).
		// Depth comes from attrs as-is: 0 = flat, 1 = default whisper.
		if attrs.Depth > 0 {
			topH := float32(6) * attrs.Depth
			topA := float32(0.04) * attrs.Depth
			if topH > 16 {
				topH = 16
			}
			if topA > 0.14 {
				topA = 0.14
			}
			Element(Attrs(NoAnimate, ClickThrough, Float(0, 0), FixSize(size[0], topH),
				Background(0, 0, 0, topA), Grad(0, 0, 0, -topA)))
		}
		DrawTextInputPlain(st, cfg)
	})
}

// ProcessTextInput runs the text-field edit path on the current container:
// focus, hooks, layout, mouse/keys, Apply, clipboard, external-buf clamp.
// It creates no child elements so the caller may still ModAttrs for chrome.
//
// Contract: call inside the focusable field container; that container's
// padding is the text geometry padding.
func ProcessTextInput(buf *string, cfg TextInputConfig) TextInputState {
	// Always pass unscaled config in; withComfort fills defaults then multiplies.
	cfg = cfg.withComfort()
	var st TextInputState
	st.cfg = cfg
	st.lineHeight = cfg.FontSize

	padSize := PadSize(cfg.Padding)
	size := GetResolvedSize()
	if size == (Vec2{}) {
		rows := textInputConfigRows(cfg)
		minW := padSize[0] + cfg.FontSize*10
		if cfg.MinWidth > 0 {
			minW = cfg.MinWidth
		}
		size = Vec2{minW, float32(rows)*cfg.FontSize + padSize[1]}
	}
	st.FieldSize = size
	st.availW = max(float32(0), size[0]-padSize[0])
	st.availH = max(float32(0), size[1]-padSize[1])

	var shapeMaxW float32
	if cfg.Wrap {
		shapeMaxW = st.availW
	}

	st.textAttrs = DefaultTextStyle()
	st.textAttrs.FontSize = cfg.FontSize
	st.textAttrs.TextColor = cfg.TextColor
	if cfg.SelectionColor != (Vec4{}) {
		// ShapedTextLayout uses global SelectionColor; per-field override is
		// deferred. cfg.SelectionColor reserved for a later Draw path.
		_ = cfg.SelectionColor
	}

	var bufferEditedThisFrame bool

	if !cfg.NoAutoFocus {
		AutoFocus()
	}
	FocusOnClick()
	CycleFocusOnTab()

	// Capture for drag-select; PressAction sets active on this container.
	st.Hovered = IsHovered()
	origin := GetScreenRect().Origin
	st.Local = Vec2Sub(GetInputState().MousePoint, origin)
	PressAction()
	st.Active = IsActive()

	st.ReceivedFocus = ReceivedFocusNow()
	if st.ReceivedFocus {
		g.Reset(&activeInput)
		activeInput.start = time.Now()
		activeInput.revealCaret = true
	}

	var mouseShift = slices.Contains(GetInputState().DownKeys, KeyShift)
	contentRect := GetContentRect()
	st.contentRect = contentRect

	scroll := Use[Vec2]("ti-scroll")
	hist := Use[_EditHistory]("ti-history")

	st.HasFocus = HasFocus()
	st.Composing = st.HasFocus && GetInputState().Composition != ""
	wasComposing := activeInput.composition != ""
	compositionChanged := st.HasFocus &&
		(GetInputState().Composition != activeInput.composition ||
			GetInputState().CompositionSel != activeInput.compositionSel)

	composition := ""
	var compositionSel [2]int
	if st.Composing {
		composition = GetInputState().Composition
		compositionSel = GetInputState().CompositionSel
	}

	rebuildLayout := func() textLayout {
		return makeTextLayout(*buf, activeInput.cursor, composition, compositionSel, st.textAttrs, cfg.Masked, shapeMaxW)
	}
	tl := rebuildLayout()

	if st.Composing && !wasComposing && activeInput.cursor != activeInput.anchor {
		es := _EditState{
			Runes:      []rune(*buf),
			Cursor:     activeInput.cursor,
			Anchor:     activeInput.anchor,
			Bounds:     tl.bounds,
			LineStarts: tl.lineStarts,
		}
		pre := snapshotOf(&es)
		r := es.Apply(_EditCommand{Op: _EditDeleteSelection})
		if r.Edited {
			hist.Record(_EditCommand{Op: _EditDeleteSelection}, pre)
			*buf = string(es.Runes)
			bufferEditedThisFrame = true
			activeInput.cursor = es.Cursor
			activeInput.anchor = es.Anchor
			activeInput.preferPrevLineCaret = false
			activeInput.motionArrivalSide = caretMotionNone
			activeInput.start = time.Now()
			activeInput.revealCaret = true
			tl = rebuildLayout()
		}
	}

	if compositionChanged {
		activeInput.revealCaret = true
	}

	var cmds []_EditCommand
	var dragging bool
	if IsClicked() {
		pos := tl.DisplayToDoc(tl.IndexAt(contentRect, GetInputState().MousePoint, *scroll))
		switch {
		case GetFrameInput().ClickCount >= 3:
			cmds = append(cmds, _EditCommand{Op: _EditSelectAll})
		case GetFrameInput().ClickCount == 2:
			cmds = append(cmds, _EditCommand{Op: _EditSelectWord, Pos: pos})
		default:
			cmds = append(cmds, _EditCommand{Op: _EditMoveTo, Pos: pos, Extend: mouseShift && !ReceivedFocusNow()})
		}
		activeInput.clickStreak = GetFrameInput().ClickCount
		activeInput.motionArrivalSide = caretMotionNone
	} else if IsActive() && activeInput.clickStreak <= 1 {
		dragging = true
		pos := tl.DisplayToDoc(tl.IndexAt(contentRect, GetInputState().MousePoint, *scroll))
		cmds = append(cmds, _EditCommand{Op: _EditMoveTo, Pos: pos, Extend: true})
		activeInput.motionArrivalSide = caretMotionNone
	}

	if st.HasFocus {
		WantKeyboard()

		if GetFrameInput().Key != KeyCodeNone || GetFrameInput().Text != "" {
			opts := editKeyOpts{
				UpDownLineEdges: cfg.MaxLines == 1 && !cfg.NoUpDownLineEdges,
				VerticalMotion:  cfg.MaxLines != 1,
				Newlines:        cfg.MaxLines != 1,
			}
			cmds = append(cmds, decodeEditKeys(GetFrameInput().Key, GetInputState().Modifiers, GetFrameInput().Text, editPrimaryMod(), opts)...)
		}

		if len(cmds) > 0 {
			es := _EditState{
				Runes:      []rune(*buf),
				Cursor:     activeInput.cursor,
				Anchor:     activeInput.anchor,
				Bounds:     tl.bounds,
				LineStarts: tl.lineStarts,
			}
			var caret bool
			for _, cmd := range cmds {
				if cfg.Masked && (cmd.Op == _EditCopy || cmd.Op == _EditCut) {
					continue
				}
				cmdEdited := false
				switch cmd.Op {
				case _EditUndo:
					if hist.Undo(&es) {
						cmdEdited, caret = true, true
						activeInput.preferPrevLineCaret = false
						activeInput.hasVerticalGoalX = false
						activeInput.motionArrivalSide = caretMotionNone
					}
				case _EditRedo:
					if hist.Redo(&es) {
						cmdEdited, caret = true, true
						activeInput.preferPrevLineCaret = false
						activeInput.hasVerticalGoalX = false
						activeInput.motionArrivalSide = caretMotionNone
					}
				default:
					if cmd.Op == _EditInsert && cfg.MaxLines > 1 {
						cmd.Text = enforceMaxLines(es, cmd.Text, cfg.MaxLines)
						if cmd.Text == "" {
							continue
						}
					}
					sourceOp := cmd.Op
					cmd, activeInput.verticalGoalX, activeInput.hasVerticalGoalX = resolveEditCommand(
						cmd, es.Cursor, tl, activeInput.preferPrevLineCaret,
						activeInput.verticalGoalX, activeInput.hasVerticalGoalX,
					)
					pre := snapshotOf(&es)
					preCursor := es.Cursor
					r := es.Apply(cmd)
					if r.Edited {
						hist.Record(cmd, pre)
					} else if r.Caret {
						hist.BreakRun()
					}
					if r.Caret {
						activeInput.preferPrevLineCaret = false
						switch sourceOp {
						case _EditMoveLeft:
							activeInput.motionArrivalSide = caretMotionLeft
						case _EditMoveRight:
							activeInput.motionArrivalSide = caretMotionRight
							if es.Cursor >= preCursor && tl.IsSoftWrapStart(es.Cursor) {
								activeInput.preferPrevLineCaret = true
							}
						case _EditMoveLineEnd:
							activeInput.motionArrivalSide = caretMotionNone
							if es.Cursor >= preCursor && tl.IsSoftWrapStart(es.Cursor) {
								activeInput.preferPrevLineCaret = true
							}
						default:
							activeInput.motionArrivalSide = caretMotionNone
						}
					}
					cmdEdited = r.Edited
					caret = caret || r.Caret
					if r.Copy != "" {
						shirei.RequestTextCopy(r.Copy)
					}
					if r.Paste {
						shirei.RequestPaste()
					}
				}
				if cmdEdited {
					*buf = string(es.Runes)
					bufferEditedThisFrame = true
					activeInput.cursor = es.Cursor
					activeInput.anchor = es.Anchor
					tl = rebuildLayout()
					es.Bounds = tl.bounds
					es.LineStarts = tl.lineStarts
				}
			}
			if bufferEditedThisFrame {
				*buf = string(es.Runes)
			}
			activeInput.cursor = es.Cursor
			activeInput.anchor = es.Anchor
			if caret {
				activeInput.start = time.Now()
				activeInput.revealCaret = true
			}
			tl = rebuildLayout()
		}

		st.SelFrom = activeInput.anchor
		st.SelTo = activeInput.cursor
		if st.SelFrom > st.SelTo {
			st.SelFrom, st.SelTo = st.SelTo, st.SelFrom
		}
		if st.Composing {
			st.SelFrom, st.SelTo = 0, 0
		}
	}

	lastBuf := Use[string]("ti-last-buf")
	if *buf != *lastBuf {
		if !bufferEditedThisFrame && *lastBuf != "" {
			n := utf8.RuneCountInString(*buf)
			activeInput.cursor = min(max(activeInput.cursor, 0), n)
			activeInput.anchor = min(max(activeInput.anchor, 0), n)
			activeInput.motionArrivalSide = caretMotionNone
			activeInput.revealCaret = true
			tl = rebuildLayout()
		}
		*lastBuf = *buf
	}

	st.Cursor = activeInput.cursor
	st.Anchor = activeInput.anchor
	st.Scroll = *scroll
	st.FocusedAt = activeInput.start
	st.dragging = dragging
	st.wrap = cfg.Wrap
	st.tl = tl
	st.ContentSize = tl.ContentSize(cfg.FontSize)
	aff := caretAffinityDefault
	if !st.Composing {
		aff = tl.drawCaretAffinity(activeInput.cursor, activeInput.preferPrevLineCaret)
	}
	st.CaretPos = tl.CaretPos(tl.displayCursor, aff)
	st.CompositionFrom = tl.compositionFrom
	st.CompositionTo = tl.compositionTo
	st.CompositionSelFrom = tl.compositionSelFrom
	st.CompositionSelTo = tl.compositionSelTo

	// Blink alpha for chrome that wants it; DrawTextInputCaret recomputes too.
	st.BlinkAlpha = 1
	const caretBlinkTimeout = 5 * time.Second
	if st.HasFocus && !shirei.GetHost().HeadlessRender && time.Since(activeInput.start) < caretBlinkTimeout {
		if int(time.Since(activeInput.start)/(time.Millisecond*600))%2 == 1 {
			st.BlinkAlpha = 0
		}
	}

	if st.HasFocus {
		activeInput.composition = GetInputState().Composition
		activeInput.compositionSel = GetInputState().CompositionSel
	}
	return st
}

// DrawTextInputPlain draws the scrollable text body and the caret.
// Call after ProcessTextInput, still inside the field container, after any
// chrome ModAttrs / decoration floats.
func DrawTextInputPlain(st TextInputState, cfg TextInputConfig) {
	DrawTextInputContent(st, cfg)
	DrawTextInputCaret(st, cfg)
}

// DrawTextInputContent draws the viewport: scroll policy, composition marks,
// bidi preview, and shaped text. Must run inside the field outer container.
func DrawTextInputContent(st TextInputState, cfg TextInputConfig) {
	cfg = cfg.withDefaults()
	tl := st.tl
	availW, availH := st.availW, st.availH
	lineHeight := st.lineHeight
	inputTextAttrs := st.textAttrs
	if inputTextAttrs.FontSize == 0 {
		inputTextAttrs = DefaultTextStyle()
		inputTextAttrs.FontSize = cfg.FontSize
		inputTextAttrs.TextColor = cfg.TextColor
	}
	contentRect := st.contentRect
	hasFocus := st.HasFocus
	composing := st.Composing
	dragging := st.dragging
	selectionFrom, selectionTo := st.SelFrom, st.SelTo

	scroll := Use[Vec2]("ti-scroll")

	// NoClip: the field outer already clips. This content box is only the
	// em-tall scroll viewport; glyph descenders intentionally paint into the
	// outer's bottom padding and must not be shaved here.
	Container(Attrs(FixSize(availW, availH), NoClip), func() {
		contentSize := tl.ContentSize(inputTextAttrs.FontSize)
		clampScroll := func() {
			g.Clamp(0, &scroll[0], max(0, contentSize[0]-availW))
			g.Clamp(0, &scroll[1], max(0, contentSize[1]-availH))
		}
		SetScrollOffset(*scroll)
		ScrollOnInput()
		*scroll = GetScrollOffset()
		clampScroll()
		if hasFocus {
			if dragging {
				if over := GetInputState().MousePoint[0] - (contentRect.Origin[0] + contentRect.Size[0]); over > 0 {
					scroll[0] += min(over, 24)
				}
				if over := contentRect.Origin[0] - GetInputState().MousePoint[0]; over > 0 {
					scroll[0] -= min(over, 24)
				}
				if over := GetInputState().MousePoint[1] - (contentRect.Origin[1] + contentRect.Size[1]); over > 0 {
					scroll[1] += min(over, 24)
				}
				if over := contentRect.Origin[1] - GetInputState().MousePoint[1]; over > 0 {
					scroll[1] -= min(over, 24)
				}
			}
			if activeInput.revealCaret {
				const margin = 2
				affinity := caretAffinityDefault
				if !composing {
					affinity = tl.drawCaretAffinity(activeInput.cursor, activeInput.preferPrevLineCaret)
				}
				caretPos := tl.CaretPos(tl.displayCursor, affinity)
				if caretPos[0]-scroll[0] > availW-margin {
					scroll[0] = caretPos[0] - availW + margin
				}
				if caretPos[0]-scroll[0] < margin {
					scroll[0] = caretPos[0] - margin
				}
				if caretPos[1]+lineHeight-scroll[1] > availH-margin {
					scroll[1] = caretPos[1] + lineHeight - availH + margin
				}
				if caretPos[1]-scroll[1] < margin {
					scroll[1] = caretPos[1] - margin
				}
			}
		} else {
			*scroll = Vec2{}
		}
		clampScroll()
		SetScrollOffset(*scroll)
		*scroll = GetScrollOffset()
		if hasFocus && activeInput.revealCaret {
			affinity := caretAffinityDefault
			if !composing {
				affinity = tl.drawCaretAffinity(activeInput.cursor, activeInput.preferPrevLineCaret)
			}
			caretPos := tl.CaretPos(tl.displayCursor, affinity)
			if caretPos[0]-scroll[0] >= 0 &&
				caretPos[0]-scroll[0] <= availW &&
				caretPos[1]-scroll[1] >= 0 &&
				caretPos[1]+lineHeight-scroll[1] <= availH {
				activeInput.revealCaret = false
			}
		}

		if composing {
			drawTextInputUnderline(tl.displayShaped, inputTextAttrs.FontSize, *scroll, tl.compositionFrom, tl.compositionTo, 1)
			if tl.compositionSelFrom != tl.compositionSelTo {
				drawTextInputUnderline(tl.displayShaped, inputTextAttrs.FontSize, *scroll, tl.compositionSelFrom, tl.compositionSelTo, 2)
			}
		}
		if hasFocus && !composing &&
			activeInput.cursor == activeInput.anchor &&
			activeInput.motionArrivalSide != caretMotionNone {
			aff := tl.drawCaretAffinity(activeInput.cursor, activeInput.preferPrevLineCaret)
			var caretAlpha float32 = 1
			if !shirei.GetHost().HeadlessRender && time.Since(activeInput.start) < 5*time.Second {
				if int(time.Since(activeInput.start)/(time.Millisecond*600))%2 == 1 {
					caretAlpha = 0
				}
			}
			drawCaretMotionPreview(tl, activeInput.cursor, *scroll, lineHeight, aff,
				activeInput.motionArrivalSide, caretAlpha)
		}
		Container(AttrSet{}, func() {
			if !st.wrap {
				ModAttrs(UnsetMaxCross)
			}
			if cfg.Placeholder != "" && len(tl.displayShaped.Runes) == 0 {
				// empty buffer (and no composition — it splices into the
				// display string, so the rune check covers it): draw the
				// placeholder dimmed; never masked, even on password fields.
				phAttrs := inputTextAttrs
				phAttrs.TextColor = textInputPlaceholderColor(cfg)
				var phMaxW float32
				if st.wrap {
					phMaxW = availW
				}
				ShapedTextLayout(ShapeTextMax(cfg.Placeholder, phAttrs, phMaxW), phAttrs, 0, 0)
			} else {
				ShapedTextLayout(tl.displayShaped, inputTextAttrs, selectionFrom, selectionTo)
			}
		})
	})
}

// DrawTextInputCaret draws the blinking caret and reports IME anchor positions
// to the host. Must run inside the field outer (caret is a float of that node).
func DrawTextInputCaret(st TextInputState, cfg TextInputConfig) {
	cfg = cfg.withDefaults()
	tl := st.tl
	scroll := Use[Vec2]("ti-scroll")
	lineHeight := st.lineHeight
	if lineHeight == 0 {
		lineHeight = cfg.FontSize
	}
	caretColor := cfg.CaretColor

	if !(st.HasFocus && shirei.GetHost().WindowFocused) {
		return
	}
	const caretBlinkTimeout = 5 * time.Second
	var alpha float32 = 1
	if !shirei.GetHost().HeadlessRender && time.Since(activeInput.start) < caretBlinkTimeout {
		RequestNextFrame()
		if int(time.Since(activeInput.start)/(time.Millisecond*600))%2 == 1 {
			alpha = 0
		}
	}
	var rd = GetRenderData()
	affinity := caretAffinityDefault
	composing := st.Composing
	if composing {
		compositionPos := tl.CaretPos(tl.compositionFrom, caretAffinityDefault)
		compositionPos[0] += rd.Padding[PAD_LEFT] - scroll[0]
		compositionPos[1] += rd.Padding[PAD_TOP] - scroll[1]
		Container(Attrs(MinSize(1, lineHeight), Background(0, 0, 30, 0), FloatVec(compositionPos)), func() {
			r := GetScreenRect()
			shirei.GetHost().CompositionPos = Vec2Add(r.Origin, Vec2{0, r.Size[1]})
		})
	} else {
		affinity = tl.drawCaretAffinity(activeInput.cursor, activeInput.preferPrevLineCaret)
	}
	var pos = tl.CaretPos(tl.displayCursor, affinity)
	pos[0] += rd.Padding[PAD_LEFT] - scroll[0]
	pos[1] += rd.Padding[PAD_TOP] - scroll[1]
	cc := caretColor
	cc[3] = alpha
	Container(Attrs(MinSize(1, lineHeight), BackgroundVec(cc), FloatVec(pos)), func() {
		r := GetScreenRect()
		shirei.GetHost().CaretPos = Vec2Add(r.Origin, Vec2{0, r.Size[1]})
		shirei.GetHost().CaretHeight = r.Size[1]
	})
}
