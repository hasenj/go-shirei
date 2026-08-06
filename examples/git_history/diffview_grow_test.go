package main

import (
	"testing"
)

func TestDiffViewGrowBootstrapAndNewFile(t *testing.T) {
	doc := &DiffDoc{Rows: []DiffRow{
		{Kind: RowFileHeader, Text: "a.go"},
		{Kind: RowAdd, Text: "+a"},
	}}
	v := newDiffView("id", nil)
	v.Grow(doc, 0, nil)
	if !v.HasSegs() || v.ItemCount() != 2 {
		t.Fatalf("after bootstrap segs=%+v count=%d", v.segs, v.ItemCount())
	}
	prev := len(doc.Rows)
	doc.Rows = append(doc.Rows,
		DiffRow{Kind: RowFileHeader, Text: "b.go"},
		DiffRow{Kind: RowAdd, Text: "+b"},
		DiffRow{Kind: RowAdd, Text: "+b2"},
	)
	v.Grow(doc, prev, map[string]bool{"b.go": true})
	if len(v.segs) != 2 {
		t.Fatalf("segs=%+v", v.segs)
	}
	if v.segs[0].End != 2 || v.segs[1].Header != 2 || v.segs[1].End != 5 {
		t.Fatalf("seg spans=%+v", v.segs)
	}
	if !v.IsCollapsed(1) {
		t.Fatal("b.go should be collapsed from remembered")
	}
	// collapsed: header a + body a + header b + placeholder = 4
	if v.ItemCount() != 4 {
		t.Fatalf("ItemCount=%d want 4", v.ItemCount())
	}
}

func TestGrowDocSegsNilView(t *testing.T) {
	var segs []DiffFileSeg
	rows := []DiffRow{
		{Kind: RowFileHeader, Text: "a.go"},
		{Kind: RowAdd, Text: "+1"},
	}
	growDocSegs(&segs, rows, 0, len(rows), nil)
	if len(segs) != 1 || segs[0].End != 2 {
		t.Fatalf("%+v", segs)
	}
	prev := len(rows)
	rows = append(rows, DiffRow{Kind: RowFileHeader, Text: "b.go"}, DiffRow{Kind: RowDel, Text: "-x"})
	growDocSegs(&segs, rows, prev, len(rows), nil)
	if len(segs) != 2 || segs[0].End != 2 || segs[1].End != 4 {
		t.Fatalf("%+v", segs)
	}
}

func TestStreamPublishAppendBeforePaint(t *testing.T) {
	// Simulate stream with diffView nil: segs on doc only, then sync-like newDiffView.
	live := &DiffDoc{}
	pub := func(batch []DiffRow, done bool) bool {
		if len(batch) > 0 {
			prev := len(live.Rows)
			live.Rows = append(live.Rows, batch...)
			growDocSegs(&live.Segs, live.Rows, prev, len(live.Rows), live.Stats)
		}
		if done {
			live.Segs = buildDiffFileSegs(live)
		}
		return true
	}
	// Manual two-file stream
	pub([]DiffRow{{Kind: RowFileHeader, Text: "a.go"}}, false)
	pub([]DiffRow{{Kind: RowAdd, Text: "+a"}}, false)
	if len(live.Segs) != 1 {
		t.Fatalf("mid segs=%+v", live.Segs)
	}
	pub([]DiffRow{{Kind: RowFileHeader, Text: "b.go"}}, false)
	pub([]DiffRow{{Kind: RowAdd, Text: "+b"}}, false)
	pub(nil, true)
	v := newDiffView("id", live.Segs)
	if v.ItemCount() != len(live.Rows) {
		t.Fatalf("count=%d rows=%d segs=%+v", v.ItemCount(), len(live.Rows), live.Segs)
	}
}
