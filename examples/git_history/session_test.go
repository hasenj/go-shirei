package main

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	. "go.hasen.dev/shirei"
)

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

// Regression: background session save snapshots under the frame lock so it
// does not race UI mutations of tabs/recents. Run with -race.
func TestSessionBackgroundSnapshotNoRace(t *testing.T) {
	prevTabs := appData.tabs
	prevActive := appData.active
	prevRecents := appData.recents
	prevDisplay := appData.displayByPath
	defer func() {
		appData.tabs = prevTabs
		appData.active = prevActive
		appData.recents = prevRecents
		appData.displayByPath = prevDisplay
	}()

	// Point session file at a temp path via config dir override is hard;
	// exercise snapshot under lock only (write path is pure).
	appData.tabs = []*RepoTab{
		{path: "/tmp/r1", showStats: true},
		{path: "/tmp/r2", showAuthor: true, showStats: true},
	}
	appData.active = appData.tabs[0]
	appData.recents = []string{"/tmp/r1"}
	appData.displayByPath = map[string]histDisplay{}

	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			WithFrameLock(func() {
				// Mutate like the UI tab bar.
				if len(appData.tabs) > 0 {
					appData.active = appData.tabs[len(appData.tabs)-1]
				}
				appData.recents = append([]string{"/tmp/rX"}, appData.recents...)
				if len(appData.recents) > 8 {
					appData.recents = appData.recents[:8]
				}
			})
		}
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			var s sessionData
			WithFrameLock(func() {
				s = snapshotSession()
			})
			if len(s.Tabs) == 0 {
				t.Error("empty tabs snapshot")
				return
			}
			_ = s
		}
	}()
	time.Sleep(50 * time.Millisecond)
	close(stop)
	wg.Wait()
}

// Regression: snapshotSession captures open-tab display toggles for save.
func TestSnapshotSessionCapturesTabs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "repo")

	prevTabs := appData.tabs
	prevActive := appData.active
	prevRecents := appData.recents
	prevDisplay := appData.displayByPath
	defer func() {
		appData.tabs = prevTabs
		appData.active = prevActive
		appData.recents = prevRecents
		appData.displayByPath = prevDisplay
	}()
	appData.tabs = []*RepoTab{{path: path, showAuthor: true, showStats: false}}
	appData.active = appData.tabs[0]
	appData.recents = nil
	appData.displayByPath = nil
	got := snapshotSession()
	if len(got.Tabs) != 1 || got.Tabs[0] != path {
		t.Fatalf("tabs: %+v", got.Tabs)
	}
	if got.Display == nil || got.Display[path].ShowAuthor != true {
		t.Fatalf("display: %+v", got.Display)
	}
	if got.Display[path].ShowDiffStats == nil || *got.Display[path].ShowDiffStats {
		t.Fatal("expected showDiffStats false")
	}
}
