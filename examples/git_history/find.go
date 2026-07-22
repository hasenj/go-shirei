package main

import (
	"strings"
	"unicode/utf8"
)

// diffMatch is one occurrence of the find query in the stacked diff stream.
type diffMatch struct {
	row      int // index into DiffDoc.Rows
	from, to int // rune range [from, to) within that row's Text
}

// findMatchesInDoc scans all rows for case-insensitive substring matches.
// O(total text); fine for multi-thousand-line diffs when run only on
// query/doc change (not every frame).
func findMatchesInDoc(doc *DiffDoc, query string) []diffMatch {
	q := strings.TrimSpace(query)
	if doc == nil || q == "" {
		return nil
	}
	// Cap pathological queries / docs for safety.
	const maxMatches = 10_000
	var out []diffMatch
	for i := range doc.Rows {
		out = appendMatchesInLine(out, i, doc.Rows[i].Text, q)
		if len(out) >= maxMatches {
			break
		}
	}
	return out
}

func appendMatchesInLine(out []diffMatch, row int, text, query string) []diffMatch {
	for _, r := range findSubstringRanges(text, query) {
		out = append(out, diffMatch{row: row, from: r[0], to: r[1]})
	}
	return out
}

// findSubstringRanges returns case-insensitive non-overlapping rune ranges
// [from, to) of query inside text.
func findSubstringRanges(text, query string) [][2]int {
	if text == "" || query == "" {
		return nil
	}
	lowerText := strings.ToLower(text)
	lowerQ := strings.ToLower(query)
	qBytes := len(lowerQ)
	if qBytes == 0 {
		return nil
	}
	var out [][2]int
	start := 0
	for start <= len(lowerText)-qBytes {
		j := strings.Index(lowerText[start:], lowerQ)
		if j < 0 {
			break
		}
		j += start
		end := j + qBytes
		from := utf8.RuneCountInString(text[:j])
		to := utf8.RuneCountInString(text[:end])
		out = append(out, [2]int{from, to})
		start = end
	}
	return out
}

// historyIndexHasMatch reports whether history index i is in the sorted match list.
func historyIndexHasMatch(matches []int, i int) bool {
	// matches are append-ordered by ascending index.
	lo, hi := 0, len(matches)
	for lo < hi {
		mid := (lo + hi) / 2
		if matches[mid] < i {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	return lo < len(matches) && matches[lo] == i
}

// matchesOnRow returns find hits for a single row (for paint).
func matchesOnRow(matches []diffMatch, row int) []diffMatch {
	// Linear scan is fine: only a few matches typically; if many, still cheap vs paint.
	var out []diffMatch
	for _, m := range matches {
		if m.row == row {
			out = append(out, m)
		}
		if m.row > row {
			break // matches are row-ordered
		}
	}
	return out
}

// findMatchesInHistory returns indices of history entries whose id, short
// hash, subject, author, or sidebar label contain the query (case-insensitive).
func findMatchesInHistory(history []HistoryEntry, query string) []int {
	q := strings.TrimSpace(query)
	if q == "" || len(history) == 0 {
		return nil
	}
	lowerQ := strings.ToLower(q)
	var out []int
	for i, e := range history {
		if historyEntryMatches(e, lowerQ) {
			out = append(out, i)
		}
	}
	return out
}

func historyEntryMatches(e HistoryEntry, lowerQ string) bool {
	if lowerQ == "" {
		return false
	}
	if strings.Contains(strings.ToLower(e.ID), lowerQ) {
		return true
	}
	if e.Short != "" && strings.Contains(strings.ToLower(e.Short), lowerQ) {
		return true
	}
	if e.Subject != "" && strings.Contains(strings.ToLower(e.Subject), lowerQ) {
		return true
	}
	if e.Author != "" && strings.Contains(strings.ToLower(e.Author), lowerQ) {
		return true
	}
	// Synthetic slots (Working tree / Staging) only have a label.
	if e.Kind != KindCommit {
		return strings.Contains(strings.ToLower(e.SidebarLabel()), lowerQ)
	}
	return false
}
