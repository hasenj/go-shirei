package shirei

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestRubyShapingAndWrapping(t *testing.T) {
	waitForFontScan(t)
	st := TextStyleWith(DefaultTextStyle(), FontSize(24))
	source := "前" + Ruby("日本", "にほん") + "後"
	shaped := ShapeText(source, st, Span(4, 8, FontSize(30)))
	if string(shaped.Runes) != source {
		t.Fatal("shaping changes source text")
	}
	var group *GlyphsSegment
	for i := range shaped.Lines[0].Segments {
		s := &shaped.Lines[0].Segments[i]
		if len(s.ruby) > 0 {
			group = s
		}
	}
	if group == nil || len(group.Glyphs) == 0 || len(group.ruby[0].Glyphs) == 0 {
		t.Fatal("missing base or reading")
	}
	if group.rubyTop <= 0 || group.Width <= 0 {
		t.Fatal("missing annotation geometry")
	}
	for _, g := range group.Glyphs {
		if g.Cluster != 1 {
			t.Fatal("ruby group does not preserve its source boundary")
		}
	}
	wrapped := ShapeTextMax(source, st, group.Width, Span(4, 8, FontSize(30)))
	if len(wrapped.Lines) < 2 {
		t.Fatal("width budget does not wrap")
	}
	count := 0
	for _, l := range wrapped.Lines {
		for _, s := range l.Segments {
			if len(s.ruby) > 0 {
				count++
				if len(s.Glyphs) != len(group.Glyphs) {
					t.Fatal("ruby group splits at wrap")
				}
			}
		}
	}
	if count != 1 {
		t.Fatalf("ruby group occurs %d times", count)
	}
	cached := ShapeText(source, st, Span(4, 8, FontSize(30)))
	if !slices.Equal(cached.Runes, shaped.Runes) {
		t.Fatal("cache changes source")
	}
	for _, bad := range []string{"\uFFF9日本\uFFFAにほん", Ruby("", "a"), Ruby("a", ""), Ruby("a", "b\nc"), Ruby("a", Ruby("b", "c"))} {
		sh := ShapeText(bad, st)
		for _, l := range sh.Lines {
			for _, s := range l.Segments {
				if len(s.ruby) > 0 {
					t.Fatalf("unsupported block is interpreted: %q", bad)
				}
			}
		}
	}
}

func TestRubyMixedSizeLineGeometry(t *testing.T) {
	waitForFontScan(t)
	st := TextStyleWith(DefaultTextStyle(), FontSize(24))
	text := Ruby("字", "ながいよみ") + "大\n" + Ruby("字", "じ")
	largeAt := len([]rune(Ruby("字", "ながいよみ")))
	sh := ShapeText(text, st, Span(largeAt, largeAt+1, FontSize(60)), Span(4, 6, FontSize(50)))
	if len(sh.Lines) != 2 {
		t.Fatalf("lines=%d, want 2", len(sh.Lines))
	}
	for _, line := range sh.Lines {
		if line.Height < line.lineEm {
			t.Fatalf("line height %.2f cannot contain ruby line em %.2f", line.Height, line.lineEm)
		}
		if line.maxEm < line.lineEm {
			t.Fatal("paint surface clips ruby line")
		}
		if len(line.runs) != len(line.stamps) {
			t.Fatal("style mapping misses reading glyphs")
		}
		var advance float32
		for _, stamp := range line.stamps {
			advance += stamp.Advance
		}
		if advance != line.Width {
			t.Fatalf("reading changes interaction width: %.2f versus %.2f", advance, line.Width)
		}
		baseGlyphs := 0
		for _, seg := range line.Segments {
			baseGlyphs += len(seg.Glyphs)
		}
		if len(line.runs) <= baseGlyphs {
			t.Fatal("reading does not paint")
		}
	}
}

func TestRubyCenteringPreservesBaseGlyphSpacing(t *testing.T) {
	waitForFontScan(t)
	st := TextStyleWith(DefaultTextStyle(), FontSize(24))
	plain := ShapeText("勉強", st)
	prefix := ShapeText("前", st).Lines[0].Width
	annotated := ShapeText("前"+Ruby("勉強", "べんきょう"), st)
	base := plain.Lines[0].runs
	ruby := annotated.Lines[0].runs[1:]
	if len(base) < 2 || len(ruby) <= len(base) {
		t.Fatal("missing base or reading glyphs")
	}
	pad := (annotated.Lines[0].Width - prefix - plain.Lines[0].Width) * .5
	if pad <= 0 {
		t.Fatal("reading does not exercise wider-group centering")
	}
	for i, glyph := range base {
		before := glyph.Rect.Origin[0] + glyph.GlyphOffset[0]
		after := ruby[i].Rect.Origin[0] + ruby[i].GlyphOffset[0] - prefix
		if math.Abs(float64(after-before-pad)) > .001 {
			t.Fatalf("base glyph %d moves %.3f, want common centering shift %.3f", i, after-before, pad)
		}
	}
}

