package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// History and diff models for the git_history viewer.
// Selection is always a HistoryEntry id; the main pane shows its DiffDoc.

type EntryKind int

const (
	KindWorkingTree EntryKind = iota
	KindStaging
	KindCommit
)

const (
	idWorkingTree = "WORK"
	idStaging     = "STAGE"
)

// HistoryEntry is one sidebar row: synthetic dirty slots or a real commit.
type HistoryEntry struct {
	Kind    EntryKind
	ID      string // WORK | STAGE | full hash
	Short   string // empty for synthetic; short hash for commits
	Subject string // empty for synthetic; commit subject for commits
	Author  string    // commit author name; empty for synthetic
	When    time.Time // author timestamp; zero for synthetic
}

func (e HistoryEntry) SidebarLabel() string {
	switch e.Kind {
	case KindWorkingTree:
		return "Working tree"
	case KindStaging:
		return "Staging area"
	default:
		if e.Short == "" {
			return e.Subject
		}
		if e.Subject == "" {
			return e.Short
		}
		return e.Short + "  " + e.Subject
	}
}

// DiffRowKind classifies a single virtual-list row in the stacked diff stream.
type DiffRowKind int

const (
	RowFileHeader DiffRowKind = iota
	RowHunkHeader
	RowContext
	RowAdd
	RowDel
	RowMeta  // binary notice, "\ No newline…", etc.
	RowImage // binary image pair; Text is the file path (optional " (untracked)")
)

// DiffRow is one painted line (or header) in the continuous diff view.
type DiffRow struct {
	Kind DiffRowKind
	Text string
}

// FileStat is one --numstat line.
type FileStat struct {
	Path    string
	Added   int // -1 means binary / unavailable
	Deleted int // -1 means binary / unavailable
	Binary  bool
}

// DiffDoc is everything the main pane needs for the current selection.
type DiffDoc struct {
	// Message / meta (commits only in v1)
	Subject string
	Body    string
	Author  string
	Email   string
	Date    string // ISO-ish from %aI
	Parents []string

	Stats []FileStat
	Rows  []DiffRow
	// Segs partitions Rows by file (built once after Rows are final).
	// Empty when there are no RowFileHeader rows.
	Segs []DiffFileSeg

	// TotalAdded / TotalDeleted sum non-binary numstat lines.
	TotalAdded   int
	TotalDeleted int
	// FileCount is used when Stats is empty (e.g. instant selection stub from
	// sidebar CommitStats) so the header can still show "· N files".
	FileCount int
}

func (d *DiffDoc) recomputeTotals() {
	d.TotalAdded = 0
	d.TotalDeleted = 0
	for _, s := range d.Stats {
		if s.Binary {
			continue
		}
		if s.Added > 0 {
			d.TotalAdded += s.Added
		}
		if s.Deleted > 0 {
			d.TotalDeleted += s.Deleted
		}
	}
}

// CommitStats is a compact +/−/files summary for the history sidebar.
type CommitStats struct {
	Added   int
	Deleted int
	Files   int
	Ready   bool
}

func (s CommitStats) Label() string {
	if !s.Ready {
		return ""
	}
	files := "files"
	if s.Files == 1 {
		files = "file"
	}
	return fmt.Sprintf("+%d −%d · %d %s", s.Added, s.Deleted, s.Files, files)
}

func statsFromDoc(doc *DiffDoc) CommitStats {
	if doc == nil {
		return CommitStats{}
	}
	files := len(doc.Stats)
	// Stubs / mid-stream docs may only carry FileCount from sidebar numstat.
	if files == 0 && doc.FileCount > 0 {
		files = doc.FileCount
	}
	return CommitStats{
		Added:   doc.TotalAdded,
		Deleted: doc.TotalDeleted,
		Files:   files,
		Ready:   true,
	}
}

