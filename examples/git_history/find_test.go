package main

import "testing"

func TestAppendMatchesInLine(t *testing.T) {
	// Case-insensitive, multiple hits.
	got := appendMatchesInLine(nil, 3, "Foo foo FOO", "foo")
	if len(got) != 3 {
		t.Fatalf("got %d matches, want 3: %#v", len(got), got)
	}
	if got[0].row != 3 || got[0].from != 0 || got[0].to != 3 {
		t.Fatalf("first match = %#v", got[0])
	}
	// Overlapping: "aaa" in "aaaa" → two non-overlapping from Index advance
	got = appendMatchesInLine(nil, 0, "aaaa", "aa")
	if len(got) != 2 {
		t.Fatalf("non-overlap got %d, want 2", len(got))
	}
}

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

func TestFindSubstringRanges(t *testing.T) {
	got := findSubstringRanges("Foo foo FOO", "foo")
	if len(got) != 3 {
		t.Fatalf("ranges = %v, want 3", got)
	}
	if got[0] != [2]int{0, 3} || got[1] != [2]int{4, 7} {
		t.Fatalf("first ranges = %v", got[:2])
	}
	if findSubstringRanges("abc", "") != nil {
		t.Fatal("empty query")
	}
}

func TestHistoryIndexHasMatch(t *testing.T) {
	ms := []int{1, 3, 7, 12}
	for _, want := range ms {
		if !historyIndexHasMatch(ms, want) {
			t.Fatalf("missing %d", want)
		}
	}
	for _, no := range []int{0, 2, 4, 100} {
		if historyIndexHasMatch(ms, no) {
			t.Fatalf("false positive %d", no)
		}
	}
	if historyIndexHasMatch(nil, 0) {
		t.Fatal("empty list")
	}
}
