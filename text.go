package shirei

import (
	"slices"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/bidi"

	g "go.hasen.dev/generic"

	"github.com/cespare/xxhash/v2"
	"github.com/go-text/typesetting/harfbuzz"
	"github.com/go-text/typesetting/language"

	"github.com/dboslee/lru"
)

type TextStyle struct {
	Families []string
	FontAspect

	Color Vec4
	Size  f32

	// Background is a highlight painted behind glyphs (zero = none).
	// Distinct from layout AttrSet.Background.
	Background Vec4
	Underline  bool
	Strike     bool
}

// StyleSpan is one half-open rune range [From, To) with a COMPLETE style
// for that range. Callers build spans with Span(from, to, base, mods...);
// each Style is typically copy(base)+mods.
//
// Overlapping spans are composed internally before shaping/layout: fields that
// differ from the paragraph base are treated as deltas and stacked in list
// order (so bold then highlight keeps both on the intersection). A later span
// cannot clear an earlier override back to the base value (delta-vs-base
// limitation). Spans should use the same base as attrs.TextStyle.
type StyleSpan struct {
	From, To int
	Style    TextStyle
}

type TextAttrSet struct {
	TextStyle

	MaxWidth f32
	Spans    []StyleSpan // nil/empty = today's whole-run style only
}

func DefaultFontAspect() FontAspect {
	return FontAspect{
		Weight:  WeightNormal,
		Style:   StyleNormal,
		Stretch: StretchNormal,
	}
}

const DefaultTextSize = 12

func DefaultTextStyle() TextStyle {
	return TextStyle{
		Color:      Vec4{0, 0, 0, 1},
		Size:       DefaultTextSize,
		FontAspect: DefaultFontAspect(),
	}
}

func DefaultTextAttrs() TextAttrSet {
	return TextAttrSet{
		TextStyle: DefaultTextStyle(),
	}
}

// the background color of selected text
var SelectionColor = Vec4{220, 50, 70, 0.5}

// styleAt returns the full style covering rune index i: last StyleSpan in
// spans whose [From, To) contains i, otherwise base. After flattenStyleSpans,
// at most one span covers each index.
func styleAt(base TextStyle, spans []StyleSpan, i int) TextStyle {
	style := base
	for _, sp := range spans {
		if i >= sp.From && i < sp.To {
			style = sp.Style
		}
	}
	return style
}

// overlayStyle copies into dst every field of spanStyle that differs from base
// (delta-vs-base). Fields equal to base are left as in dst so earlier stacked
// overrides are preserved.
func overlayStyle(dst, spanStyle, base TextStyle) TextStyle {
	if spanStyle.Color != base.Color {
		dst.Color = spanStyle.Color
	}
	if spanStyle.Size != base.Size {
		dst.Size = spanStyle.Size
	}
	if spanStyle.Background != base.Background {
		dst.Background = spanStyle.Background
	}
	if spanStyle.Underline != base.Underline {
		dst.Underline = spanStyle.Underline
	}
	if spanStyle.Strike != base.Strike {
		dst.Strike = spanStyle.Strike
	}
	if spanStyle.FontAspect != base.FontAspect {
		dst.FontAspect = spanStyle.FontAspect
	}
	if !slices.Equal(spanStyle.Families, base.Families) {
		dst.Families = spanStyle.Families
	}
	return dst
}

// spanBreakpoints returns sorted unique From/To endpoints of spans, clamped
// to [0, textLen]. Empty/inverted ranges contribute nothing.
func spanBreakpoints(spans []StyleSpan, textLen int) []int {
	if textLen < 0 {
		textLen = 0
	}
	set := make(map[int]struct{}, len(spans)*2)
	for _, sp := range spans {
		from, to := sp.From, sp.To
		if from < 0 {
			from = 0
		}
		if to > textLen {
			to = textLen
		}
		if from >= to {
			continue
		}
		set[from] = struct{}{}
		set[to] = struct{}{}
	}
	if len(set) == 0 {
		return nil
	}
	out := make([]int, 0, len(set))
	for p := range set {
		out = append(out, p)
	}
	slices.Sort(out)
	return out
}

