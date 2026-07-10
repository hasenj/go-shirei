package main

import (
	"bytes"
	"regexp"
)

// Matcher decides whether text matches the query. It has two shapes:
//
//   - Literal (the default, and by far the common case): a byte-level search
//     built on bytes.Index (SIMD-accelerated), so the whole-file prefilter
//     costs memory bandwidth, not a regex-engine pass over every byte. This is
//     the single biggest speed lever — Go's regexp gives no fast literal path
//     for case-insensitive queries, so a literal search routed through it spent
//     ~all its time in the NFA.
//   - Regex (opt-in): a real *regexp.Regexp, honoring the same case/whole-word
//     toggles by rewriting the pattern.
//
// Case-insensitivity is ASCII-fold (fine for source code): we lowercase both
// the needle and the haystack, which keeps byte positions 1:1 so whole-word
// boundary checks still land on the original text. Whole-word uses ASCII word
// boundaries, matching Go's regexp \b so the two modes agree.
type Matcher struct {
	re        *regexp.Regexp // regex mode
	needle    []byte         // literal mode; ASCII-lowercased when fold
	fold      bool           // ASCII case-insensitive
	wholeWord bool
}

// buildMatcher compiles a query into a Matcher, or returns an error for an
// invalid regex.
func buildMatcher(p Params) (*Matcher, error) {
	if p.Regex {
		expr := p.Query
		if p.WholeWord {
			expr = `\b(?:` + expr + `)\b`
		}
		if !p.MatchCase {
			expr = `(?i)` + expr
		}
		re, err := regexp.Compile(expr)
		if err != nil {
			return nil, err
		}
		return &Matcher{re: re}, nil
	}

	needle := []byte(p.Query)
	fold := !p.MatchCase
	if fold {
		needle = asciiLowerBytes(needle)
	}
	return &Matcher{needle: needle, fold: fold, wholeWord: p.WholeWord}, nil
}

// MatchBuffer is the whole-file prefilter: a fast, boundary-agnostic test for
// whether any match could exist anywhere in data. It must never be a false
// negative (that would drop a real match); a false positive just costs an
// unnecessary line scan. Literal whole-word searches deliberately ignore the
// boundary here — substring presence is a superset of whole-word matches.
func (m *Matcher) MatchBuffer(data []byte) bool {
	if m.re != nil {
		return m.re.Match(data)
	}
	if len(m.needle) == 0 {
		return false
	}
	if m.fold {
		data = asciiLowerBytes(data)
	}
	return bytes.Index(data, m.needle) >= 0
}

// MatchLine reports whether one line matches, enforcing whole-word boundaries.
func (m *Matcher) MatchLine(line []byte) bool {
	if m.re != nil {
		return m.re.Match(line)
	}
	if len(m.needle) == 0 {
		return false
	}
	// Fold on a lowercased copy; positions map 1:1 to line, so word-boundary
	// checks below still read the original bytes.
	hay := line
	if m.fold {
		hay = asciiLowerBytes(line)
	}
	from := 0
	for from <= len(hay)-len(m.needle) {
		idx := bytes.Index(hay[from:], m.needle)
		if idx < 0 {
			return false
		}
		pos := from + idx
		if !m.wholeWord || wordBounded(line, pos, pos+len(m.needle)) {
			return true
		}
		from = pos + 1
	}
	return false
}

func wordBounded(line []byte, start, end int) bool {
	if start > 0 && isWordByte(line[start-1]) {
		return false
	}
	if end < len(line) && isWordByte(line[end]) {
		return false
	}
	return true
}

func isWordByte(b byte) bool {
	return b == '_' ||
		(b >= '0' && b <= '9') ||
		(b >= 'a' && b <= 'z') ||
		(b >= 'A' && b <= 'Z')
}

func asciiLower(b byte) byte {
	if b >= 'A' && b <= 'Z' {
		return b + ('a' - 'A')
	}
	return b
}

func asciiLowerBytes(bs []byte) []byte {
	out := make([]byte, len(bs))
	for i, b := range bs {
		out[i] = asciiLower(b)
	}
	return out
}
