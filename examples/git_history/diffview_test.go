package main

import "testing"

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
	// Collapse file a to its header.
	if !v.ToggleFile(0) {
		t.Fatal("toggle failed")
	}
	if v.ItemCount() != 5 { // 1 + 4
		t.Fatalf("after collapse a: count = %d, want 5", v.ItemCount())
	}
	// Visible 0 is header a; visible 1 is header b.
	if v.SourceOf(0) != 0 || v.SourceOf(1) != 3 {
		t.Fatalf("header sources = %d, %d", v.SourceOf(0), v.SourceOf(1))
	}
	// body of a is hidden
	if _, ok := v.VisOf(1); ok {
		t.Fatal("collapsed body should not map")
	}
	if vis, ok := v.VisOf(0); !ok || vis != 0 {
		t.Fatalf("header a vis = %d ok=%v", vis, ok)
	}
	if vis, ok := v.VisOf(4); !ok || vis != 2 {
		// Source 4 is the first body row of b, after two headers.
		t.Fatalf("source 4 vis = %d ok=%v, want 2", vis, ok)
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
	// Every collapsed file occupies exactly one header row.
	if !v.AllCollapsed() || v.ItemCount() != 2 {
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
	if vis, ok := v.VisOf(5); !ok || vis != 3 {
		// Header a, header b, hunk b, then the matching deletion.
		t.Fatalf("vis of 5 = %d ok=%v want 3", vis, ok)
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
	// Both collapsed and expanded headers remain in navigation order.
	if len(h) != 2 || h[0] != 0 || h[1] != 1 {
		t.Fatalf("headers vis = %v", h)
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

func TestStatForHeaderUntracked(t *testing.T) {
	stats := []FileStat{{Path: "x.md (untracked)", Added: 4, Deleted: 0}}
	st, ok := statForHeader(stats, "x.md (untracked)")
	if !ok || st.Added != 4 {
		t.Fatalf("got %+v ok=%v", st, ok)
	}
}
