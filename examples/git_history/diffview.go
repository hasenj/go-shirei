package main

import (
	"fmt"
	"strings"
)

// DiffFileSeg is one file's contiguous span in DiffDoc.Rows.
// Header is the RowFileHeader index; [Header, End) is the full expanded span.
type DiffFileSeg struct {
	Path    string
	Header  int // inclusive source index
	End     int // exclusive source index
	Added   int // -1 when binary / unavailable
	Deleted int
	Binary  bool
}

// DiffView is a collapse-aware projection of a DiffDoc for VirtualList.
// Rows are append-only while a patch streams, then immutable when complete.
// The list indexes into a visible space mapped via prefix sums over per-file
// segments (approach B). Use Grow during streaming; rebuild only at complete
// or recovery.
//
// Collapsed files with a body occupy two visible slots: the file header, then
// a synthetic summary placeholder (not present in DiffDoc.Rows).
type DiffView struct {
	docID     string
	segs      []DiffFileSeg
	collapsed []bool // parallel to segs; true → header + optional placeholder
	// prefix[i] = total visible rows before segs[i]; len = nsegs+1
	// prefix[n] = ItemCount()
	prefix []int
}

// collapsedVisCount is how many list rows a segment uses when collapsed.
// Files with only a header stay at 1; files with a body get a summary row.
func collapsedVisCount(s DiffFileSeg) int {
	if s.End-s.Header > 1 {
		return 2
	}
	return 1
}

func newDiffView(docID string, segs []DiffFileSeg) *DiffView {
	v := &DiffView{
		docID:     docID,
		segs:      segs,
		collapsed: make([]bool, len(segs)), // all expanded
	}
	v.rebuildPrefix()
	return v
}

// buildDiffFileSegs scans Rows for file headers and attaches stats.
func buildDiffFileSegs(doc *DiffDoc) []DiffFileSeg {
	if doc == nil || len(doc.Rows) == 0 {
		return nil
	}
	var headers []int
	for i, r := range doc.Rows {
		if r.Kind == RowFileHeader {
			headers = append(headers, i)
		}
	}
	if len(headers) == 0 {
		return nil
	}
	segs := make([]DiffFileSeg, 0, len(headers))
	for hi, start := range headers {
		end := len(doc.Rows)
		if hi+1 < len(headers) {
			end = headers[hi+1]
		}
		path := doc.Rows[start].Text
		seg := DiffFileSeg{
			Path:   path,
			Header: start,
			End:    end,
		}
		if st, ok := statForHeader(doc.Stats, path); ok {
			seg.Added = st.Added
			seg.Deleted = st.Deleted
			seg.Binary = st.Binary
		} else {
			seg.Added, seg.Deleted, seg.Binary = countSegStats(doc.Rows[start:end])
		}
		segs = append(segs, seg)
	}
	return segs
}

// statForHeader matches a numstat entry to a file-header label.
func statForHeader(stats []FileStat, header string) (FileStat, bool) {
	if header == "" || len(stats) == 0 {
		return FileStat{}, false
	}
	candidates := headerStatCandidates(header)
	for _, c := range candidates {
		for _, st := range stats {
			if st.Path == c {
				return st, true
			}
		}
	}
	return FileStat{}, false
}

func headerStatCandidates(header string) []string {
	out := []string{header}
	if strings.HasSuffix(header, " (untracked)") {
		base := strings.TrimSuffix(header, " (untracked)")
		if base != "" {
			out = append(out, base)
		}
	}
	if old, newPath, ok := strings.Cut(header, " → "); ok {
		if newPath != "" {
			out = append(out, newPath)
		}
		if old != "" {
			out = append(out, old)
		}
	}
	return out
}

func countSegStats(rows []DiffRow) (added, deleted int, binary bool) {
	for _, r := range rows {
		switch r.Kind {
		case RowAdd:
			added++
		case RowDel:
			deleted++
		case RowImage:
			binary = true
		case RowMeta:
			if strings.Contains(r.Text, "Binary") || strings.Contains(r.Text, "binary") {
				binary = true
			}
		}
	}
	if binary && added == 0 && deleted == 0 {
		return -1, -1, true
	}
	return added, deleted, binary
}