// rememberStats merges doc totals into the sidebar map without wiping a better
// file count when Stats is still empty (streaming / stub race).
func (t *RepoTab) rememberStats(id string, doc *DiffDoc) {
	if t == nil || id == "" || doc == nil {
		return
	}
	if t.commitStats == nil {
		t.commitStats = map[string]CommitStats{}
	}
	st := statsFromDoc(doc)
	if old, ok := t.commitStats[id]; ok && old.Ready {
		if st.Files == 0 && old.Files > 0 {
			st.Files = old.Files
		}
		// Keep prior totals if doc only has zeros and no per-file stats yet.
		if len(doc.Stats) == 0 && st.Added == 0 && st.Deleted == 0 && (old.Added > 0 || old.Deleted > 0) {
			st.Added = old.Added
			st.Deleted = old.Deleted
		}
	}
	t.commitStats[id] = st
}

// RepoTab is one open repository: its own history, selection, caches, and load gen.
type RepoTab struct {
	id    int    // stable identity for ContainerWithKey
	path  string // work-tree root
	label string // short name for the tab chrome

	// loaded is true after the first successful history load (session restore
	// creates tabs lazy until the user activates them).
	loaded bool

	repoErr     string
	history     []HistoryEntry
	listErr     string
	listLoading bool

	historyAfter       string
	historyHasMore     bool
	historyLoadingMore bool

	selected string

	doc        *DiffDoc
	docID      string
	docErr     string
	docLoading bool
	docGen     int

	docCache      *docCache
	commitStats   map[string]CommitStats
	statsInflight map[string]bool
	// statsSchedMu guards statsInflight for paint + background enqueue + workers.
	// (Frame lock alone is insufficient: bulk enqueue runs off the UI path.)
	statsSchedMu sync.Mutex
	// statsCtx is canceled on clearStats / tab teardown so pure-Go workers
	// abandon mid-commit between files. Replaced with a fresh context after cancel.
	statsCtx    context.Context
	statsCancel context.CancelFunc

	// dirtyCancel aborts this tab's in-flight worktree/staging stream only.
	// Per-tab so a dirty load on another tab cannot cancel this one.
	dirtyCancelMu sync.Mutex
	dirtyCancel   context.CancelFunc

	// Diff find (optional bar; ⌘/Ctrl+F). Per-tab so switching repos keeps
	// queries independent. Query/matches persist while the bar is closed.
	diffFindOpen     bool
	diffFindFocusReq bool // focus the field once after open via shortcut
	findQuery        string
	findMatches      []diffMatch // row-ordered
	findIdx          int         // index into findMatches; -1 if none
	findDocID        string      // doc identity last used to build findMatches
	findQ            string      // query last used to build findMatches

	// History filter (optional bar; ⌘/Ctrl+L). Narrows the sidebar to matching
	// commits (hash / subject / label). Not jump-find — the list itself filters.
	histFindOpen     bool
	histFindFocusReq bool
	histFindQuery    string
	histFindMatches  []int // indices into history when filtering; nil = show all
	histFindQ        string
	histFindN        int // len(history) last used to build matches

	// Commit-list column toggles for this repo (persisted per path).
	showAuthor bool
	showTime   bool
	showStats  bool // default true — matches pre-toggle always-shown stats

	// Image wipe: highlight differing pixels (purple @ 50% when on).
	showImageDiffHL bool

	// Diff collapse projection for the current doc (prefix-sum visible map).
	// Rebuilt when docID changes; collapse toggles stay until the doc changes.
	diffView *DiffView
	// diffPinSource is the source row last seen at the top of the diff viewport
	// (for expand/collapse-all scroll stability).
	diffPinSource int
	// collapsedByDoc remembers collapsed file paths per HistoryEntry.ID for this
	// app session only (not persisted). Restored when revisiting a commit.
	collapsedByDoc map[string]map[string]bool

	// imgPairCache keys "docID\x00path" → decoded old/new sides for ImageWipe.
	// Only entries for the current docID are kept (see pruneDocSideCaches).
	imgPairCache map[string]imgPair
	// imgPairInflight prevents duplicate background loads for the same key.
	imgPairInflight map[string]bool

	// imgDimsCache keys "docID\x00path" → pixel box (max of old/new) from
	// image.DecodeConfig only — used for virtual-list row heights before the
	// full wipe pair is decoded.
	imgDimsCache    map[string]imgDims
	imgDimsInflight map[string]bool

	// Live update (fsnotify). watch is owned by the tab; watchedHead is the
	// last HEAD we applied so auto-refresh can skip full history reloads.
	watch       *repoWatch
	watchedHead string
}

