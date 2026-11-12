package shirei

import (
	"math"
	"testing"
)

func TestTabStopsThroughShapingAndWrapping(t *testing.T) {
	attrs := requireTextShaping(t)
	attrs.SetFontFamilies(Monospace...)
	attrs.FontSize = 13
	space := ShapeText(" ", attrs).Lines[0].Width
	const source = "a\tb\nabc\tb\nabcd\tb\n\t\tb\n//\tlabel"
	shaped := ShapeText(source, attrs)
	for i, columns := range []int{5, 5, 9, 9, 9} {
		line := shaped.Lines[i]
		if math.Abs(float64(line.Width-space*float32(columns))) > .01 {
			t.Fatalf("line %d width = %g, want %d spaces (%g)", i, line.Width, columns, space*float32(columns))
		}
		var x float32
		for _, segment := range line.Segments {
			for _, glyph := range segment.Glyphs {
				x += glyph.XAdvance
				if shaped.Runes[glyph.Cluster] == '\t' {
					if glyph.FontId != ShapeText(" ", attrs).Lines[0].Segments[0].Glyphs[0].FontId {
						t.Fatal("tab uses a fallback font instead of the space's face")
					}
					stop := float64(x / (space * 4))
					if math.Abs(stop-math.Round(stop)) > .001 {
						t.Fatalf("tab at cluster %d ends between stops: %g", glyph.Cluster, stop)
					}
				}
			}
		}
	}
	// A tab that wraps starts at a fresh stop. Each width derives its own
	// advances without mutating either cached geometry or document clusters.
	const wrappedSource = "abcd\tx"
	unwrapped := ShapeText(wrappedSource, attrs)
	wrapped := ShapeTextMax(wrappedSource, attrs, space*5.5)
	if len(wrapped.Lines) != 2 || math.Abs(float64(wrapped.Lines[1].Width-space*5)) > .01 {
		t.Fatalf("wrapped tab placement: %+v", wrapped.Lines)
	}
	for _, width := range []float32{space * 10, 0} {
		again := ShapeTextMax(wrappedSource, attrs, width)
		if len(again.Lines) != 1 || math.Abs(float64(again.Lines[0].Width-space*9)) > .01 {
			t.Fatalf("cached tab placement at width %g: %+v", width, again.Lines)
		}
	}
	if unwrapped.Lines[0].Segments[1].Glyphs[0].XAdvance != space*4 || string(wrapped.Runes) != wrappedSource {
		t.Fatal("wrapping changes cached advances or document text")
	}
}

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
	if shaped.Lines[1].Height != shaped.Lines[0].Height {
		t.Errorf("empty hard line height = %.2f, filled line height = %.2f", shaped.Lines[1].Height, shaped.Lines[0].Height)
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
	if trailing.Lines[1].Height != trailing.Lines[0].Height {
		t.Errorf("trailing phantom line height = %.2f, filled line height = %.2f", trailing.Lines[1].Height, trailing.Lines[0].Height)
	}
}

func TestShapeCacheStampsAreGeometry(t *testing.T) {
	attrs := requireTextShaping(t)
	shaped := ShapeText("Hello", attrs)
	line := shaped.Lines[0]
	if len(line.stamps) == 0 || len(line.runs) == 0 {
		t.Fatal("expected stamps and runs at shape time")
	}
	n := 0
	for _, seg := range line.Segments {
		n += len(seg.Glyphs)
	}
	if len(line.stamps) != n || len(line.runs) != n {
		t.Fatalf("stamps=%d runs=%d glyphs=%d", len(line.stamps), len(line.runs), n)
	}
	for i, r := range line.runs {
		// Cached runs must stay colorless: the shape-cache key excludes
		// render-tier color, so a baked color would leak across entries.
		if r.Color != (Vec4{}) {
			t.Fatalf("run[%d] color = %v, want zero", i, r.Color)
		}
		if r.FontId == 0 || r.GlyphId == 0 {
			t.Fatalf("run[%d] missing font/glyph", i)
		}
	}
	if line.lineEm != attrs.FontSize {
		t.Fatalf("lineEm=%v want %v", line.lineEm, attrs.FontSize)
	}
}

func TestShapeCacheStampsDoNotLeakColor(t *testing.T) {
	requireTextShaping(t)
	red := Vec4{0, 80, 50, 1}
	blue := Vec4{210, 80, 50, 1}
	ui.Host.WindowSize = Vec2{240, 100}
	ui.Host.WindowScale = 1
	sid := softScope("shape_stamp_color")
	var out FrameOutputData
	for range 2 {
		out = RunFrameFn(func() {
			ModAttrs(func(a *AttrSet) { a.Animations = 0 })
			ContainerWithKey(sid, AttrSet{}, func() {
				Label("Hello", TextColor(red[0], red[1], red[2], red[3]))
				Label("Hello", TextColor(blue[0], blue[1], blue[2], blue[3]))
			})
		})
	}
	var sawRed, sawBlue bool
	for _, s := range out.Surfaces {
		for i := 0; i < int(s.GlyphRunCount); i++ {
			g := s.GlyphRunAt(i, out.GlyphRuns)
			if g.Color == red {
				sawRed = true
			}
			if g.Color == blue {
				sawBlue = true
			}
		}
	}
	if !sawRed || !sawBlue {
		t.Fatalf("glyph colors missing or leaked: red=%v blue=%v runs=%d", sawRed, sawBlue, len(out.GlyphRuns))
	}
}

