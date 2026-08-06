package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	. "go.hasen.dev/shirei"
)

const (
	maxRecents     = 24
	sessionRelPath = "git_history/session.json"
)

// sessionDisplay is one repo's commit-list toggles on disk.
// ShowDiffStats is a pointer so legacy files (missing the key) keep the
// historical default of true.
type sessionDisplay struct {
	ShowAuthor    bool  `json:"showAuthor"`
	ShowTimestamp bool  `json:"showTimestamp"`
	ShowDiffStats *bool `json:"showDiffStats,omitempty"`
}

// sessionData is the on-disk window state.
type sessionData struct {
	Tabs    []string `json:"tabs"`    // work-tree paths, left-to-right
	Active  int      `json:"active"`  // index into Tabs
	Recents []string `json:"recents"` // MRU paths (not necessarily open)

	// Per-repo commit-list display toggles, keyed by work-tree path.
	Display map[string]sessionDisplay `json:"display,omitempty"`

	// Legacy global toggles (pre-per-repo). Used as fallback when Display is
	// empty for a path; omitted on write after migration.
	ShowAuthor    bool  `json:"showAuthor,omitempty"`
	ShowTimestamp bool  `json:"showTimestamp,omitempty"`
	ShowDiffStats *bool `json:"showDiffStats,omitempty"`
}

var (
	sessionSaveMu   sync.Mutex
	sessionSavePend bool
)

func sessionFilePath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, sessionRelPath), nil
}

func loadSession() (sessionData, error) {
	var s sessionData
	path, err := sessionFilePath()
	if err != nil {
		return s, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(b, &s); err != nil {
		return s, err
	}
	return s, nil
}

// snapshotSession copies tab/recents/display state for disk. Caller must hold
// exclusive access to appData (frame path, or WithFrameLock from background).
func snapshotSession() sessionData {
	var s sessionData
	for _, t := range appData.tabs {
		s.Tabs = append(s.Tabs, t.path)
	}
	s.Active = 0
	for i, t := range appData.tabs {
		if t == appData.active {
			s.Active = i
			break
		}
	}
	s.Recents = append([]string(nil), appData.recents...)
	// Snapshot open-tab toggles into the map, then write the whole map.
	for _, t := range appData.tabs {
		rememberTabDisplay(t)
	}
	if len(appData.displayByPath) > 0 {
		s.Display = make(map[string]sessionDisplay, len(appData.displayByPath))
		for path, d := range appData.displayByPath {
			stats := d.ShowStats
			s.Display[path] = sessionDisplay{
				ShowAuthor:    d.ShowAuthor,
				ShowTimestamp: d.ShowTime,
				ShowDiffStats: &stats,
			}
		}
	}
	return s
}

func writeSession(s sessionData) error {
	path, err := sessionFilePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// saveSessionNow snapshots and writes. Frame-path only (menu builder already
// holds the frame lock). Background callers must use saveSessionBackground.
func saveSessionNow() error {
	return writeSession(snapshotSession())
}

// saveSessionBackground snapshots under the frame lock then writes outside it.
func saveSessionBackground() error {
	var s sessionData
	WithFrameLock(func() {
		s = snapshotSession()
	})
	return writeSession(s)
}

// resolveSessionDisplay turns a sessionDisplay into runtime toggles.
// Nil ShowDiffStats defaults to true (pre-options / partial JSON).
func resolveSessionDisplay(d sessionDisplay) histDisplay {
	out := histDisplay{
		ShowAuthor: d.ShowAuthor,
		ShowTime:   d.ShowTimestamp,
		ShowStats:  true,
	}
	if d.ShowDiffStats != nil {
		out.ShowStats = *d.ShowDiffStats
	}
	return out
}

// applySessionDisplay loads per-repo toggles (and legacy globals) into appData.
func applySessionDisplay(s sessionData) {
	legacy := histDisplay{
		ShowAuthor: s.ShowAuthor,
		ShowTime:   s.ShowTimestamp,
		ShowStats:  true,
	}
	if s.ShowDiffStats != nil {
		legacy.ShowStats = *s.ShowDiffStats
	}
	hasLegacy := s.ShowAuthor || s.ShowTimestamp || s.ShowDiffStats != nil

	m := make(map[string]histDisplay)
	for path, d := range s.Display {
		m[path] = resolveSessionDisplay(d)
	}
	// Seed open tabs / recents that lack a Display entry with legacy globals
	// so one upgrade migrates the old single-prefs session.
	if hasLegacy {
		seed := func(path string) {
			if path == "" {
				return
			}
			if _, ok := m[path]; !ok {
				m[path] = legacy
			}
		}
		for _, path := range s.Tabs {
			seed(path)
		}
		for _, path := range s.Recents {
			seed(path)
		}
	}
	appData.displayByPath = m
}

// scheduleSaveSession debounces disk writes (many tab ops in one gesture).
func scheduleSaveSession() {
	sessionSaveMu.Lock()
	if sessionSavePend {
		sessionSaveMu.Unlock()
		return
	}
	sessionSavePend = true
	sessionSaveMu.Unlock()
	go func() {
		time.Sleep(200 * time.Millisecond)
		sessionSaveMu.Lock()
		sessionSavePend = false
		sessionSaveMu.Unlock()
		_ = saveSessionBackground()
	}()
}

func rememberRecent(path string) {
	if path == "" {
		return
	}
	// Drop duplicates, then prepend.
	out := make([]string, 0, len(appData.recents)+1)
	out = append(out, path)
	for _, p := range appData.recents {
		if p == path {
			continue
		}
		out = append(out, p)
	}
	if len(out) > maxRecents {
		out = out[:maxRecents]
	}
	appData.recents = out
}

// restoreSessionTabs creates lazy tabs from s (no history load).
// Returns the tab that should be active, or nil.
func restoreSessionTabs(s sessionData) *RepoTab {
	if len(s.Tabs) == 0 {
		return nil
	}
	var active *RepoTab
	for i, path := range s.Tabs {
		// Skip missing paths; do not toast on every startup miss.
		if _, err := os.Stat(path); err != nil {
			continue
		}
		t, err := openRepoTabLazy(path)
		if err != nil {
			continue
		}
		if i == s.Active || active == nil {
			active = t
		}
	}
	if active != nil {
		appData.active = active
	}
	return active
}

// openRepoTabLazy is like openRepoTab but does not mark the tab as needing
// immediate history load; caller activates and ensureTabLoaded.
func openRepoTabLazy(path string) (*RepoTab, error) {
	repo, err := findRepo(path)
	if err != nil {
		return nil, err
	}
	for _, t := range appData.tabs {
		if t.path == repo {
			return t, nil
		}
	}
	label := filepath.Base(repo)
	if label == "" || label == "." || label == string(filepath.Separator) {
		label = repo
	}
	if err := retainRepoPath(repo); err != nil {
		return nil, err
	}
	t := newRepoTab(repo, label)
	appData.tabs = append(appData.tabs, t)
	return t, nil
}
