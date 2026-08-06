package main

import "testing"

func TestFindMatchesInDoc(t *testing.T) {
	doc := &DiffDoc{Rows: []DiffRow{
		{Kind: RowFileHeader, Text: "foo.go"},
		{Kind: RowAdd, Text: "+func Foo() {}"},
		{Kind: RowContext, Text: " bar"},
	}}
	ms := findMatchesInDoc(doc, "foo")
	if len(ms) != 2 {
		t.Fatalf("matches = %#v, want 2", ms)
	}
	if ms[0].row != 0 || ms[1].row != 1 {
		t.Fatalf("rows = %d,%d", ms[0].row, ms[1].row)
	}
	if findMatchesInDoc(doc, "") != nil {
		t.Fatal("empty query should yield nil")
	}
}

func TestFindMatchesInHistory(t *testing.T) {
	hist := []HistoryEntry{
		{Kind: KindWorkingTree, ID: idWorkingTree},
		{Kind: KindCommit, ID: "abcdef0123456789", Short: "abcdef0", Subject: "Fix the finder", Author: "Ada"},
		{Kind: KindCommit, ID: "deadbeef00000000", Short: "deadbee", Subject: "Unrelated", Author: "Grace"},
		{Kind: KindCommit, ID: "1111222233334444", Short: "1111222", Subject: "Finder docs", Author: "Ada Lovelace"},
	}
	ms := findMatchesInHistory(hist, "find")
	if len(ms) != 2 || ms[0] != 1 || ms[1] != 3 {
		t.Fatalf("find matches = %v, want [1 3]", ms)
	}
	ms = findMatchesInHistory(hist, "deadbee")
	if len(ms) != 1 || ms[0] != 2 {
		t.Fatalf("hash matches = %v, want [2]", ms)
	}
	ms = findMatchesInHistory(hist, "working")
	if len(ms) != 1 || ms[0] != 0 {
		t.Fatalf("synthetic matches = %v, want [0]", ms)
	}
	ms = findMatchesInHistory(hist, "ada")
	if len(ms) != 2 || ms[0] != 1 || ms[1] != 3 {
		t.Fatalf("author matches = %v, want [1 3]", ms)
	}
	if findMatchesInHistory(hist, "") != nil {
		t.Fatal("empty query should yield nil")
	}
}