// flattenStyleSpans composes overlapping spans into disjoint fully-resolved
// StyleSpans. Each input span's Style is interpreted as deltas relative to
// base (fields equal to base do not clobber). List order is stack order.
// textLen clamps ranges (rune count of the string being shaped/laid out).
func flattenStyleSpans(base TextStyle, spans []StyleSpan, textLen int) []StyleSpan {
	if len(spans) == 0 || textLen <= 0 {
		return nil
	}
	bps := spanBreakpoints(spans, textLen)
	if len(bps) < 2 {
		return nil
	}
	var out []StyleSpan
	for i := 0; i < len(bps)-1; i++ {
		a, b := bps[i], bps[i+1]
		if a >= b {
			continue
		}
		// Is this atom covered by any span?
		covered := false
		st := base
		for _, sp := range spans {
			from, to := sp.From, sp.To
			if from < 0 {
				from = 0
			}
			if to > textLen {
				to = textLen
			}
			if from >= to {
				continue
			}
			// atom wholly inside span (breakpoints guarantee no partial cover)
			if a >= from && b <= to {
				covered = true
				st = overlayStyle(st, sp.Style, base)
			}
		}
		if !covered || textStylesEqual(st, base) {
			continue
		}
		// coalesce with previous if same style and adjacent
		if n := len(out); n > 0 && out[n-1].To == a && textStylesEqual(out[n-1].Style, st) {
			out[n-1].To = b
			continue
		}
		out = append(out, StyleSpan{From: a, To: b, Style: st})
	}
	return out
}

// effectiveSpans returns the spans used by shaping/layout: flattened composition
// of attrs.Spans against attrs.TextStyle.
func effectiveSpans(attrs TextAttrSet, textLen int) []StyleSpan {
	if len(attrs.Spans) == 0 {
		return nil
	}
	return flattenStyleSpans(attrs.TextStyle, attrs.Spans, textLen)
}

// styleRun is a disjoint resolved range after last-wins evaluation.
type styleRun struct {
	From, To int
	Style    TextStyle
}

// resolveStyleRuns covers [0, textLen) with disjoint runs of constant
// resolved style (base + last-wins spans). Prefer passing already-flattened
// spans from effectiveSpans.
func resolveStyleRuns(base TextStyle, spans []StyleSpan, textLen int) []styleRun {
	if textLen <= 0 {
		return nil
	}
	if len(spans) == 0 {
		return []styleRun{{From: 0, To: textLen, Style: base}}
	}
	runs := make([]styleRun, 0, len(spans)*2+1)
	start := 0
	cur := styleAt(base, spans, 0)
	for i := 1; i < textLen; i++ {
		next := styleAt(base, spans, i)
		if !textStylesEqual(cur, next) {
			runs = append(runs, styleRun{From: start, To: i, Style: cur})
			start = i
			cur = next
		}
	}
	runs = append(runs, styleRun{From: start, To: textLen, Style: cur})
	return runs
}

func textStylesEqual(a, b TextStyle) bool {
	return a.Color == b.Color &&
		a.Size == b.Size &&
		a.Background == b.Background &&
		a.Underline == b.Underline &&
		a.Strike == b.Strike &&
		a.FontAspect == b.FontAspect &&
		slices.Equal(a.Families, b.Families)
}

func fontShapeEqual(a, b TextStyle) bool {
	return a.Size == b.Size &&
		a.FontAspect == b.FontAspect &&
		slices.Equal(a.Families, b.Families)
}

func fontIdsForStyle(style TextStyle) []FontId {
	fontIds := make([]FontId, 0, len(style.Families))
	for _, fontName := range style.Families {
		fontIds = append(fontIds, LookupFace(FaceLookupKey{fontName, style.FontAspect}))
	}
	return fontIds
}

// the smallest rune index on the line, or -1 if the line has no glyphs
func lineFirstCluster(line *ShapedTextLine) int {
	first := -1
	for _, s := range line.Segments {
		for _, g := range s.Glyphs {
			if first < 0 || int(g.Cluster) < first {
				first = int(g.Cluster)
			}
		}
	}
	return first
}

