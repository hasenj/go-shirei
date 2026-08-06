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
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	app "go.hasen.dev/shirei/app"

	. "go.hasen.dev/shirei"
)

// Selection loader: cancelable git CLI work for the latest selection only.
// Full diffs are never prefetched; sidebar stats load in parallel via stats workers.

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
	// Abandon pure-Go sidebar stats, dirty streams, and commit flights for this tab.
	if t.statsCancel != nil {
		t.statsCancel()
	}
	t.cancelDirtyLoad()
	cancelCommitFlightsForRepo(t.path)
	path := t.path
	appData.tabs = append(appData.tabs[:idx], appData.tabs[idx+1:]...)
	// Drop go-git gate when no tab still uses this work tree.
	stillOpen := false
	for _, x := range appData.tabs {
		if x.path == path {
			stillOpen = true
			break
		}
	}
	if !stillOpen {
		releaseRepoPath(path)
	}
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
		cancelCommitFlightsForRepo(t.path)
		t.cancelDirtyLoad()
		t.docCache.clear()
		t.clearDocSideCaches()
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
	// Sidebar +/− for the whole first page (CLI, parallel workers).
	if err == nil {
		enqueueHistoryStats(t)
	}
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
		var doStats bool
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
			doStats = true
		})
		RequestNextFrame()
		if doStats {
			enqueueHistoryStats(t)
		}
	}()
}

// enqueueHistoryStats starts (or continues) ordered sidebar +/− loading for t.
// Work is batched: at most statsBatchSize commits in flight, always the earliest
// unfinished rows in history order — not a free-for-all over the whole list.
// Background only (uses WithFrameLock).
func enqueueHistoryStats(t *RepoTab) {
	pumpHistoryStatsBackground(t)
}

// pumpHistoryStats fills free batch slots with the next history-order commits
// that still lack Ready stats. Frame-path safe (no WithFrameLock).
func pumpHistoryStats(t *RepoTab) {
	startStatsIDs(t, nextStatsToStart(t))
}

// pumpHistoryStatsBackground is for workers / post-refresh (takes frame lock).
func pumpHistoryStatsBackground(t *RepoTab) {
	var ids []string
	WithFrameLock(func() {
		ids = nextStatsToStart(t)
	})
	startStatsIDs(t, ids)
}

func nextStatsToStart(t *RepoTab) []string {
	if t == nil || !tabStillOpen(t) || !t.showStats {
		return nil
	}
	ordered := make([]string, 0, len(t.history))
	for _, e := range t.history {
		if e.Kind == KindCommit {
			ordered = append(ordered, e.ID)
		}
	}
	return nextStatsBatch(ordered, func(id string) bool {
		st, ok := t.commitStats[id]
		return ok && st.Ready
	}, t.hasStatsInflight, statsBatchSize)
}

func startStatsIDs(t *RepoTab, ids []string) {
	if len(ids) == 0 {
		return
	}
	if DEBUG_ENV && t != nil {
		statsSessionFor(t.path).markPump()
		shorts := make([]string, len(ids))
		for i, id := range ids {
			shorts[i] = shortHash(id)
		}
		ready, need := statsProgress(t)
		inflight := 0
		for _, e := range t.history {
			if e.Kind == KindCommit && t.hasStatsInflight(e.ID) {
				inflight++
			}
		}
		statsLog("pump start n=%d ids=%v ready=%d/%d inflight≈%d→%d",
			len(ids), shorts, ready, need, inflight, inflight+len(ids))
	}
	for _, id := range ids {
		requestCommitStatsPrio(t, id, false)
	}
}

// nextStatsBatch picks up to limit commit IDs to start, in list order.
// Already-inflight earlier rows occupy batch slots so later rows wait their turn.
func nextStatsBatch(ordered []string, isReady, isInflight func(string) bool, limit int) []string {
	if limit < 1 || len(ordered) == 0 {
		return nil
	}
	slots := 0
	var start []string
	for _, id := range ordered {
		if isReady(id) {
			continue
		}
		if isInflight(id) {
			slots++
			continue
		}
		if slots >= limit {
			// Window full of earlier work; do not start later commits yet.
			break
		}
		start = append(start, id)
		slots++
	}
	return start
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
		return
	}
	requestLoad(t, repo, entry, gen)
}

