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
	deadline := time.Now().Add(drive.Timeout)
	for {
		result, err := drive.Query(port, name)
		if err != nil {
			t.Fatal(err)
		}
		for _, n := range result.Nodes {
			if value == "" || n.Value == value {
				return n
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("missing %s %q: %v", name, value, result)
		}
		// Command replies acknowledge input delivery; the rendered state can
		// follow later, especially with race instrumentation on the child app.
		time.Sleep(10 * time.Millisecond)
	}
}
func clickScan(t *testing.T, port int, name, value string) {
	t.Helper()
	n := scanNode(t, port, name, value)
	if _, err := drive.ClickOne(port, fmt.Sprintf("#%d", n.ID)); err != nil {
		t.Fatal(err)
	}
	// A pointer move supplies a settle frame even while the native window
	// is occluded and its display link is idle.
	if err := drive.Move(port, n.Rect.Origin[0]+n.Rect.Size[0]/2, n.Rect.Origin[1]+n.Rect.Size[1]/2); err != nil {
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
	root := rescanFixture(t)
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
	for i := range 195 {
		if err := os.Remove(filepath.Join(root, fmt.Sprintf("file-%03d.bin", i))); err != nil {
			t.Fatal(err)
		}
	}
	file := filepath.Join(root, "file-199.bin")
	info, err := os.Lstat(file)
	if err != nil {
		t.Fatal(err)
	}
	clickScan(t, port, "rescan_all", "")
	waitScanTotal(t, port, 5*int(PhysicalSize(file, info)))
	if err := drive.WaitCount(port, "entry", 6); err != nil {
		t.Fatal("rescan leaves the viewport below the shortened tree:", err)
	}
	scanNode(t, port, "entry", root)
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
	if err := drive.WaitCount(port, "scan_details", 1); err != nil {
		t.Fatal(err)
	}
	if got := scanNode(t, port, "scan_issue", "").Value; !strings.Contains(got, missing) {
		t.Fatalf("missing path in details: %s", got)
	}
	clickScan(t, port, "close_scan_details", "")
	size := writeScanFile(t, filepath.Join(missing, "data.bin"), 4096)
	clickScan(t, port, "rescan_all", "")
	waitScanTotal(t, port, size)
	if err := drive.WaitCount(port, "skipped_folders", 0); err != nil {
		t.Fatal(err)
	}
	if err := drive.WaitCount(port, "entry", 2); err != nil {
		t.Fatal(err)
	}
}

func waitScanTotal(t *testing.T, port, total int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if err := drive.Move(port, 0, 0); err != nil {
			t.Fatal(err)
		}
		if scanNode(t, port, "scan_status", "").Value == "done" && scanNode(t, port, "scan_total", "").Value == fmt.Sprint(total) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("scan never completed with total %d", total)
}

func TestDriveRescanDirectory(t *testing.T) {
	root := rescanFixture(t)
	cache := filepath.Join(root, "Cache")
	old := filepath.Join(cache, "old.bin")
	oldSize := writeScanFile(t, old, 64*1024)
	archive := filepath.Join(root, "Archive", "archive.bin")
	archiveSize := writeScanFile(t, archive, 32*1024)
	port := drive.Start(t, ".", root)
	waitScanTotal(t, port, oldSize+archiveSize)
	clickScan(t, port, "entry_expand", cache)
	clickScan(t, port, "entry", old)
	clickScan(t, port, "rescan_folder", old)
	if scanNode(t, port, "selected_path", "").Value != old {
		t.Fatal("disabled folder action changes file selection")
	}
	waitScanTotal(t, port, oldSize+archiveSize)
	clickScan(t, port, "entry", cache)
	if err := os.Remove(old); err != nil {
		t.Fatal(err)
	}
	data := filepath.Join(cache, "data.bin")
	size := writeScanFile(t, data, 8192)
	clickScan(t, port, "rescan_folder", cache)
	waitScanTotal(t, port, size+archiveSize)
	if err := drive.WaitCount(port, "scan_tab", 1); err != nil {
		t.Fatal(err)
	}
	if err := drive.WaitCount(port, "entry", 4); err != nil {
		t.Fatal(err)
	}
	if !scanNode(t, port, "entry_expand", cache).Checked || scanNode(t, port, "selected_path", "").Value != cache {
		t.Fatal("folder expansion or selection lost after rescan")
	}
	rows, err := drive.Query(port, "entry")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows.Nodes) != 4 || rows.Nodes[1].Value != filepath.Dir(archive) || rows.Nodes[3].Value != data {
		t.Fatalf("rescan rows do not reflect new ordering and contents: %v", rows.Nodes)
	}
	clickScan(t, port, "entry", data)
	if err := drive.Type(port, "filter", "data.bin"); err != nil {
		t.Fatal(err)
	}
	size = writeScanFile(t, data, 128*1024)
	archiveSize = writeScanFile(t, archive, 16*1024)
	if err := drive.TabUntil(port, "rescan_all"); err != nil {
		t.Fatal(err)
	}
	if err := drive.Key(port, "enter"); err != nil {
		t.Fatal(err)
	}
	waitScanTotal(t, port, size+archiveSize)
	if err := drive.WaitCount(port, "entry", 1); err != nil {
		t.Fatal(err)
	}
	if scanNode(t, port, "filter", "").Value != "data.bin" || scanNode(t, port, "selected_path", "").Value != data {
		t.Fatal("root rescan loses filter or surviving file selection")
	}
	if got := scanNode(t, port, "entry_size", "").Value; got != fmt.Sprint(size) {
		t.Fatalf("row size remains stale: %s, want %d", got, size)
	}
}

