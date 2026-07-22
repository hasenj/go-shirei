// git_history: read-only git history and stacked unified-diff viewer.
//
// Multiple repos open as tabs. Left: commit history. Right: message + stats
// and a continuous virtualized diff stream.
//
//	go run .                 # GUI; opens cwd's repo if any
//	go run . /path/to/repo
//	go run . --png out.png   # headless frame
package main

import (
	"fmt"
	"os"
	"sync"
	"sync/atomic"

	app "go.hasen.dev/shirei/app"

	. "go.hasen.dev/shirei"
)

// Single-flight loader: at most one load at a time, always chasing the latest
// selection on the tab that requested it. Optional light prefetch — see
// prefetchAhead (0 = off, 1 = next row).

func main() {
	repoArg := ""
	pngPath := ""
	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--png":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "--png requires an output path")
				os.Exit(2)
			}
			pngPath = args[i+1]
			i++
		default:
			if stringsHasPrefixDash(args[i]) {
				fmt.Fprintf(os.Stderr, "unknown flag %s\n", args[i])
				os.Exit(2)
			}
			if repoArg == "" {
				repoArg = args[i]
			}
		}
	}

	start := repoArg
	if start == "" {
		start, _ = os.Getwd()
	}

	if pngPath != "" {
		// Headless: open one tab, load sync, render. Prefer a real commit over
		// Working tree / Staging so marketing frames show history + diffs even
		// when the target repo is dirty. Preload sidebar +/− stats for every
		// commit in the first history page (GUI loads them lazily on paint).
		if tab, err := openRepoTab(start); err != nil {
			fmt.Fprintln(os.Stderr, err)
		} else {
			if err := refreshHistorySync(tab, true); err != nil {
				fmt.Fprintln(os.Stderr, err)
			}
			for _, e := range tab.history {
				if e.Kind == KindCommit {
					tab.selected = e.ID
					break
				}
			}
			if tab.selected != "" {
				if err := loadSelectedSync(tab); err != nil {
					fmt.Fprintln(os.Stderr, err)
				}
			}
			preloadHistoryStats(tab)
		}
		if err := RenderToPNG(pngPath, 1100, 700, RootView); err != nil {
			fmt.Println("render to png failed:", err)
			os.Exit(1)
		}
		return
	}

	// GUI: restore session / CLI path / cwd. History loads only for the active tab.
	bootstrapTabs(repoArg, start)

	app.SetupIconBytes(iconPNG)
	app.SetupWindow("git_history", 1100, 700)
	app.Run(RootView)
}

// bootstrapTabs restores session tabs (lazy) or opens a CLI/cwd path.
func bootstrapTabs(repoArg, cwd string) {
	// Always load recents + per-repo display opts; restore open tabs only when no CLI path was given.
	if s, err := loadSession(); err == nil {
		appData.recents = s.Recents
		applySessionDisplay(s)
		if repoArg == "" && len(s.Tabs) > 0 {
			if active := restoreSessionTabs(s); active != nil {
				ensureTabLoaded(active)
				return
			}
		}
	}

	path := repoArg
	if path == "" {
		path = cwd
	}
	if tab, err := openRepoTab(path); err != nil {
		appData.browseCwd = path
	} else {
		ensureTabLoaded(tab)
	}
}

// ensureTabLoaded starts history load if this tab has never been loaded.
// Frame-path safe when called from click handlers (no WithFrameLock).
func ensureTabLoaded(t *RepoTab) {
	if t == nil || t.loaded || t.listLoading {
		return
	}
	go refreshHistory(t, true)
}

func stringsHasPrefixDash(s string) bool {
	return len(s) > 0 && s[0] == '-'
}

// openRepoTab resolves path to a work tree, creates a tab (or focuses an
// existing one for the same path), and activates it. Does not load history
// (call ensureTabLoaded). Updates recents + session.
func openRepoTab(path string) (*RepoTab, error) {
	t, err := openRepoTabLazy(path)
	if err != nil {
		return nil, err
	}
	appData.active = t
	rememberRecent(t.path)
	scheduleSaveSession()
	return t, nil
}

