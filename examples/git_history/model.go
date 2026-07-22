package main

import (
	"fmt"
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
	RowMeta // binary notice, "\ No newline…", etc.
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

	// TotalAdded / TotalDeleted sum non-binary numstat lines.
	TotalAdded   int
	TotalDeleted int
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
	return CommitStats{
		Added:   doc.TotalAdded,
		Deleted: doc.TotalDeleted,
		Files:   len(doc.Stats),
		Ready:   true,
	}
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

	// Single bottom-right toast (auto-dismiss + ×).
	toastMsg   string
	toastUntil time.Time // zero = no toast
}

var appData = &App{}

func newRepoTab(path, label string) *RepoTab {
	appData.nextTabID++
	d := displayForPath(path)
	return &RepoTab{
		id:            appData.nextTabID,
		path:          path,
		label:         label,
		docCache:      newDocCache(80),
		commitStats:   map[string]CommitStats{},
		statsInflight: map[string]bool{},
		showAuthor:    d.ShowAuthor,
		showTime:      d.ShowTime,
		showStats:     d.ShowStats,
	}
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

func (t *RepoTab) rememberStats(id string, doc *DiffDoc) {
	if t == nil || id == "" || doc == nil {
		return
	}
	if t.commitStats == nil {
		t.commitStats = map[string]CommitStats{}
	}
	t.commitStats[id] = statsFromDoc(doc)
}

func (t *RepoTab) clearStats() {
	if t == nil {
		return
	}
	clear(t.commitStats)
	clear(t.statsInflight)
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

// docCache is a tiny LRU of loaded DiffDocs keyed by HistoryEntry.ID.
type docCache struct {
	max   int
	order []string // oldest at [0]
	m     map[string]*DiffDoc
}

func newDocCache(max int) *docCache {
	if max < 1 {
		max = 1
	}
	return &docCache{max: max, m: make(map[string]*DiffDoc, max)}
}

func (c *docCache) has(id string) bool {
	if c == nil {
		return false
	}
	_, ok := c.m[id]
	return ok
}

func (c *docCache) get(id string) *DiffDoc {
	if c == nil {
		return nil
	}
	doc, ok := c.m[id]
	if !ok {
		return nil
	}
	for i, k := range c.order {
		if k == id {
			c.order = append(c.order[:i], c.order[i+1:]...)
			break
		}
	}
	c.order = append(c.order, id)
	return doc
}

func (c *docCache) put(id string, doc *DiffDoc) {
	if c == nil || doc == nil || id == "" {
		return
	}
	if _, ok := c.m[id]; ok {
		c.m[id] = doc
		c.get(id)
		return
	}
	c.m[id] = doc
	c.order = append(c.order, id)
	for len(c.order) > c.max {
		old := c.order[0]
		c.order = c.order[1:]
		delete(c.m, old)
	}
}

func (c *docCache) clear() {
	if c == nil {
		return
	}
	c.order = c.order[:0]
	clear(c.m)
}
