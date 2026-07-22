package main

// File-header navigation in the stacked diff stream.
//
// Viewport truth comes from VirtualList OutFirstVisible / OutLastVisible.
//
// Prev: pin the last file-header row strictly above firstVis
// (start of current file if mid-file; previous file if already at its header).
//
// Next: pin the first file-header row after the last header in the painted
// range (or at/before firstVis if mid-file). No following header → ScrollToEnd.

const fileNavScrollEps f32 = 1.5

// fileHeaderIndices returns row indices of RowFileHeader in order.
func fileHeaderIndices(rows []DiffRow) []int {
	var out []int
	for i, r := range rows {
		if r.Kind == RowFileHeader {
			out = append(out, i)
		}
	}
	return out
}

// lastFileHeaderInRange is the last file-header row in [firstVis, lastVis]
// (inclusive). If none, the last header at or before firstVis (file spanning
// the viewport). Returns -1 if there are no headers.
func lastFileHeaderInRange(headers []int, firstVis, lastVis int) int {
	if len(headers) == 0 {
		return -1
	}
	if firstVis < 0 || lastVis < firstVis {
		return -1
	}
	lastInView := -1
	for _, hi := range headers {
		if hi >= firstVis && hi <= lastVis {
			lastInView = hi
		}
	}
	if lastInView >= 0 {
		return lastInView
	}
	// Mid-file: header is above the painted window.
	lastAtOrBefore := -1
	for _, hi := range headers {
		if hi <= firstVis {
			lastAtOrBefore = hi
		}
	}
	return lastAtOrBefore
}

// prevFileHeaderBefore returns the last file-header row strictly above
// firstVis (index < firstVis), or -1 if none.
func prevFileHeaderBefore(headers []int, firstVis int) int {
	if firstVis <= 0 || len(headers) == 0 {
		return -1
	}
	prev := -1
	for _, hi := range headers {
		if hi < firstVis {
			prev = hi
			continue
		}
		break
	}
	return prev
}

// nextFileHeaderAfter returns the next file-header row strictly after
// lastHeaderRow, or -1 if none (caller should ScrollToEnd).
func nextFileHeaderAfter(headers []int, lastHeaderRow int) int {
	for _, hi := range headers {
		if hi > lastHeaderRow {
			return hi
		}
	}
	return -1
}

// fileNavCanScrollDown is true when the list can still move downward.
func fileNavCanScrollDown(scrollY, maxScroll f32) bool {
	if maxScroll <= fileNavScrollEps {
		return false
	}
	return scrollY < maxScroll-fileNavScrollEps
}

// fileNavCanScrollUp is true when the list can still move upward.
func fileNavCanScrollUp(scrollY f32) bool {
	return scrollY > fileNavScrollEps
}