// closeTab removes t and activates a neighbour (haystack pattern).
func closeTab(t *RepoTab) {
	idx := -1
	for i, x := range appData.tabs {
		if x == t {
			idx = i
			break
		}
	}
	if idx < 0 {
		return
	}
	stopTabWatch(t)
	appData.tabs = append(appData.tabs[:idx], appData.tabs[idx+1:]...)
	if appData.active == t {
		if len(appData.tabs) == 0 {
			appData.active = nil
		} else {
			appData.active = appData.tabs[min(idx, len(appData.tabs)-1)]
			ensureTabLoaded(appData.active)
		}
	}
	scheduleSaveSession()
	RequestNextFrame()
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// refreshHistory reloads the tab's sidebar (first page only).
func refreshHistory(t *RepoTab, pickDefault bool) {
	if t == nil {
		return
	}
	WithFrameLock(func() {
		t.listLoading = true
		t.listErr = ""
		t.historyLoadingMore = false
	})
	RequestNextFrame()

	repo := t.path
	entries, after, hasMore, err := loadHistory(repo)

	var loadEntry HistoryEntry
	var loadGen int
	var loadOK, cached bool

	WithFrameLock(func() {
		if !tabStillOpen(t) {
			return
		}
		t.listLoading = false
		if err != nil {
			t.listErr = err.Error()
			return
		}
		t.history = entries
		t.historyAfter = after
		t.historyHasMore = hasMore
		t.listErr = ""
		t.loaded = true
		t.docCache.clear()
		t.clearStats()
		invalidateStatusCache()
		t.doc = nil
		t.docID = ""
		needSelect := pickDefault || t.selected == "" || !selectionStillValid(entries, t.selected)
		if needSelect {
			id := defaultSelection(entries)
			if id == "" {
				t.selected = ""
				t.docErr = ""
				t.docLoading = false
				return
			}
			loadEntry, _, loadGen, loadOK, cached = beginSelect(t, id)
		}
	})
	RequestNextFrame()
	// Live updates once the tab has a successful load.
	if err == nil {
		startTabWatch(t)
		if h, e := currentHeadHash(repo); e == nil {
			WithFrameLock(func() {
				if tabStillOpen(t) {
					t.watchedHead = h
				}
			})
		}
	}
	if loadOK && !cached {
		requestLoad(t, t.path, loadEntry, loadGen)
	} else if loadOK && cached {
		go prefetchAround(t, loadEntry.ID)
	}
}

func refreshHistorySync(t *RepoTab, pickDefault bool) error {
	if t == nil {
		return fmt.Errorf("no tab")
	}
	t.listLoading = true
	entries, after, hasMore, err := loadHistory(t.path)
	t.listLoading = false
	if err != nil {
		t.listErr = err.Error()
		return err
	}
	t.history = entries
	t.historyAfter = after
	t.historyHasMore = hasMore
	t.listErr = ""
	t.loaded = true
	if pickDefault || t.selected == "" || !selectionStillValid(entries, t.selected) {
		t.selected = defaultSelection(entries)
	}
	return nil
}

// maybeLoadMoreHistory fetches the next log page when the UI nears the end.
// Frame-path safe: no WithFrameLock (lock already held).
func maybeLoadMoreHistory(t *RepoTab) {
	if t == nil {
		return
	}
	if !t.historyHasMore || t.historyLoadingMore || t.listLoading {
		return
	}
	if t.historyAfter == "" {
		return
	}
	t.historyLoadingMore = true
	repo := t.path
	after := t.historyAfter
	go func() {
		more, nextAfter, hasMore, err := loadMoreHistory(repo, after)
		WithFrameLock(func() {
			if !tabStillOpen(t) {
				return
			}
			t.historyLoadingMore = false
			if after != t.historyAfter {
				return
			}
			if err != nil {
				t.listErr = err.Error()
				return
			}
			seen := make(map[string]struct{}, len(t.history))
			for _, e := range t.history {
				seen[e.ID] = struct{}{}
			}
			for _, e := range more {
				if _, ok := seen[e.ID]; ok {
					continue
				}
				t.history = append(t.history, e)
			}
			t.historyAfter = nextAfter
			t.historyHasMore = hasMore
		})
		RequestNextFrame()
	}()
}

// selectEntry is safe on the UI thread for the active tab.
func selectEntry(t *RepoTab, id string) {
	if t == nil {
		return
	}
	entry, repo, gen, ok, cached := beginSelect(t, id)
	RequestNextFrame()
	if !ok {
		t.docLoading = false
		t.docErr = "entry not found"
		return
	}
	if cached {
		go prefetchAround(t, id)
		return
	}
	requestLoad(t, repo, entry, gen)
}

func beginSelect(t *RepoTab, id string) (entry HistoryEntry, repo string, gen int, ok bool, cached bool) {
	t.selected = id
	t.docErr = ""
	t.docGen++
	gen = t.docGen
	repo = t.path

	for _, e := range t.history {
		if e.ID == id {
			entry = e
			ok = true
			break
		}
	}
	if !ok {
		return
	}

	if t.docID == id && t.doc != nil {
		t.docLoading = false
		t.rememberStats(id, t.doc)
		return entry, repo, gen, true, true
	}
	if doc := t.docCache.get(id); doc != nil {
		t.doc = doc
		t.docID = id
		t.docLoading = false
		t.rememberStats(id, doc)
		return entry, repo, gen, true, true
	}

	t.docLoading = true
	return entry, repo, gen, true, false
}

type loadTarget struct {
	tab   *RepoTab
	repo  string
	entry HistoryEntry
	gen   int
}

var (
	loadOnce    sync.Once
	loadWake    = make(chan struct{}, 1)
	targetMu    sync.Mutex
	loadPending *loadTarget
)

func requestLoad(t *RepoTab, repo string, entry HistoryEntry, gen int) {
	targetMu.Lock()
	loadPending = &loadTarget{tab: t, repo: repo, entry: entry, gen: gen}
	targetMu.Unlock()
	loadOnce.Do(func() { go loadWorker() })
	select {
	case loadWake <- struct{}{}:
	default:
	}
}

func takePending() *loadTarget {
	targetMu.Lock()
	defer targetMu.Unlock()
	t := loadPending
	loadPending = nil
	return t
}

func loadWorker() {
	for range loadWake {
		for {
			lt := takePending()
			if lt == nil {
				break
			}
			t := lt.tab
			var stale bool
			WithFrameLock(func() {
				stale = !tabStillOpen(t) || lt.gen != t.docGen
			})
			if stale {
				continue
			}
			var fromCache bool
			WithFrameLock(func() {
				if !tabStillOpen(t) {
					return
				}
				if doc := t.docCache.get(lt.entry.ID); doc != nil && t.selected == lt.entry.ID {
					t.doc = doc
					t.docID = lt.entry.ID
					t.docLoading = false
					t.docErr = ""
					t.rememberStats(lt.entry.ID, doc)
					fromCache = true
				}
			})
			if fromCache {
				RequestNextFrame()
				go prefetchAround(t, lt.entry.ID)
				continue
			}

			doc, err := loadDiffDoc(lt.repo, lt.entry)
			WithFrameLock(func() {
				if !tabStillOpen(t) || lt.gen != t.docGen {
					return
				}
				t.docLoading = false
				if err != nil {
					t.docErr = err.Error()
					return
				}
				t.doc = doc
				t.docID = lt.entry.ID
				t.docErr = ""
				t.docCache.put(lt.entry.ID, doc)
				t.rememberStats(lt.entry.ID, doc)
			})
			RequestNextFrame()
			go prefetchAround(t, lt.entry.ID)
		}
	}
}

// stats workers: fill sidebar +/− without blocking history navigation.
const statsWorkers = 2

type statsReq struct {
	tab *RepoTab
	id  string
}

var (
	statsOnce sync.Once
	statsCh   = make(chan statsReq, 64)
)

func ensureStatsWorkers() {
	statsOnce.Do(func() {
		for i := 0; i < statsWorkers; i++ {
			go statsWorker()
		}
	})
}

// requestCommitStats is frame-path safe (no WithFrameLock).
func requestCommitStats(t *RepoTab, id string) {
	if t == nil || id == "" || id == idWorkingTree || id == idStaging {
		return
	}
	if st, ok := t.commitStats[id]; ok && st.Ready {
		return
	}
	if t.statsInflight[id] {
		return
	}
	if t.statsInflight == nil {
		t.statsInflight = map[string]bool{}
	}
	t.statsInflight[id] = true
	ensureStatsWorkers()
	select {
	case statsCh <- statsReq{tab: t, id: id}:
	default:
		delete(t.statsInflight, id)
	}
}

func statsWorker() {
	for req := range statsCh {
		t, id := req.tab, req.id
		repo := ""
		WithFrameLock(func() {
			if tabStillOpen(t) {
				repo = t.path
			}
		})
		if repo == "" {
			continue
		}
		st, err := loadCommitStats(repo, id)
		WithFrameLock(func() {
			if !tabStillOpen(t) {
				return
			}
			delete(t.statsInflight, id)
			if err != nil {
				return
			}
			if t.commitStats == nil {
				t.commitStats = map[string]CommitStats{}
			}
			t.commitStats[id] = st
		})
		RequestNextFrame()
	}
}

const prefetchAhead = 1

var prefetchGen atomic.Uint64

func prefetchAround(t *RepoTab, centerID string) {
	if prefetchAhead <= 0 || t == nil {
		return
	}
	gen := prefetchGen.Add(1)
	var repo string
	var next HistoryEntry
	var ok bool
	WithFrameLock(func() {
		if !tabStillOpen(t) {
			return
		}
		repo = t.path
		hist := t.history
		idx := -1
		for i, e := range hist {
			if e.ID == centerID {
				idx = i
				break
			}
		}
		if idx < 0 {
			return
		}
		for off := 1; off <= prefetchAhead; off++ {
			j := idx + off
			if j >= len(hist) {
				break
			}
			if t.docCache.has(hist[j].ID) {
				continue
			}
			next = hist[j]
			ok = true
			break
		}
	})
	if !ok {
		return
	}
	if prefetchGen.Load() != gen {
		return
	}
	var stillHere bool
	WithFrameLock(func() {
		if !tabStillOpen(t) {
			return
		}
		stillHere = t.selected == centerID || t.selected == next.ID
		if t.docCache.has(next.ID) {
			stillHere = false
		}
	})
	if !stillHere {
		return
	}
	doc, err := loadDiffDoc(repo, next)
	if err != nil {
		return
	}
	if prefetchGen.Load() != gen {
		return
	}
	WithFrameLock(func() {
		if !tabStillOpen(t) {
			return
		}
		t.docCache.put(next.ID, doc)
		t.rememberStats(next.ID, doc)
	})
}

func loadSelectedSync(t *RepoTab) error {
	if t == nil {
		return fmt.Errorf("no tab")
	}
	id := t.selected
	var entry HistoryEntry
	found := false
	for _, e := range t.history {
		if e.ID == id {
			entry = e
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("entry not found")
	}
	if doc := t.docCache.get(id); doc != nil {
		t.doc = doc
		t.docID = id
		t.docLoading = false
		t.docErr = ""
		t.rememberStats(id, doc)
		return nil
	}
	t.docLoading = true
	doc, err := loadDiffDoc(t.path, entry)
	t.docLoading = false
	if err != nil {
		t.docErr = err.Error()
		return err
	}
	t.doc = doc
	t.docID = id
	t.docErr = ""
	t.docCache.put(id, doc)
	t.rememberStats(id, doc)
	return nil
}

// preloadHistoryStats fills sidebar +/−/files for every commit currently in
// the history list (first page after open). Used by --png so the frame is not
// missing lazy stats that the GUI would load a frame later.
func preloadHistoryStats(t *RepoTab) {
	if t == nil || !t.showStats {
		return
	}
	if t.commitStats == nil {
		t.commitStats = map[string]CommitStats{}
	}
	for _, e := range t.history {
		if e.Kind != KindCommit {
			continue
		}
		if st, ok := t.commitStats[e.ID]; ok && st.Ready {
			continue
		}
		st, err := loadCommitStats(t.path, e.ID)
		if err != nil {
			continue
		}
		t.commitStats[e.ID] = st
	}
}
