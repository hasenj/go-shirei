package shirei

import (
	"slices"
	"testing"

	g "go.hasen.dev/generic"
)

func TestStyleAtLastWins(t *testing.T) {
	base := DefaultTextStyle()
	base.TextColor = Vec4{0, 0, 0, 1}

	red := base
	red.TextColor = Vec4{0, 80, 50, 1}
	blue := base
	blue.TextColor = Vec4{210, 80, 50, 1}

	spans := []StyleSpan{
		{From: 0, To: 10, Style: red},
		{From: 5, To: 15, Style: blue}, // overlaps; phase 1 last wins
	}

	if got := styleAt(base, spans, 3); got.TextColor != red.TextColor {
		t.Fatalf("index 3 color = %v, want red", got.TextColor)
	}
	if got := styleAt(base, spans, 7); got.TextColor != blue.TextColor {
		t.Fatalf("index 7 color = %v, want blue (last wins)", got.TextColor)
	}
	if got := styleAt(base, spans, 12); got.TextColor != blue.TextColor {
		t.Fatalf("index 12 color = %v, want blue", got.TextColor)
	}
	if got := styleAt(base, spans, 20); got.TextColor != base.TextColor {
		t.Fatalf("index 20 color = %v, want base", got.TextColor)
	}
}

func TestResolveStyleRunsDisjoint(t *testing.T) {
	base := DefaultTextStyle()
	bold := base
	bold.Weight = WeightBold
	hi := base
	hi.Background = Vec4{50, 80, 70, 0.5}

	// non-overlapping
	spans := []StyleSpan{
		{From: 2, To: 5, Style: bold},
		{From: 8, To: 10, Style: hi},
	}
	runs := resolveStyleRuns(base, spans, 12)
	want := []styleRun{
		{0, 2, base},
		{2, 5, bold},
		{5, 8, base},
		{8, 10, hi},
		{10, 12, base},
	}
	if len(runs) != len(want) {
		t.Fatalf("run count = %d, want %d: %+v", len(runs), len(want), runs)
	}
	for i := range want {
		if runs[i].From != want[i].From || runs[i].To != want[i].To {
			t.Fatalf("run[%d] range = [%d,%d), want [%d,%d)", i, runs[i].From, runs[i].To, want[i].From, want[i].To)
		}
		if !textStylesEqual(runs[i].Style, want[i].Style) {
			t.Fatalf("run[%d] style mismatch", i)
		}
	}
}

func TestResolveStyleRunsEmptySpans(t *testing.T) {
	base := DefaultTextStyle()
	runs := resolveStyleRuns(base, nil, 5)
	if len(runs) != 1 || runs[0].From != 0 || runs[0].To != 5 || !textStylesEqual(runs[0].Style, base) {
		t.Fatalf("got %+v", runs)
	}
}

func TestSpanBuilder(t *testing.T) {
	base := DefaultTextStyle()
	sp := resolveTextSpans(base, []TextSpan{
		Span(1, 4, FontWeight(WeightBold), TextColor(10, 80, 40, 1)),
	})[0]
	if sp.From != 1 || sp.To != 4 {
		t.Fatalf("range = [%d,%d)", sp.From, sp.To)
	}
	if sp.Style.Weight != WeightBold {
		t.Fatalf("weight = %v, want bold", sp.Style.Weight)
	}
	if sp.Style.TextColor != (Vec4{10, 80, 40, 1}) {
		t.Fatalf("color = %v", sp.Style.TextColor)
	}
	// base size preserved
	if sp.Style.FontSize != base.FontSize {
		t.Fatalf("size = %v, want %v", sp.Style.FontSize, base.FontSize)
	}
}

func TestResolveTextSpans(t *testing.T) {
	base := DefaultTextStyle()
	resolved := resolveTextSpans(base, []TextSpan{
		Span(0, 1, TextUnderline(true)),
	})
	if len(resolved) != 1 || !resolved[0].Style.Underline {
		t.Fatalf("spans = %+v", resolved)
	}
	// base is not mutated
	if base.Underline {
		t.Fatalf("resolve must not mutate base")
	}
}

