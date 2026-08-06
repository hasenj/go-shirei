package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	maxRecents      = 32
	historyRelPath  = "dir_weight/history.json"
	historySaveWait = 200 * time.Millisecond
)

// historyData is the on-disk path list (most-recent first).
type historyData struct {
	Paths []string `json:"paths"`
}

var (
	historyMu     sync.Mutex
	historySaving bool
	historyDirty  bool
)

// historyPathOverride, when set (tests), replaces the config-dir path.
var historyPathOverride string

func historyFilePath() (string, error) {
	if historyPathOverride != "" {
		return historyPathOverride, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, historyRelPath), nil
}

func loadHistory() {
	path, err := historyFilePath()
	if err != nil {
		return
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var h historyData
	if err := json.Unmarshal(b, &h); err != nil {
		return
	}
	historyMu.Lock()
	appData.recents = normalizePaths(h.Paths)
	historyMu.Unlock()
}

func writeHistory(paths []string) error {
	path, err := historyFilePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	h := historyData{Paths: paths}
	b, err := json.MarshalIndent(h, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// snapshotRecents copies the MRU list under historyMu.
func snapshotRecents() []string {
	historyMu.Lock()
	defer historyMu.Unlock()
	return append([]string(nil), appData.recents...)
}

// scheduleSaveHistory debounces disk writes. Mutate recents only under
// historyMu; the save loop snapshots under that same mutex so a concurrent
// rememberPath never races the copy (and a mutation during write reschedules).
func scheduleSaveHistory() {
	historyMu.Lock()
	historyDirty = true
	if historySaving {
		historyMu.Unlock()
		return
	}
	historySaving = true
	historyMu.Unlock()
	go historySaveLoop()
}

func historySaveLoop() {
	for {
		time.Sleep(historySaveWait)
		historyMu.Lock()
		paths := append([]string(nil), appData.recents...)
		historyDirty = false
		historyMu.Unlock()

		_ = writeHistory(paths)

		historyMu.Lock()
		if historyDirty {
			// A rememberPath landed during write — loop and snapshot again.
			historyMu.Unlock()
			continue
		}
		historySaving = false
		historyMu.Unlock()
		return
	}
}

// rememberPath records a scan root as most-recent and persists the list.
func rememberPath(path string) {
	path = cleanPath(path)
	if path == "" {
		return
	}
	historyMu.Lock()
	out := make([]string, 0, len(appData.recents)+1)
	out = append(out, path)
	for _, p := range appData.recents {
		if p != path {
			out = append(out, p)
		}
	}
	if len(out) > maxRecents {
		out = out[:maxRecents]
	}
	appData.recents = out
	historyMu.Unlock()
	scheduleSaveHistory()
}

// candidatePaths is the New Scan list: recent roots (MRU) then platform defaults.
func candidatePaths() []string {
	recents := snapshotRecents()
	seen := make(map[string]bool, len(recents)+8)
	var out []string
	add := func(p string) {
		p = cleanPath(p)
		if p == "" || seen[p] {
			return
		}
		seen[p] = true
		out = append(out, p)
	}
	for _, p := range recents {
		add(p)
	}
	for _, p := range scanCandidates() {
		add(p)
	}
	return out
}

func cleanPath(p string) string {
	p = filepath.Clean(p)
	if p == "." || p == "" {
		return ""
	}
	return p
}

func normalizePaths(in []string) []string {
	seen := make(map[string]bool, len(in))
	var out []string
	for _, p := range in {
		p = cleanPath(p)
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	if len(out) > maxRecents {
		out = out[:maxRecents]
	}
	return out
}