func ShapedTextLineLayout(line *ShapedTextLine, attrs TextAttrSet, baseDir Direction, selectionFrom int, selectionTo int, nextLinePaddingTop *f32) {
	// the line box is lineEm tall (max em on the line); the rest of the line
	// height (the leading) is applied as top padding, spacing this line from
	// the previous one. Glyph bitmaps are keyed by container height
	// (GlyphKeyForSurface uses Rect.Size[1]), so each glyph box MUST use its
	// resolved style Size — not always attrs.Size — or size spans shape at one
	// scale (wide advances) and draw at another (letter-spaced normal glyphs).
	leading := *nextLinePaddingTop
	hasSpans := len(attrs.Spans) > 0

	lineEm := attrs.Size
	if hasSpans {
		for _, s := range line.Segments {
			for _, g := range s.Glyphs {
				sz := styleAt(attrs.TextStyle, attrs.Spans, int(g.Cluster)).Size
				if sz > lineEm {
					lineEm = sz
				}
			}
		}
	}
	if lineEm <= 0 {
		lineEm = attrs.Size
	}

	// expand-across is necessary for the alignment to work
	var lineAttrs AttrSet
	lineAttrs.Row = true
	lineAttrs.NoAnimate = true
	lineAttrs.ExpandAcross = true
	lineAttrs.MaxSize[0] = attrs.MaxWidth
	lineAttrs.MinSize[1] = lineEm
	lineAttrs.Padding[PAD_TOP] = leading
	*nextLinePaddingTop = line.Height - lineEm

	// TODO: allow text attribute to control alignment
	if baseDir == RTL {
		lineAttrs.MainAlign = AlignEnd
	}

	// selection highlight geometry: floats anchor at the container origin,
	// above the top padding, so the em box sits at y=leading. When the
	// selection comes in from an earlier line, the highlight grows upward to
	// also cover the leading, so consecutive selected lines form one
	// continuous block.
	selOrigin := Vec2{0, leading}
	selHeight := lineEm
	first := lineFirstCluster(line)
	if leading > 0 && first >= 0 && selectionFrom < first {
		selOrigin[1] = 0
		selHeight += leading
	}
	hasSelection := selectionFrom != selectionTo

	Container(lineAttrs, func() {
		// pass 1a: span backgrounds — full line em so the band covers
		// baseline-shifted ink (never grow into leading)
		if hasSpans {
			Container(AttrSet{Floats: true, Float: Vec2{0, leading}, Row: true, ExpandAcross: true}, func() {
				for _, s := range line.Segments {
					for _, g := range s.Glyphs {
						// Cluster is the first rune of the glyph cluster; the
						// whole cluster takes that rune's style (half a ligature
						// cannot be two colors).
						st := styleAt(attrs.TextStyle, attrs.Spans, int(g.Cluster))
						var bg AttrSet
						bg.MinSize[0] = g.XAdvance // FIXME: use width instead of x advance?
						bg.MinSize[1] = lineEm
						if st.Background != (Vec4{}) {
							bg.Background = st.Background
						}
						Element(bg)
					}
				}
			})
		}

		// pass 1b: selection (paints over span backgrounds; may include leading)
		if hasSelection {
			Container(AttrSet{Floats: true, Float: selOrigin, Row: true, ExpandAcross: true}, func() {
				for _, s := range line.Segments {
					for _, g := range s.Glyphs {
						var bg AttrSet
						bg.MinSize[0] = g.XAdvance
						bg.MinSize[1] = selHeight
						runeIndex := int(g.Cluster)
						if runeIndex >= selectionFrom && runeIndex < selectionTo {
							bg.Background = SelectionColor
						}
						Element(bg)
					}
				}
			})
		}

		// pass 1c: underline at emBottom+1 (IME preedit convention)
		if hasSpans {
			Container(AttrSet{Floats: true, Float: Vec2{0, leading + lineEm + 1}, Row: true, ExpandAcross: true}, func() {
				for _, s := range line.Segments {
					for _, g := range s.Glyphs {
						var u AttrSet
						u.MinSize[0] = g.XAdvance
						u.MinSize[1] = 1
						st := styleAt(attrs.TextStyle, attrs.Spans, int(g.Cluster))
						if st.Underline {
							u.Background = st.Color
						}
						Element(u)
					}
				}
			})
			// pass 1d: strike at ~55% of line em height
			Container(AttrSet{Floats: true, Float: Vec2{0, leading + lineEm*0.55}, Row: true, ExpandAcross: true}, func() {
				for _, s := range line.Segments {
					for _, g := range s.Glyphs {
						var u AttrSet
						u.MinSize[0] = g.XAdvance
						u.MinSize[1] = 1
						st := styleAt(attrs.TextStyle, attrs.Spans, int(g.Cluster))
						if st.Strike {
							u.Background = st.Color
						}
						Element(u)
					}
				}
			})
		}

		// pass 2: actual glyphs — Size[1] is the glyph raster size; shift
		// smaller boxes down so pen baselines meet at frac*lineEm
		for _, s := range line.Segments {
			for _, g := range s.Glyphs {
				st := attrs.TextStyle
				if hasSpans {
					st = styleAt(attrs.TextStyle, attrs.Spans, int(g.Cluster))
				}
				em := glyphEmSize(st, lineEm)
				var a AttrSet
				a.MinSize[0] = g.XAdvance
				a.MinSize[1] = em
				a.Background = st.Color

				Container(a, func() {
					current.fontId = g.FontId
					current.glyphId = g.GlyphId
					shift := baselineShiftY(lineEm, em)
					current.glyphOffset = Vec2{g.Offset[0], g.Offset[1] + shift}
				})
			}
		}
	})
}

