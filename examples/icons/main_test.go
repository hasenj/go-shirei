package main

import (
	"testing"

	"go.hasen.dev/shirei/internal/snaptest"
)

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
	snaptest.Snapshot(t, "gallery", 1080, 720, RootView)
}

// Filtered grid regrouped to the matches, with a selection inspected in the
// footer (name, family, codepoint, copy button + "copied" note, snippet).
func TestSnapshotFilteredSelected(t *testing.T) {
	resetState()
	filter = "arrow"
	selected = iconNamed(t, "TypArrowUpThick")
	copied = selected
	snaptest.Snapshot(t, "gallery_filtered", 1080, 720, RootView)
	resetState()
}

func TestSnapshotNoMatch(t *testing.T) {
	resetState()
	filter = "no such icon"
	snaptest.Snapshot(t, "gallery_no_match", 1080, 500, RootView)
	resetState()
}
