package main

import (
	"strings"
	"testing"
	"time"

	"go.hasen.dev/shirei"
)

// resetState returns the viewer to a deterministic launch state — package
// vars (and the cached family list) survive between tests in one process,
// and the sample is normally random.
func resetState() {
	filter = ""
	appData.sample = "The quick brown fox jumps over the lazy dog."
	appData.fontSize = 28
	appData.families = nil
	appData.loaded = false
	appData.copiedFam = nil
	appData.copiedAt = time.Time{}
	appData.prewarming = false
}

// Fontviewer’s grid is a live catalog of AllFontFaces() — host install set
// and background-scan timing — so full-UI PNG goldens of the gallery are not
// meaningful regression tests. We keep behavioral coverage instead:
// filter/copy state, empty UI, and a -race prewarm stress test.

func TestVisibleFamiliesFilter(t *testing.T) {
	resetState()
	appData.families = []*FontFamily{
		{Name: "Arial"},
		{Name: "Noto Sans"},
		{Name: "Menlo"},
		{Name: "Noto Sans Mono"},
	}
	appData.loaded = true

	filter = ""
	if n := len(visibleFamilies()); n != 4 {
		t.Fatalf("unfiltered count = %d, want 4", n)
	}

	filter = "noto"
	got := visibleFamilies()
	if len(got) != 2 {
		t.Fatalf("filter %q: got %d families, want 2", filter, len(got))
	}
	for _, fam := range got {
		if !strings.Contains(strings.ToLower(fam.Name), "noto") {
			t.Errorf("unexpected family under filter: %q", fam.Name)
		}
	}

	filter = "no such font family"
	if n := len(visibleFamilies()); n != 0 {
		t.Fatalf("empty filter match: got %d, want 0", n)
	}
	resetState()
}

func TestLoadFamiliesSkipsDotPrefixed(t *testing.T) {
	resetState()
	shirei.InitFontSubsystem()
	// Wait briefly so a background system scan can register faces; without
	// that the list may be only the critical set (still fine for this check).
	time.Sleep(200 * time.Millisecond)
	fams := loadFamilies()
	for _, fam := range fams {
		if strings.HasPrefix(fam.Name, ".") {
			t.Fatalf("dot-prefixed family should be skipped: %q", fam.Name)
		}
	}
	resetState()
}

// Empty filter match: chrome only, no per-family samples. Still host-font
// dependent for label metrics, so this is a same-machine refactor rail.
func TestSnapshotNoMatch(t *testing.T) {
	resetState()
	// Fixed empty catalog so the empty-state panel is not a function of
	// which system fonts the scan has finished loading.
	appData.families = []*FontFamily{
		{Name: "Fixture A"},
		{Name: "Fixture B"},
	}
	appData.loaded = true
	filter = "no such font family"
	r := shirei.Snapshot(t.Name(), "gallery_no_match", 1120, 500, RootView)
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
	resetState()
}

// TestPrewarmRace renders frames while a background goroutine prewarms fonts
// — the exact windowed-mode concurrency — so `go test -race` proves the
// render thread and the parser don't race on the font table.
func TestPrewarmRace(t *testing.T) {
	resetState()
	shirei.InitFontSubsystem()
	appData.families = loadFamilies()
	appData.loaded = true
	appData.prewarming = true
	if len(appData.families) == 0 {
		t.Skip("no system fonts")
	}

	fams := appData.families
	if len(fams) > 40 {
		fams = fams[:40]
	}

	done := make(chan struct{})
	go func() {
		for _, fam := range fams {
			shirei.PrewarmFont(fam.fid)
		}
		close(done)
	}()

	for {
		select {
		case <-done:
			resetState()
			return
		default:
			shirei.RenderToImage(1120, 760, RootView)
		}
	}
}
