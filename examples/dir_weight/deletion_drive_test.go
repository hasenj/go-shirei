package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"go.hasen.dev/shirei/drive"
)

func TestMain(m *testing.M) {
	if os.Getenv("DIR_WEIGHT_TEST_SLOW_DELETE") == "1" {
		removeScannedEntry = func(parent *os.Root, name string) error {
			time.Sleep(500 * time.Millisecond)
			return parent.Remove(name)
		}
		main()
		return
	}
	os.Exit(m.Run())
}

// The test executable opens the real app with a slow filesystem operation.
// Production binaries have no delay or test-only command-line switches.
func startSlowDeletion(t *testing.T, paths ...string) int {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	port, err := drive.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(executable, paths...)
	cmd.Env = append(os.Environ(), "DIR_WEIGHT_TEST_SLOW_DELETE=1", "SHIREI_DRIVE_PORT="+strconv.Itoa(port))
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	t.Cleanup(func() {
		_ = drive.Quit(port)
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("slow deletion app: %v", err)
			}
		case <-time.After(3 * time.Second):
			_ = cmd.Process.Kill()
			<-done
			t.Error("slow deletion app does not quit")
		}
	})
	deadline := time.Now().Add(20 * time.Second)
	for {
		if err := drive.Ping(port); err == nil {
			// The command server can start before the native window. Wait for
			// a rendered scan instead of assuming a fixed startup duration.
			if err := drive.Move(port, 0, 0); err != nil {
				t.Fatal(err)
			}
			if count, err := drive.Count(port, "scan_status"); err == nil && count == 1 {
				// Allow the OS to finish mapping the rendered window before clicks.
				time.Sleep(500 * time.Millisecond)
				return port
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("slow deletion app does not open")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestDriveSlowDeletion(t *testing.T) {
	root, other := rescanFixture(t), rescanFixture(t)
	folder := filepath.Join(root, "Old")
	var total int
	for i := range 40 {
		total += writeScanFile(t, filepath.Join(folder, fmt.Sprintf("old-%02d.bin", i)), 4096)
	}
	keep := filepath.Join(root, "Keep", "keep.bin")
	kept := writeScanFile(t, keep, 8192)
	total += kept
	port := startSlowDeletion(t, other, root)
	waitScanTotal(t, port, total)
	clickScan(t, port, "entry", folder)
	scanNode(t, port, "selected_path", folder)
	clickScan(t, port, "queue_delete", "")
	scanNode(t, port, "delete_queue", "1")
	clickScan(t, port, "review_deletions", "")
	clickScan(t, port, "delete_permanently", "")
	scanNode(t, port, "scan_status", "deleting")
	// The review must not trap the user for the duration of disk operations.
	if err := drive.WaitCount(port, "delete_review", 0); err != nil {
		t.Fatal("deletion blocks the scan behind its review dialog:", err)
	}
	before := scanNode(t, port, "delete_progress", "").Value
	time.Sleep(650 * time.Millisecond)
	// Native display links can idle behind another window. Supply a frame
	// without changing the app's state, just as clickScan does.
	if err := drive.Move(port, 0, 0); err != nil {
		t.Fatal(err)
	}
	after := scanNode(t, port, "delete_progress", "").Value
	var removed int
	if _, err := fmt.Sscanf(after, "Deleting… %d removed", &removed); err != nil || removed == 0 || after == before {
		t.Fatalf("deletion progress does not update: %q -> %q", before, after)
	}
	if err := drive.Type(port, "filter", "keep.bin"); err != nil {
		t.Fatal(err)
	}
	scanNode(t, port, "filter", "keep.bin")
	if err := drive.WaitCount(port, "entry", 1); err != nil {
		t.Fatal("filter does not respond during deletion:", err)
	}
	clickScan(t, port, "entry", keep)
	scanNode(t, port, "selected_path", keep)
	scanNode(t, port, "scan_status", "deleting")
	clickScan(t, port, "scan_tab", other)
	scanNode(t, port, "scan_status", "done")
	clickScan(t, port, "scan_tab", root)
	scanNode(t, port, "filter", "keep.bin")
	scanNode(t, port, "scan_status", "deleting")
	clickScan(t, port, "review_deletions", "")
	scanNode(t, port, "delete_review", "1")
	clickScan(t, port, "delete_back", "")
	if err := drive.WaitCount(port, "delete_review", 0); err != nil {
		t.Fatal("Back to scan does not work during deletion:", err)
	}
	clickScan(t, port, "review_deletions", "")
	if err := drive.Key(port, "escape"); err != nil {
		t.Fatal(err)
	}
	if err := drive.WaitCount(port, "delete_review", 0); err != nil {
		t.Fatal("Escape does not dismiss deletion progress:", err)
	}
	scanNode(t, port, "scan_status", "deleting")
	// Wait for all slow operations, then check accounting and preserved browsing state.
	deadline := time.Now().Add(25 * time.Second)
	for scanNode(t, port, "scan_status", "").Value == "deleting" {
		if time.Now().After(deadline) {
			t.Fatal("slow deletion never finishes")
		}
		if err := drive.Move(port, 0, 0); err != nil {
			t.Fatal(err)
		}
	}
	waitScanTotal(t, port, kept)
	scanNode(t, port, "filter", "keep.bin")
	scanNode(t, port, "selected_path", keep)
	if err := drive.WaitCount(port, "delete_queue", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(folder); !os.IsNotExist(err) {
		t.Fatal("queued folder remains:", err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatal("unselected file removed:", err)
	}
}
