package widgets

// The text input WIDGET SHELL: input analysis (mouse geometry, key
// decoding via editdecode.go) issues _EditCommand values; textlayout.go
// freezes the frame's rune↔pixel map and resolves geometry-dependent
// intents (Up/Down, soft-wrap Home/End) to document MoveTo; the pure
// model (editcore.go) executes them; this file syncs results back to
// the world (buffer, blink, clipboard) and renders. Editing logic does
// not belong here — see notes/textinput-architecture.md before adding
// features.

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

func textInputShapedText(buf string, attrs TextAttrSet, masked bool) ShapedText {
	if masked {
		buf = strings.Repeat("•", utf8.RuneCountInString(buf))
	}
	return ShapeText(buf, attrs)
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

// TextInputAttrs configures TextInputExt. DefaultTextInputAttrs and
// DefaultMultilineTextInputAttrs give the usual starting points.
type TextInputAttrs struct {
	FontSize float32 // text size; also drives default padding and row height
	Padding  Vec4    // inner padding around the text
	MinWidth float32 // minimum box width; 0 uses a default (about 10em)
	MaxWidth float32 // maximum box width; 0 means unconstrained

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

	// Accent colors the focused bottom underline. Zero value: use the
	// package-level Accent.
	Accent Vec4
}

// DefaultTextInputAttrs returns the attributes for a standard single-line text
// field: one line, default font size and padding.
func DefaultTextInputAttrs() (out TextInputAttrs) {
	out.FontSize = DefaultTextSize
	out.Padding = N4(out.FontSize / 2)
	out.MaxLines = 1
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
	if attrs.Rows > 0 {
		return attrs.Rows
	}
	if attrs.MaxLines == 0 {
		return 4
	}
	return max(1, min(attrs.MaxLines, 4))
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

// TextInputExt renders a text field bound to buf, configured by attrs. TextInput,
// TextArea, and PasswordInput are the shortcuts over it. It handles selection,
// mouse and keyboard editing, the clipboard, IME composition, scrolling, and the
// caret.
func TextInputExt(buf *string, attrs TextInputAttrs) {
	var padSize = PadSize(attrs.Padding)
	lineHeight := attrs.FontSize
	rows := textInputRows(attrs)

	// The box width comes from the attrs (or the 10em default), never
	// from the text: content wider than the box scrolls in the viewport
	// below instead of growing the box.
	minW := padSize[0] + attrs.FontSize*10
	if attrs.MinWidth > 0 {
		minW = attrs.MinWidth
	}
	var inputContainerAttrs = AttrSet{
		Focusable:  true,
		Corners:    N4(2),
		Background: Vec4{0, 0, 90, 1},
		Gradient:   Vec4{0, 0, 4, 0},
		Padding:    attrs.Padding,
		MinSize:    Vec2{minW, float32(rows)*lineHeight + padSize[1]},
		MaxSize:    Vec2{attrs.MaxWidth, float32(rows)*lineHeight + padSize[1]},
		// the box clips, not the text viewport: glyph descenders extend
		// below the em box and need the bottom padding to draw into
		Clip: true,
		Border: Border{
			BorderWidth: 1,
			BorderColor: Vec4{0, 0, 0, 0.15},
		},
	}
	var inputTextAttrs = DefaultTextAttrs()
	inputTextAttrs.Size = attrs.FontSize
	inputTextAttrs.Color = Vec4{0, 0, 0, 1}

	Container(inputContainerAttrs, func() {
		var size = GetResolvedSize()
		if size == (Vec2{}) {
			size = inputContainerAttrs.MinSize
		}
		availW := max(float32(0), size[0]-padSize[0])
		availH := max(float32(0), size[1]-padSize[1])
		if attrs.Wrap {
			inputTextAttrs.MaxWidth = availW
		}

		var selectionFrom = 0
		var selectionTo = 0
		var bufferEditedThisFrame bool

		if !attrs.NoAutoFocus {
			AutoFocus()
		}
		FocusOnClick()
		CycleFocusOnTab()

		PressAction()

		if ReceivedFocusNow() {
			g.Reset(&activeInput)
			activeInput.start = time.Now()
			activeInput.revealCaret = true
		}

		// shift via DownKeys, not Modifiers: the Modifiers flag is only
		// refreshed when a regular key is pressed, which mouse clicks are not
		var mouseShift = slices.Contains(InputState.DownKeys, KeyShift)

		contentRect := GetContentRect()

		// per-input hook state, surviving blur and focus switches. Both
		// hooks must be claimed every frame — an unclaimed hook expires
		// after one frame (hooks.go) and would silently reset.
		scroll := Use[Vec2]("ti-scroll")
		hist := Use[_EditHistory]("ti-history")

		hasFocus := HasFocus()
		composing := hasFocus && InputState.Composition != ""
		wasComposing := activeInput.composition != ""
		compositionChanged := hasFocus &&
			(InputState.Composition != activeInput.composition ||
				InputState.CompositionSel != activeInput.compositionSel)

		composition := ""
		var compositionSel [2]int
		if composing {
			composition = InputState.Composition
			compositionSel = InputState.CompositionSel
		}

		rebuildLayout := func() textLayout {
			return makeTextLayout(*buf, activeInput.cursor, composition, compositionSel, inputTextAttrs, attrs.Masked)
		}
		tl := rebuildLayout()

		if composing && !wasComposing && activeInput.cursor != activeInput.anchor {
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

		// input analysis → edit commands, in application order: mouse caret
		// placement first, then decoded keys/combos/typed text below.
		// Hit-testing uses display space; MoveTo/SelectWord get document
		// indices via DisplayToDoc so composition never feeds the model.
		var cmds []_EditCommand
		var dragging bool
		if IsClicked() {
			pos := tl.DisplayToDoc(tl.IndexAt(contentRect, InputState.MousePoint, *scroll))
			switch {
			case FrameInput.ClickCount >= 3:
				cmds = append(cmds, _EditCommand{Op: _EditSelectAll})
			case FrameInput.ClickCount == 2:
				cmds = append(cmds, _EditCommand{Op: _EditSelectWord, Pos: pos})
			default:
				cmds = append(cmds, _EditCommand{Op: _EditMoveTo, Pos: pos, Extend: mouseShift && !ReceivedFocusNow()})
			}
			activeInput.clickStreak = FrameInput.ClickCount
			activeInput.motionArrivalSide = caretMotionNone
		} else if IsActive() && activeInput.clickStreak <= 1 {
			// mouse is dragging a selection (single-click streaks only:
			// dragging must not collapse a double/triple-click selection)
			dragging = true
			pos := tl.DisplayToDoc(tl.IndexAt(contentRect, InputState.MousePoint, *scroll))
			cmds = append(cmds, _EditCommand{Op: _EditMoveTo, Pos: pos, Extend: true})
			activeInput.motionArrivalSide = caretMotionNone
		}

		if HasFocus() {
			// ModAttrs is only legal before children, so this must stay
			// ahead of the decoration elements at the end of this builder.
			// The border stays constant; the accent underline below carries
			// the focus signal.
			ModAttrs(Background(0, 0, 91, 1), Grad(0, 0, 4, 0))

			if FrameInput.Key != KeyCodeNone || FrameInput.Text != "" {
				opts := editKeyOpts{
					UpDownLineEdges: attrs.MaxLines == 1 && !attrs.NoUpDownLineEdges,
					VerticalMotion:  attrs.MaxLines != 1,
					Newlines:        attrs.MaxLines != 1,
				}
				cmds = append(cmds, decodeEditKeys(FrameInput.Key, InputState.Modifiers, FrameInput.Text, editPrimaryMod, opts)...)
			}

			// execute the frame's commands against the editing model, then
			// sync the outside world: the buffer only if it changed, the
			// blink epoch only on caret interaction, clipboard as requested.
			// Undo history is per-input hook state, recorded at this choke
			// point; _EditUndo/_EditRedo dispatch here rather than in Apply
			// because the model doesn't hold the history.
			if len(cmds) > 0 {
				es := _EditState{
					Runes:  []rune(*buf),
					Cursor: activeInput.cursor,
					Anchor: activeInput.anchor,
					// motions and deletes stop at shaped cluster boundaries
					// (masked inputs shape one bullet per rune, so their
					// bounds are per-rune — passwords edit rune-by-rune)
					Bounds:     tl.bounds,
					LineStarts: tl.lineStarts,
				}
				var caret bool
				for _, cmd := range cmds {
					if attrs.Masked && (cmd.Op == _EditCopy || cmd.Op == _EditCut) {
						// a masked input's content never goes on the clipboard
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
						if cmd.Op == _EditInsert && attrs.MaxLines > 1 {
							cmd.Text = enforceMaxLines(es, cmd.Text, attrs.MaxLines)
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
						// Bounds/LineStarts must track the buffer after any
						// mutation so later commands in this frame don't snap
						// to a stale cluster/line map.
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

			selectionFrom = activeInput.anchor
			selectionTo = activeInput.cursor
			if selectionFrom > selectionTo {
				selectionFrom, selectionTo = selectionTo, selectionFrom
			}
			if composing {
				selectionFrom, selectionTo = 0, 0
			}
		}

		// External buffer writes (path acceptance, tilde expansion,
		// host code) bypass the edit-command path; sync layout + clamp the
		// caret so a shortened buffer can't leave cursor past EOF.
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

		// top shadow! (a float in the outer container: floats don't scroll,
		// so it stays pinned to the box while the text shifts)
		Element(Attrs(NoAnimate, Float(0, 0), FixSize(size[0], 10), Background(0, 0, 70, 0.15), Grad(0, 0, 0, -0.15)))

		// bottom accent underline: neutral when idle, Accent when focused —
		// the underline carries the focus signal instead of the border.
		underline := Vec4{0, 0, 70, 1}
		if hasFocus {
			underline = AccentOrFallback(attrs.Accent, DefaultAccent)
		}
		Element(Attrs(NoAnimate, Float(0, size[1]-2), FixSize(size[0], 2), BackgroundVec(underline)))

		// text viewport: bounds and scrolls the text horizontally (the
		// CLIP is on the outer box — see inputContainerAttrs — so
		// descenders can draw into the bottom padding). The scroll policy
		// runs here, after the frame's commands, so it sees the final
		// caret position. Focus is captured outside: HasFocus() inside
		// the builder would test the viewport node, not the input's.
		Container(Attrs(FixSize(availW, availH)), func() {
			// The hook can carry an offset that predates this frame's
			// edit. Layout clamps what renders (resolveOrigins), but that
			// runs after this build — the caret (a float, placed during
			// the build) and the reveal logic would keep the stale value.
			// The visible failure: backspace at the end of an overflowing
			// line rendered the text pinned to the right edge while the
			// caret walked left, detached from where the deletions were
			// landing. Clamp against the text actually shaping this frame
			// so the hook and the rendered text always agree.
			contentSize := tl.ContentSize(inputTextAttrs.Size)
			clampScroll := func() {
				g.Clamp(0, &scroll[0], max(0, contentSize[0]-availW))
				g.Clamp(0, &scroll[1], max(0, contentSize[1]-availH))
			}
			SetScrollOffset(*scroll)
			ScrollOnInput()
			*scroll = GetScrollOffset()
			clampScroll()
			if hasFocus {
				// drag auto-scroll: selecting past the box edges pulls more
				// text into view, rate proportional to the overshoot
				if dragging {
					if over := InputState.MousePoint[0] - (contentRect.Origin[0] + contentRect.Size[0]); over > 0 {
						scroll[0] += min(over, 24)
					}
					if over := contentRect.Origin[0] - InputState.MousePoint[0]; over > 0 {
						scroll[0] -= min(over, 24)
					}
					if over := InputState.MousePoint[1] - (contentRect.Origin[1] + contentRect.Size[1]); over > 0 {
						scroll[1] += min(over, 24)
					}
					if over := contentRect.Origin[1] - InputState.MousePoint[1]; over > 0 {
						scroll[1] -= min(over, 24)
					}
				}
				// keep the caret in view
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
				// platform convention: an unfocused field shows the beginning
				*scroll = Vec2{}
			}
			// second clamp: the reveal adjustments above can overshoot the
			// scrollable range by their margin
			clampScroll()
			SetScrollOffset(*scroll)
			// core clamps against the real content size; keep the hook (and
			// the caret math below) in agreement with what actually renders
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
				drawTextInputUnderline(tl.displayShaped, inputTextAttrs.Size, *scroll, tl.compositionFrom, tl.compositionTo, 1)
				if tl.compositionSelFrom != tl.compositionSelTo {
					drawTextInputUnderline(tl.displayShaped, inputTextAttrs.Size, *scroll, tl.compositionSelFrom, tl.compositionSelTo, 2)
				}
			}
			// After Left/Right onto (or about to exit) an LTR↔RTL edge,
			// ghost the next stop in that direction (caret-alike, low
			// alpha) and faintly tint the Shift-select glyph.
			if hasFocus && !composing &&
				activeInput.cursor == activeInput.anchor &&
				activeInput.motionArrivalSide != caretMotionNone {
				aff := tl.drawCaretAffinity(activeInput.cursor, activeInput.preferPrevLineCaret)
				var caretAlpha float32 = 1
				if !shirei.HeadlessRender && time.Since(activeInput.start) < 5*time.Second {
					if int(time.Since(activeInput.start)/(time.Millisecond*600))%2 == 1 {
						caretAlpha = 0
					}
				}
				drawCaretMotionPreview(tl, activeInput.cursor, *scroll, lineHeight, aff,
					activeInput.motionArrivalSide, caretAlpha)
			}
			ShapedTextLayout(tl.displayShaped, inputTextAttrs, selectionFrom, selectionTo)
		})

		if HasFocus() && shirei.WindowFocused {
			// Blink the caret on a 600ms cycle for a while after the last edit or
			// caret move (activeInput.start), then hold it solid — so a field left
			// untouched stops blinking and lets the loop sleep (like VS Code).
			// activeInput.start resets on typing / caret motion / focus but NOT on
			// scrolling, which is the point: scrolling isn't caret activity. Headless
			// holds steady, so snapshots don't depend on settle timing. The caret is
			// dropped entirely while the app is unfocused (most apps hide it there).
			const caretBlinkTimeout = 5 * time.Second
			var alpha float32 = 1
			if !shirei.HeadlessRender && time.Since(activeInput.start) < caretBlinkTimeout {
				RequestNextFrame()
				if int(time.Since(activeInput.start)/(time.Millisecond*600))%2 == 1 {
					alpha = 0
				}
			}
			var rd = GetRenderData()
			affinity := caretAffinityDefault
			if composing {
				compositionPos := tl.CaretPos(tl.compositionFrom, caretAffinityDefault)
				compositionPos[0] += rd.Padding[PAD_LEFT] - scroll[0]
				compositionPos[1] += rd.Padding[PAD_TOP] - scroll[1]
				Container(Attrs(MinSize(1, inputTextAttrs.Size), Background(0, 0, 30, 0), FloatVec(compositionPos)), func() {
					r := GetScreenRect()
					shirei.CompositionPos = Vec2Add(r.Origin, Vec2{0, r.Size[1]})
				})
			} else {
				affinity = tl.drawCaretAffinity(activeInput.cursor, activeInput.preferPrevLineCaret)
			}
			var pos = tl.CaretPos(tl.displayCursor, affinity)
			pos[0] += rd.Padding[PAD_LEFT] - scroll[0] // the caret is a float; floats don't scroll
			pos[1] += rd.Padding[PAD_TOP] - scroll[1]
			Container(Attrs(MinSize(1, inputTextAttrs.Size), Background(0, 0, 30, alpha), FloatVec(pos)), func() {
				r := GetScreenRect()
				shirei.CaretPos = Vec2Add(r.Origin, Vec2{0, r.Size[1]})
				shirei.CaretHeight = r.Size[1]
			})
		}
		if hasFocus {
			activeInput.composition = InputState.Composition
			activeInput.compositionSel = InputState.CompositionSel
		}
	})
}
