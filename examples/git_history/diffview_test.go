package main

import (
	"strings"
	"testing"
)

func sampleDocTwoFiles() *DiffDoc {
	// file a: header + 2 body; file b: header + 3 body
	doc := &DiffDoc{
		Rows: []DiffRow{
			{Kind: RowFileHeader, Text: "a.go"},
			{Kind: RowHunkHeader, Text: "@@ -1 +1 @@"},
			{Kind: RowAdd, Text: "+a"},
			{Kind: RowFileHeader, Text: "b.go"},
			{Kind: RowHunkHeader, Text: "@@ -1 +1 @@"},
			{Kind: RowDel, Text: "-b"},
			{Kind: RowAdd, Text: "+b"},
		},
		Stats: []FileStat{
			{Path: "a.go", Added: 1, Deleted: 0},
			{Path: "b.go", Added: 1, Deleted: 1},
		},
	}
	doc.Segs = buildDiffFileSegs(doc)
	return doc
}

func TestBuildDiffFileSegs(t *testing.T) {
	doc := sampleDocTwoFiles()
	if len(doc.Segs) != 2 {
		t.Fatalf("segs = %d, want 2", len(doc.Segs))
	}
	if doc.Segs[0].Header != 0 || doc.Segs[0].End != 3 {
		t.Fatalf("seg0 = %+v", doc.Segs[0])
	}
	if doc.Segs[1].Header != 3 || doc.Segs[1].End != 7 {
		t.Fatalf("seg1 = %+v", doc.Segs[1])
	}
	if doc.Segs[0].Added != 1 || doc.Segs[1].Deleted != 1 {
		t.Fatalf("stats not attached: %+v", doc.Segs)
	}
}

func TestBuildDiffFileSegsRename(t *testing.T) {
	doc := &DiffDoc{
		Rows: []DiffRow{
			{Kind: RowFileHeader, Text: "old.txt → new.txt"},
			{Kind: RowMeta, Text: "rename"},
		},
		Stats: []FileStat{{Path: "new.txt", Added: 0, Deleted: 0}},
	}
	segs := buildDiffFileSegs(doc)
	if len(segs) != 1 || segs[0].Path != "old.txt → new.txt" {
		t.Fatalf("segs = %+v", segs)
	}
	// matched via "new.txt" candidate
	if segs[0].Added != 0 || segs[0].Deleted != 0 {
		t.Fatalf("rename stat match failed: %+v", segs[0])
	}
}

func TestDiffViewCollapseMapping(t *testing.T) {
	doc := sampleDocTwoFiles()
	v := newDiffView("id", doc.Segs)
	if v.ItemCount() != 7 {
		t.Fatalf("expanded count = %d, want 7", v.ItemCount())
	}
	// collapse file a (3 rows → header + placeholder)
	if !v.ToggleFile(0) {
		t.Fatal("toggle failed")
	}
	if v.ItemCount() != 6 { // 2 + 4
		t.Fatalf("after collapse a: count = %d, want 6", v.ItemCount())
	}
	// visible 0 → header a; visible 1 → placeholder for a
	if v.SourceOf(0) != 0 {
		t.Fatalf("vis0 source = %d", v.SourceOf(0))
	}
	if !v.IsPlaceholder(1) {
		t.Fatal("vis1 should be placeholder")
	}
	if v.IsPlaceholder(0) {
		t.Fatal("header should not be placeholder")
	}
	// visible 2 → source 3 (header b)
	if v.SourceOf(2) != 3 {
		t.Fatalf("vis2 source = %d, want 3", v.SourceOf(2))
	}
	// body of a is hidden
	if _, ok := v.VisOf(1); ok {
		t.Fatal("collapsed body should not map")
	}
	if vis, ok := v.VisOf(0); !ok || vis != 0 {
		t.Fatalf("header a vis = %d ok=%v", vis, ok)
	}
	if vis, ok := v.VisOf(4); !ok || vis != 3 {
		// source 4 = first body of b; prefix[1]=2, offset=1 → vis 3
		t.Fatalf("source 4 vis = %d ok=%v, want 3", vis, ok)
	}

	// expand a again
	v.ToggleFile(0)
	if v.ItemCount() != 7 {
		t.Fatalf("re-expanded count = %d", v.ItemCount())
	}
}