// histDisplay is the persisted commit-list column toggles for one repo path.
type histDisplay struct {
	ShowAuthor bool
	ShowTime   bool
	ShowStats  bool
}

func defaultHistDisplay() histDisplay {
	return histDisplay{ShowStats: true}
}

// App is the window shell: a row of repo tabs plus the active one.
type App struct {
	tabs   []*RepoTab
	active *RepoTab

	nextTabID int

	// recents is MRU work-tree paths (not necessarily open as tabs).
	recents []string

	// displayByPath remembers commit-list toggles per work-tree path
	// (including closed repos). Tabs copy from here on open.
	displayByPath map[string]histDisplay

	// New-tab directory browser (modal FileBrowserPanel).
	browseOpen     bool
	browseCwd      string
	browseFilter   string
	browseSelected int
	browsePick     string // filled by FileBrowserPanel on Choose
}

var appData = &App{}

func newRepoTab(path, label string) *RepoTab {
	appData.nextTabID++
	d := displayForPath(path)
	ctx, cancel := context.WithCancel(context.Background())
	return &RepoTab{
		id:              appData.nextTabID,
		path:            path,
		label:           label,
		docCache:        newDocCache(defaultDiffCacheSize),
		commitStats:     map[string]CommitStats{},
		statsInflight:   map[string]bool{},
		statsCtx:        ctx,
		statsCancel:     cancel,
		showAuthor:      d.ShowAuthor,
		showTime:        d.ShowTime,
		showStats:       d.ShowStats,
		showImageDiffHL: false,
		imgPairCache:    map[string]imgPair{},
		imgPairInflight: map[string]bool{},
		imgDimsCache:    map[string]imgDims{},
		imgDimsInflight: map[string]bool{},
		collapsedByDoc:  map[string]map[string]bool{},
	}
}

// maxCollapsedDocs caps how many docs keep collapse state in collapsedByDoc.
const maxCollapsedDocs = 16

// pruneDocSideCaches keeps image pairs for keepDocID only and caps collapse
// memory so long browsing sessions do not retain every visited commit.
func (t *RepoTab) pruneDocSideCaches(keepDocID string) {
	if t == nil {
		return
	}
	prefix := keepDocID + "\x00"
	if t.imgPairCache != nil {
		for k := range t.imgPairCache {
			if keepDocID == "" || !strings.HasPrefix(k, prefix) {
				delete(t.imgPairCache, k)
			}
		}
	}
	if t.imgPairInflight != nil {
		for k := range t.imgPairInflight {
			if keepDocID == "" || !strings.HasPrefix(k, prefix) {
				delete(t.imgPairInflight, k)
			}
		}
	}
	if t.imgDimsCache != nil {
		for k := range t.imgDimsCache {
			if keepDocID == "" || !strings.HasPrefix(k, prefix) {
				delete(t.imgDimsCache, k)
			}
		}
	}
	if t.imgDimsInflight != nil {
		for k := range t.imgDimsInflight {
			if keepDocID == "" || !strings.HasPrefix(k, prefix) {
				delete(t.imgDimsInflight, k)
			}
		}
	}
	if t.collapsedByDoc == nil {
		return
	}
	if keepDocID != "" && len(t.collapsedByDoc) > maxCollapsedDocs {
		// Drop arbitrary other docs until under cap (keep current).
		for id := range t.collapsedByDoc {
			if id == keepDocID {
				continue
			}
			delete(t.collapsedByDoc, id)
			if len(t.collapsedByDoc) <= maxCollapsedDocs {
				break
			}
		}
	}
}

