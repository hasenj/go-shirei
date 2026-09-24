package shirei

import (
	"slices"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
	"unsafe"

	"golang.org/x/text/unicode/bidi"

	g "go.hasen.dev/generic"

	"github.com/cespare/xxhash/v2"
	"github.com/go-text/typesetting/harfbuzz"
	"github.com/go-text/typesetting/language"
)

type TextStyleAttrs struct {
	// fontFamilies is the interned font family preference list, in priority
	// order. Nil and emptyFamilyList are the empty list. Mutation goes
	// through SetFontFamilies / Fonts, which intern, so copies and cascade
	// share the pointer. Unexported so callers cannot store a non-interned list.
	fontFamilies *internedFamilies
	FontAspect

	TextColor Vec4
	FontSize  f32

	// Background is a highlight painted behind glyphs (zero = none).
	// Distinct from layout AttrSet.Background.
	Background Vec4
	Underline  bool
	Strike     bool
}

// SetFontFamilies replaces the style's font family preference list (priority
// order; per-rune fallback walks it first to last). The list is interned
// (lowercase, canonical pointer) so equal name lists share one identity.
func (s *TextStyleAttrs) SetFontFamilies(families ...string) {
	s.fontFamilies = internFamilyList(families)
}

// StyleSpan is one half-open rune range [From, To) with a COMPLETE style
// for that range. Produced by resolving TextSpan requests against a paragraph
// base (copy(base)+mods). The pipeline only consumes these fully resolved
// ranges after flattenStyleSpans.
//
// Overlapping spans are composed internally before shaping/layout: fields that
// differ from the paragraph base are treated as deltas and stacked in list
// order (so bold then highlight keeps both on the intersection). A later span
// cannot clear an earlier override back to the base value (delta-vs-base
// limitation).
type StyleSpan struct {
	From, To int
	Style    TextStyleAttrs
}

// TextSpan is a deferred range style for Text / ShapeText: mods are applied to
// the call's paragraph base when the call runs (never field-wise inherit).
// Build with Span(from, to, mods...).
type TextSpan struct {
	From, To int
	mods     []TextStyleFn
}

func DefaultFontAspect() FontAspect {
	return FontAspect{
		Weight:  WeightNormal,
		Style:   StyleNormal,
		Stretch: StretchNormal,
	}
}

const DefaultTextSize = 12

func DefaultTextStyle() TextStyleAttrs {
	return TextStyleAttrs{
		fontFamilies: emptyFamilyList,
		TextColor:    Vec4{0, 0, 0, 1},
		FontSize:     DefaultTextSize,
		FontAspect:   DefaultFontAspect(),
	}
}

// TextStyleWith returns a copy of base with mods applied in order. A plain
// struct copy is a full clone: interned family lists are shared by pointer.
func TextStyleWith(base TextStyleAttrs, mods ...TextStyleFn) TextStyleAttrs {
	s := base
	p := (*TextStyleAttrs)(noescape(unsafe.Pointer(&s)))
	for _, m := range mods {
		m(p)
	}
	return s
}

// resolveTextSpans applies each TextSpan's mods to base, producing StyleSpans.
func resolveTextSpans(base TextStyleAttrs, spans []TextSpan) []StyleSpan {
	if len(spans) == 0 {
		return nil
	}
	out := make([]StyleSpan, len(spans))
	for i, sp := range spans {
		out[i] = StyleSpan{
			From:  sp.From,
			To:    sp.To,
			Style: TextStyleWith(base, sp.mods...),
		}
	}
	return out
}

// the background color of selected text
var SelectionColor = Vec4{220, 50, 70, 0.5}

