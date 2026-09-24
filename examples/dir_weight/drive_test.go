package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"go.hasen.dev/shirei"
	"go.hasen.dev/shirei/drive"
)

func scanNode(t *testing.T, port int, name, value string) shirei.AccessNode {
	t.Helper()
	result, err := drive.Query(port, name)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range result.Nodes {
		if value == "" || n.Value == value {
			return n
		}
	}
	t.Fatalf("missing %s %q: %v", name, value, result)
	return shirei.AccessNode{}
}
func clickScan(t *testing.T, port int, name, value string) {
	t.Helper()
	n := scanNode(t, port, name, value)
	if _, err := drive.ClickOne(port, fmt.Sprintf("#%d", n.ID)); err != nil {
		t.Fatal(err)
	}
}
func TestDriveSizeTree(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "Cache")
	if err := os.Mkdir(nested, 0700); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{filepath.Join(nested, "data.bin"), filepath.Join(root, "notes.txt")} {
		if err := os.WriteFile(p, make([]byte, 4096), 0600); err != nil {
			t.Fatal(err)
		}
	}
	other := t.TempDir()
	port := drive.Start(t, ".", other, root)
	if err := drive.WaitCount(port, "scan_status", 1); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for scanNode(t, port, "scan_status", "").Value != "done" {
		if time.Now().After(deadline) {
			t.Fatal("scan never completed")
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err := drive.WaitCount(port, "entry", 3); err != nil {
		t.Fatal(err)
	}
	clickScan(t, port, "entry_expand", nested)
	if err := drive.WaitCount(port, "entry", 4); err != nil {
		t.Fatal(err)
	}
	clickScan(t, port, "entry", filepath.Join(nested, "data.bin"))
	if got := scanNode(t, port, "selected_path", "").Value; got != filepath.Join(nested, "data.bin") {
		t.Fatalf("selection = %s", got)
	}
	clickScan(t, port, "entry_expand", root)
	if err := drive.WaitCount(port, "entry", 1); err != nil {
		t.Fatal(err)
	}
	// Filtering includes descendants of folded folders.
	if err := drive.Type(port, "filter", "data.bin"); err != nil {
		t.Fatal(err)
	}
	if err := drive.WaitCount(port, "entry", 1); err != nil {
		t.Fatal(err)
	}
	scanNode(t, port, "entry", filepath.Join(nested, "data.bin"))
	clickScan(t, port, "scan_tab", other)
	if err := drive.WaitCount(port, "empty_results", 1); err != nil {
		t.Fatal(err)
	}
	clickScan(t, port, "scan_tab", root)
	if got := scanNode(t, port, "filter", "").Value; got != "data.bin" {
		t.Fatalf("filter not restored: %s", got)
	}
	clickScan(t, port, "filter", "")
	mod := "ctrl"
	if runtime.GOOS == "darwin" {
		mod = "cmd"
	}
	if err := drive.Key(port, "a", mod); err != nil {
		t.Fatal(err)
	}
	if err := drive.Key(port, "backspace"); err != nil {
		t.Fatal(err)
	}
	clickScan(t, port, "entry_expand", root)
	if err := drive.WaitCount(port, "entry", 4); err != nil {
		t.Fatal(err)
	}
	clickScan(t, port, "minimum_size", "")
	if err := drive.WaitCount(port, "empty_results", 1); err != nil {
		t.Fatal(err)
	}
	if err := drive.Key(port, "home"); err != nil {
		t.Fatal(err)
	}
	if err := drive.WaitCount(port, "entry", 4); err != nil {
		t.Fatal(err)
	}
	clickScan(t, port, "new_scan", "")
	scanNode(t, port, "start_scan", "")
	clickScan(t, port, "cancel_scan", "")
	if err := drive.WaitCount(port, "cancel_scan", 0); err != nil {
		t.Fatal(err)
	}
	// Keyboard activation of the shared close control selects the remaining tab.
	close := scanNode(t, port, "close_tab", root)
	if err := drive.TabUntil(port, fmt.Sprintf("#%d", close.ID)); err != nil {
		t.Fatal(err)
	}
	if err := drive.Key(port, "enter"); err != nil {
		t.Fatal(err)
	}
	if err := drive.WaitCount(port, "scan_tab", 1); err != nil {
		t.Fatal(err)
	}
	if !scanNode(t, port, "scan_tab", other).Checked {
		t.Fatal("remaining tab is not selected")
	}
	clickScan(t, port, "close_tab", other)
	if err := drive.WaitCount(port, "scan_tab", 0); err != nil {
		t.Fatal(err)
	}
	scanNode(t, port, "start_scan", "")
}

func TestDriveTreeScrollRestore(t *testing.T) {
	root := t.TempDir()
	for i := range 200 {
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("file-%03d.bin", i)), make([]byte, 4096), 0600); err != nil {
			t.Fatal(err)
		}
	}
	other := t.TempDir()
	port := drive.Start(t, ".", other, root)
	if err := drive.WaitCount(port, "scan_tab", 2); err != nil {
		t.Fatal(err)
	}
	clickScan(t, port, "entry", root)
	if err := drive.Wheel(port, 50000); err != nil {
		t.Fatal(err)
	}
	rows, err := drive.Query(port, "entry")
	if err != nil {
		t.Fatal(err)
	}
	if rows.Count < 2 || rows.Count > 25 || len(rows.Nodes) == 0 {
		t.Fatalf("tree not virtualized: %v", rows)
	}
	first := rows.Nodes[0].Value
	if first == root {
		t.Fatal("tree did not scroll")
	}
	clickScan(t, port, "scan_tab", other)
	clickScan(t, port, "scan_tab", root)
	rows, err = drive.Query(port, "entry")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows.Nodes) == 0 || rows.Nodes[0].Value != first {
		t.Fatalf("lost scroll position %q: %v", first, rows)
	}
}

