package shirei

import (
	"strings"
	"unicode"
)

const (
	RubyAnchor     = '\uFFF9'
	RubySeparator  = '\uFFFA'
	RubyTerminator = '\uFFFB'
)

// Ruby associates a reading with base text using Unicode interlinear markers.
// Text rendering places the reading above the base. Both strings must be
// nonempty and contain no annotation markers or paragraph breaks.
func Ruby(base, reading string) string {
	return string(RubyAnchor) + base + string(RubySeparator) + reading + string(RubyTerminator)
}

type rubyRange struct{ from, separator, to int }

// rubyRanges recognizes single, nonempty readings within one paragraph.
// Malformed, nested, and multipart annotation blocks remain literal text.
func rubyRanges(text []rune) []rubyRange {
	var out []rubyRange
	start, sep, depth := -1, -1, 0
	invalid := false
	for i, r := range text {
		switch r {
		case RubyAnchor:
			if depth == 0 {
				start, sep, invalid = i, -1, false
			} else {
				invalid = true
			}
			depth++
		case RubySeparator:
			if depth == 1 && sep < 0 {
				sep = i
			} else if depth > 0 {
				invalid = true
			}
		case RubyTerminator:
			if depth == 0 {
				continue
			}
			depth--
			if depth == 0 && !invalid && sep > start+1 && i > sep+1 {
				out = append(out, rubyRange{start, sep, i + 1})
			}
		case '\n', '\r', '\u2028', '\u2029':
			start, sep, depth, invalid = -1, -1, 0, false
		}
	}
	return out
}

func produceRubySegments(runes []rune, base TextStyleAttrs, spans []StyleSpan) []GlyphsSegment {
	ranges := rubyRanges(runes)
	if len(ranges) == 0 {
		return produceShapedSegments(runes, ParagraphBidi(string(runes)), base, spans)
	}
	// Bidi sees the main text stream; readings have their own direction runs.
	var main []rune
	var source []int
	at := 0
	for _, r := range ranges {
		for i := at; i < r.separator; i++ {
			if i != r.from {
				main = append(main, runes[i])
				source = append(source, i)
			}
		}
		at = r.to
	}
	for i := at; i < len(runes); i++ {
		main = append(main, runes[i])
		source = append(source, i)
	}
	mainDirs := ParagraphBidi(string(main))
	dirs := make([]Direction, len(runes))
	mainLTR := true
	for i, index := range source {
		dirs[index] = mainDirs[i]
		mainLTR = mainLTR && mainDirs[i] == LTR
	}
	shape := func(from, to int, reading bool) []GlyphsSegment {
		if from == to {
			return nil
		}
		style := base
		var local []StyleSpan
		for _, sp := range spans {
			a, b := max(from, sp.From), min(to, sp.To)
			if a < b {
				sp.From, sp.To = a-from, b-from
				if reading {
					sp.Style.FontSize *= .5
				}
				local = append(local, sp)
			}
		}
		d := dirs[from:to]
		if reading {
			style.FontSize *= .5
			d = ParagraphBidi(string(runes[from:to]))
		}
		segments := produceShapedSegments(runes[from:to], d, style, local)
		for i := range segments {
			for j := range segments[i].Glyphs {
				g := &segments[i].Glyphs[j]
				g.Cluster += int32(from)
				g.styleCluster = g.Cluster
				if !reading && segments[i].Dir == LTR {
					s := &segments[i]
					ch := runes[g.Cluster]
					kana := unicode.Is(unicode.Hiragana, ch) || unicode.Is(unicode.Katakana, ch) || ch == 'ー'
					if g.Cluster == s.Glyphs[0].Cluster && kana {
						s.rubyHeadRoom += g.XAdvance
					}
					if j == 0 || g.Cluster != s.Glyphs[j-1].Cluster {
						s.rubyTailRoom = 0
					}
					if kana {
						s.rubyTailRoom += g.XAdvance
					}
				}
			}
		}
		return segments
	}
	var out []GlyphsSegment
	at, lineNo := 0, 0
	appendPlain := func(to int) {
		pieces := shape(at, to, false)
		for i := range pieces {
			pieces[i].lineNo += lineNo
		}
		out = append(out, pieces...)
		lineNo += strings.Count(string(runes[at:to]), "\n")
	}
	for _, r := range ranges {
		appendPlain(r.from)
		bases := shape(r.from+1, r.separator, false)
		readings := shape(r.separator+1, r.to-1, true)
		group := GlyphsSegment{GlyphSegmentProps: bases[0].GlyphSegmentProps, ruby: readings}
		group.lineNo = lineNo
		group.rubyOverhang = mainLTR
		var baseWidth, readingWidth float32
		for _, s := range bases {
			baseWidth += s.Width
			group.Height = max(group.Height, s.Height)
			group.size = max(group.size, s.size)
			group.descenderDepth = max(group.descenderDepth, s.descenderDepth)
			for _, g := range s.Glyphs {
				g.em = s.size
				g.Cluster = int32(r.from)
				group.Glyphs = append(group.Glyphs, g)
			}
		}
		for _, s := range readings {
			readingWidth += s.Width
			group.rubyEm = max(group.rubyEm, s.size)
			group.rubyOverhang = group.rubyOverhang && s.Dir == LTR
			group.rubyTop = max(group.rubyTop, s.size+s.descenderDepth)
		}
		group.rubyTop += group.size * .05
		group.Width = max(baseWidth, readingWidth)
		group.Height += group.rubyTop
		group.rubyBaseWidth = baseWidth
		group.rubyWidth = readingWidth
		out = append(out, group)
		at = r.to
	}
	appendPlain(len(runes))
	return out
}

// RubyHeight is the maximum space reserved above a base line for ruby readings.
func (text ShapedText) RubyHeight() float32 {
	var height float32
	for _, line := range text.Lines {
		height = max(height, line.rubyTop)
	}
	return height
}