func beginSelect(t *RepoTab, id string) (entry HistoryEntry, repo string, gen int, ok bool, cached bool) {
	t.selected = id
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
		t.docErr = ""
		return
	}

	// Successful cache hit — instant switch (arrow thrash). Failed slots are
	// not hits (getReady); reopen them so a re-select retries the load.
	// Do not short-circuit on docID alone: a sticky Ready+Err would look like
	// "already showing" after docErr is cleared.
	if doc := t.docCache.getReady(id); doc != nil {
		t.pruneDocSideCaches(id)
		t.doc = doc
		t.docID = id
		t.docLoading = false
		t.docErr = ""
		t.diffView = nil
		t.rememberStats(id, doc)
		return entry, repo, gen, true, true
	}
	t.docCache.reopenFailed(id)

	// Acquire cache-owned buffer (may already be Loading from a flight).
	ce, created := t.docCache.acquire(id)
	if created || (ce.Doc != nil && ce.Doc.Subject == "" && len(ce.Doc.Rows) == 0) {
		// Instant stub into the cache slot so this frame paints chrome.
		fillStubDoc(ce.Doc, t, entry)
	}
	t.pruneDocSideCaches(id)
	t.doc = ce.Doc
	t.docID = id
	t.docLoading = !ce.Ready || ce.Err != ""
	t.docErr = ""
	t.diffView = nil
	if ce.Ready && ce.Err == "" {
		t.rememberStats(id, ce.Doc)
		return entry, repo, gen, true, true
	}
	// Ready+Err should have been reopened; treat any residual failure as reload.
	if ce.Ready && ce.Err != "" {
		t.docCache.reopenFailed(id)
		ce, _ = t.docCache.acquire(id)
		fillStubDoc(ce.Doc, t, entry)
		t.doc = ce.Doc
		t.docLoading = true
	}
	return entry, repo, gen, true, false
}

// stubDocFromEntry is a zero-I/O DiffDoc so selection chrome paints this frame.
func stubDocFromEntry(t *RepoTab, e HistoryEntry) *DiffDoc {
	doc := &DiffDoc{}
	fillStubDoc(doc, t, e)
	return doc
}

// fillStubDoc writes selection chrome into an existing cache-owned doc.
func fillStubDoc(doc *DiffDoc, t *RepoTab, e HistoryEntry) {
	if doc == nil {
		return
	}
	doc.Subject = e.Subject
	doc.Author = e.Author
	if !e.When.IsZero() {
		doc.Date = e.When.Format(time.RFC3339)
	}
	if t != nil {
		if st, ok := t.commitStats[e.ID]; ok && st.Ready {
			doc.TotalAdded = st.Added
			doc.TotalDeleted = st.Deleted
			doc.FileCount = st.Files
		}
	}
}

// requestLoad may be called from the UI/frame path (click, keyboard) or from
// background (history refresh). Never take WithFrameLock on the caller's
// goroutine — always hop to a background worker first.
func requestLoad(t *RepoTab, repo string, entry HistoryEntry, gen int) {
	if t == nil {
		return
	}
	go func() {
		if entry.Kind == KindCommit {
			// Single-flight into cache-owned buffer. Selecting away does not
			// cancel the flight — re-select joins the same job / hits Ready.
			requestCommitSelection(t, repo, entry, gen)
			return
		}

		// Dirty slots: cancel this tab's prior dirty load only (not other tabs,
		// not commit flights).
		ctx, cancel := context.WithCancel(context.Background())
		t.replaceDirtyCancel(cancel)
		runDirtySelectionLoad(ctx, t, repo, entry, gen)
	}()
}

// requestCommitSelection attaches the tab UI to a commit flight filling the
// cache slot for entry.ID. Background only (see requestLoad).
func requestCommitSelection(t *RepoTab, repo string, entry HistoryEntry, gen int) {
	var ce *CacheEntry
	var ready bool
	WithFrameLock(func() {
		if !tabStillOpen(t) {
			return
		}
		// beginSelect already acquired the slot on the UI path; re-acquire is
		// the same pointer. Re-sync selection chrome if a race filled Ready.
		// Failed Ready slots are reopened so we do not stick on a permanent err.
		if e := t.docCache.entry(entry.ID); e != nil && e.Ready && e.Err != "" {
			t.docCache.reopenFailed(entry.ID)
		}
		ce, _ = t.docCache.acquire(entry.ID)
		if ce == nil {
			return
		}
		ready = ce.Ready && ce.Err == ""
		if ready && ce.Doc != nil {
			t.doc = ce.Doc
			t.docID = entry.ID
			t.docLoading = false
			t.docErr = ""
			t.rememberStats(entry.ID, ce.Doc)
		}
	})
	if ce == nil {
		return
	}
	if ready {
		RequestNextFrame()
		return
	}

	id := entry.ID
	live := ce.Doc
	// markReady / uiNotify run on flight goroutines only → WithFrameLock OK.
	markReady := func(err error) {
		WithFrameLock(func() {
			if !tabStillOpen(t) {
				return
			}
			t.docCache.markReady(id, err)
			if t.selected == id && t.doc == live {
				t.docLoading = false
				if err != nil {
					t.docErr = err.Error()
				} else {
					t.docErr = ""
					t.rememberStats(id, live)
				}
			}
		})
		RequestNextFrame()
	}
	uiNotify := makeFlightUINotify(t, gen, id, live)
	requestCommitDiff(repo, id, ce, markReady, uiNotify, RequestNextFrame)
}

