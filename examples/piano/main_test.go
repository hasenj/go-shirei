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

// TestSnapshotPianoMain renders the idle piano: merged title bar (icon,
// title, voice, volume) and the full keyboard (white row + floated black
// keys with keycap chips).
func TestSnapshotPianoMain(t *testing.T) {
	checkSnap(t, shirei.Snapshot(t.Name(), "piano_main", winW, winH, RootView))
}

// TestSnapshotPianoPressed renders with G (G4, white) and Y (G#4, black)
// held down: covers the pressed key styling on both key colors and the
// pressed keycap chips. DownKeys is injected inside the frame because
// RenderToImage resets the input session before the settle loop.
func TestSnapshotPianoPressed(t *testing.T) {
	checkSnap(t, shirei.Snapshot(t.Name(), "piano_pressed", winW, winH, func() {
		shirei.GetInputState().DownKeys = []shirei.KeyCode{shirei.KeyG, shirei.KeyY}
		RootView()
	}))
}
