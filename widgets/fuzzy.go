package widgets

import (
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

// fuzzyMatch scores how well query matches candidate (case-insensitive
// subsequence). Higher is better; -1 means no match. Empty query matches
// everything at score 0 (callers rank by recency / path instead).
func fuzzyMatch(query, candidate string) int {
	if query == "" {
		return 0
	}
	q := []rune(strings.ToLower(query))
	c := []rune(strings.ToLower(candidate))
	orig := []rune(candidate)

	score := 0
	qi := 0
	prevMatch := -2 // last matched index in c; -2 so first match isn't "adjacent"
	for ci := 0; ci < len(c) && qi < len(q); ci++ {
		if c[ci] != q[qi] {
			continue
		}
		score += 10
		if ci == prevMatch+1 {
			score += 15 // contiguous run
		}
		if ci == 0 || isPathBoundary(orig, ci) {
			score += 20 // start of segment / camel bump
		}
		score -= ci / 8
		prevMatch = ci
		qi++
	}
	if qi < len(q) {
		return -1
	}
	score -= len(c) / 16
	return score
}

func isPathBoundary(s []rune, i int) bool {
	if i == 0 {
		return true
	}
	prev := s[i-1]
	cur := s[i]
	if prev == '/' || prev == '\\' || prev == '_' || prev == '-' || prev == '.' {
		return true
	}
	return unicode.IsLower(prev) && unicode.IsUpper(cur)
}

// fuzzyRankPaths returns paths whose display form matches query, best first.
// When query is empty, paths are returned in the given order.
//
// Queries that look like a filename/path fragment (contain '.' or '/')
// require a case-insensitive substring match — pure subsequence matching
// is too loose on long paths.
func fuzzyRankPaths(query string, paths []string, display func(string) string) []string {
	type hit struct {
		path  string
		score int
		idx   int
	}
	var hits []hit
	q := strings.ToLower(query)
	strict := strings.ContainsAny(query, "./\\")
	for i, p := range paths {
		d := display(p)
		base := filepath.Base(d)
		s := -1
		if q != "" {
			dl, bl := strings.ToLower(d), strings.ToLower(base)
			switch {
			case bl == q:
				s = 10_000
			case strings.HasPrefix(bl, q):
				s = 5_000
			case strings.Contains(bl, q):
				s = 2_000
			case strings.Contains(dl, q):
				s = 1_000
			case !strict:
				s = fuzzyMatch(query, d)
				if s < 0 {
					s = fuzzyMatch(query, base)
					if s >= 0 {
						s -= 5
					}
				}
			}
		} else {
			s = 0
		}
		if s < 0 {
			continue
		}
		hits = append(hits, hit{p, s, i})
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score > hits[j].score
		}
		return hits[i].idx < hits[j].idx
	})
	out := make([]string, len(hits))
	for i, h := range hits {
		out[i] = h.path
	}
	return out
}