func TestInternFamilyList(t *testing.T) {
	var a, b TextStyleAttrs
	a.SetFontFamilies("Menlo", "Monaco")
	b.SetFontFamilies("menlo", "MONACO")
	if a.fontFamilies == nil || a.fontFamilies != b.fontFamilies {
		t.Fatal("equal names (any case) intern to one list")
	}
	if a.fontFamilies.id == 0 {
		t.Fatal("non-empty list must not be id 0")
	}

	var c TextStyleAttrs
	c.fontFamilies = emptyFamilyList
	Fonts("Menlo", "Monaco")(&c)
	if c.fontFamilies != a.fontFamilies {
		t.Fatal("Fonts intern matches SetFontFamilies")
	}

	var d TextStyleAttrs
	d.SetFontFamilies("Noto Sans")
	Fonts("Menlo")(&d)
	var e TextStyleAttrs
	e.SetFontFamilies("Menlo", "Noto Sans")
	if d.fontFamilies != e.fontFamilies {
		t.Fatal("Fonts prepend intern")
	}

	var empty TextStyleAttrs
	empty.SetFontFamilies()
	if empty.fontFamilies != emptyFamilyList {
		t.Fatal("empty list is the singleton")
	}
}

// Shape-cache keys hash interned family-list ids. Two SetFontFamilies
// clones (including different case) must hit; a different name must miss
// even if both LookupFace answers are 0.
func TestShapeCacheKeyHashesFamilyNames(t *testing.T) {
	attrs := requireTextShaping(t)
	const text = "shape-cache-family-names"

	a := attrs
	a.SetFontFamilies("Menlo", "Monaco")
	b := attrs
	b.SetFontFamilies("menlo", "MONACO")

	ShapeStats.Calls = 0
	ShapeStats.Hits = 0
	_ = ShapeText(text, a)
	_ = ShapeText(text, b)
	if ShapeStats.Calls != 2 || ShapeStats.Hits != 1 {
		t.Fatalf("interned family lists: calls=%d hits=%d (want 2/1)", ShapeStats.Calls, ShapeStats.Hits)
	}

	c := attrs
	c.SetFontFamilies("DefinitelyNotARegisteredFamilyXXXX")
	_ = ShapeText(text, c)
	if ShapeStats.Hits != 1 {
		t.Fatalf("different family names must miss, hits=%d", ShapeStats.Hits)
	}
}

func TestShapeCacheMissesOnEpochBump(t *testing.T) {
	attrs := requireTextShaping(t)
	const text = "shape-cache-epoch-bump"

	ShapeStats.Calls = 0
	ShapeStats.Hits = 0
	_ = ShapeText(text, attrs)
	_ = ShapeText(text, attrs)
	if ShapeStats.Hits != 1 {
		t.Fatalf("want hit before bump, calls=%d hits=%d", ShapeStats.Calls, ShapeStats.Hits)
	}

	faceRegistryMu.Lock()
	res.fontLookupEpoch++
	faceRegistryMu.Unlock()
	t.Cleanup(func() {
		faceRegistryMu.Lock()
		res.fontLookupEpoch--
		faceRegistryMu.Unlock()
	})

	_ = ShapeText(text, attrs)
	if ShapeStats.Hits != 1 {
		t.Fatalf("epoch bump must miss, calls=%d hits=%d", ShapeStats.Calls, ShapeStats.Hits)
	}
	_ = ShapeText(text, attrs)
	if ShapeStats.Hits != 2 {
		t.Fatalf("want settle after wipe, calls=%d hits=%d", ShapeStats.Calls, ShapeStats.Hits)
	}
}

func TestShapeCacheSharesUnwrappedAcrossWidths(t *testing.T) {
	attrs := requireTextShaping(t)
	ui.Host.WindowScale = 1
	const text = "shape-cache-unwrapped-width-reuse ABCDEFGHIJKLMNOPQRSTUVWXYZ"

	ShapeStats.Calls = 0
	ShapeStats.Hits = 0
	ShapeStats.ShapeHits = 0
	a := ShapeTextMax(text, attrs, 40)
	b := ShapeTextMax(text, attrs, 4000)
	if ShapeStats.Calls != 2 {
		t.Fatalf("calls=%d", ShapeStats.Calls)
	}
	if ShapeStats.Hits != 0 {
		t.Fatalf("different wrap widths must miss the wrap cache, hits=%d", ShapeStats.Hits)
	}
	if ShapeStats.ShapeHits != 1 {
		t.Fatalf("second width should reuse unwrapped shape, ShapeHits=%d", ShapeStats.ShapeHits)
	}
	if len(a.Lines) <= len(b.Lines) {
		t.Fatalf("narrow wrap %d lines, wide wrap %d; want narrow > wide", len(a.Lines), len(b.Lines))
	}

	// 40.2 and 40.4 round to the same device px at scale 1.
	const qtext = "quantize-wrap-width-probe-XXXX"
	ShapeStats.Calls = 0
	ShapeStats.Hits = 0
	ShapeStats.ShapeHits = 0
	_ = ShapeTextMax(qtext, attrs, 40.2)
	_ = ShapeTextMax(qtext, attrs, 40.4)
	if ShapeStats.Hits != 1 || ShapeStats.ShapeHits != 1 {
		t.Fatalf("quantized widths: hits=%d shapeHits=%d (want 1/1)", ShapeStats.Hits, ShapeStats.ShapeHits)
	}
}
