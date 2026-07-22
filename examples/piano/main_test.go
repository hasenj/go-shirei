package main

import (
	"testing"

	"go.hasen.dev/shirei"
	"go.hasen.dev/shirei/internal/snaptest"
)

// TestSnapshotPianoMain renders the idle piano: merged title bar (icon,
// title, voice, volume) and the full keyboard (white row + floated black
// keys with keycap chips).
func TestSnapshotPianoMain(t *testing.T) {
	snaptest.Snapshot(t, "piano_main", winW, winH, RootView)
}

// TestSnapshotPianoPressed renders with G (G4, white) and Y (G#4, black)
// held down: covers the pressed key styling on both key colors and the
// pressed keycap chips. DownKeys is injected inside the frame because
// RenderToImage resets the input session before the settle loop.
func TestSnapshotPianoPressed(t *testing.T) {
	snaptest.Snapshot(t, "piano_pressed", winW, winH, func() {
		shirei.GetInputState().DownKeys = []shirei.KeyCode{shirei.KeyG, shirei.KeyY}
		RootView()
	})
}