// glyphEmSize is the layout/raster em for a resolved style. Glyph bitmaps are
// keyed by this height; it must match the size used when shaping advances.
func glyphEmSize(st TextStyle, fallback f32) f32 {
	if st.Size > 0 {
		return st.Size
	}
	return fallback
}

// glyphBaselineFrac is the pen baseline as a fraction of the glyph box height.
// Must match softrender/cocoa drawGlyph (Origin.Y + Size[1]*frac + GlyphOffset.Y).
const glyphBaselineFrac = 0.82

// baselineShiftY returns the extra GlyphOffset.Y so a top-aligned glyph box of
// height glyphEm shares a baseline with a line whose em is lineEm:
//
//	penY = top + frac*glyphEm + shift  ==  top + frac*lineEm
func baselineShiftY(lineEm, glyphEm f32) f32 {
	if glyphEm <= 0 || lineEm <= glyphEm {
		return 0
	}
	return glyphBaselineFrac * (lineEm - glyphEm)
}

func ShapedTextLayout(shaped ShapedText, attrs TextAttrSet, selectionFrom int, selectionTo int) {
	// Compose overlapping spans once; layout only sees disjoint full styles.
	attrs.Spans = effectiveSpans(attrs, len(shaped.Runes))

	var blockAttrs AttrSet
	blockAttrs.MaxSize[0] = attrs.MaxWidth
	// TODO: allow text attribute to control alignment
	if shaped.BaseDir == RTL {
		blockAttrs.SelfAlign = AlignEnd
	}

	var nextLinePaddingTop float32 // to manage spaces between lines

	Container(blockAttrs, func() {
		for idx := range shaped.Lines {
			line := &shaped.Lines[idx]
			ShapedTextLineLayout(line, attrs, shaped.BaseDir, selectionFrom, selectionTo, &nextLinePaddingTop)
		}
	})
}

// Generated by ChatGPT (initially)
func SafeTruncateUTF8(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	cut := limit
	backstop := max(0, limit-4)

	// step back while in continuation bytes (10xxxxxx)
	for cut > backstop && (s[cut]&0xC0) == 0x80 {
		cut--
	}
	return s[:cut]
}

// Text renders a run of text with the given text attributes as a leaf of the
// current container. Label is the usual convenience wrapper over it.
func Text(label string, attrs TextAttrSet) {
	// For performance reasons, do not accept text larger than 16kb
	// We will add a segmented text view in the future to handle large text blobs
	label = SafeTruncateUTF8(label, 16*1024)

	shaped := ShapeText(label, attrs)
	ShapedTextLayout(shaped, attrs, 0, 0)
}

type TextLayout struct {
	Segments []GlyphsSegment
}

type GlyphsSegment struct {
	GlyphSegmentProps
	Width           float32
	Height          float32
	EndsWithNewline bool
	Glyphs          []Glyph
}