func TestDriveSkippedFolders(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission fixture uses Unix directory permissions")
	}
	root := t.TempDir()
	blocked := filepath.Join(root, "Private")
	if err := os.Mkdir(blocked, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "readable.bin"), make([]byte, 4096), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(blocked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(blocked, 0700) })
	if _, err := os.ReadDir(blocked); err == nil {
		t.Skip("current user can read directories without permissions")
	}
	port := drive.Start(t, ".", root)
	if err := drive.WaitCount(port, "skipped_folders", 1); err != nil {
		t.Fatal(err)
	}
	if got := scanNode(t, port, "scan_status", "").Value; got != "done" {
		t.Fatalf("partial scan status = %q", got)
	}
	if got := scanNode(t, port, "skipped_folders", "").Value; got != "1" {
		t.Fatalf("skipped count = %q", got)
	}
	scanNode(t, port, "entry", filepath.Join(root, "readable.bin"))
	clickScan(t, port, "skipped_folders", "")
	if err := drive.WaitCount(port, "scan_details", 1); err != nil {
		t.Fatal(err)
	}
	if got := scanNode(t, port, "scan_issue", "").Value; !strings.Contains(got, blocked) {
		t.Fatalf("missing path in details: %s", got)
	}
	clickScan(t, port, "close_scan_details", "")
	if err := drive.WaitCount(port, "scan_details", 0); err != nil {
		t.Fatal(err)
	}
	clickScan(t, port, "skipped_folders", "")
	if err := drive.Key(port, "escape"); err != nil {
		t.Fatal(err)
	}
	if err := drive.WaitCount(port, "scan_details", 0); err != nil {
		t.Fatal(err)
	}
}

func TestDriveUnreadableRoot(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	port := drive.Start(t, ".", missing)
	if err := drive.WaitCount(port, "skipped_folders", 1); err != nil {
		t.Fatal(err)
	}
	if got := scanNode(t, port, "scan_status", "").Value; got != "error" {
		t.Fatalf("unreadable root status = %q", got)
	}
	if err := drive.WaitCount(port, "empty_results", 1); err != nil {
		t.Fatal(err)
	}
	clickScan(t, port, "skipped_folders", "")
	if got := scanNode(t, port, "scan_issue", "").Value; !strings.Contains(got, missing) {
		t.Fatalf("missing path in details: %s", got)
	}
}