// makeFlightUINotify updates selection chrome when this tab still shows live.
// Invoked only from flight workers (never the frame builder).
func makeFlightUINotify(t *RepoTab, gen int, id string, live *DiffDoc) func(batch []DiffRow, done bool) {
	return func(batch []DiffRow, done bool) {
		WithFrameLock(func() {
			if !tabStillOpen(t) || t.doc != live {
				return
			}
			if t.selected == id && ltGenCurrent(t, gen) {
				if len(batch) > 0 || done {
					if t.diffView != nil && t.diffView.docID == id {
						if len(live.Segs) > 0 {
							t.diffView.ReplaceSegsPreservingCollapse(live.Segs)
						}
					} else if len(live.Rows) > 0 {
						t.diffView = newDiffView(id, live.Segs)
					}
				}
				if done {
					t.docLoading = false
					t.docErr = ""
					t.rememberStats(id, live)
				}
			}
		})
		RequestNextFrame()
	}
}

// runDirtySelectionLoad streams worktree/staging diffs (not commit flights).
func runDirtySelectionLoad(ctx context.Context, t *RepoTab, repo string, entry HistoryEntry, gen int) {
	stillCurrent := func() bool {
		ok := false
		WithFrameLock(func() {
			ok = tabStillOpen(t) && ltGenCurrent(t, gen) && t.selected == entry.ID
		})
		return ok
	}
	if !stillCurrent() {
		return
	}

	// Cache hit race.
	var cached *DiffDoc
	WithFrameLock(func() {
		if tabStillOpen(t) {
			cached = t.docCache.getReady(entry.ID)
		}
	})
	if cached != nil {
		WithFrameLock(func() {
			if !tabStillOpen(t) || !ltGenCurrent(t, gen) || t.selected != entry.ID {
				return
			}
			t.doc = cached
			t.docID = entry.ID
			t.docLoading = false
			t.docErr = ""
		})
		RequestNextFrame()
		return
	}

	var live *DiffDoc
	WithFrameLock(func() {
		if !tabStillOpen(t) {
			return
		}
		ce, _ := t.docCache.acquire(entry.ID)
		live = ce.Doc
		switch entry.Kind {
		case KindWorkingTree:
			live.Subject = "Working tree changes"
		case KindStaging:
			live.Subject = "Staged changes"
		}
		// Fresh dirty body each load.
		live.Rows, live.Segs, live.Stats = nil, nil, nil
		t.doc = live
		t.docID = entry.ID
		t.docLoading = true
		t.docErr = ""
		t.diffView = nil
	})
	if live == nil {
		return
	}
	RequestNextFrame()

	pub := makePatchPublisher(t, gen, entry.ID, live)
	var err error
	switch entry.Kind {
	case KindWorkingTree:
		err = streamWorkingTreeDiffGo(ctx, repo, pub)
	case KindStaging:
		err = streamStagingDiffGo(ctx, repo, pub)
	default:
		err = fmt.Errorf("unknown dirty kind")
	}
	if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, errStreamAbandoned) {
		return
	}
	if err != nil {
		WithFrameLock(func() {
			if !stillCurrentLocked(t, gen, entry.ID) || t.doc != live {
				return
			}
			live.Rows, live.Segs = nil, nil
			t.diffView = nil
			t.docLoading = false
			t.docErr = err.Error()
			t.docCache.markReady(entry.ID, err)
		})
		RequestNextFrame()
	}
	// Success handled in publish(done=true).
}

func ltGenCurrent(t *RepoTab, gen int) bool {
	return t != nil && t.docGen == gen
}

func stillCurrentLocked(t *RepoTab, gen int, id string) bool {
	return tabStillOpen(t) && ltGenCurrent(t, gen) && t.selected == id
}