func TestRubyOverhangLayout(t *testing.T) {
	waitForFontScan(t)
	st := TextStyleWith(DefaultTextStyle(), FontSize(24))
	source := "を" + Ruby("勉強", "べんきょう") + "しています"
	plain := ShapeText("を勉強しています", st)
	shaped := ShapeText(source, st)
	if math.Abs(float64(plain.Lines[0].Width-shaped.Lines[0].Width)) > .01 {
		t.Fatal("ordinary kana neighbors do not absorb the reading's extra width")
	}
	long := Ruby("字", "ながいながいよみ")
	samples := []struct {
		text  string
		spans []TextSpan
	}{
		{source, nil},
		{long + "を" + long, nil},
		{long + "を" + long, []TextSpan{Span(len([]rune(long)), len([]rune(long))+1, FontSize(10))}},
		{"あ" + long + long + "い", nil},
		{long + "\u200B" + long, nil},
		{long + "\u200B\u200B" + long, nil},
		{long + "\u200B" + long, []TextSpan{Span(len([]rune(long))+4, len([]rune(long))+8, FontSize(36))}},
		{long + "\n" + long + "し", nil},
		{"あ" + long + "い", []TextSpan{Span(0, 1, FontSize(60))}},
		{Ruby("勉強", "べんきょう") + "しています", nil},
	}
	for _, sample := range samples {
		for _, width := range []float32{0, 24, 35, 48, 59.99, 60, 60.01, 72, 96, 110, 128, 144, 180, 240} {
			sh := ShapeTextMax(sample.text, st, width, sample.spans...)
			if string(sh.Runes) != sample.text {
				t.Fatal("overhang changes source indices")
			}
			for _, line := range sh.Lines {
				var x, end, previousEm float32
				for i, seg := range line.Segments {
					if len(seg.ruby) > 0 {
						left, right := x+seg.rubyX, x+seg.rubyX+seg.rubyWidth
						if left < -0.001 || right > line.Width+.001 {
							t.Fatalf("reading escapes line at width %.2f: [%.2f,%.2f] in %.2f for %q", width, left, right, line.Width, sample.text)
						}
						var gap float32
						if i > 0 && len(line.Segments[i-1].ruby) == 0 {
							gap = .5 * max(previousEm, seg.rubyEm)
						}
						if previousEm > 0 && left < end+gap-.001 {
							t.Fatalf("reading gap is too small at width %.2f: %.2f for %q", width, left-end, sample.text)
						}
						end, previousEm = right, seg.rubyEm
					}
					var advance float32
					for _, glyph := range seg.Glyphs {
						advance += glyph.XAdvance
					}
					if math.Abs(float64(advance-seg.Width)) > .001 {
						t.Fatal("paint and caret advances disagree")
					}
					x += seg.Width
				}
			}
		}
	}
	// A rewrap owns its placements, while the shared natural shape stays intact.
	runes := []rune(source)
	segments := produceRubySegments(runes, st, nil)
	saved := make([][]Glyph, len(segments))
	for i := range segments {
		saved[i] = slices.Clone(segments[i].Glyphs)
	}
	u := unwrappedShaped{Runes: runes, Segments: segments, BaseDir: LTR}
	wide := wrapUnwrapped(u, st, 400)
	placed := make([][]Glyph, len(wide.Lines[0].Segments))
	for i, segment := range wide.Lines[0].Segments {
		placed[i] = slices.Clone(segment.Glyphs)
	}
	wrapUnwrapped(u, st, 60)
	for i, segment := range wide.Lines[0].Segments {
		if !slices.Equal(placed[i], segment.Glyphs) {
			t.Fatal("rewrap changes previously returned glyph positions")
		}
	}
	for i := range segments {
		if !slices.Equal(saved[i], segments[i].Glyphs) {
			t.Fatal("rewrap mutates natural glyphs")
		}
	}
}