// styleAt returns the full style covering rune index i: last StyleSpan in
// spans whose [From, To) contains i, otherwise base. After flattenStyleSpans,
// at most one span covers each index.
func styleAt(base TextStyleAttrs, spans []StyleSpan, i int) TextStyleAttrs {
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
func overlayStyle(dst, spanStyle, base TextStyleAttrs) TextStyleAttrs {
	if spanStyle.TextColor != base.TextColor {
		dst.TextColor = spanStyle.TextColor
	}
	if spanStyle.FontSize != base.FontSize {
		dst.FontSize = spanStyle.FontSize
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
	if !familyListEq(spanStyle.fontFamilies, base.fontFamilies) {
		dst.fontFamilies = spanStyle.fontFamilies
	}
	return dst
}

// spanBreakpoints returns sorted unique From/To endpoints of spans, clamped
// to [0, textLen]. Empty/inverted ranges contribute nothing.
func spanBreakpoints(spans []StyleSpan, textLen int) []int {
	if textLen < 0 {
		textLen = 0
	}
	out := make([]int, 0, len(spans)*2)
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
		out = append(out, from, to)
	}
	if len(out) == 0 {
		return nil
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// flattenStyleSpans composes overlapping spans into disjoint fully-resolved
// StyleSpans. Each input span's Style is interpreted as deltas relative to
// base (fields equal to base do not clobber). List order is stack order.
// textLen clamps ranges (rune count of the string being shaped/laid out).
func flattenStyleSpans(base TextStyleAttrs, spans []StyleSpan, textLen int) []StyleSpan {
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
// of spans against the paragraph base style.
func effectiveSpans(base TextStyleAttrs, spans []StyleSpan, textLen int) []StyleSpan {
	if len(spans) == 0 {
		return nil
	}
	return flattenStyleSpans(base, spans, textLen)
}

// styleRun is a disjoint resolved range after last-wins evaluation.
type styleRun struct {
	From, To int
	Style    TextStyleAttrs
}

// resolveStyleRuns covers [0, textLen) with disjoint runs of constant
// resolved style. spans must be flattened (effectiveSpans output: sorted,
// disjoint, clamped) — the runs are built straight from the span boundaries,
// with base filling the gaps. No per-rune walk.
func resolveStyleRuns(base TextStyleAttrs, spans []StyleSpan, textLen int) []styleRun {
	if textLen <= 0 {
		return nil
	}
	runs := make([]styleRun, 0, len(spans)*2+1)
	pos := 0
	for _, sp := range spans {
		from, to := sp.From, sp.To
		if from < pos {
			from = pos
		}
		if to > textLen {
			to = textLen
		}
		if from >= to {
			continue
		}
		if from > pos {
			runs = append(runs, styleRun{From: pos, To: from, Style: base})
		}
		runs = append(runs, styleRun{From: from, To: to, Style: sp.Style})
		pos = to
	}
	if pos < textLen {
		runs = append(runs, styleRun{From: pos, To: textLen, Style: base})
	}
	return runs
}

func textStylesEqual(a, b TextStyleAttrs) bool {
	return a.TextColor == b.TextColor &&
		a.FontSize == b.FontSize &&
		a.Background == b.Background &&
		a.Underline == b.Underline &&
		a.Strike == b.Strike &&
		a.FontAspect == b.FontAspect &&
		familyListEq(a.fontFamilies, b.fontFamilies)
}

func fontShapeEqual(a, b TextStyleAttrs) bool {
	return a.FontSize == b.FontSize &&
		a.FontAspect == b.FontAspect &&
		familyListEq(a.fontFamilies, b.fontFamilies)
}

func familyListEq(a, b *internedFamilies) bool {
	if a == b {
		return true
	}
	// nil and emptyFamilyList are the same empty list.
	return familyListId(a) == 0 && familyListId(b) == 0
}

func familyListId(f *internedFamilies) uint32 {
	if f == nil {
		return 0
	}
	return f.id
}

func fontIdsForStyle(style TextStyleAttrs) []FontId {
	ids, _ := style.fontFamilies.resolve(style.FontAspect)
	return ids
}

// hashFontFamilies writes the interned list id. Same names intern to the
// same pointer/id, so Fonts() clones hash equal without walking strings.
func hashFontFamilies(h *xxhash.Digest, f *internedFamilies) {
	id := familyListId(f)
	Hash(h, &id)
}

// internedFamilies is one canonical family-preference list. Equal name
// lists (case-insensitive) share one of these for the process lifetime.
type internedFamilies struct {
	id    uint32
	names []string // lowercase, immutable
	next  *internedFamilies

	resolvedEpoch uint64
	resolved      []familyListResolved
}

type familyListResolved struct {
	aspect  FontAspect
	ids     []FontId
	primary FontId
}

// emptyFamilyList is the interned empty preference list (id 0). Fallback
// faces cover shaping when the style has no named families.
var emptyFamilyList = &internedFamilies{id: 0}

var familyIntern struct {
	mu     sync.Mutex
	nextId uint32
	byHash map[uint64]*internedFamilies
}

// internFamilyList returns the canonical list for the concatenation of
// name groups. Empty groups yield emptyFamilyList. Lookup hashes lowercase
// names without allocating; the interned slice is built only on a miss.
func internFamilyList(groups ...[]string) *internedFamilies {
	n := 0
	for _, g := range groups {
		n += len(g)
	}
	if n == 0 {
		return emptyFamilyList
	}

	var d xxhash.Digest
	d.Reset()
	Hash(&d, &n)
	for _, g := range groups {
		for _, name := range g {
			writeLowerFamilyName(&d, name)
		}
	}
	sum := d.Sum64()

	familyIntern.mu.Lock()
	defer familyIntern.mu.Unlock()
	if familyIntern.byHash == nil {
		familyIntern.byHash = make(map[uint64]*internedFamilies)
	}
	for f := familyIntern.byHash[sum]; f != nil; f = f.next {
		if familyListMatches(f, groups) {
			return f
		}
	}

	names := make([]string, 0, n)
	for _, g := range groups {
		for _, name := range g {
			names = append(names, strings.ToLower(name))
		}
	}
	familyIntern.nextId++
	f := &internedFamilies{
		id:    familyIntern.nextId,
		names: names,
		next:  familyIntern.byHash[sum],
	}
	familyIntern.byHash[sum] = f
	return f
}

func familyListMatches(f *internedFamilies, groups [][]string) bool {
	i := 0
	for _, g := range groups {
		for _, name := range g {
			if i >= len(f.names) || !strings.EqualFold(f.names[i], name) {
				return false
			}
			i++
		}
	}
	return i == len(f.names)
}

// writeLowerFamilyName hashes length + lowercase bytes. ASCII case folding
// uses a stack buffer so Fonts(Monospace...) intern hits stay allocation-free.
func writeLowerFamilyName(h *xxhash.Digest, s string) {
	ascii, hasUpper := true, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= utf8.RuneSelf {
			ascii = false
			break
		}
		if c >= 'A' && c <= 'Z' {
			hasUpper = true
		}
	}
	if !ascii {
		lower := strings.ToLower(s)
		ln := len(lower)
		Hash(h, &ln)
		h.WriteString(lower)
		return
	}
	ln := len(s)
	Hash(h, &ln)
	if !hasUpper {
		h.WriteString(s)
		return
	}
	var buf [128]byte
	b := buf[:]
	if ln > len(buf) {
		b = make([]byte, ln)
	}
	for i := 0; i < ln; i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 32
		}
		b[i] = c
	}
	h.Write(b[:ln])
}

func (f *internedFamilies) resolve(aspect FontAspect) (ids []FontId, primary FontId) {
	if f == nil {
		f = emptyFamilyList
	}
	epoch := fontLookupEpoch()
	if f.resolvedEpoch != epoch {
		f.resolved = f.resolved[:0]
		f.resolvedEpoch = epoch
	}
	for i := range f.resolved {
		if f.resolved[i].aspect == aspect {
			return f.resolved[i].ids, f.resolved[i].primary
		}
	}
	ids = make([]FontId, len(f.names))
	for i, name := range f.names {
		ids[i] = LookupFace(FaceLookupKey{name, aspect})
	}
	// Primary is a face on THIS list that covers space. Do not call
	// FallbackFontFor here: fallbackScan resolves interned lists, and
	// that would recurse.
	for _, fid := range ids {
		if fid == 0 {
			continue
		}
		gid := LookupGlyph(fid, ' ')
		if gid == 0 || GetFace(fid).colorPaintOnly {
			continue
		}
		primary = fid
		break
	}
	f.resolved = append(f.resolved, familyListResolved{
		aspect:  aspect,
		ids:     ids,
		primary: primary,
	})
	return ids, primary
}

// lineFirstCluster is the smallest rune index on the line, or -1 if the line
// has no glyphs. Computed at shape time into ShapedTextLine.firstCluster.
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

func ShapedTextLineLayout(line *ShapedTextLine, style TextStyleAttrs, spans []StyleSpan, baseDir Direction, selectionFrom int, selectionTo int, nextLinePaddingTop *f32) {
	shapedTextLineLayoutColor(line, style, spans, baseDir, selectionFrom, selectionTo, nextLinePaddingTop, SelectionColor)
}

func shapedTextLineLayoutColor(line *ShapedTextLine, style TextStyleAttrs, spans []StyleSpan, baseDir Direction, selectionFrom int, selectionTo int, nextLinePaddingTop *f32, selectionColor Vec4) {
	// the line box is lineEm tall (max em on the line); the rest of the line
	// height (the leading) is applied as top padding, spacing this line from
	// the previous one. Glyph bitmaps are keyed by container height
	// (GlyphKeyForSurface uses Rect.Size[1]), so each glyph box MUST use its
	// resolved style Size — not always style.Size — or size spans shape at one
	// scale (wide advances) and draw at another (letter-spaced normal glyphs).
	leading := *nextLinePaddingTop
	hasSpans := len(spans) > 0

	lineEm := line.lineEm
	if lineEm <= 0 {
		lineEm = style.FontSize
	}

	// expand-across is necessary for the alignment to work.
	// Wrap width comes from the parent via MaxSize cascade (see Text /
	// ShapedTextLayout block); do not set MaxSize here.
	var lineAttrs AttrSet
	lineAttrs.Row = true
	lineAttrs.Animations = 0
	lineAttrs.ExpandAcross = true
	// MinSize is the outer box (padding is applied before the min clamp), so
	// include leading here. With no in-flow children, content height is 0 and
	// MinSize[1]=lineEm alone would drop the leading.
	lineAttrs.MinSize[1] = leading + lineEm
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
	// continuous block. firstCluster is filled at shape time.
	selOrigin := Vec2{0, leading}
	selHeight := lineEm
	hasSelection := selectionFrom != selectionTo
	if hasSelection && leading > 0 && line.firstCluster >= 0 && selectionFrom < line.firstCluster {
		selOrigin[1] = 0
		selHeight += leading
	}
	lineAttrs.MinSize[0] = line.Width

	// Cloning the shared cache stamps is only needed when some span actually
	// recolors glyphs. Background/underline/strike-only spans paint bands
	// below and keep the shared stamps with the uniform tint.
	spanRecolors := false
	for i := range spans {
		if spans[i].Style.TextColor != style.TextColor {
			spanRecolors = true
			break
		}
	}

	Container(lineAttrs, func() {
		ui.current.textRunWidth = line.Width
		ui.current.textRunEm = line.maxEm
		stamps := line.stamps
		if spanRecolors {
			var hash xxhash.Digest
			Hash(&hash, &style.TextColor)
			for i := range spans {
				Hash(&hash, &spans[i].From)
				Hash(&hash, &spans[i].To)
				Hash(&hash, &spans[i].Style.TextColor)
			}
			key := coloredGlyphKey{line.runData, hash.Sum64()}
			data, ok := res.coloredGlyphCache.Get(key)
			if !ok {
				colored := slices.Clone(line.runs)
				for i := range colored {
					colored[i].Color = styleAt(style, spans, int(stamps[i].Cluster)).TextColor
				}
				data = &GlyphRunData{glyphs: colored, hash: xxhash.Sum64(g.UnsafeSliceBytes(colored)), dependencies: line.runData.dependencies}
				res.coloredGlyphCache.Set(key, data)
			}
			ui.current.glyphData = data
		} else {
			ui.current.glyphData = line.runData
			ui.current.glyphRunColor = style.TextColor
		}

		if !hasSpans && !hasSelection {
			return
		}

		var rects []paintRect
		if hasSpans {
			appendAdvanceBands(&rects, stamps, leading, lineEm, func(g *glyphStamp) Vec4 {
				return styleAt(style, spans, int(g.Cluster)).Background
			})
			appendAdvanceBands(&rects, stamps, leading+lineEm+1, 1, func(g *glyphStamp) Vec4 {
				st := styleAt(style, spans, int(g.Cluster))
				if st.Underline {
					return st.TextColor
				}
				return Vec4{}
			})
			appendAdvanceBands(&rects, stamps, leading+lineEm*0.55, 1, func(g *glyphStamp) Vec4 {
				st := styleAt(style, spans, int(g.Cluster))
				if st.Strike {
					return st.TextColor
				}
				return Vec4{}
			})
		}
		if hasSelection {
			appendAdvanceBands(&rects, stamps, selOrigin[1], selHeight, func(g *glyphStamp) Vec4 {
				i := int(g.Cluster)
				if i >= selectionFrom && i < selectionTo {
					return selectionColor
				}
				return Vec4{}
			})
		}
		ui.current.paintRects = rects
	})
}

func appendAdvanceBands(dst *[]paintRect, stamps []glyphStamp, y, h float32, color func(*glyphStamp) Vec4) {
	var x, runX, runW float32
	var runC Vec4
	var have bool
	flush := func() {
		if have && runC != (Vec4{}) && runW > 0 {
			*dst = append(*dst, paintRect{Origin: Vec2{runX, y}, Size: Vec2{runW, h}, Color: runC})
		}
		have = false
		runW = 0
	}
	for i := range stamps {
		g := &stamps[i]
		c := color(g)
		if have && c == runC {
			runW += g.Advance
		} else {
			flush()
			runX, runW, runC, have = x, g.Advance, c, true
		}
		x += g.Advance
	}
	flush()
}

// glyphEmSize is the layout/raster em for a resolved style. Glyph bitmaps are
// keyed by this height; it must match the size used when shaping advances.
func glyphEmSize(st TextStyleAttrs, fallback f32) f32 {
	if st.FontSize > 0 {
		return st.FontSize
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

// faceDescenderDepth is ink below the baseline in logical pixels at em size,
// from already-loaded HHEA/OS2 extents (font units × InvUPM × size). Zero
// when the face is unknown or unparsed.
func faceDescenderDepth(invUPM, descender, size f32) f32 {
	if invUPM <= 0 || size <= 0 {
		return 0
	}
	// OpenType descender is typically negative (below baseline).
	d := descender
	if d > 0 {
		d = -d
	}
	return -d * invUPM * size
}

// fontDescenderDepth looks up a face and returns faceDescenderDepth.
func fontDescenderDepth(fontId FontId, size f32) f32 {
	if fontId == 0 || size <= 0 {
		return 0
	}
	face := GetFace(fontId)
	return faceDescenderDepth(face.InvUPM, face.Descender, size)
}

// CaretHeightForStyle is the caret bar height for a uniform run of style:
// pen baseline (glyphBaselineFrac × em) plus the face's descender depth.
// Falls back to the em when the face has no descender metrics.
func CaretHeightForStyle(style TextStyleAttrs) f32 {
	em := style.FontSize
	if em <= 0 {
		return 0
	}
	fid, _ := findMatchingFontAndGlyph(' ', fontIdsForStyle(style), style.FontAspect)
	d := fontDescenderDepth(fid, em)
	if d <= 0 {
		return em
	}
	return glyphBaselineFrac*em + d
}

// descenderPadForLine is bottom pad for a text line so glyph ink stays inside
// a clipped ancestor. The line box is lineEm tall with the pen at
// glyphBaselineFrac×lineEm; only (1−glyphBaselineFrac)×lineEm is reserved
// below the baseline. Real faces can need more — taken from each segment's
// stored descenderDepth (primary face at that size, inline sizes included).
// Fallback coverage faces do not contribute. Line-to-line spacing is unchanged.
func descenderPadForLine(line *ShapedTextLine, style TextStyleAttrs) f32 {
	if line == nil {
		return 0
	}
	lineEm := style.FontSize
	var maxDepth f32
	for _, s := range line.Segments {
		if s.size > lineEm {
			lineEm = s.size
		}
		if s.descenderDepth > maxDepth {
			maxDepth = s.descenderDepth
		}
	}
	if lineEm <= 0 {
		return 0
	}
	// Empty / trailing-newline line: no segment extents — paragraph primary
	// face at the base size so a blank last line still has a sensible box.
	if maxDepth == 0 {
		em := glyphEmSize(style, lineEm)
		fid, _ := findMatchingFontAndGlyph(' ', fontIdsForStyle(style), style.FontAspect)
		maxDepth = fontDescenderDepth(fid, em)
	}

	reserved := (1 - glyphBaselineFrac) * lineEm
	pad := maxDepth - reserved
	if pad < 0 {
		return 0
	}
	return pad
}

func ShapedTextLayout(shaped ShapedText, style TextStyleAttrs, selectionFrom int, selectionTo int, spans ...StyleSpan) {
	// Compose overlapping spans once; layout only sees disjoint full styles.
	flat := effectiveSpans(style, spans, len(shaped.Runes))
	shapedTextLayoutFlat(shaped, style, selectionFrom, selectionTo, flat, string(shaped.Runes), SelectionColor)
}

// ShapedTextLayoutStyled draws a shaped paragraph with an explicit selection color.
// Transparent zero is literal; the package SelectionColor is not consulted.
func ShapedTextLayoutStyled(shaped ShapedText, style TextStyleAttrs, selectionFrom, selectionTo int, selectionColor Vec4, spans ...StyleSpan) {
	flat := effectiveSpans(style, spans, len(shaped.Runes))
	shapedTextLayoutFlat(shaped, style, selectionFrom, selectionTo, flat, string(shaped.Runes), selectionColor)
}

// shapedTextLayoutFlat is ShapedTextLayout after span flattening: spans must
// be effectiveSpans output. Text calls it directly with the spans it already
// resolved for shaping.
func shapedTextLayoutFlat(shaped ShapedText, style TextStyleAttrs, selectionFrom int, selectionTo int, spans []StyleSpan, source string, selectionColor Vec4) {
	ui.anyAccess = true
	// Block size is content-driven; wrap constraint is the parent's cascaded
	// MaxSize (set by Text under a max-width container, or by an explicit
	// MaxWidth host). Soft-wrap line breaks were already applied at shape time.
	var blockAttrs AttrSet
	// TODO: allow text attribute to control alignment
	if shaped.BaseDir == RTL {
		blockAttrs.SelfAlign = AlignEnd
	}
	if n := len(shaped.Lines); n > 0 {
		blockAttrs.Padding[PAD_BOTTOM] = shaped.Lines[n-1].descenderPad
		blockAttrs.Padding[PAD_TOP] = blockAttrs.Padding[PAD_BOTTOM]
	}

	// A single line with no spans and no selection — the overwhelmingly
	// common label — folds the line onto the block: one container instead of
	// two. The line container otherwise only contributes its min box (the
	// first line has zero leading) plus decoration bands, absent here. Same
	// outer geometry: glyphs at y = descender pad via the block's top pad.
	if len(shaped.Lines) == 1 && len(spans) == 0 && selectionFrom == selectionTo {
		line := &shaped.Lines[0]
		lineEm := line.lineEm
		if lineEm <= 0 {
			lineEm = style.FontSize
		}
		blockAttrs.Row = true
		if shaped.BaseDir == RTL {
			blockAttrs.MainAlign = AlignEnd
		}
		blockAttrs.MinSize[0] = line.Width
		// MinSize is the outer box: em plus the symmetric descender pads.
		blockAttrs.MinSize[1] = lineEm + 2*blockAttrs.Padding[PAD_TOP]
		Container(blockAttrs, func() {
			ui.current.accessText = source
			ui.current.textRunWidth = line.Width
			ui.current.textRunEm = line.maxEm
			ui.current.glyphData = line.runData
			ui.current.glyphRunColor = style.TextColor
		})
		return
	}

	var nextLinePaddingTop float32 // to manage spaces between lines

	Container(blockAttrs, func() {
		ui.current.accessText = source
		for idx := range shaped.Lines {
			line := &shaped.Lines[idx]
			shapedTextLineLayoutColor(line, style, spans, shaped.BaseDir, selectionFrom, selectionTo, &nextLinePaddingTop, selectionColor)
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

// Text renders a run of text as a leaf of the current container.
// style is a fully resolved paragraph base — usually TextStyle(mods...) so the
// current container text style is the starting point. spans are optional range
// styles resolved against that same base (see Span).
//
// Soft-wrap width is the current container's content-box max width: MaxSize[0]
// minus horizontal padding (including a MaxSize cascaded from an ancestor).
// Zero MaxSize means unconstrained (no soft wrap). Matches TextInput and the
// MaxSize cascade peel for children.
//
// Label is the convenience for current text style + call-local mods with no spans.
func Text(label string, style TextStyleAttrs, spans ...TextSpan) {
	text(label, style, false, spans...)
}

// DecorativeText draws text without exposing its characters to assistive
// technology. Icon fonts use this; the surrounding control supplies a label.
func DecorativeText(label string, style TextStyleAttrs) {
	text(label, style, true)
}

func text(label string, style TextStyleAttrs, decorative bool, spans ...TextSpan) {
	// For performance reasons, do not accept text larger than 16kb
	// We will add a segmented text view in the future to handle large text blobs
	label = SafeTruncateUTF8(label, 16*1024)

	var maxWidth float32
	if ui.current != nil && ui.current.MaxSize[0] > 0 {
		maxWidth = ui.current.MaxSize[0] - PadSize(ui.current.Padding)[0]
		if maxWidth < 0 {
			maxWidth = 0
		}
	}
	// Resolve + flatten spans once; shaping and layout share the result.
	var flat []StyleSpan
	if len(spans) > 0 {
		flat = effectiveSpans(style, resolveTextSpans(style, spans), utf8.RuneCountInString(label))
	}
	shaped := shapeTextMaxFlat(label, style, maxWidth, flat)
	source := label
	if decorative {
		source = ""
	}
	shapedTextLayoutFlat(shaped, style, 0, 0, flat, source, SelectionColor)
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
	// descenderDepth is ink below the baseline at this segment's em, from
	// the style's primary face (not the coverage/fallback face). Line pad
	// uses the max over the last line instead of GetFace per glyph.
	descenderDepth float32
}

type Glyph struct {
	FontId   FontId
	GlyphId  GlyphId
	Cluster  int32
	Offset   Vec2
	XAdvance float32
	Width    float32
	// Scale float32
}

// sharedShapeBuffer is the one HarfBuzz buffer all shaping goes through.
// HarfBuzz's shape-plan cache (the compiled OpenType feature map for a
// face+script+direction+language combination) lives ON the buffer, so a
// fresh buffer per segment recompiles the plan every time — measured at
// ~35% of the entire shaping cost. Shaping is already single-threaded
// (res.hbfonts is accessed unsynchronized under the same assumption).
var sharedShapeBuffer = harfbuzz.NewBuffer()

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
	// Line box from the style's primary face. Coverage/fallback faces
	// (Arabic, emoji, CJK, …) keep their own advances and outlines; their
	// hhea extents must not inflate pad or inter-line height.
	metricsFace := face
	if props.metrics != 0 && props.metrics != fontId {
		if mf := GetFace(props.metrics); mf.InvUPM > 0 {
			metricsFace = mf
		}
	}
	s.descenderDepth = faceDescenderDepth(metricsFace.InvUPM, metricsFace.Descender, props.size)

	buf := sharedShapeBuffer
	buf.Clear()

	buf.AddRunes(text, start, length)
	buf.Props.Script = props.sc
	buf.Props.Direction = harfbuzz.LeftToRight + harfbuzz.Direction(props.Dir)
	buf.Props.Language = "en-EN"

	// this could set language to utf-8 which would *crash* the language parser!!
	// buf.GuessSegmentProperties() // this seems to just set the default locale language; regardless of content!

	font := res.hbfonts[fontId]
	if font == nil {
		ttf := GetParsedFont(fontId)
		if ttf == nil {
			return s
		}
		font = harfbuzz.NewFont(ttf)
		res.hbfonts[fontId] = font
		// TODO use lru cache instead of map?
	} else {
		touchFont(fontId)
	}

	buf.Shape(font, nil)

	scaleFactor := face.InvUPM * props.size

	s.Height = metricsFace.InvUPM * (metricsFace.Ascender - metricsFace.Descender) * props.size

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
			FontId:   fontId,
			GlyphId:  inf.Glyph,
			Cluster:  int32(inf.Cluster),
			Offset:   Vec2{float32(pos.XOffset) * scaleFactor, float32(pos.YOffset) * scaleFactor},
			XAdvance: xAdvance,
			Width:    width,
		})
		// width is accumulated xadvances
		// only the last item we should take the max of width and xadvance but
		// for now we don't bother. we'll look into this if it proves to be a
		// real problem
		s.Width += xAdvance
	}

	return s
}

func produceShapedSegments(runes []rune, dirs []Direction, base TextStyleAttrs, spans []StyleSpan) []GlyphsSegment {
	var allSegments = make([]GlyphsSegment, 0, len(runes)/2)

	var lineNo int

	getSegmentProps := func(i int) GlyphSegmentProps {
		ch := runes[i]
		st := styleAt(base, spans, i)
		ids, primary := st.fontFamilies.resolve(st.FontAspect)
		if primary == 0 {
			primary, _ = findMatchingFontAndGlyph(' ', ids, st.FontAspect)
		}
		fontChar := ch
		if ch == '\n' {
			// A hard break is structural and has no drawable glyph, but its
			// line still needs the same face metrics as ordinary text.
			fontChar = ' '
		}
		font, _ := findMatchingFontAndGlyph(fontChar, ids, st.FontAspect)
		return GlyphSegmentProps{
			font:    font,
			metrics: primary,
			size:    st.FontSize,
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

func lineBreakShapedSegments(allSegments []GlyphsSegment, style TextStyleAttrs, maxWidth float32) []ShapedTextLine {
	lineHeight := func(height float32) float32 {
		if height <= 0 {
			return style.FontSize
		}
		return height
	}

	// break segments into lines
	var lines []ShapedTextLine
	{
		var prevLineNo int // first segment always has line number set to 0
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
					Height:   lineHeight(height),
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
			Height:   lineHeight(height),
		})
		if allSegments[len(allSegments)-1].EndsWithNewline {
			lines = append(lines, ShapedTextLine{
				Height: lineHeight(height),
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

	for i := range lines {
		line := &lines[i]
		line.descenderPad = descenderPadForLine(line, style)
		line.firstCluster = lineFirstCluster(line)

		lineEm := style.FontSize
		n := 0
		for _, s := range line.Segments {
			if s.size > lineEm {
				lineEm = s.size
			}
			n += len(s.Glyphs)
		}
		if lineEm <= 0 {
			lineEm = style.FontSize
		}
		line.lineEm = lineEm

		stamps := make([]glyphStamp, 0, n)
		runs := make([]GlyphRun, 0, n)
		var x float32
		var maxEm float32
		for _, s := range line.Segments {
			em := s.size
			if em <= 0 {
				em = lineEm
			}
			shift := baselineShiftY(lineEm, em)
			for j := range s.Glyphs {
				g := &s.Glyphs[j]
				stamps = append(stamps, glyphStamp{Advance: g.XAdvance, Cluster: g.Cluster})
				runs = append(runs, GlyphRun{
					Rect:        Rect{Origin: Vec2{x, 0}, Size: Vec2{g.XAdvance, em}},
					FontId:      g.FontId,
					GlyphId:     g.GlyphId,
					GlyphOffset: Vec2{g.Offset[0], g.Offset[1] + shift},
				})
				x += g.XAdvance
				if em > maxEm {
					maxEm = em
				}
			}
		}
		line.stamps = stamps
		line.runs = runs
		line.runData = ownGlyphRunData(runs)
		line.maxEm = maxEm
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
	metrics FontId // style primary face; line box (Height, descenderDepth)
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
	// descenderPad is extra block padding so last-line ink stays inside a
	// clipped ancestor. Filled at shape time from segment face extents.
	descenderPad float32
	// firstCluster is the smallest rune index on the line, or -1 if empty.
	// Filled at shape time; layout uses it for selection leading.
	firstCluster int
	// lineEm is the line box em (max shaping size on the line).
	lineEm float32
	// runs are the line's paint geometry precomputed at shape time:
	// line-relative GlyphRuns (origin = advance accumulation, zero Color —
	// the shape-cache key does not include render-tier color). runData owns
	// this slice and its cached hash/dependencies. stamps carry the per-glyph
	// advance + cluster for decoration bands and span/selection mapping.
	// maxEm is the tallest glyph em — the emitted run surface height.
	runs    []GlyphRun
	runData *GlyphRunData
	stamps  []glyphStamp
	maxEm   float32
}

// unwrappedShaped is HarfBuzz output before wrap: segments in logical
// order (lineBreak may reverse copies). Cached without maxWidth.
type unwrappedShaped struct {
	Runes    []rune
	BaseDir  Direction
	Segments []GlyphsSegment
}

// ShapeStats counts ShapeText invocations vs cache hits — the diagnostic
// for shape-cache effectiveness. Hits is a wrap-cache hit (no wrap, no
// HarfBuzz). ShapeHits is an unwrapped-cache hit (no HarfBuzz; wrap may
// still run). In steady state Hits should track Calls. Pinned by
// see_pprof's TestShapeCacheSteadyState.
var ShapeStats struct {
	Calls     int64
	Hits      int64
	ShapeHits int64
}

// ShapeText shapes text with no soft-wrap width (single long lines until
// hard breaks). Prefer ShapeTextMax when the wrap budget is known.
// style must be fully resolved — callers supply the base (no container cascade).
// spans are optional.
func ShapeText(text string, style TextStyleAttrs, spans ...TextSpan) ShapedText {
	return ShapeTextMax(text, style, 0, spans...)
}

// ShapeTextMax shapes text, soft-wrapping when maxWidth > 0. Use this for
// measurement outside layout (virtual-list item heights) and whenever the
// wrap budget is not the current container's MaxSize. style is explicit —
// offline measurement has no open container to read a style from.
func ShapeTextMax(text string, style TextStyleAttrs, maxWidth float32, spans ...TextSpan) ShapedText {
	// Resolve deferred span mods, then compose overlaps before cache key +
	// shaping so bold∩highlight stacks field deltas instead of last-full-style-wins.
	var flat []StyleSpan
	if len(spans) > 0 {
		flat = effectiveSpans(style, resolveTextSpans(style, spans), utf8.RuneCountInString(text))
	}
	return shapeTextMaxFlat(text, style, maxWidth, flat)
}

// shapeTextMaxFlat is ShapeTextMax after span resolution: flat must be
// flattened (effectiveSpans output — sorted, disjoint, full styles). Text
// resolves spans once and shares the result between shaping and layout.
func shapeTextMaxFlat(text string, style TextStyleAttrs, maxWidth float32, flat []StyleSpan) ShapedText {
	if len(text) == 0 {
		return ShapedText{}
	}
	ShapeStats.Calls++

	res.syncShapeCachesToEpoch()

	// Unwrapped key: paragraph + font request. No wrap width — HarfBuzz
	// does not depend on the column. Registry changes drop the LRUs
	// (syncShapeCachesToEpoch) rather than salting the key. Wrap key
	// adds quantized device-px width. Text contents, not string headers.
	// Render-tier span props stay out of both keys.
	uKey := hashUnwrappedShapeKey(text, style, flat)
	wPx := wrapWidthDevicePx(maxWidth)
	wKey := hashWrapKey(uKey, wPx)

	if cached, ok := res.shapeCache.Get(wKey); ok {
		ShapeStats.Hits++
		ShapeStats.ShapeHits++
		return cached
	}

	width := wrapWidthLogical(wPx)

	if u, ok := res.unwrappedCache.Get(uKey); ok {
		ShapeStats.ShapeHits++
		shaped := wrapUnwrapped(u, style, width)
		res.shapeCache.Set(wKey, shaped)
		return shaped
	}

	runes := []rune(text)
	dirs := ParagraphBidi(text)
	segs := produceShapedSegments(runes, dirs, style, flat)
	u := unwrappedShaped{
		Runes:    runes,
		BaseDir:  segs[0].Dir,
		Segments: segs,
	}
	res.unwrappedCache.Set(uKey, u)
	shaped := wrapUnwrapped(u, style, width)
	res.shapeCache.Set(wKey, shaped)
	return shaped
}

func wrapUnwrapped(u unwrappedShaped, style TextStyleAttrs, maxWidth float32) ShapedText {
	// lineBreakShapedSegments reverses RTL runs in place on the segment
	// slice. Clone so the unwrapped cache keeps logical order.
	segs := slices.Clone(u.Segments)
	return ShapedText{
		Runes:   u.Runes,
		BaseDir: u.BaseDir,
		Lines:   lineBreakShapedSegments(segs, style, maxWidth),
	}
}

func hashUnwrappedShapeKey(text string, style TextStyleAttrs, flat []StyleSpan) uint64 {
	var d xxhash.Digest
	d.Reset()
	d.WriteString(text)
	Hash(&d, &style.FontSize)
	Hash(&d, &style.FontAspect)
	hashFontFamilies(&d, style.fontFamilies)
	for _, sp := range flat {
		if fontShapeEqual(sp.Style, style) {
			continue
		}
		Hash(&d, &sp.From)
		Hash(&d, &sp.To)
		Hash(&d, &sp.Style.FontSize)
		Hash(&d, &sp.Style.FontAspect)
		hashFontFamilies(&d, sp.Style.fontFamilies)
	}
	return d.Sum64()
}

func hashWrapKey(shapeKey uint64, widthPx int) uint64 {
	var d xxhash.Digest
	d.Reset()
	Hash(&d, &shapeKey)
	Hash(&d, &widthPx)
	return d.Sum64()
}

// wrapWidthDevicePx rounds a logical wrap budget onto the device-pixel
// grid. Zero/negative means no wrap (single long lines).
func wrapWidthDevicePx(maxWidth float32) int {
	if maxWidth <= 0 {
		return 0
	}
	scale := ui.Host.WindowScale
	if scale <= 0 {
		scale = 1
	}
	px := int(maxWidth*scale + 0.5)
	if px < 1 {
		px = 1
	}
	return px
}

func wrapWidthLogical(px int) float32 {
	if px <= 0 {
		return 0
	}
	scale := ui.Host.WindowScale
	if scale <= 0 {
		scale = 1
	}
	return float32(px) / scale
}

func findMatchingFontAndGlyph(ch rune, fonts []FontId, aspect FontAspect) (FontId, GlyphId) {
	var fontId FontId
	var glyphId GlyphId
	for _, fid := range fonts {
		gid := LookupGlyph(fid, ch)
		if gid == 0 || GetFace(fid).colorPaintOnly {
			continue
		}
		fontId = fid
		glyphId = gid
		break
	}

	if fontId == 0 || glyphId == 0 {
		return FallbackFontFor(ch, aspect)
	}

	return fontId, glyphId
}

// ParagraphBidi returns per-rune direction for txt. A function of the
// string only; the unwrapped shape cache is the paragraph identity, so
// this is not cached on its own.
func ParagraphBidi(txt string) []Direction {
	out := make([]Direction, 0, len(txt))

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

	return out
}
