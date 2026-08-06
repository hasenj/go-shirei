package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
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

// Regression: tab retain/release drops the go-git gate when the last tab closes.
func TestRepoGateRetainRelease(t *testing.T) {
	clearRepoGates()
	repo, _ := gitTestRepo(t)
	clearRepoGates()

	if err := retainRepoPath(repo); err != nil {
		t.Fatal(err)
	}
	if repoGateRefs(repo) != 1 {
		t.Fatalf("refs=%d want 1", repoGateRefs(repo))
	}
	// lockRepo must reuse the retained pool
	r, unlock, err := lockRepo(repo)
	if err != nil || r == nil {
		t.Fatal(err)
	}
	unlock()
	if repoGateRefs(repo) != 1 {
		t.Fatalf("lockRepo must not change refs, got %d", repoGateRefs(repo))
	}

	releaseRepoPath(repo)
	if repoGateRefs(repo) != -1 {
		t.Fatalf("after release refs=%d want absent (-1)", repoGateRefs(repo))
	}
}

// Pool allows repoPoolSize concurrent checkouts of distinct handles.
func TestRepoPoolParallelHolds(t *testing.T) {
	clearRepoGates()
	repo, _ := gitTestRepo(t)
	clearRepoGates()

	type hold struct {
		r      any
		unlock func()
	}
	holds := make([]hold, 0, repoPoolSize)
	seen := map[any]bool{}
	for i := 0; i < repoPoolSize; i++ {
		r, unlock, err := lockRepo(repo)
		if err != nil {
			t.Fatal(err)
		}
		if seen[r] {
			t.Fatalf("duplicate handle at checkout %d", i)
		}
		seen[r] = true
		holds = append(holds, hold{r, unlock})
	}
	if repoPoolOpened(repo) != repoPoolSize {
		t.Fatalf("opened=%d want %d", repoPoolOpened(repo), repoPoolSize)
	}

	// Next acquire blocks until a release.
	blocked := make(chan struct{})
	unblocked := make(chan struct{})
	go func() {
		close(blocked)
		r, unlock, err := lockRepo(repo)
		if err != nil {
			t.Error(err)
			return
		}
		unlock()
		_ = r
		close(unblocked)
	}()
	<-blocked
	select {
	case <-unblocked:
		t.Fatal("acquire should block while pool is fully checked out")
	case <-time.After(80 * time.Millisecond):
	}
	holds[0].unlock()
	select {
	case <-unblocked:
	case <-time.After(3 * time.Second):
		t.Fatal("acquire did not unblock after release")
	}
	for i := 1; i < len(holds); i++ {
		holds[i].unlock()
	}
}

// Concurrent stats/history must stay correct with a multi-handle pool.
func TestRepoPoolConcurrentStats(t *testing.T) {
	clearRepoGates()
	repo, run := gitTestRepo(t)
	writeFile(t, repo, "a.txt", "one\n")
	run("add", "a.txt")
	run("commit", "-m", "init")
	writeFile(t, repo, "a.txt", "two\n")
	run("add", "a.txt")
	run("commit", "-m", "mod")
	h := headHash(t, repo)
	clearRepoGates()

	var wg sync.WaitGroup
	var mu sync.Mutex
	var errs []string
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			st, err := loadCommitStats(repo, h)
			if err != nil {
				mu.Lock()
				errs = append(errs, err.Error())
				mu.Unlock()
				return
			}
			if !st.Ready || st.Files != 1 {
				mu.Lock()
				errs = append(errs, fmt.Sprintf("stats=%+v", st))
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	for _, e := range errs {
		t.Error(e)
	}
	if n := repoPoolOpened(repo); n < 1 || n > repoPoolSize {
		t.Fatalf("opened=%d", n)
	}
}

// Regression: closeTab releases the gate for that path.
func TestCloseTabReleasesRepoGate(t *testing.T) {
	clearRepoGates()
	repo, _ := gitTestRepo(t)
	clearRepoGates()

	prevTabs := appData.tabs
	prevActive := appData.active
	defer func() {
		appData.tabs = prevTabs
		appData.active = prevActive
		clearRepoGates()
	}()

	if err := retainRepoPath(repo); err != nil {
		t.Fatal(err)
	}
	tab := newRepoTab(repo, "t")
	appData.tabs = []*RepoTab{tab}
	appData.active = tab

	closeTab(tab)
	if repoGateRefs(repo) != -1 {
		t.Fatalf("gate still present after closeTab, refs=%d", repoGateRefs(repo))
	}
	if len(appData.tabs) != 0 {
		t.Fatal("tab should be gone")
	}
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
