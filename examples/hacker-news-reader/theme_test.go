package main

import (
	"testing"

	"go.hasen.dev/shirei/examples/internal/themetest"
)

func TestSnapshotColorSchemes(t *testing.T) {
	previous := appData
	defer func() { appData = previous }()
	appData = &AppState{expanded: map[int]bool{}, kidsLoading: map[int]bool{}}
	seedSampleData(false)
	appData.storyIDs = make([]int, 30)
	themetest.Snapshot(t, "feed", 460, 800, RootView)
	themetest.Snapshot(t, "feed_narrow", 360, 740, RootView)
	seedSampleData(true)
	themetest.Snapshot(t, "post", 460, 800, RootView)
	themetest.Snapshot(t, "post_narrow", 360, 740, RootView)
	themetest.Snapshot(t, "post_wide", 860, 700, RootView)
}