func TestRubyAnnotationBoundaries(t *testing.T) {
	waitForFontScan(t)
	st := TextStyleWith(DefaultTextStyle(), FontSize(24))
	for _, word := range []struct{ base, first, second string }{
		{"方角", "ほう", "がく"},
		{"特別", "とく", "べつ"},
	} {
		base := []rune(word.base)
		first := Ruby(string(base[0]), word.first)
		second := Ruby(string(base[1]), word.second)
		plain := ShapeText(word.base, st)
		for _, separator := range []string{"", "\u200B", "\u200B\u200B", " ", "と", "・"} {
			t.Run(word.base+fmt.Sprintf("/%q", separator), func(t *testing.T) {
				source := first + separator + second
				shaped := ShapeText(source, st)
				line := shaped.Lines[0]
				plainWidth := ShapeText(string(base[0])+separator+string(base[1]), st).Lines[0].Width
				if separator == "" && math.Abs(float64(line.Width-plainWidth)) > .001 {
					t.Fatalf("width %.3f, want unannotated width %.3f", line.Width, plainWidth)
				}
				if separator != "" {
					left, right := line.Segments[0], line.Segments[len(line.Segments)-1]
					gap := line.Width - right.Width + right.rubyX - (left.rubyX + left.rubyWidth)
					if gap < 6-.001 || line.Width > plainWidth+6+.001 || line.Width < plainWidth-.001 {
						t.Fatalf("reading gap %.3f, width %.3f, unannotated width %.3f", gap, line.Width, plainWidth)
					}
					if plainWidth == plain.Lines[0].Width && math.Abs(float64(gap-6)) > .001 {
						t.Fatalf("zero-width separator produces gap %.3f, want 6", gap)
					}
				}
				if string(shaped.Runes) != source {
					t.Fatal("layout changes source text")
				}
				if separator == "" {
					// Base ink retains the same spacing as the unannotated word.
					var x float32
					for i, seg := range line.Segments {
						g := plain.Lines[0].runs[i]
						want := g.Rect.Origin[0] + g.GlyphOffset[0]
						if math.Abs(float64(x+seg.Glyphs[0].Offset[0]-want)) > .001 {
							t.Fatal("annotation separates base characters")
						}
						x += seg.Width
					}
				}
				wrapped := ShapeTextMax(source, st, plain.Lines[0].Width)
				wantLines := 2
				if separator == "" {
					wantLines = 1
				}
				if len(wrapped.Lines) != wantLines {
					t.Fatalf("lines=%d, want %d", len(wrapped.Lines), wantLines)
				}
				last := wrapped.Lines[len(wrapped.Lines)-1]
				if separator != "" && math.Abs(float64(last.Width-plain.Lines[0].Width*.5)) > .001 {
					t.Fatal("reading clearance carries across a line break")
				}
			})
		}
	}
}

func TestRubyOverhangFallback(t *testing.T) {
	waitForFontScan(t)
	st := TextStyleWith(DefaultTextStyle(), FontSize(24))
	for _, source := range []string{"A" + Ruby("勉強", "べんきょう") + "B", "前" + Ruby("勉強", "べんきょう") + "後", "عربي を" + Ruby("勉強", "べんきょう") + "し"} {
		sh := ShapeText(source, st)
		for _, line := range sh.Lines {
			for _, seg := range line.Segments {
				if len(seg.ruby) > 0 && seg.Width != max(seg.rubyBaseWidth, seg.rubyWidth) {
					t.Fatalf("unsupported neighbor/direction borrows space in %q", source)
				}
			}
		}
	}
}

// Shape once, then measure wrapping and paint geometry at increasing sizes.
func BenchmarkRubyWrap(b *testing.B) {
	InitFontSubsystem()
	for !SystemFontScanDone() {
		time.Sleep(time.Millisecond)
	}
	st := TextStyleWith(DefaultTextStyle(), FontSize(24))
	for _, count := range []int{100, 1000, 10000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			runes := []rune(strings.Repeat("を"+Ruby("勉強", "べんきょう")+"し ", count))
			u := unwrappedShaped{Runes: runes, Segments: produceRubySegments(runes, st, nil), BaseDir: LTR}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				wrapUnwrapped(u, st, 480)
			}
		})
	}
}

func TestRubyLineStartAlignment(t *testing.T) {
	waitForFontScan(t)
	st := TextStyleWith(DefaultTextStyle(), FontSize(24))
	first := Ruby("鶯", "うぐいす")
	text := first + "と" + Ruby("梟", "ふくろう")
	for _, width := range []float32{0, 48, 60, 96, 144} {
		for _, kanaSize := range []float32{24, 12} {
			at := len([]rune(first))
			shaped := ShapeTextMax(text, st, width, Span(at, at+1, FontSize(kanaSize)))
			for _, line := range shaped.Lines {
				segment := line.Segments[0]
				if len(segment.ruby) == 0 {
					continue
				}
				base := []rune("鶯")
				if segment.Glyphs[0].Cluster > 0 {
					base = []rune("梟")
				}
				plain := ShapeText(string(base), st).Lines[0].runs[0]
				painted := line.runs[0]
				if math.Abs(float64(painted.Rect.Origin[0]+painted.GlyphOffset[0]-plain.GlyphOffset[0])) > .001 {
					t.Fatal("line-start base is indented")
				}
				if segment.rubyX < 0 {
					t.Fatal("line-start reading hangs outside margin")
				}
			}
		}
	}
}