func TestDriveDeletionReview(t *testing.T) {
	root := rescanFixture(t)
	folder := filepath.Join(root, "Old")
	old := filepath.Join(folder, "old.bin")
	oldSize := writeScanFile(t, old, 65536)
	restored := filepath.Join(root, "keep.bin")
	keptSize := writeScanFile(t, restored, 8192)
	port := drive.Start(t, ".", root)
	waitScanTotal(t, port, oldSize+keptSize)
	clickScan(t, port, "entry_expand", folder)
	clickScan(t, port, "entry", old)
	clickScan(t, port, "queue_delete", "")
	clickScan(t, port, "entry", folder)
	clickScan(t, port, "queue_delete", "")
	scanNode(t, port, "delete_queue", "1")
	clickScan(t, port, "entry", restored)
	clickScan(t, port, "queue_delete", "")
	scanNode(t, port, "delete_queue", "2")
	waitScanTotal(t, port, oldSize+keptSize)
	if _, err := os.Stat(old); err != nil {
		t.Fatal("queueing deleted a file:", err)
	}
	clickScan(t, port, "review_deletions", "")
	scanNode(t, port, "delete_item", folder)
	clickScan(t, port, "restore_delete", restored)
	scanNode(t, port, "delete_review", "1")
	clickScan(t, port, "delete_back", "")
	clickScan(t, port, "close_tab", root)
	scanNode(t, port, "close_queued", "")
	clickScan(t, port, "keep_queue", "")
	scanNode(t, port, "scan_tab", root)
	// An incidental Enter in the review does not confirm deletion.
	clickScan(t, port, "review_deletions", "")
	if err := drive.Key(port, "enter"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(old); err != nil {
		t.Fatal("review opens with destructive keyboard focus:", err)
	}
	if err := drive.Key(port, "escape"); err != nil {
		t.Fatal(err)
	}
	if err := drive.WaitCount(port, "delete_review", 0); err != nil {
		t.Fatal(err)
	}
	// These changes must stay unobserved: deletion updates stored data only.
	writeScanFile(t, restored, 131072)
	unseen := filepath.Join(root, "unseen.bin")
	writeScanFile(t, unseen, 4096)
	clickScan(t, port, "review_deletions", "")
	clickScan(t, port, "delete_permanently", "")
	waitScanTotal(t, port, keptSize)
	if err := drive.WaitCount(port, "delete_queue", 0); err != nil {
		t.Fatal(err)
	}
	if err := drive.WaitCount(port, "entry", 2); err != nil {
		t.Fatal("deletion rescans or keeps removed entries:", err)
	}
	if _, err := os.Stat(folder); !os.IsNotExist(err) {
		t.Fatal("confirmed folder remains:", err)
	}
	for _, path := range []string{restored, unseen} {
		if _, err := os.Stat(path); err != nil {
			t.Fatal("unqueued file removed:", err)
		}
	}
	clickScan(t, port, "entry", restored)
	clickScan(t, port, "queue_delete", "")
	clickScan(t, port, "close_tab", root)
	clickScan(t, port, "discard_queue_close", "")
	if err := drive.WaitCount(port, "scan_tab", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(restored); err != nil {
		t.Fatal("closing a queued tab deletes its files:", err)
	}
}