// makePatchPublisher appends streamed rows under the frame lock and grows segs.
func makePatchPublisher(t *RepoTab, gen int, id string, live *DiffDoc) patchPublish {
	return func(batch []DiffRow, done bool) bool {
		ok := false
		WithFrameLock(func() {
			if !stillCurrentLocked(t, gen, id) || t.doc != live {
				return
			}
			if len(batch) > 0 {
				prev := len(live.Rows)
				live.Rows = append(live.Rows, batch...)
				var remembered map[string]bool
				if t.collapsedByDoc != nil {
					remembered = t.collapsedByDoc[id]
				}
				if t.diffView != nil && t.diffView.docID == id {
					t.diffView.Grow(live, prev, remembered)
					live.Segs = cloneSegs(t.diffView.segs)
				} else {
					growDocSegs(&live.Segs, live.Rows, prev, len(live.Rows), live.Stats)
				}
			}
			if done {
				// Final segs with best stats (countSegStats via buildDiffFileSegs).
				live.Segs = buildDiffFileSegs(live)
				if len(live.Stats) == 0 && len(live.Segs) > 0 {
					for _, s := range live.Segs {
						live.Stats = append(live.Stats, FileStat{
							Path: s.Path, Added: s.Added, Deleted: s.Deleted, Binary: s.Binary,
						})
					}
					live.recomputeTotals()
				}
				if t.diffView != nil && t.diffView.docID == id {
					t.diffView.ReplaceSegsPreservingCollapse(live.Segs)
				}
				t.docLoading = false
				t.docErr = ""
				// Cache owns the same live pointer (dirty path uses acquire).
				if e := t.docCache.entry(id); e != nil && e.Doc == live {
					t.docCache.markReady(id, nil)
				} else {
					t.docCache.put(id, live)
				}
				t.rememberStats(id, live)
			}
			ok = true
		})
		if ok {
			RequestNextFrame()
		}
		return ok
	}
}

// stats scheduling: ordered batches via pumpHistoryStats (see nextStatsBatch).
// Workers pull a small queue; each completion re-pumps so the next history-order
// commits fill free batch slots. Single-flight per hash via commit flights.
const (
	statsWorkers   = 4
	statsBatchSize = 4 // max concurrent sidebar stats loads per tab (history order)
)

type statsReq struct {
	tab      *RepoTab
	id       string
	enqueued time.Time
	seq      uint64
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

// requestCommitStats enqueues one id (frame-path safe). Prefer pumpHistoryStats
// for bulk list fills so order stays history-first.
func requestCommitStats(t *RepoTab, id string) {
	requestCommitStatsPrio(t, id, false)
}

// requestCommitStatsPrio enqueues a joinable stats request on the flight pool.
// Safe from the frame path and from background bulk enqueue: statsInflight is
// guarded by t.statsSchedMu (not the frame lock).
// hi is reserved for API compatibility; batching uses a single ordered queue.
func requestCommitStatsPrio(t *RepoTab, id string, hi bool) {
	if t == nil || id == "" || id == idWorkingTree || id == idStaging {
		return
	}
	if st, ok := t.commitStats[id]; ok && st.Ready {
		return
	}
	if !t.tryMarkStatsInflight(id) {
		if DEBUG_ENV {
			statsLog("skip already-inflight %s", shortHash(id))
		}
		return
	}
	ensureStatsWorkers()
	req := statsReq{
		tab:      t,
		id:       id,
		enqueued: time.Now(),
		seq:      statsSeq.Add(1),
	}
	select {
	case statsCh <- req:
		if DEBUG_ENV {
			statsLog("enqueue #%d %s", req.seq, shortHash(id))
		}
	default:
		// Queue full: drop mark so a later pump can retry.
		t.clearStatsInflight(id)
		statsLog("DROP queue-full %s", shortHash(id))
	}
	_ = hi
}

func statsWorker() {
	for req := range statsCh {
		t, id := req.tab, req.id
		queueWait := time.Since(req.enqueued)
		repo := ""
		WithFrameLock(func() {
			if tabStillOpen(t) {
				repo = t.path
			}
		})
		if repo == "" {
			t.clearStatsInflight(id)
			statsLog("worker drop closed-tab #%d %s queue=%s", req.seq, shortHash(id), queueWait.Round(time.Microsecond))
			pumpHistoryStatsBackground(t)
			continue
		}
		statsLog("worker begin #%d %s queue=%s", req.seq, shortHash(id), queueWait.Round(time.Microsecond))
		workStart := time.Now()
		// Idempotent join/start on the shared flight for this hash.
		requestCommitFlightStats(repo, id, statsSink{
			apply: func(st CommitStats) {
				if !tabStillOpen(t) {
					return
				}
				t.clearStatsInflight(id)
				if t.commitStats == nil {
					t.commitStats = map[string]CommitStats{}
				}
				t.commitStats[id] = st
				if DEBUG_ENV {
					ready, need := statsProgress(t)
					statsSessionFor(repo).markCommitDone(shortHash(id), ready, need, time.Since(workStart))
				}
			},
			onDone: func() {
				t.clearStatsInflight(id)
				RequestNextFrame()
				// Free a batch slot → start the next history-order commit(s).
				pumpHistoryStatsBackground(t)
			},
		})
	}
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
	if doc := t.docCache.getReady(id); doc != nil {
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
	ctx := t.statsCtx
	if ctx == nil {
		ctx = context.Background()
	}
	for _, e := range t.history {
		if e.Kind != KindCommit {
			continue
		}
		if st, ok := t.commitStats[e.ID]; ok && st.Ready {
			continue
		}
		st, err := loadCommitStatsCtx(ctx, t.path, e.ID)
		if err != nil {
			continue
		}
		t.commitStats[e.ID] = st
	}
}