type Glyph struct {
	FontId   FontId
	GlyphId  GlyphId
	Cluster  int32
	Offset   Vec2
	XAdvance float32
	Width    float32
	// Scale float32

	// FIXME should this be here?
	Direction Direction
}

var hbfonts = make(map[FontId]*harfbuzz.Font)

func shapeSegment(props GlyphSegmentProps, text []rune, start, length int) (s GlyphsSegment) {
	s.GlyphSegmentProps = props
	s.EndsWithNewline = length > 0 && text[start+length-1] == '\n'
	s.Glyphs = make([]Glyph, 0, length)

	fontId := props.font

	if fontId == 0 {
		// FIXME should we fill in some values??
		return s
	}

	face := GetFace(fontId)

	buf := harfbuzz.NewBuffer()

	buf.AddRunes(text, start, length)
	buf.Props.Script = props.sc
	buf.Props.Direction = harfbuzz.LeftToRight + harfbuzz.Direction(props.Dir)
	buf.Props.Language = "en-EN"

	// this could set language to utf-8 which would *crash* the language parser!!
	// buf.GuessSegmentProperties() // this seems to just set the default locale language; regardless of content!

	font := hbfonts[fontId]
	if font == nil {
		ttf := GetParsedFont(fontId)
		if ttf == nil {
			return s
		}
		font = harfbuzz.NewFont(ttf)
		hbfonts[fontId] = font
		// TODO use lru cache instead of map?
	}

	buf.Shape(font, nil)

	scaleFactor := face.InvUPM * props.size

	s.Height = scaleFactor * (face.Ascender - face.Descender)

	for i := range buf.Info {
		inf := buf.Info[i]
		pos := buf.Pos[i]
		r := text[inf.Cluster]

		xAdvance := float32(pos.XAdvance) * scaleFactor
		width := max(xAdvance, GlyphWidth(fontId, inf.Glyph)*scaleFactor)

		// special support for tabs!
		if r == '\t' {
			stdg := LookupGlyph(fontId, 'M')
			width = GlyphWidth(fontId, stdg) * 4
			width *= scaleFactor
			xAdvance = width
		}
		// Newlines force line breaks at the segment layer; they should
		// still occupy an index for editing, but they must not render as
		// the phantom advance that indents the following hard line.
		if r == '\n' {
			width = 0
			xAdvance = 0
		}

		g.Append(&s.Glyphs, Glyph{
			FontId:    fontId,
			GlyphId:   inf.Glyph,
			Cluster:   int32(inf.Cluster),
			Offset:    Vec2{float32(pos.XOffset) * scaleFactor, float32(pos.YOffset) * scaleFactor},
			XAdvance:  xAdvance,
			Width:     width,
			Direction: props.Dir,
		})
		// width is accumulated xadvances
		// only the last item we should take the max of width and xadvance but
		// for now we don't bother. we'll look into this if it proves to be a
		// real problem
		s.Width += xAdvance
	}

	return s
}

func produceShapedSegments(runes []rune, dirs []Direction, base TextStyle, spans []StyleSpan) []GlyphsSegment {
	var allSegments = make([]GlyphsSegment, 0, len(runes)/2)

	var lineNo int

	// Cache resolved face lists per distinct shaping style so LookupFace is
	// not paid per rune when spans repeat the same families/aspect.
	type faceCacheKey struct {
		aspect FontAspect
		// families joined — rare and short for UI labels
		families string
	}
	faceCache := make(map[faceCacheKey][]FontId)

	fontIdsFor := func(st TextStyle) []FontId {
		key := faceCacheKey{aspect: st.FontAspect, families: strings.Join(st.Families, "\x00")}
		if ids, ok := faceCache[key]; ok {
			return ids
		}
		ids := fontIdsForStyle(st)
		faceCache[key] = ids
		return ids
	}

	getSegmentProps := func(i int) GlyphSegmentProps {
		ch := runes[i]
		st := styleAt(base, spans, i)
		fontIds := fontIdsFor(st)
		font, _ := findMatchingFontAndGlyph(ch, fontIds, st.FontAspect)
		return GlyphSegmentProps{
			font:    font,
			size:    st.Size,
			sc:      language.LookupScript(ch),
			Dir:     dirs[i],
			isSpace: isSpace(ch),
			lineNo:  lineNo,
		}
	}

	var segment = getSegmentProps(0)
	if runes[0] == '\n' {
		lineNo++
	}
	var start = 0
	for i := 1; i < len(runes); i++ {
		segmentNext := getSegmentProps(i)
		if runes[i] == '\n' {
			lineNo++
		}

		// special case!!
		if segmentNext.sc == language.Inherited {
			segmentNext.sc = segment.sc
		}

		if segmentNext != segment {
			length := i - start
			allSegments = append(allSegments, shapeSegment(segment, runes, start, length))
			segment = segmentNext
			start = i
		}
	}
	// last segment!
	length := len(runes) - start
	allSegments = append(allSegments, shapeSegment(segment, runes, start, length))

	return allSegments
}

