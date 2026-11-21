package main

import (
	"testing"

	"go.hasen.dev/shirei/examples/internal/themetest"
)

func TestSnapshotBoard(t *testing.T) {
	matches = nil
	active = nil
	newGameOpen = false
	addMatch(19)
	for _, m := range [][2]int{{3, 3}, {15, 15}, {3, 15}, {15, 3}, {2, 5}, {16, 13}} {
		if ok, reason := active.game.Play(m[0], m[1]); !ok {
			t.Fatalf("play %v: %s", m, reason)
		}
	}
	themetest.Snapshot(t, "board", winW, winH, RootView)
}

func TestSnapshotNine(t *testing.T) {
	matches = nil
	active = nil
	newGameOpen = false
	addMatch(9)
	for _, m := range [][2]int{{2, 2}, {6, 6}, {2, 6}, {6, 2}} {
		if ok, reason := active.game.Play(m[0], m[1]); !ok {
			t.Fatalf("play %v: %s", m, reason)
		}
	}
	themetest.Snapshot(t, "board9", winW, winH, RootView)
}

func TestSnapshotNewGame(t *testing.T) {
	matches = nil
	active = nil
	addMatch(19)
	newSize = 9
	newGameOpen = true
	themetest.Snapshot(t, "new_game", winW, winH, RootView)
}