func TestDiffViewSetAllAndEnsureExpanded(t *testing.T) {
	doc := sampleDocTwoFiles()
	v := newDiffView("id", doc.Segs)
	v.SetAllCollapsed(true)
	// each file with body → header + placeholder
	if !v.AllCollapsed() || v.ItemCount() != 4 {
		t.Fatalf("all collapsed: count=%d all=%v", v.ItemCount(), v.AllCollapsed())
	}
	// find hit on body of b (source 5)
	if !v.EnsureExpandedSource(5) {
		t.Fatal("expected expand")
	}
	if v.IsCollapsed(1) {
		t.Fatal("file b should be expanded")
	}
	if v.IsCollapsed(0) != true {
		t.Fatal("file a should stay collapsed")
	}
	if vis, ok := v.VisOf(5); !ok || vis != 4 {
		// header a (0), ph a (1), header b (2), hunk (3), del (4)=source 5? 
		// segs[1] header=3: source 5 → offset 2 → prefix[1]=2 + 2 = 4
		t.Fatalf("vis of 5 = %d ok=%v want 4", vis, ok)
	}
	if v.EnsureExpandedSource(5) {
		t.Fatal("second ensure should be no-op")
	}
}

func TestDiffViewHeadersVis(t *testing.T) {
	doc := sampleDocTwoFiles()
	v := newDiffView("id", doc.Segs)
	v.ToggleFile(0)
	h := v.HeadersVis()
	// header a at 0, header b after a's placeholder at 2
	if len(h) != 2 || h[0] != 0 || h[1] != 2 {
		t.Fatalf("headers vis = %v", h)
	}
}

func TestCollapsedPlaceholderLines(t *testing.T) {
	l1, l2 := CollapsedPlaceholderLines(DiffFileSeg{Added: 3, Deleted: 1})
	if l1 == "" || l2 == "" {
		t.Fatal("empty lines")
	}
	if !strings.Contains(l1, "+3") || !strings.Contains(l1, "−1") {
		t.Fatalf("line1 = %q", l1)
	}
	l1, _ = CollapsedPlaceholderLines(DiffFileSeg{Binary: true, Added: -1, Deleted: -1})
	if l1 != "binary file" {
		t.Fatalf("binary = %q", l1)
	}
}

func TestApplyCollapsedPaths(t *testing.T) {
	doc := sampleDocTwoFiles()
	v := newDiffView("commitA", doc.Segs)
	v.ApplyCollapsedPaths(map[string]bool{"b.go": true})
	if v.IsCollapsed(0) {
		t.Fatal("a.go should stay expanded")
	}
	if !v.IsCollapsed(1) {
		t.Fatal("b.go should be collapsed")
	}
	paths := v.CollapsedPaths()
	if len(paths) != 1 || !paths["b.go"] {
		t.Fatalf("CollapsedPaths = %v", paths)
	}
	// Round-trip onto a fresh view (simulates switching away and back).
	v2 := newDiffView("commitA", doc.Segs)
	v2.ApplyCollapsedPaths(paths)
	if !v2.IsCollapsed(1) || v2.IsCollapsed(0) {
		t.Fatalf("restore: collapsed=%v %v", v2.IsCollapsed(0), v2.IsCollapsed(1))
	}
}

func TestFileStatLabel(t *testing.T) {
	if FileStatLabel(DiffFileSeg{Added: 3, Deleted: 1}) != "+3 −1" {
		t.Fatal(FileStatLabel(DiffFileSeg{Added: 3, Deleted: 1}))
	}
	if FileStatLabel(DiffFileSeg{Binary: true, Added: -1, Deleted: -1}) != "binary" {
		t.Fatal("binary label")
	}
}

func TestStatForHeaderUntracked(t *testing.T) {
	stats := []FileStat{{Path: "x.md (untracked)", Added: 4, Deleted: 0}}
	st, ok := statForHeader(stats, "x.md (untracked)")
	if !ok || st.Added != 4 {
		t.Fatalf("got %+v ok=%v", st, ok)
	}
}