func (v *DiffView) rebuildPrefix() {
	if v == nil {
		return
	}
	n := len(v.segs)
	if cap(v.prefix) < n+1 {
		v.prefix = make([]int, n+1)
	} else {
		v.prefix = v.prefix[:n+1]
	}
	if n == 0 {
		return
	}
	v.prefix[0] = 0
	for i, s := range v.segs {
		var c int
		if v.collapsed[i] {
			c = collapsedVisCount(s)
		} else {
			c = s.End - s.Header
			if c < 1 {
				c = 1
			}
		}
		v.prefix[i+1] = v.prefix[i] + c
	}
}

// ItemCount is the number of visible virtual-list rows.
func (v *DiffView) ItemCount() int {
	if v == nil || len(v.prefix) == 0 {
		return 0
	}
	return v.prefix[len(v.prefix)-1]
}

// HasSegs reports whether collapse mapping is active.
func (v *DiffView) HasSegs() bool {
	return v != nil && len(v.segs) > 0
}

// fileOfVis returns the segment index owning visible index vis.
func (v *DiffView) fileOfVis(vis int) int {
	n := len(v.segs)
	if n == 0 || vis < 0 {
		return -1
	}
	// largest i with prefix[i] <= vis
	lo, hi := 0, n
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if v.prefix[mid] <= vis {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	if lo >= n || vis >= v.prefix[lo+1] {
		return -1
	}
	return lo
}

// fileOfSource returns the segment containing source row index.
func (v *DiffView) fileOfSource(source int) int {
	if v == nil || len(v.segs) == 0 || source < 0 {
		return -1
	}
	// largest i with Header <= source
	lo, hi := 0, len(v.segs)
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if mid < len(v.segs) && v.segs[mid].Header <= source {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	if lo >= len(v.segs) {
		return -1
	}
	s := v.segs[lo]
	if source < s.Header || source >= s.End {
		return -1
	}
	return lo
}

// SourceOf maps a visible list index to a DiffDoc.Rows index.
// Collapsed placeholders map to their file's header source index.
func (v *DiffView) SourceOf(vis int) int {
	if v == nil || !v.HasSegs() {
		return vis
	}
	fi := v.fileOfVis(vis)
	if fi < 0 {
		return vis
	}
	s := v.segs[fi]
	if v.collapsed[fi] {
		return s.Header
	}
	return s.Header + (vis - v.prefix[fi])
}

// IsPlaceholder is true when vis is the synthetic summary row under a
// collapsed file header (not a real DiffDoc.Rows entry).
func (v *DiffView) IsPlaceholder(vis int) bool {
	if v == nil || !v.HasSegs() {
		return false
	}
	fi := v.fileOfVis(vis)
	if fi < 0 || !v.collapsed[fi] {
		return false
	}
	return vis > v.prefix[fi]
}

// FileIndexOfVis returns the segment index for a visible row, or -1.
func (v *DiffView) FileIndexOfVis(vis int) int {
	if v == nil {
		return -1
	}
	return v.fileOfVis(vis)
}

// VisOf maps a source row to its visible index. ok is false when the row is
// hidden inside a collapsed file (not the header).
func (v *DiffView) VisOf(source int) (vis int, ok bool) {
	if v == nil || !v.HasSegs() {
		return source, true
	}
	fi := v.fileOfSource(source)
	if fi < 0 {
		return 0, false
	}
	s := v.segs[fi]
	if source == s.Header {
		return v.prefix[fi], true
	}
	if v.collapsed[fi] {
		return 0, false
	}
	return v.prefix[fi] + (source - s.Header), true
}

// HeadersVis returns visible indices of every file header (always shown).
func (v *DiffView) HeadersVis() []int {
	if v == nil || !v.HasSegs() {
		return nil
	}
	out := make([]int, len(v.segs))
	for i := range v.segs {
		out[i] = v.prefix[i]
	}
	return out
}

// ToggleFile flips collapse for fileIdx. Returns whether state changed.
func (v *DiffView) ToggleFile(fileIdx int) bool {
	if v == nil || fileIdx < 0 || fileIdx >= len(v.collapsed) {
		return false
	}
	v.collapsed[fileIdx] = !v.collapsed[fileIdx]
	v.rebuildPrefix()
	return true
}

// SetAllCollapsed expands or collapses every file.
func (v *DiffView) SetAllCollapsed(collapsed bool) {
	if v == nil {
		return
	}
	for i := range v.collapsed {
		v.collapsed[i] = collapsed
	}
	v.rebuildPrefix()
}

// Grow updates segs for an append-only growth of doc.Rows from prevRowCount
// to len(doc.Rows). O(new rows + files) — not a full rescan of prior rows.
// remembered is collapsedByDoc paths (optional).
func (v *DiffView) Grow(doc *DiffDoc, prevRowCount int, remembered map[string]bool) {
	if v == nil || doc == nil {
		return
	}
	n := len(doc.Rows)
	if n == 0 {
		v.segs = nil
		v.collapsed = nil
		v.rebuildPrefix()
		return
	}
	if prevRowCount < 0 {
		prevRowCount = 0
	}
	if prevRowCount > n {
		prevRowCount = n
	}

	if len(v.segs) == 0 {
		v.segs = buildDiffFileSegs(doc)
		v.collapsed = make([]bool, len(v.segs))
		for i, s := range v.segs {
			if remembered[s.Path] {
				v.collapsed[i] = true
			}
		}
		v.rebuildPrefix()
		return
	}

	// Extend open span, then split on any new file headers in the appended range.
	last := len(v.segs) - 1
	v.segs[last].End = n
	for i := prevRowCount; i < n; i++ {
		if doc.Rows[i].Kind != RowFileHeader {
			continue
		}
		// Close previous segment at this header.
		if i > v.segs[last].Header {
			v.segs[last].End = i
			refreshSegStats(&v.segs[last], doc)
		}
		path := doc.Rows[i].Text
		seg := DiffFileSeg{Path: path, Header: i, End: n}
		refreshSegStats(&seg, doc)
		v.segs = append(v.segs, seg)
		v.collapsed = append(v.collapsed, remembered[path])
		last = len(v.segs) - 1
	}
	refreshSegStats(&v.segs[last], doc)
	v.rebuildPrefix()
}

// ReplaceSegsPreservingCollapse swaps in a full segs rebuild while keeping
// collapse flags by path (complete load or recovery).
func (v *DiffView) ReplaceSegsPreservingCollapse(segs []DiffFileSeg) {
	if v == nil {
		return
	}
	old := v.CollapsedPaths()
	v.segs = segs
	v.collapsed = make([]bool, len(segs))
	for i, s := range segs {
		if old[s.Path] {
			v.collapsed[i] = true
		}
	}
	v.rebuildPrefix()
}

// cloneSegs returns a shallow copy of segs (or nil).
func cloneSegs(segs []DiffFileSeg) []DiffFileSeg {
	if len(segs) == 0 {
		return nil
	}
	out := make([]DiffFileSeg, len(segs))
	copy(out, segs)
	return out
}

// growDocSegs incrementally maintains doc-level Segs when DiffView is nil
// (before first paint). Same header/End rules as DiffView.Grow without collapse.
func growDocSegs(segs *[]DiffFileSeg, rows []DiffRow, prev, n int, stats []FileStat) {
	if segs == nil {
		return
	}
	if n == 0 {
		*segs = nil
		return
	}
	if prev < 0 {
		prev = 0
	}
	if prev > n {
		prev = n
	}
	if len(*segs) == 0 {
		*segs = buildDiffFileSegs(&DiffDoc{Rows: rows[:n], Stats: stats})
		return
	}
	s := *segs
	last := len(s) - 1
	s[last].End = n
	for i := prev; i < n; i++ {
		if rows[i].Kind != RowFileHeader {
			continue
		}
		if i > s[last].Header {
			s[last].End = i
			refreshSegStatsFrom(&s[last], rows, stats)
		}
		path := rows[i].Text
		seg := DiffFileSeg{Path: path, Header: i, End: n}
		refreshSegStatsFrom(&seg, rows, stats)
		s = append(s, seg)
		last = len(s) - 1
	}
	refreshSegStatsFrom(&s[last], rows, stats)
	*segs = s
}

func refreshSegStats(seg *DiffFileSeg, doc *DiffDoc) {
	if seg == nil || doc == nil {
		return
	}
	refreshSegStatsFrom(seg, doc.Rows, doc.Stats)
}

func refreshSegStatsFrom(seg *DiffFileSeg, rows []DiffRow, stats []FileStat) {
	if seg == nil || seg.Header < 0 || seg.End > len(rows) || seg.Header >= seg.End {
		return
	}
	if st, ok := statForHeader(stats, seg.Path); ok {
		seg.Added = st.Added
		seg.Deleted = st.Deleted
		seg.Binary = st.Binary
		return
	}
	seg.Added, seg.Deleted, seg.Binary = countSegStats(rows[seg.Header:seg.End])
}

// ApplyCollapsedPaths marks matching file paths as collapsed (session restore).
// Unknown paths are ignored; files not in the set stay expanded.
func (v *DiffView) ApplyCollapsedPaths(paths map[string]bool) {
	if v == nil || len(paths) == 0 || !v.HasSegs() {
		return
	}
	changed := false
	for i, s := range v.segs {
		if paths[s.Path] && !v.collapsed[i] {
			v.collapsed[i] = true
			changed = true
		}
	}
	if changed {
		v.rebuildPrefix()
	}
}

// CollapsedPaths returns the set of currently collapsed file paths.
// Empty when nothing is collapsed (caller may omit storing that entry).
func (v *DiffView) CollapsedPaths() map[string]bool {
	if v == nil || !v.HasSegs() {
		return nil
	}
	var out map[string]bool
	for i, s := range v.segs {
		if !v.collapsed[i] {
			continue
		}
		if out == nil {
			out = make(map[string]bool)
		}
		out[s.Path] = true
	}
	return out
}

// AllCollapsed is true when every file is collapsed (and there is at least one).
func (v *DiffView) AllCollapsed() bool {
	if v == nil || len(v.collapsed) == 0 {
		return false
	}
	for _, c := range v.collapsed {
		if !c {
			return false
		}
	}
	return true
}

// AnyCollapsed is true when at least one file is collapsed.
func (v *DiffView) AnyCollapsed() bool {
	if v == nil {
		return false
	}
	for _, c := range v.collapsed {
		if c {
			return true
		}
	}
	return false
}

// EnsureExpandedSource expands the file containing source if needed.
// Returns true if a rebuild happened.
func (v *DiffView) EnsureExpandedSource(source int) bool {
	if v == nil || !v.HasSegs() {
		return false
	}
	fi := v.fileOfSource(source)
	if fi < 0 || !v.collapsed[fi] {
		return false
	}
	v.collapsed[fi] = false
	v.rebuildPrefix()
	return true
}

// IsCollapsed reports whether fileIdx is collapsed.
func (v *DiffView) IsCollapsed(fileIdx int) bool {
	if v == nil || fileIdx < 0 || fileIdx >= len(v.collapsed) {
		return false
	}
	return v.collapsed[fileIdx]
}

// FileStatLabel is the compact +/− text for a file header.
func FileStatLabel(s DiffFileSeg) string {
	if s.Binary || (s.Added < 0 && s.Deleted < 0) {
		return "binary"
	}
	return fmt.Sprintf("+%d −%d", s.Added, s.Deleted)
}

// CollapsedPlaceholderLines is the copy for the synthetic summary under a
// collapsed file. Line1 is the stats phrase; line2 is the expand hint.
func CollapsedPlaceholderLines(s DiffFileSeg) (line1, line2 string) {
	line2 = "collapsed · click to expand"
	if s.Binary || (s.Added < 0 && s.Deleted < 0) {
		return "binary file", line2
	}
	addWord := "lines"
	if s.Added == 1 {
		addWord = "line"
	}
	delWord := "lines"
	if s.Deleted == 1 {
		delWord = "line"
	}
	// Prefer a single compact stats line; avoid "0 lines" noise when one side is zero.
	switch {
	case s.Added > 0 && s.Deleted > 0:
		line1 = fmt.Sprintf("+%d %s added  ·  −%d %s removed", s.Added, addWord, s.Deleted, delWord)
	case s.Added > 0:
		line1 = fmt.Sprintf("+%d %s added", s.Added, addWord)
	case s.Deleted > 0:
		line1 = fmt.Sprintf("−%d %s removed", s.Deleted, delWord)
	default:
		line1 = "no line changes"
	}
	return line1, line2
}