func TestShapeTextColorOnlySpanCacheHit(t *testing.T) {
	attrs := requireTextShaping(t)
	text := "hello world"

	ShapeStats.Calls = 0
	ShapeStats.Hits = 0
	// cold
	_ = ShapeText(text, attrs)
	if ShapeStats.Calls != 1 || ShapeStats.Hits != 0 {
		t.Fatalf("cold: calls=%d hits=%d", ShapeStats.Calls, ShapeStats.Hits)
	}

	// same text, color-only span — must hit cache (render tier not in key)
	_ = ShapeText(text, attrs, Span(0, 5, TextColor(0, 80, 50, 1)))
	if ShapeStats.Calls != 2 || ShapeStats.Hits != 1 {
		t.Fatalf("color span: calls=%d hits=%d (want hit)", ShapeStats.Calls, ShapeStats.Hits)
	}

	// shaping-tier span must miss
	_ = ShapeText(text, attrs, Span(0, 5, FontWeight(WeightBold)))
	if ShapeStats.Calls != 3 {
		t.Fatalf("bold span calls=%d", ShapeStats.Calls)
	}
	// hit count unchanged if bold face exists and shapes differently, or +0 miss
	if ShapeStats.Hits != 1 {
		// bold might coincide with regular if no bold face — still must not falsely hit color key
		// If bold resolves to same font ids as regular, key may still match. Accept hit only if font shape equal.
		st := ResolveSpan(0, 5, attrs, FontWeight(WeightBold)).Style
		if !fontShapeEqual(st, attrs) && ShapeStats.Hits != 1 {
			t.Fatalf("bold span should miss when font shape differs; hits=%d", ShapeStats.Hits)
		}
	}
}

func TestShapeTextNilSpansUnchangedGeometry(t *testing.T) {
	attrs := requireTextShaping(t)
	text := "alpha beta"

	a := ShapeText(text, attrs)
	b := ShapeText(text, attrs) // no spans vs explicit empty — both no overlay
	if len(a.Lines) != len(b.Lines) {
		t.Fatalf("lines %d vs %d", len(a.Lines), len(b.Lines))
	}
	for i := range a.Lines {
		if a.Lines[i].Width != b.Lines[i].Width || a.Lines[i].Height != b.Lines[i].Height {
			t.Fatalf("line %d geometry differs", i)
		}
		if len(a.Lines[i].Segments) != len(b.Lines[i].Segments) {
			t.Fatalf("line %d segments %d vs %d", i, len(a.Lines[i].Segments), len(b.Lines[i].Segments))
		}
	}
}

func TestShapeTextColorSpanSameGeometry(t *testing.T) {
	attrs := requireTextShaping(t)
	text := "hello"

	plain := ShapeText(text, attrs)
	spanned := ShapeText(text, attrs, Span(1, 4, TextColor(120, 80, 40, 1)))

	if plain.Lines[0].Width != spanned.Lines[0].Width {
		t.Fatalf("width plain=%v spanned=%v", plain.Lines[0].Width, spanned.Lines[0].Width)
	}
	// same number of glyphs
	pg, sg := 0, 0
	for _, s := range plain.Lines[0].Segments {
		pg += len(s.Glyphs)
	}
	for _, s := range spanned.Lines[0].Segments {
		sg += len(s.Glyphs)
	}
	if pg != sg {
		t.Fatalf("glyph count plain=%d spanned=%d", pg, sg)
	}
}

func TestShapeTextSizeSpanWiderAdvances(t *testing.T) {
	attrs := requireTextShaping(t)
	text := "big"
	plain := ShapeText(text, attrs)

	spanned := ShapeText(text, attrs, Span(0, 3, FontSize(attrs.FontSize*2)))
	if spanned.Lines[0].Width <= plain.Lines[0].Width*1.2 {
		t.Fatalf("size span width=%v plain=%v; expected substantially wider advances",
			spanned.Lines[0].Width, plain.Lines[0].Width)
	}
	// shaping-tier size is on the segment props
	var maxSegSize float32
	for _, s := range spanned.Lines[0].Segments {
		if s.size > maxSegSize {
			maxSegSize = s.size
		}
	}
	if maxSegSize != attrs.FontSize*2 {
		t.Fatalf("segment size = %v, want %v", maxSegSize, attrs.FontSize*2)
	}
}

