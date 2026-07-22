package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/format/gitignore"

	. "go.hasen.dev/shirei"
)

// Debounce filesystem bursts (save, go build, git gc) into one refresh.
const watchDebounce = 300 * time.Millisecond

// repoWatch is the per-tab fsnotify state. One watcher per open tab; closed on
// tab close. Refresh uses pure-Go status (same fast path as manual refresh).
type repoWatch struct {
	tab     *RepoTab
	watcher *fsnotify.Watcher
	stop    chan struct{}

	mu       sync.Mutex
	debounce *time.Timer
}

// startTabWatch begins watching t.path (idempotent). Safe from any goroutine.
// Does not hold the frame lock across watcher setup (avoids deadlock with
// newRepoWatch / currentHeadHash paths that also take the lock).
func startTabWatch(t *RepoTab) {
	if t == nil || t.path == "" {
		return
	}
	var already bool
	WithFrameLock(func() {
		already = t.watch != nil
	})
	if already {
		return
	}
	w, err := newRepoWatch(t)
	if err != nil {
		// Non-fatal: manual Refresh still works.
		return
	}
	WithFrameLock(func() {
		if !tabStillOpen(t) || t.watch != nil {
			w.close()
			return
		}
		t.watch = w
	})
}

// stopTabWatch tears down the watcher (idempotent).
func stopTabWatch(t *RepoTab) {
	if t == nil {
		return
	}
	var w *repoWatch
	WithFrameLock(func() {
		w = t.watch
		t.watch = nil
	})
	if w != nil {
		w.close()
	}
}

func newRepoWatch(t *RepoTab) (*repoWatch, error) {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	rw := &repoWatch{
		tab:     t,
		watcher: fw,
		stop:    make(chan struct{}),
	}
	dirs, err := collectWatchDirs(t.path)
	if err != nil {
		fw.Close()
		return nil, err
	}
	for _, d := range dirs {
		_ = fw.Add(d) // best-effort: some dirs may vanish mid-walk
	}
	if h, err := currentHeadHash(t.path); err == nil {
		WithFrameLock(func() {
			if tabStillOpen(t) {
				t.watchedHead = h
			}
		})
	}
	go rw.loop()
	return rw, nil
}

func (w *repoWatch) close() {
	if w == nil {
		return
	}
	select {
	case <-w.stop:
	default:
		close(w.stop)
	}
	w.mu.Lock()
	if w.debounce != nil {
		w.debounce.Stop()
		w.debounce = nil
	}
	w.mu.Unlock()
	if w.watcher != nil {
		_ = w.watcher.Close()
	}
}

func (w *repoWatch) loop() {
	for {
		select {
		case <-w.stop:
			return
		case _, ok := <-w.watcher.Errors:
			if !ok {
				return
			}
		case ev, ok := <-w.watcher.Events:
			if !ok {
				return
			}
			if !watchEventInteresting(w.tab.path, ev) {
				continue
			}
			// New directories: watch them so future edits are seen.
			if ev.Has(fsnotify.Create) {
				if fi, err := os.Stat(ev.Name); err == nil && fi.IsDir() {
					if !watchPathNoisy(w.tab.path, ev.Name) {
						_ = w.watcher.Add(ev.Name)
					}
				}
			}
			w.scheduleRefresh()
		}
	}
}

func (w *repoWatch) scheduleRefresh() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.debounce != nil {
		w.debounce.Stop()
	}
	tab := w.tab
	w.debounce = time.AfterFunc(watchDebounce, func() {
		autoRefreshTab(tab)
	})
}

// watchEventInteresting filters chmod noise, git object DB churn, and junk.
func watchEventInteresting(repoPath string, ev fsnotify.Event) bool {
	// macOS especially emits chmod on touch/scan; content did not change.
	if ev.Op&fsnotify.Chmod != 0 && ev.Op&^fsnotify.Chmod == 0 {
		return false
	}
	return !watchPathNoisy(repoPath, ev.Name)
}

// watchPathNoisy reports paths that should not trigger a refresh.
func watchPathNoisy(repoPath, name string) bool {
	if name == "" {
		return true
	}
	rel, err := filepath.Rel(repoPath, name)
	if err != nil {
		return false
	}
	rel = filepath.ToSlash(rel)
	base := filepath.Base(name)

	switch {
	case base == ".DS_Store", base == "Thumbs.db":
		return true
	case strings.HasSuffix(base, "~"), strings.HasSuffix(base, ".swp"), strings.HasSuffix(base, ".swo"):
		return true
	case strings.HasPrefix(base, ".#"):
		return true
	}

	// .git: care about HEAD, index, refs/, packed-refs — not the object DB.
	if rel == ".git" || strings.HasPrefix(rel, ".git/") {
		rest := strings.TrimPrefix(rel, ".git/")
		if rest == "" {
			return false
		}
		if rest == "objects" || strings.HasPrefix(rest, "objects/") {
			return true
		}
		if strings.HasSuffix(rest, ".lock") {
			return true
		}
		// Common transient/noise files.
		switch rest {
		case "FETCH_HEAD", "ORIG_HEAD", "COMMIT_EDITMSG", "AUTO_MERGE":
			return true
		}
		return false
	}
	return false
}

