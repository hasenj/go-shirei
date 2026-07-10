package main

import (
	"testing"
	"time"

	"go.hasen.dev/shirei"
	"go.hasen.dev/shirei/internal/snaptest"
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
}

// Default grid: two columns of cards, each family previewing the sample in
// its own face with the name labelled above.
func TestSnapshotGallery(t *testing.T) {
	resetState()
	snaptest.Snapshot(t, "gallery", 1120, 760, RootView)
	resetState()
}

// Larger preview size drives taller cards and more wrapping — guards the
// slider-driven cardHeight math.
func TestSnapshotLarge(t *testing.T) {
	resetState()
	appData.fontSize = 60
	snaptest.Snapshot(t, "gallery_large", 1120, 760, RootView)
	resetState()
}

// Filtering narrows the grid by family name (host-dependent set — goldens
// are refactor guard-rails, not cross-platform artifacts).
func TestSnapshotFiltered(t *testing.T) {
	resetState()
	filter = "arial"
	snaptest.Snapshot(t, "gallery_filtered", 1120, 760, RootView)
	resetState()
}

// The transient confirmation badge shown on the card whose name was just
// copied (driven by app state, so it renders headlessly — hover, which is
// input-driven, can't be reproduced through RenderToImage's reset).
func TestSnapshotCopied(t *testing.T) {
	resetState()
	shirei.InitFontSubsystem()
	appData.families = loadFamilies()
	appData.loaded = true // keep our pointers; don't let RootView reload
	if len(appData.families) == 0 {
		t.Skip("no system fonts to copy")
	}
	appData.copiedFam = appData.families[0]
	appData.copiedAt = time.Now()
	snaptest.Snapshot(t, "gallery_copied", 1120, 760, RootView)
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

	// A subset keeps the test quick while still overlapping parses with
	// renders on the shared face table.
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

// Empty state when nothing matches the filter.
func TestSnapshotNoMatch(t *testing.T) {
	resetState()
	filter = "no such font family"
	snaptest.Snapshot(t, "gallery_no_match", 1120, 500, RootView)
	resetState()
}