func TestGlyphEmSize(t *testing.T) {
	st := DefaultTextStyle()
	st.FontSize = 22
	if glyphEmSize(st, 12) != 22 {
		t.Fatal(glyphEmSize(st, 12))
	}
	st.FontSize = 0
	if glyphEmSize(st, 12) != 12 {
		t.Fatal(glyphEmSize(st, 12))
	}
}

func TestFlattenStyleSpansBoldAndHighlight(t *testing.T) {
	base := DefaultTextStyle()
	// "Make just this phrase bold" — bold [5,21), highlight "phrase" [15,21)
	// Use synthetic indices on a short string.
	textLen := 20
	bold := ResolveSpan(0, 15, base, FontWeight(WeightBold))
	hi := ResolveSpan(5, 20, base, TextBackground(50, 70, 85, 0.5))
	flat := flattenStyleSpans(base, []StyleSpan{bold, hi}, textLen)
	// expect [0,5) bold only, [5,15) bold+bg, [15,20) bg only
	if len(flat) != 3 {
		t.Fatalf("flat = %+v, want 3 spans", flat)
	}
	if flat[0].From != 0 || flat[0].To != 5 {
		t.Fatalf("atom0 range %v", flat[0])
	}
	if flat[0].Style.Weight != WeightBold || flat[0].Style.Background != (Vec4{}) {
		t.Fatalf("atom0 style weight/bg = %v %v", flat[0].Style.Weight, flat[0].Style.Background)
	}
	if flat[1].From != 5 || flat[1].To != 15 {
		t.Fatalf("atom1 range %v", flat[1])
	}
	if flat[1].Style.Weight != WeightBold || flat[1].Style.Background == (Vec4{}) {
		t.Fatalf("atom1 should be bold+bg: weight=%v bg=%v", flat[1].Style.Weight, flat[1].Style.Background)
	}
	if flat[2].From != 15 || flat[2].To != 20 {
		t.Fatalf("atom2 range %v", flat[2])
	}
	if flat[2].Style.Weight != base.Weight || flat[2].Style.Background == (Vec4{}) {
		t.Fatalf("atom2 should be bg only: weight=%v bg=%v", flat[2].Style.Weight, flat[2].Style.Background)
	}
}

func TestFlattenStyleSpansReverseOrder(t *testing.T) {
	base := DefaultTextStyle()
	hi := ResolveSpan(5, 20, base, TextBackground(50, 70, 85, 0.5))
	bold := ResolveSpan(0, 15, base, FontWeight(WeightBold))
	flat := flattenStyleSpans(base, []StyleSpan{hi, bold}, 20)
	// middle [5,15): bg then bold → both
	var mid *StyleSpan
	for i := range flat {
		if flat[i].From == 5 && flat[i].To == 15 {
			mid = &flat[i]
			break
		}
	}
	if mid == nil {
		t.Fatalf("no middle atom in %+v", flat)
	}
	if mid.Style.Weight != WeightBold || mid.Style.Background == (Vec4{}) {
		t.Fatalf("middle should stack both: %+v", mid.Style)
	}
}

func TestFlattenStyleSpansLaterColorWins(t *testing.T) {
	base := DefaultTextStyle()
	base.TextColor = Vec4{0, 0, 0, 1}
	a := ResolveSpan(0, 10, base, TextColor(0, 80, 50, 1))
	b := ResolveSpan(0, 10, base, TextColor(120, 80, 50, 1))
	flat := flattenStyleSpans(base, []StyleSpan{a, b}, 10)
	if len(flat) != 1 {
		t.Fatalf("got %+v", flat)
	}
	if flat[0].Style.TextColor != (Vec4{120, 80, 50, 1}) {
		t.Fatalf("color = %v", flat[0].Style.TextColor)
	}
}

func TestFlattenStyleSpansContained(t *testing.T) {
	base := DefaultTextStyle()
	outer := ResolveSpan(0, 10, base, FontWeight(WeightBold))
	inner := ResolveSpan(3, 7, base, TextColor(30, 90, 50, 1))
	flat := flattenStyleSpans(base, []StyleSpan{outer, inner}, 10)
	if len(flat) != 3 {
		t.Fatalf("contained flat=%+v", flat)
	}
	// middle has bold+color
	if flat[1].Style.Weight != WeightBold || flat[1].Style.TextColor == base.TextColor {
		t.Fatalf("inner atom: %+v", flat[1].Style)
	}
}

