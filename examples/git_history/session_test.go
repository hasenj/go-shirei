package main

import "testing"

func TestResolveSessionDisplay(t *testing.T) {
	// Legacy / partial: missing showDiffStats → stats default true.
	d := resolveSessionDisplay(sessionDisplay{
		ShowAuthor:    false,
		ShowTimestamp: true,
		ShowDiffStats: nil,
	})
	if d.ShowAuthor {
		t.Fatal("author should be false")
	}
	if !d.ShowTime {
		t.Fatal("timestamp should be true")
	}
	if !d.ShowStats {
		t.Fatal("stats should default true when nil")
	}

	off := false
	d = resolveSessionDisplay(sessionDisplay{
		ShowAuthor:    true,
		ShowTimestamp: false,
		ShowDiffStats: &off,
	})
	if !d.ShowAuthor || d.ShowTime || d.ShowStats {
		t.Fatalf("got author=%v time=%v stats=%v", d.ShowAuthor, d.ShowTime, d.ShowStats)
	}
}

func TestApplySessionDisplayPerRepo(t *testing.T) {
	prev := appData.displayByPath
	defer func() { appData.displayByPath = prev }()

	off := false
	applySessionDisplay(sessionData{
		Tabs: []string{"/a", "/b"},
		Display: map[string]sessionDisplay{
			"/a": {ShowAuthor: true, ShowTimestamp: false, ShowDiffStats: &off},
			"/b": {ShowAuthor: false, ShowTimestamp: true},
		},
	})
	da := displayForPath("/a")
	if !da.ShowAuthor || da.ShowTime || da.ShowStats {
		t.Fatalf("/a: got %+v", da)
	}
	db := displayForPath("/b")
	if db.ShowAuthor || !db.ShowTime || !db.ShowStats {
		t.Fatalf("/b: got %+v (stats should default true)", db)
	}
	// Unknown path → defaults.
	d0 := displayForPath("/unknown")
	if d0.ShowAuthor || d0.ShowTime || !d0.ShowStats {
		t.Fatalf("default: got %+v", d0)
	}
}

func TestApplySessionDisplayLegacyMigration(t *testing.T) {
	prev := appData.displayByPath
	defer func() { appData.displayByPath = prev }()

	// Global-only session (pre-per-repo): seed tabs/recents with those prefs.
	off := false
	applySessionDisplay(sessionData{
		Tabs:          []string{"/repo-a"},
		Recents:       []string{"/repo-b"},
		ShowAuthor:    true,
		ShowTimestamp: true,
		ShowDiffStats: &off,
	})
	for _, path := range []string{"/repo-a", "/repo-b"} {
		d := displayForPath(path)
		if !d.ShowAuthor || !d.ShowTime || d.ShowStats {
			t.Fatalf("%s: got %+v, want author+time on, stats off", path, d)
		}
	}
	// Path not in tabs/recents still gets defaults (not legacy).
	d0 := displayForPath("/other")
	if d0.ShowAuthor || d0.ShowTime || !d0.ShowStats {
		t.Fatalf("unseeded path: got %+v", d0)
	}
}

func TestHistoryRowHeight(t *testing.T) {
	tab := &RepoTab{}

	if got := historyRowHeight(tab, KindWorkingTree); got != historyRowMinH {
		t.Fatalf("synthetic height = %v, want %v", got, historyRowMinH)
	}

	// Default-like: stats only → two lines (legacy 42).
	tab.showAuthor, tab.showTime, tab.showStats = false, false, true
	if got := historyRowHeight(tab, KindCommit); got != 42 {
		t.Fatalf("stats-only height = %v, want 42", got)
	}

	// All off → single-line min.
	tab.showStats = false
	if got := historyRowHeight(tab, KindCommit); got != historyRowMinH {
		t.Fatalf("subject-only height = %v, want %v", got, historyRowMinH)
	}

	// All on → three lines.
	tab.showAuthor, tab.showTime, tab.showStats = true, true, true
	if got := historyRowHeight(tab, KindCommit); got != 60 {
		t.Fatalf("all-on height = %v, want 60", got)
	}
}

func TestRememberTabDisplay(t *testing.T) {
	prev := appData.displayByPath
	defer func() { appData.displayByPath = prev }()
	appData.displayByPath = nil

	tab := &RepoTab{path: "/tmp/repo", showAuthor: true, showTime: false, showStats: false}
	if !tabDisplayDirty(tab) {
		t.Fatal("expected dirty before remember (differs from defaults)")
	}
	rememberTabDisplay(tab)
	d := displayForPath("/tmp/repo")
	if !d.ShowAuthor || d.ShowTime || d.ShowStats {
		t.Fatalf("got %+v", d)
	}
	if tabDisplayDirty(tab) {
		t.Fatal("expected clean after remember")
	}
	tab.showTime = true
	if !tabDisplayDirty(tab) {
		t.Fatal("expected dirty after toggle")
	}
}
