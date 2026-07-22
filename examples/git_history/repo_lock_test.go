package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func testRepoPaths(t *testing.T) []string {
	t.Helper()
	// Prefer the monorepo root (and optional second work tree) when present;
	// skip when not available (CI sandboxes without this layout).
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root, err := findRepo(cwd)
	if err != nil {
		t.Skip("no git repo from cwd:", err)
	}
	paths := []string{root}
	// Optional sibling checkout used in local multi-tab sessions.
	alt := filepath.Join(filepath.Dir(root), "public-go.hasen.dev", "shirei")
	if st, err := os.Stat(filepath.Join(alt, ".git")); err == nil && (st.IsDir() || st.Mode().IsRegular()) {
		paths = append(paths, alt)
	}
	return paths
}

func clearRepoGates() {
	repoMu.Lock()
	repos = map[string]*repoGate{}
	repoMu.Unlock()
}

// Regression: go-git Repository is not concurrent-safe. Before per-path
// locking, parallel loadHistory (status + log) and multi-tab loads failed
// intermittently with "object not found".
func TestLoadHistoryConcurrent(t *testing.T) {
	clearRepoGates()
	paths := testRepoPaths(t)

	var wg sync.WaitGroup
	var mu sync.Mutex
	var errs []string
	for n := 0; n < 24; n++ {
		for _, p := range paths {
			wg.Add(1)
			go func(p string) {
				defer wg.Done()
				entries, _, _, err := loadHistory(p)
				if err != nil {
					mu.Lock()
					errs = append(errs, fmt.Sprintf("%s: %v (n=%d)", p, err, len(entries)))
					mu.Unlock()
				}
			}(p)
		}
	}
	wg.Wait()
	for _, e := range errs {
		t.Error(e)
	}
}

func TestLoadHistoryAndStatsConcurrent(t *testing.T) {
	clearRepoGates()
	paths := testRepoPaths(t)
	path := paths[0]

	entries, _, _, err := loadHistory(path)
	if err != nil {
		t.Fatal(err)
	}
	var hashes []string
	for _, e := range entries {
		if e.Kind == KindCommit {
			hashes = append(hashes, e.ID)
			if len(hashes) >= 6 {
				break
			}
		}
	}
	if len(hashes) == 0 {
		t.Skip("no commits")
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	var errs []string
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, _, _, err := loadHistory(path); err != nil {
				mu.Lock()
				errs = append(errs, "history: "+err.Error())
				mu.Unlock()
			}
		}()
		for _, h := range hashes {
			h := h
			wg.Add(1)
			go func() {
				defer wg.Done()
				if _, err := loadCommitStats(path, h); err != nil {
					mu.Lock()
					errs = append(errs, "stats: "+err.Error())
					mu.Unlock()
				}
			}()
		}
	}
	wg.Wait()
	for _, e := range errs {
		t.Error(e)
	}
}
