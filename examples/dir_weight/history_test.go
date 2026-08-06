package main

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestNormalizePathsDedupeAndCap(t *testing.T) {
	got := normalizePaths([]string{"/a", "/a/", "", ".", "/b", "/a"})
	if len(got) != 2 || got[0] != filepath.Clean("/a") || got[1] != filepath.Clean("/b") {
		t.Fatalf("got %v", got)
	}
}

func TestRememberPathMRUAndDedupe(t *testing.T) {
	tmp := t.TempDir()
	historyPathOverride = filepath.Join(tmp, "history.json")
	t.Cleanup(func() { historyPathOverride = "" })

	historyMu.Lock()
	appData.recents = nil
	historyMu.Unlock()

	rememberPath("/z")
	rememberPath("/y")
	rememberPath("/z")
	got := snapshotRecents()
	if len(got) != 2 || got[0] != filepath.Clean("/z") || got[1] != filepath.Clean("/y") {
		t.Fatalf("MRU+dedupe: got %v", got)
	}

	// Wait for debounced write + possible dirty loop, then reload.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		historyMu.Lock()
		saving := historySaving
		historyMu.Unlock()
		if !saving {
			if _, err := os.Stat(historyPathOverride); err == nil {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}

	historyMu.Lock()
	appData.recents = nil
	historyMu.Unlock()
	loadHistory()
	got = snapshotRecents()
	if len(got) != 2 || got[0] != filepath.Clean("/z") {
		t.Fatalf("reload: got %v", got)
	}
}

func TestCandidatePathsRecentsBeforeDefaults(t *testing.T) {
	custom := filepath.Clean("/custom/path")
	historyMu.Lock()
	appData.recents = []string{custom}
	historyMu.Unlock()
	list := candidatePaths()
	if list[0] != custom {
		t.Fatalf("recents should lead: %v", list)
	}
	seen := map[string]int{}
	for _, p := range list {
		seen[p]++
		if seen[p] > 1 {
			t.Fatalf("duplicate %q in %v", p, list)
		}
	}
}

// TestHistoryConcurrentRememberNoRace: concurrent MRU updates + debounced
// save must not race under -race (snapshot under historyMu).
func TestHistoryConcurrentRememberNoRace(t *testing.T) {
	tmp := t.TempDir()
	historyPathOverride = filepath.Join(tmp, "history.json")
	t.Cleanup(func() { historyPathOverride = "" })

	historyMu.Lock()
	appData.recents = nil
	historyMu.Unlock()

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				rememberPath(filepath.Join("/p", itoa(i), itoa(j)))
			}
		}(i)
	}
	wg.Wait()

	// Drain save loop.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		historyMu.Lock()
		saving := historySaving
		historyMu.Unlock()
		if !saving {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	got := snapshotRecents()
	if len(got) == 0 || len(got) > maxRecents {
		t.Fatalf("unexpected recents len %d", len(got))
	}
}
