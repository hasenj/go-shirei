package main

import (
	"go.hasen.dev/shirei/examples/internal/themetest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func treeFixture() *Scanner {
	root := &ScanEntry{Name: "Home", Path: "/Users/alex", IsDir: true, Expanded: true, state: Done}
	add := func(parent *ScanEntry, name string, gb float64, expanded bool) *ScanEntry {
		e := &ScanEntry{Name: name, Path: filepath.Join(parent.Path, name), Parent: parent, Depth: parent.Depth + 1, IsDir: true, Size: int(gb * GB1), Expanded: expanded, state: Done}
		parent.Entries = append(parent.Entries, e)
		return e
	}
	library := add(root, "Library", 76.8, true)
	caches := add(library, "Caches", 31.2, true)
	add(caches, "Browser caches", 14, false)
	add(caches, "Build caches", 10.8, false)
	add(caches, "Other caches", 6.4, false)
	add(library, "Application Support", 24.6, false)
	add(library, "Containers", 12.1, false)
	add(library, "Developer", 7.4, false)
	add(library, "Other", 1.5, false)
	add(root, "Pictures", 48.6, false)
	add(root, "Code", 22.4, false)
	add(root, "Downloads", 17.3, false)
	add(root, "Movies", 11.9, false)
	root.Size = int(184.2 * GB1)
	return &Scanner{root: root, rootPath: root.Path, state: Done, selected: caches, scanned: 1284, submitted: 1284, started: time.Unix(100, 0), done: time.Unix(102, 400000000)}
}

func TestSnapshotSizeTree(t *testing.T) {
	old := appData
	defer func() { appData = old }()
	s := treeFixture()
	second := &Scanner{rootPath: "/Volumes/Archive", root: &ScanEntry{Name: "Archive", Path: "/Volumes/Archive"}, state: Done}
	appData = &DiskUsageAnalyzer{scanners: []*Scanner{s, second}, activeScanner: s}
	themetest.Snapshot(t, "size_tree", 1100, 820, RootView)
	themetest.Snapshot(t, "size_tree_narrow", 680, 600, RootView)
	s.filter = "caches"
	themetest.Snapshot(t, "size_tree_filtered", 1100, 820, RootView)
	s.filter = "no such name"
	themetest.Snapshot(t, "size_tree_empty", 680, 600, RootView)
}

func TestSnapshotSkippedFolders(t *testing.T) {
	old := appData
	defer func() { appData = old }()
	s := treeFixture()
	s.readErrors = []error{
		&os.PathError{Op: "open", Path: "/Users/alex/.Trash", Err: os.ErrPermission},
		&os.PathError{Op: "open", Path: "/Users/alex/Library/Private", Err: os.ErrPermission},
	}
	appData = &DiskUsageAnalyzer{scanners: []*Scanner{s}, activeScanner: s}
	themetest.Snapshot(t, "size_tree_skipped", 1100, 820, RootView)
	s.showReadErrors = true
	themetest.Snapshot(t, "scan_details", 680, 600, RootView)
}
