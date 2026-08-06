package main

import (
	"testing"
	"time"

	"go.hasen.dev/shirei"
)

func checkSnap(t *testing.T, r shirei.SnapResult) {
	t.Helper()
	switch {
	case r.Status == shirei.SnapSkip:
		t.Skip(r.Reason)
	case r.Err != nil:
		t.Fatal(r.Err)
	case r.Status == shirei.SnapMismatch:
		t.Errorf("render does not match snapshot %s; wrote %s", shirei.SnapAbsPath(r.Golden), shirei.SnapAbsPath(r.Actual))
	case r.Status == shirei.SnapCreated:
		t.Logf("created snapshot %s; review it and commit it", shirei.SnapAbsPath(r.Golden))
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
// only on the host's fonts (like every Snapshot golden), not on the checkout
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

	checkSnap(t, shirei.Snapshot(t.Name(), "haystack_main", winW, winH, RootView))
}
