package main

import (
	"testing"

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

// resetState returns the gallery to its launch state — package vars survive
// between tests in one process.
func resetState() {
	filter = ""
	selected = nil
	copied = nil
}

func iconNamed(t *testing.T, name string) *NamedIcon {
	t.Helper()
	for _, ic := range allIcons {
		if ic.Name == name {
			return ic
		}
	}
	t.Fatalf("icon %s not in the generated table", name)
	return nil
}

func TestSnapshotGallery(t *testing.T) {
	resetState()
	checkSnap(t, shirei.Snapshot(t.Name(), "gallery", 1080, 720, RootView))
}

// Filtered grid regrouped to the matches, with a selection inspected in the
// footer (name, family, codepoint, copy button + "copied" note, snippet).
func TestSnapshotFilteredSelected(t *testing.T) {
	resetState()
	filter = "arrow"
	selected = iconNamed(t, "TypArrowUpThick")
	copied = selected
	checkSnap(t, shirei.Snapshot(t.Name(), "gallery_filtered", 1080, 720, RootView))
	resetState()
}

func TestSnapshotNoMatch(t *testing.T) {
	resetState()
	filter = "no such icon"
	checkSnap(t, shirei.Snapshot(t.Name(), "gallery_no_match", 1080, 500, RootView))
	resetState()
}