// collectWatchDirs returns directories for fsnotify.Add.
//
// Same cautions as pure-Go status:
//   - worktree: every non-ignored directory (skip trees gitignore would skip)
//   - git dir: .git + refs hierarchy, never objects/
//
// Hundreds of dir watches are fine; watching every object pack is not.
func collectWatchDirs(repoPath string) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	add := func(d string) {
		d = filepath.Clean(d)
		if d == "" || seen[d] {
			return
		}
		if fi, err := os.Stat(d); err != nil || !fi.IsDir() {
			return
		}
		seen[d] = true
		out = append(out, d)
	}

	add(repoPath)

	gitDir, err := resolveGitDir(repoPath)
	if err == nil && gitDir != "" {
		add(gitDir)
		_ = filepath.WalkDir(gitDir, func(path string, d os.DirEntry, err error) error {
			if err != nil || !d.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(gitDir, path)
			if err != nil {
				return nil
			}
			rel = filepath.ToSlash(rel)
			switch {
			case rel == "objects", strings.HasPrefix(rel, "objects/"):
				return filepath.SkipDir
			case rel == "logs", strings.HasPrefix(rel, "logs/"):
				return filepath.SkipDir
			case rel == "hooks", rel == "modules", rel == "lfs", rel == "cursor":
				return filepath.SkipDir
			}
			add(path)
			return nil
		})
	}

	base, _ := loadBaseIgnorePatterns(repoPath)
	active := append([]gitignore.Pattern(nil), base...)
	matcher := gitignore.NewMatcher(active)

	_ = filepath.WalkDir(repoPath, func(path string, d os.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(repoPath, path)
		if err != nil {
			return nil
		}
		if rel == "." {
			return nil
		}
		if d.Name() == ".git" {
			return filepath.SkipDir // handled above via resolveGitDir
		}
		relSlash := filepath.ToSlash(rel)
		parts := strings.Split(relSlash, "/")
		if matcher != nil && matcher.Match(parts, true) {
			return filepath.SkipDir
		}
		// Nested .gitignore (same lazy approach as status walk).
		extra := readIgnoreFile(filepath.Join(path, ".gitignore"), parts)
		if len(extra) > 0 {
			active = append(active, extra...)
			matcher = gitignore.NewMatcher(active)
		}
		add(path)
		return nil
	})

	return out, nil
}

// resolveGitDir returns the absolute .git directory (handles gitfile worktrees).
func resolveGitDir(repoPath string) (string, error) {
	p := filepath.Join(repoPath, ".git")
	fi, err := os.Stat(p)
	if err != nil {
		return "", err
	}
	if fi.IsDir() {
		return p, nil
	}
	// .git file: "gitdir: /path/to/worktrees/name"
	b, err := os.ReadFile(p)
	if err != nil {
		return "", err
	}
	line := strings.TrimSpace(string(b))
	const pref = "gitdir:"
	if !strings.HasPrefix(strings.ToLower(line), pref) {
		return "", os.ErrNotExist
	}
	g := strings.TrimSpace(line[len(pref):])
	if !filepath.IsAbs(g) {
		g = filepath.Join(repoPath, g)
	}
	return filepath.Clean(g), nil
}