func TestFlattenStyleSpansAdjacent(t *testing.T) {
	base := DefaultTextStyle()
	a := ResolveSpan(0, 5, base, FontWeight(WeightBold))
	b := ResolveSpan(5, 10, base, TextColor(0, 80, 50, 1))
	flat := flattenStyleSpans(base, []StyleSpan{a, b}, 10)
	if len(flat) != 2 {
		t.Fatalf("adjacent: %+v", flat)
	}
}

func TestFlattenStyleSpansEmptyAndNoop(t *testing.T) {
	base := DefaultTextStyle()
	if flattenStyleSpans(base, nil, 10) != nil {
		t.Fatal("empty")
	}
	// span equal to base → no emit
	sp := StyleSpan{From: 0, To: 5, Style: base}
	if flat := flattenStyleSpans(base, []StyleSpan{sp}, 10); len(flat) != 0 {
		t.Fatalf("noop span should drop: %+v", flat)
	}
	// inverted
	if flat := flattenStyleSpans(base, []StyleSpan{{From: 5, To: 2, Style: base}}, 10); len(flat) != 0 {
		t.Fatalf("inverted: %+v", flat)
	}
}

func TestBaselineShiftY(t *testing.T) {
	if baselineShiftY(22, 22) != 0 {
		t.Fatal("uniform size must not shift")
	}
	if baselineShiftY(15, 22) != 0 {
		t.Fatal("glyph taller than line em must not shift")
	}
	got := baselineShiftY(22, 15)
	want := float32(glyphBaselineFrac * (22 - 15))
	if got != want {
		t.Fatalf("shift = %v, want %v", got, want)
	}
	// pen baselines meet: frac*15 + shift == frac*22
	small := float32(glyphBaselineFrac)*15 + got
	large := float32(glyphBaselineFrac) * 22
	if d := small - large; d > 1e-5 || d < -1e-5 {
		t.Fatalf("baselines do not meet: small=%v large=%v", small, large)
	}
}

func TestFontShapeEqual(t *testing.T) {
	a := DefaultTextStyle()
	b := a
	b.TextColor = Vec4{1, 2, 3, 1}
	b.Background = Vec4{4, 5, 6, 1}
	b.Underline = true
	if !fontShapeEqual(a, b) {
		t.Fatal("render-tier diffs must not affect fontShapeEqual")
	}
	b.Weight = WeightBold
	if fontShapeEqual(a, b) {
		t.Fatal("weight is shaping tier")
	}
	if !slices.Equal(a.FontFamilies, DefaultTextStyle().FontFamilies) {
		t.Fatal("sanity")
	}
}

func TestTextStyleCascade(t *testing.T) {
	def := DefaultTextStyle()

	ui.Host.WindowSize = Vec2{400, 300}
	scope := new(int)
	var childStyle TextStyleAttrs
	var nestedStyle TextStyleAttrs
	var afterReset TextStyleAttrs

	RunFrameFn(func() {
		ContainerWithKey(scope, AttrSet{}, func() {
			// root ui.current text style is non-zero default
			if g.IsZeroBytes(GetAttrs().TextStyle) {
				t.Fatal("root should have non-zero text style")
			}
			Container(Attrs(AmendTextStyle(FontSize(18), TextColor(10, 20, 30, 1))), func() {
				childStyle = GetAttrs().TextStyle
				Container(AttrSet{}, func() {
					nestedStyle = GetAttrs().TextStyle
				})
				Container(Attrs(SetTextStyle(def)), func() {
					afterReset = GetAttrs().TextStyle
				})
			})
		})
	})

	if childStyle.FontSize != 18 {
		t.Fatalf("amended size = %v, want 18", childStyle.FontSize)
	}
	if childStyle.TextColor != (Vec4{10, 20, 30, 1}) {
		t.Fatalf("amended color = %v", childStyle.TextColor)
	}
	if nestedStyle.FontSize != 18 || nestedStyle.TextColor != childStyle.TextColor {
		t.Fatalf("nested should wholesale inherit: %+v", nestedStyle)
	}
	if afterReset.FontSize != def.FontSize || afterReset.TextColor != def.TextColor {
		t.Fatalf("SetTextStyle should override style without inheriting: %+v", afterReset)
	}
}


