package main

import (
	"testing"
	"time"

	"go.hasen.dev/shirei"
	"go.hasen.dev/shirei/internal/snaptest"
)

// TestMatcher pins the toggle combinations down: literal vs regex, case
// sensitivity, and whole-word boundaries. Each was a place a naive
// implementation could silently do the wrong thing.
func TestMatcher(t *testing.T) {
	cases := []struct {
		name  string
		p     Params
		input string
		want  bool
	}{
		{"literal hit", Params{Query: "hello"}, "well hello there", true},
		{"literal miss", Params{Query: "hello"}, "goodbye", false},
		{"literal is not regex", Params{Query: "a.c"}, "axc", false},
		{"literal dot is literal", Params{Query: "a.c"}, "a.c", true},

		{"case-insensitive by default", Params{Query: "Hello"}, "say hello", true},
		{"case-sensitive misses", Params{Query: "Hello", MatchCase: true}, "say hello", false},
		{"case-sensitive hits", Params{Query: "Hello", MatchCase: true}, "say Hello", true},

		{"whole word hits", Params{Query: "cat", WholeWord: true}, "the cat sat", true},
		{"whole word misses substring", Params{Query: "cat", WholeWord: true}, "concatenate", false},

		{"regex on", Params{Query: `a.c`, Regex: true}, "axc", true},
		{"regex with word boundary", Params{Query: `func \w+`, Regex: true}, "func greet() {", true},
		{"regex whole word", Params{Query: `hel+o`, Regex: true, WholeWord: true}, "concathelloword", false},
	}
	for _, c := range cases {
		m, err := buildMatcher(c.p)
		if err != nil {
			t.Errorf("%s: buildMatcher failed: %v", c.name, err)
			continue
		}
		// The whole-file prefilter must never miss what a line match would find.
		if got := m.MatchLine([]byte(c.input)); got != c.want {
			t.Errorf("%s: MatchLine(%q) = %v, want %v", c.name, c.input, got, c.want)
		}
		if c.want && !m.MatchBuffer([]byte(c.input)) {
			t.Errorf("%s: MatchBuffer(%q) = false, must be a superset of MatchLine", c.name, c.input)
		}
	}

	if _, err := buildMatcher(Params{Query: "(unclosed", Regex: true}); err == nil {
		t.Error("expected an error for an invalid regex, got nil")
	}
}

func TestMatchRanges(t *testing.T) {
	m, err := buildMatcher(Params{Query: "cat"})
	if err != nil {
		t.Fatal(err)
	}
	got := m.MatchRanges([]byte("the cat sat on the catmat"))
	// "cat" at "the cat " and "the catmat" — second is substring of catmat
	want := [][2]int{{4, 7}, {19, 22}}
	if len(got) != len(want) {
		t.Fatalf("ranges = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ranges[%d] = %v, want %v", i, got[i], want[i])
		}
	}

	ww, err := buildMatcher(Params{Query: "cat", WholeWord: true})
	if err != nil {
		t.Fatal(err)
	}
	got = ww.MatchRanges([]byte("the cat sat on the catmat"))
	if len(got) != 1 || got[0] != [2]int{4, 7} {
		t.Fatalf("whole-word ranges = %v, want only [4,7)", got)
	}

	re, err := buildMatcher(Params{Query: `hel+o`, Regex: true})
	if err != nil {
		t.Fatal(err)
	}
	got = re.MatchRanges([]byte("say helo and hello"))
	if len(got) != 2 {
		t.Fatalf("regex ranges = %v, want 2 hits", got)
	}
}

func TestRuneRangesFromBytes(t *testing.T) {
	s := "日本語 hello"
	// "hello" starts after "日本語 " = 3 runes + space = 4 runes; bytes: 9 + 1 = 10
	br := [][2]int{{10, 15}}
	got := runeRangesFromBytes(s, br)
	if len(got) != 1 || got[0] != [2]int{4, 9} {
		t.Fatalf("got %v, want [[4 9]]", got)
	}
}

func TestMergeHitWindows(t *testing.T) {
	// ctxBefore=2, ctxAfter=2. Hits on lines 0 and 2:
	//   0 → [0,2], 2 → [0,4] → one block [0,4] with both hits.
	blocks := mergeHitWindows([]int{0, 2}, 10)
	if len(blocks) != 1 {
		t.Fatalf("blocks = %d, want 1: %+v", len(blocks), blocks)
	}
	if blocks[0].start != 0 || blocks[0].end != 4 {
		t.Fatalf("range = [%d,%d], want [0,4]", blocks[0].start, blocks[0].end)
	}
	if len(blocks[0].hits) != 2 || blocks[0].hits[0] != 0 || blocks[0].hits[1] != 2 {
		t.Fatalf("hits = %v, want [0 2]", blocks[0].hits)
	}

	// Far-apart hits: line 0 window [0,2], line 10 window [8,12] — no overlap.
	blocks = mergeHitWindows([]int{0, 10}, 20)
	if len(blocks) != 2 {
		t.Fatalf("far hits: blocks = %d, want 2: %+v", len(blocks), blocks)
	}

	// Touching: hit 0 → [0,2], hit 5 → [3,7] (ctx=2): 3 == 2+1 → merge.
	blocks = mergeHitWindows([]int{0, 5}, 20)
	if len(blocks) != 1 {
		t.Fatalf("touching: blocks = %d, want 1: %+v", len(blocks), blocks)
	}
	if blocks[0].start != 0 || blocks[0].end != 7 {
		t.Fatalf("touching range = [%d,%d], want [0,7]", blocks[0].start, blocks[0].end)
	}
}