func currentHeadHash(repoPath string) (string, error) {
	r, unlock, err := lockRepo(repoPath)
	if err != nil {
		return "", err
	}
	defer unlock()
	ref, err := r.Head()
	if err == plumbing.ErrReferenceNotFound {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return ref.Hash().String(), nil
}

// autoRefreshTab is the debounced watch handler. Off the UI thread.
//
// Fast path when HEAD is unchanged: pure-Go dirty slots only (~status cost),
// soft-update sidebar WORK/STAGE rows, reload doc if viewing dirty slot.
// Full history reload only when HEAD moves.
func autoRefreshTab(t *RepoTab) {
	if t == nil {
		return
	}
	var open bool
	WithFrameLock(func() {
		open = tabStillOpen(t) && t.loaded && !t.listLoading
	})
	if !open {
		return
	}

	invalidateStatusCache()

	head, err := currentHeadHash(t.path)
	if err != nil {
		return
	}

	var prevHead string
	WithFrameLock(func() {
		if tabStillOpen(t) {
			prevHead = t.watchedHead
		}
	})

	if head != prevHead {
		autoRefreshHistory(t, head)
		return
	}
	autoRefreshDirtySlots(t)
}

// autoRefreshHistory reloads the first history page after HEAD moves.
// Keeps selection when still valid; does not force listLoading flash if possible.
func autoRefreshHistory(t *RepoTab, newHead string) {
	repo := t.path
	entries, after, hasMore, err := loadHistory(repo)

	var loadEntry HistoryEntry
	var loadGen int
	var loadOK, cached, needLoad bool

	WithFrameLock(func() {
		if !tabStillOpen(t) {
			return
		}
		if err != nil {
			t.listErr = err.Error()
			return
		}
		t.watchedHead = newHead
		prevSel := t.selected
		t.history = entries
		t.historyAfter = after
		t.historyHasMore = hasMore
		t.listErr = ""
		// Commit docs for unchanged hashes remain valid; drop dirty-slot docs.
		filterDocCache(t.docCache, func(id string) bool {
			return id != idWorkingTree && id != idStaging
		})
		t.clearStats()

		if prevSel == "" || !selectionStillValid(entries, prevSel) {
			id := defaultSelection(entries)
			if id == "" {
				t.selected = ""
				t.doc = nil
				t.docID = ""
				return
			}
			loadEntry, _, loadGen, loadOK, cached = beginSelect(t, id)
			needLoad = loadOK
		} else if prevSel == idWorkingTree || prevSel == idStaging {
			// Dirty content may have changed with the new commit activity.
			filterDocCache(t.docCache, func(id string) bool { return id != prevSel })
			if t.docID == prevSel {
				t.doc = nil
				t.docID = ""
			}
			loadEntry, _, loadGen, loadOK, cached = beginSelect(t, prevSel)
			needLoad = loadOK
		}
		// Else: still on a commit that exists — keep current doc painted.
	})
	RequestNextFrame()
	if needLoad && loadOK && !cached {
		requestLoad(t, t.path, loadEntry, loadGen)
	} else if needLoad && loadOK && cached {
		go prefetchAround(t, loadEntry.ID)
	}
}

// autoRefreshDirtySlots updates only WORK/STAGE sidebar rows when HEAD is stable.
func autoRefreshDirtySlots(t *RepoTab) {
	slots, err := loadDirtySlots(t.path)
	if err != nil {
		return
	}

	var loadEntry HistoryEntry
	var loadGen int
	var loadOK, cached, needLoad bool

	WithFrameLock(func() {
		if !tabStillOpen(t) {
			return
		}
		prevSel := t.selected
		oldSlots, commits := splitHistorySlots(t.history)
		if dirtySlotsEqual(oldSlots, slots) {
			// Still refresh open dirty doc — file content may have changed
			// even when the set of dirty slots is the same.
			if prevSel == idWorkingTree || prevSel == idStaging {
				filterDocCache(t.docCache, func(id string) bool { return id != prevSel })
				loadEntry, _, loadGen, loadOK, cached = beginSelect(t, prevSel)
				needLoad = loadOK
			}
			return
		}

		t.history = append(append([]HistoryEntry(nil), slots...), commits...)
		// Selection may disappear when a slot becomes clean.
		if prevSel == idWorkingTree || prevSel == idStaging {
			if !selectionStillValid(t.history, prevSel) {
				id := defaultSelection(t.history)
				if id == "" {
					t.selected = ""
					t.doc = nil
					t.docID = ""
					return
				}
				loadEntry, _, loadGen, loadOK, cached = beginSelect(t, id)
				needLoad = loadOK
			} else {
				filterDocCache(t.docCache, func(id string) bool { return id != prevSel })
				loadEntry, _, loadGen, loadOK, cached = beginSelect(t, prevSel)
				needLoad = loadOK
			}
		}
	})
	RequestNextFrame()
	if needLoad && loadOK && !cached {
		requestLoad(t, t.path, loadEntry, loadGen)
	} else if needLoad && loadOK && cached {
		go prefetchAround(t, loadEntry.ID)
	}
}

func splitHistorySlots(history []HistoryEntry) (slots, commits []HistoryEntry) {
	for _, e := range history {
		switch e.Kind {
		case KindWorkingTree, KindStaging:
			slots = append(slots, e)
		default:
			commits = append(commits, e)
		}
	}
	return slots, commits
}

func dirtySlotsEqual(a, b []HistoryEntry) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].ID != b[i].ID || a[i].Kind != b[i].Kind {
			return false
		}
	}
	return true
}

// filterDocCache keeps only entries for which keep(id) is true.
func filterDocCache(c *docCache, keep func(id string) bool) {
	if c == nil {
		return
	}
	n := c.order[:0]
	for _, id := range c.order {
		if keep(id) {
			n = append(n, id)
		} else {
			delete(c.m, id)
		}
	}
	c.order = n
}