func lineBreakShapedSegments(allSegments []GlyphsSegment, attrs TextAttrSet) []ShapedTextLine {

	// break segments into lines
	var lines []ShapedTextLine
	{
		var prevLineNo int // first segment always has line number set to 0
		var maxWidth = attrs.MaxWidth
		var widthAcc float32
		var height float32
		var start int
		for i, segment := range allSegments {
			var widthOverflow = i > start && maxWidth > 0 && segment.Width+widthAcc > maxWidth
			var forceLineBreak = segment.lineNo > prevLineNo
			if widthOverflow || forceLineBreak {
				lines = append(lines, ShapedTextLine{
					Segments: allSegments[start:i],
					Width:    widthAcc,
					Height:   height,
				})
				start = i
				widthAcc = 0
				height = 0
			}
			widthAcc += segment.Width
			height = max(height, segment.Height)
			prevLineNo = segment.lineNo
		}
		lines = append(lines, ShapedTextLine{
			Segments: allSegments[start:],
			Width:    widthAcc,
			Height:   height,
		})
		if allSegments[len(allSegments)-1].EndsWithNewline {
			if height == 0 {
				height = attrs.Size
			}
			lines = append(lines, ShapedTextLine{
				Height: height,
			})
		}
	}

	var baseDir = allSegments[0].Dir
	var reverseDir = baseDir ^ 1 // flips the lower bit, and we only have two values, so

	// reverse continuous reverse runs
	for i := range lines {
		line := &lines[i]

		// if RTL, flip the entire thing first, then flip LTR runs
		// if LTR, just flip RTL runs
		if baseDir == RTL {
			slices.Reverse(line.Segments)
		}

		var dir = baseDir
		var start = 0
		for i := range line.Segments {
			dir1 := line.Segments[i].Dir
			if dir == baseDir && dir1 == reverseDir {
				// start of a new reverse run
				start = i
				dir = dir1
			} else if dir == reverseDir && dir1 == baseDir {
				// end of revers run
				slices.Reverse(line.Segments[start:i])
				start = -1
				dir = dir1
			}
		}
		// last reverse run!
		if dir == reverseDir {
			slices.Reverse(line.Segments[start:])
		}
	}

	return lines
}

type Direction byte

const (
	LTR Direction = iota
	RTL
)

type GlyphSegmentProps struct {
	font    FontId
	size    float32
	sc      language.Script
	Dir     Direction
	isSpace bool
	lineNo  int // hack for line breaks
}

func isSpace(ch rune) bool {
	return unicode.Is(unicode.Zs, ch)
}

type ShapedText struct {
	Runes   []rune
	BaseDir Direction
	Lines   []ShapedTextLine
}

type ShapedTextLine struct {
	Segments []GlyphsSegment
	Width    float32
	Height   float32
}

// shapeCache: capacity must comfortably exceed the number of distinct
// visible strings in a busy frame, or the LRU thrashes (every entry
// evicted before its next use — the whole UI re-shaped through harfbuzz
// every frame). A profiler table view alone can show 250+ strings; 4096
// entries of label-sized ShapedText is a few MB, cheap next to what
// shaping costs. Effectiveness is pinned by see_pprof's
// TestShapeCacheSteadyState.
var shapeCache = lru.New[uint64, ShapedText](lru.WithCapacity(4096))