func TestBuildFileMatchesMergesAndHighlights(t *testing.T) {
	// Same shape as the user's dive/source.go example: hits on lines 1 and 3.
	lines := []string{
		"// virtualized read-only",
		"// gutter highlight",
		"// virtualization is simple",
		"// exact scroll",
		"package main",
	}
	m, err := buildMatcher(Params{Query: "virtual"})
	if err != nil {
		t.Fatal(err)
	}
	fr := &FileResult{Path: "/x", RelPath: "source.go"}
	got := buildFileMatches(lines, []int{0, 2}, fr, m)
	if len(got) != 1 {
		t.Fatalf("rows = %d, want 1 merged", len(got))
	}
	row := got[0]
	if row.MatchCount != 2 || row.Line != 1 {
		t.Fatalf("MatchCount=%d Line=%d, want 2 and 1", row.MatchCount, row.Line)
	}
	// Context covers lines 1..5 (0..4) once.
	if len(row.Context) != 5 {
		t.Fatalf("context lines = %d, want 5", len(row.Context))
	}
	// Both hit lines highlighted; "virtual" on line 1 and "virtual" inside "virtualization" on line 3.
	if len(row.Context[0].Highlights) == 0 {
		t.Fatal("line 1 should highlight virtual")
	}
	if len(row.Context[2].Highlights) == 0 {
		t.Fatal("line 3 should highlight virtual")
	}
}

// TestSearchSync runs the real pipeline over the committed fixture tree: three
// text files match "hello" (six lines total) and the NUL-containing blob.bin —
// which also contains the word — is skipped as binary.
func TestSearchSync(t *testing.T) {
	s := searchSync(Params{Root: "testdata/sample", Query: "hello"})
	if s.err != nil {
		t.Fatalf("search error: %v", s.err)
	}
	if s.filesScanned.Load() != 3 {
		t.Errorf("filesScanned = %d, want 3 (blob.bin should be skipped as binary)", s.filesScanned.Load())
	}
	if s.filesMatched.Load() != 3 {
		t.Errorf("filesMatched = %d, want 3", s.filesMatched.Load())
	}
	if s.matchCount.Load() != 6 {
		t.Errorf("matchCount = %d, want 6", s.matchCount.Load())
	}
	for _, m := range s.matches {
		if m.File.RelPath == "blob.bin" {
			t.Errorf("binary file blob.bin should not appear in results")
		}
	}
}

// TestAsyncSearch drives the real concurrent path — runNewSearch fanning
// files out to the worker pool and publishing under WithFrameLock — and
// checks it lands on the same totals as the deterministic sync search. Run
// under -race, it also guards the background publishing against data races.
func TestAsyncSearch(t *testing.T) {
	p := Params{Root: "testdata/sample", Query: "hello"}
	want := searchSync(p)

	appData = &App{}
	runNewSearch(p)
	s := appData.active

	deadline := time.Now().Add(10 * time.Second)
	for {
		var running bool
		shirei.WithFrameLock(func() { running = s.running })
		if !running {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("async search did not finish in time")
		}
		time.Sleep(2 * time.Millisecond)
	}

	if got, want := s.matchCount.Load(), want.matchCount.Load(); got != want {
		t.Errorf("async matchCount = %d, want %d", got, want)
	}
	if got, want := s.filesMatched.Load(), want.filesMatched.Load(); got != want {
		t.Errorf("async filesMatched = %d, want %d", got, want)
	}
}

// TestTabsAccumulateAndClose pins the history-tab bookkeeping: each search
// opens a new active tab, closing the active tab selects a neighbour, and
// closing the last one leaves an empty state.
func TestTabsAccumulateAndClose(t *testing.T) {
	appData = &App{pathInput: "testdata/sample"}

	runNewSearch(Params{Root: "testdata/sample", Query: "hello"})
	runNewSearch(Params{Root: "testdata/sample", Query: "func"})
	if len(appData.searches) != 2 {
		t.Fatalf("expected 2 tabs, got %d", len(appData.searches))
	}
	if appData.active != appData.searches[1] {
		t.Fatal("the newest search should be the active tab")
	}

	// Empty query opens no tab.
	runNewSearch(Params{Root: "testdata/sample", Query: ""})
	if len(appData.searches) != 2 {
		t.Fatalf("empty query should not open a tab; got %d", len(appData.searches))
	}

	// Closing the active tab selects a neighbour.
	closeTab(appData.active)
	if len(appData.searches) != 1 {
		t.Fatalf("expected 1 tab after close, got %d", len(appData.searches))
	}
	if appData.active != appData.searches[0] {
		t.Fatal("closing the active tab should select the remaining one")
	}
	if appData.query != "hello" {
		t.Fatalf("closing should reload the surviving tab's query; got %q", appData.query)
	}

	// Closing the last tab leaves the empty state.
	closeTab(appData.active)
	if len(appData.searches) != 0 || appData.active != nil {
		t.Fatal("closing the last tab should leave no active tab")
	}
}

// TestSnapshotHaystack renders the full app searching the fixture tree. The
// root is a relative path and the editor set is fixed, so the golden depends
// only on the host's fonts (like every snaptest golden), not on the checkout
// location or which editors happen to be installed.
func TestSnapshotHaystack(t *testing.T) {
	appData = &App{
		pathInput: "testdata/sample",
		query:     "hello",
		// Skip the startup focus grab: it uses FocusImmediateOn, which never
		// triggers the input's own focus-reset, so a caret column left over
		// from a prior test would leak in and make the golden order-dependent.
		startupFocused: true,
		editors: []Editor{
			{Name: "VS Code"}, {Name: "Sublime"}, {Name: "Zed"},
		},
	}
	s := searchSync(currentParams())
	appData.searches = []*Search{s}
	appData.active = s

	snaptest.Snapshot(t, "haystack_main", winW, winH, RootView)
}
