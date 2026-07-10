package main

import (
	"testing"

	"go.hasen.dev/shirei"
	"go.hasen.dev/shirei/internal/snaptest"
)

// TestSnapshotPianoMain renders the idle piano: header, voice toolbar, the
// full keyboard (white row + floated black keys with keycap chips), and the
// status bar hint.
func TestSnapshotPianoMain(t *testing.T) {
	snaptest.Snapshot(t, "piano_main", winW, winH, RootView)
}

// TestSnapshotPianoPressed renders with G (G4, white) and Y (G#4, black)
// held down: covers the pressed key styling on both key colors, the pressed
// keycap chips, and the sounding-notes readout in the status bar.
// DownKeys is injected inside the frame because RenderToImage resets the
// input session before the settle loop.
func TestSnapshotPianoPressed(t *testing.T) {
	snaptest.Snapshot(t, "piano_pressed", winW, winH, func() {
		shirei.InputState.DownKeys = []shirei.KeyCode{shirei.KeyG, shirei.KeyY}
		RootView()
	})
}