// ShapeStats counts ShapeText invocations vs cache hits — the diagnostic
// for shape-cache effectiveness. In steady state (no text changing between
// frames) hits should track calls; a persistent gap means the UI is paying
// harfbuzz every frame. Pinned by see_pprof's TestShapeCacheSteadyState.
var ShapeStats struct {
	Calls int64
	Hits  int64
}

func ShapeText(text string, attrs TextAttrSet) ShapedText {
	var shaped ShapedText
	if len(text) == 0 {
		return ShapedText{}
	}
	ShapeStats.Calls++

	var runes = []rune(text)
	// Compose overlapping spans before cache key + shaping so bold∩highlight
	// stacks field deltas instead of last-full-style-wins.
	attrs.Spans = effectiveSpans(attrs, len(runes))

	// Caching. The key hashes the string CONTENTS — not the header: a
	// header (pointer) key means every fmt.Sprintf-built label is a fresh
	// key each frame, so dynamic strings never hit AND their garbage
	// entries evict the stable ones (see_pprof showed harfbuzz burning 33%
	// of cumulative time on a fully static screen). Hashing the bytes costs
	// nanoseconds; shaping costs microseconds.
	//
	// Render-tier span props (Color, Background, Underline, Strike) are
	// deliberately NOT in the key: they are applied at layout time.
	// Shaping-tier span props (Families, FontAspect, Size) join the key
	// only when they differ from the base style somewhere in the string;
	// nil Spans and color-only spans share the same key as today's path.
	var cacheKey uint64
	{
		var hash = xxhash.New()
		hash.WriteString(text)
		Hash(hash, &attrs.MaxWidth)
		Hash(hash, &attrs.Size)
		Hash(hash, &attrs.FontAspect)
		baseFontIds := fontIdsForStyle(attrs.TextStyle)
		HashSlice(hash, baseFontIds)

		if len(attrs.Spans) > 0 {
			runs := resolveStyleRuns(attrs.TextStyle, attrs.Spans, len(runes))
			for _, r := range runs {
				if fontShapeEqual(r.Style, attrs.TextStyle) {
					continue
				}
				Hash(hash, &r.From)
				Hash(hash, &r.To)
				Hash(hash, &r.Style.Size)
				Hash(hash, &r.Style.FontAspect)
				HashSlice(hash, fontIdsForStyle(r.Style))
			}
		}
		cacheKey = hash.Sum64()

		cached, cacheFound := shapeCache.Get(cacheKey)
		if cacheFound {
			ShapeStats.Hits++
			return cached
		}
	}

	var dirs = ParagraphBidi(text)
	allSegments := produceShapedSegments(runes, dirs, attrs.TextStyle, attrs.Spans)
	shaped.Runes = runes
	shaped.BaseDir = allSegments[0].Dir
	shaped.Lines = lineBreakShapedSegments(allSegments, attrs)

	shapeCache.Set(cacheKey, shaped)

	return shaped
}

func findMatchingFontAndGlyph(ch rune, fonts []FontId, aspect FontAspect) (FontId, GlyphId) {
	var fontId FontId
	var glyphId GlyphId
	for _, fid := range fonts {
		gid := LookupGlyph(fid, ch)
		if gid == 0 {
			continue
		} else {
			fontId = fid
			glyphId = gid
			break
		}
	}

	if fontId == 0 || glyphId == 0 {
		return FallbackFontFor(ch, aspect)
	}

	return fontId, glyphId
}

var bidiCache = lru.New[string, []Direction]()

// works with a single line of text, not an article with multiple paragraphs!
func ParagraphBidi(txt string) []Direction {
	out, found := bidiCache.Get(txt)
	if found {
		return out
	}

	out = make([]Direction, 0, len(txt))

	for line := range strings.SplitSeq(txt, "\n") {
		var paragraph bidi.Paragraph
		paragraph.SetString(line)
		ordering, err := paragraph.Order()
		if err != nil {
			panic(err)
		}
		for i := range ordering.NumRuns() {
			run := ordering.Run(i)
			start, end := run.Pos() // NOTE: end is inclusive
			dir := Direction(run.Direction())
			for j := start; j <= end; j++ {
				out = append(out, dir)
			}
		}
		out = append(out, LTR) // FIXME the dir for the newline character ..
	}

	bidiCache.Set(txt, out)

	return out
}