// clearDocSideCaches drops image and collapse caches (hard refresh).
func (t *RepoTab) clearDocSideCaches() {
	if t == nil {
		return
	}
	clear(t.imgPairCache)
	clear(t.imgPairInflight)
	clear(t.imgDimsCache)
	clear(t.imgDimsInflight)
	clear(t.collapsedByDoc)
}

// displayForPath returns remembered toggles for path, or defaults.
func displayForPath(path string) histDisplay {
	if path != "" && appData.displayByPath != nil {
		if d, ok := appData.displayByPath[path]; ok {
			return d
		}
	}
	return defaultHistDisplay()
}

// tabDisplayOf is the runtime toggles on t as a histDisplay value.
func tabDisplayOf(t *RepoTab) histDisplay {
	if t == nil {
		return defaultHistDisplay()
	}
	return histDisplay{
		ShowAuthor: t.showAuthor,
		ShowTime:   t.showTime,
		ShowStats:  t.showStats,
	}
}

// tabDisplayDirty is true when t's toggles differ from the last remembered
// prefs for its path (or from defaults when nothing is stored yet).
func tabDisplayDirty(t *RepoTab) bool {
	if t == nil || t.path == "" {
		return false
	}
	return tabDisplayOf(t) != displayForPath(t.path)
}

// rememberTabDisplay stores the tab's toggles under its path.
func rememberTabDisplay(t *RepoTab) {
	if t == nil || t.path == "" {
		return
	}
	if appData.displayByPath == nil {
		appData.displayByPath = map[string]histDisplay{}
	}
	appData.displayByPath[t.path] = tabDisplayOf(t)
}

func (t *RepoTab) clearStats() {
	if t == nil {
		return
	}
	clear(t.commitStats)
	t.statsSchedMu.Lock()
	clear(t.statsInflight)
	t.statsSchedMu.Unlock()
	// Cancel in-flight pure-Go jobs; start a fresh context for the next page.
	if t.statsCancel != nil {
		t.statsCancel()
	}
	t.statsCtx, t.statsCancel = context.WithCancel(context.Background())
}

// tryMarkStatsInflight records id as in-flight. Returns false if already marked.
func (t *RepoTab) tryMarkStatsInflight(id string) bool {
	if t == nil || id == "" {
		return false
	}
	t.statsSchedMu.Lock()
	defer t.statsSchedMu.Unlock()
	if t.statsInflight == nil {
		t.statsInflight = map[string]bool{}
	}
	if t.statsInflight[id] {
		return false
	}
	t.statsInflight[id] = true
	return true
}

func (t *RepoTab) clearStatsInflight(id string) {
	if t == nil || id == "" {
		return
	}
	t.statsSchedMu.Lock()
	delete(t.statsInflight, id)
	t.statsSchedMu.Unlock()
}

func (t *RepoTab) hasStatsInflight(id string) bool {
	if t == nil || id == "" {
		return false
	}
	t.statsSchedMu.Lock()
	defer t.statsSchedMu.Unlock()
	return t.statsInflight[id]
}

// cancelDirtyLoad aborts this tab's worktree/staging stream, if any.
func (t *RepoTab) cancelDirtyLoad() {
	if t == nil {
		return
	}
	t.dirtyCancelMu.Lock()
	if t.dirtyCancel != nil {
		t.dirtyCancel()
		t.dirtyCancel = nil
	}
	t.dirtyCancelMu.Unlock()
}

// replaceDirtyCancel installs cancel as the active dirty-load cancel for this
// tab, canceling any previous dirty load on the same tab only.
func (t *RepoTab) replaceDirtyCancel(cancel context.CancelFunc) {
	if t == nil {
		return
	}
	t.dirtyCancelMu.Lock()
	if t.dirtyCancel != nil {
		t.dirtyCancel()
	}
	t.dirtyCancel = cancel
	t.dirtyCancelMu.Unlock()
}

func tabStillOpen(t *RepoTab) bool {
	if t == nil {
		return false
	}
	for _, x := range appData.tabs {
		if x == t {
			return true
		}
	}
	return false
}

// docCache / CacheEntry live in diff_cache.go (cache-owned buffers for flights).
