package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	. "go.hasen.dev/shirei"
)

func rescanFixture(t *testing.T) string {
	t.Helper()
	tempRoot := "/tmp"
	if runtime.GOOS == "windows" {
		tempRoot = os.TempDir()
	}
	root, err := os.MkdirTemp(tempRoot, "dir-weight-rescan-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	return root
}

func writeScanFile(t *testing.T, path string, size int) int {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, bytes.Repeat([]byte("x"), size), 0600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	return int(PhysicalSize(path, info))
}

func scanEntry(root *ScanEntry, path string) *ScanEntry {
	if root.Path == path {
		return root
	}
	for _, child := range root.Entries {
		if found := scanEntry(child, path); found != nil {
			return found
		}
	}
	return nil
}

func scanFixture(t *testing.T, path string) *Scanner {
	t.Helper()
	s := newScanner()
	WithFrameLock(func() { startScan(s, path) })
	waitScannerTerminal(t, s, 5*time.Second)
	return s
}

func rescanAndWait(t *testing.T, s *Scanner, target *ScanEntry) {
	t.Helper()
	WithFrameLock(func() { rescanDirectory(s, target) })
	if st := waitScannerTerminal(t, s, 5*time.Second); st != Done {
		t.Fatalf("rescan state = %v", st)
	}
}

func TestRescanSubtreeReconcilesTreeAndView(t *testing.T) {
	root := rescanFixture(t)
	cache := filepath.Join(root, "Library", "Cache")
	oldFile := filepath.Join(cache, "old.bin")
	writeScanFile(t, oldFile, 64*1024)
	archiveFile := filepath.Join(root, "Archive", "archive.bin")
	archiveSize := writeScanFile(t, archiveFile, 32*1024)
	s := scanFixture(t, root)
	target := scanEntry(s.root, cache)
	archive := scanEntry(s.root, filepath.Dir(archiveFile))
	library := target.Parent
	s.selected = scanEntry(s.root, oldFile)
	s.root.Expanded, library.Expanded, target.Expanded = true, true, true
	s.filter, s.minsize = "bin", 1
	if s.root.Entries[0] != library {
		t.Fatal("fixture does not put Library first")
	}
	if err := os.Remove(oldFile); err != nil {
		t.Fatal(err)
	}
	newFile := filepath.Join(cache, "Nested", "new.bin")
	newSize := writeScanFile(t, newFile, 4096)
	freshArchiveSize := writeScanFile(t, archiveFile, 128*1024)
	rescanAndWait(t, s, target)
	if s.root.Size != archiveSize+newSize || library.Size != newSize || target.Size != newSize {
		t.Fatalf("incorrect ancestor totals: root=%d library=%d target=%d", s.root.Size, library.Size, target.Size)
	}
	if s.root.Entries[0] != archive || archive.Size != archiveSize {
		t.Fatal("ordering or untouched sibling measurement changed incorrectly")
	}
	if scanEntry(s.root, oldFile) != nil || scanEntry(s.root, newFile) == nil {
		t.Fatal("new/deleted entries do not match disk")
	}
	newEntry := scanEntry(s.root, newFile)
	if newEntry.Depth != 4 || newEntry.Parent.Parent != target {
		t.Fatal("replacement ancestry is incorrect")
	}
	if s.selected != target || !target.Expanded || !library.Expanded || s.filter != "bin" || s.minsize != 1 {
		t.Fatal("rescan lost selection fallback, expansion, or filters")
	}
	if d := proportionDenominator(library, false, 0); d != s.root.Size {
		t.Fatalf("stale proportion denominator: %d", d)
	}
	s.selected = newEntry
	newEntry.Parent.Expanded = true
	newSize = writeScanFile(t, newFile, 256*1024)
	rescanAndWait(t, s, target)
	if s.root.Size != archiveSize+newSize || s.root.Entries[0] != library || s.selected.Path != newFile || !s.selected.Parent.Expanded {
		t.Fatal("repeated rescan does not preserve surviving selection and expansion or rerank sizes")
	}
	rescanAndWait(t, s, s.root)
	if s.root.Size != freshArchiveSize+newSize || s.selected.Path != newFile || !s.selected.Parent.Expanded {
		t.Fatal("whole-root rescan does not update siblings and retain view state")
	}
	if s.scanned != s.submitted || s.root.subDone != s.root.subCount {
		t.Fatal("rescan progress does not finish")
	}
}

func TestRescanHardLinksAcrossBoundary(t *testing.T) {
	root := rescanFixture(t)
	a := filepath.Join(root, "a", "file.bin")
	b := filepath.Join(root, "b", "file.bin")
	size := writeScanFile(t, a, 32*1024)
	if err := os.MkdirAll(filepath.Dir(b), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(a, b); err != nil {
		t.Skipf("hard links unavailable: %v", err)
	}
	s := scanFixture(t, root)
	owner, other := scanEntry(s.root, a), scanEntry(s.root, b)
	if owner.Skip {
		owner, other = other, owner
	}
	target := owner.Parent
	if err := os.Remove(owner.Path); err != nil {
		t.Fatal(err)
	}
	rescanAndWait(t, s, target)
	if s.root.Size != size || other.Skip || other.Size != size {
		t.Fatal("removing counted link loses the surviving file's allocation")
	}
	// Refreshing the survivor observes its lower link count.
	rescanAndWait(t, s, other.Parent)
	other = scanEntry(s.root, other.Path)
	if err := os.Link(other.Path, owner.Path); err != nil {
		t.Fatal(err)
	}
	size = writeScanFile(t, owner.Path, 96*1024)
	rescanAndWait(t, s, target)
	if s.root.Size != size || other.Skip || other.Size != size || !scanEntry(s.root, owner.Path).Skip {
		t.Fatalf("cross-boundary links are counted twice or use stale size: total=%d want=%d", s.root.Size, size)
	}
	// A file first observed with one name can gain a link in the target.
	single := filepath.Join(other.Parent.Path, "single.bin")
	singleSize := writeScanFile(t, single, 16*1024)
	rescanAndWait(t, s, other.Parent)
	if err := os.Link(single, filepath.Join(target.Path, "new-link.bin")); err != nil {
		t.Fatal(err)
	}
	rescanAndWait(t, s, target)
	if s.root.Size != size+singleSize {
		t.Fatalf("new link to formerly single-link file counted twice: %d", s.root.Size)
	}
}

func TestRescanMissingFoldersAndRetry(t *testing.T) {
	root := rescanFixture(t)
	missing := filepath.Join(root, "missing")
	s := scanFixture(t, missing)
	if s.err == nil {
		t.Fatal("missing root has no error")
	}
	file := filepath.Join(missing, "nested", "data.bin")
	size := writeScanFile(t, file, 4096)
	rescanAndWait(t, s, s.root)
	if s.err != nil || len(s.readErrors) != 0 || s.root.Size != size {
		t.Fatal("root retry retains stale errors or sizes")
	}
	target := scanEntry(s.root, filepath.Dir(file))
	s.selected = scanEntry(s.root, file)
	if err := os.RemoveAll(target.Path); err != nil {
		t.Fatal(err)
	}
	rescanAndWait(t, s, target)
	if len(s.root.Entries) != 0 || s.root.Size != 0 || s.selected != s.root {
		t.Fatal("removed subtree remains in the tree or selection")
	}
	if err := os.Remove(missing); err != nil {
		t.Fatal(err)
	}
	rescanAndWait(t, s, s.root)
	if s.err == nil || s.root.Size != 0 || len(s.readErrors) != 1 {
		t.Fatal("missing root rescan does not report its error")
	}
}

func TestRescanReadErrorsAndSymlinkReplacement(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses Unix permissions and symlinks")
	}
	root := rescanFixture(t)
	a := filepath.Join(root, "a")
	b := filepath.Join(root, "b")
	writeScanFile(t, filepath.Join(a, "a.bin"), 4096)
	writeScanFile(t, filepath.Join(b, "b.bin"), 4096)
	s := scanFixture(t, root)
	for _, path := range []string{a, b} {
		if err := os.Chmod(path, 0); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(path, 0700) })
	}
	if _, err := os.ReadDir(a); err == nil {
		t.Skip("current user can read directories without permissions")
	}
	rescanAndWait(t, s, s.root)
	if len(s.readErrors) != 2 || s.root.Size != 0 {
		t.Fatal("unreadable folders retain stale allocation")
	}
	if err := os.Chmod(a, 0700); err != nil {
		t.Fatal(err)
	}
	rescanAndWait(t, s, scanEntry(s.root, a))
	if len(s.readErrors) != 1 || s.root.Size == 0 || scanEntry(s.root, b).readErr == nil {
		t.Fatal("targeted retry loses unrelated errors or retains resolved error")
	}
	if err := os.Chmod(b, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(b); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(a, b); err != nil {
		t.Fatal(err)
	}
	rescanAndWait(t, s, scanEntry(s.root, b))
	if len(scanEntry(s.root, b).Entries) != 0 || scanEntry(s.root, b).readErr == nil {
		t.Fatal("rescan follows a replacement symlink")
	}
}

func TestCancelRescanKeepsDisplayedTree(t *testing.T) {
	root := rescanFixture(t)
	file := filepath.Join(root, "data.bin")
	size := writeScanFile(t, file, 4096)
	s := scanFixture(t, root)
	oldEntry := scanEntry(s.root, file)
	WithFrameLock(func() { rescanDirectory(s, oldEntry) })
	if s.rescan != nil || s.state != Done {
		t.Fatal("rescan accepts a file as its target")
	}
	writeScanFile(t, file, 64*1024)
	var worker *Scanner
	WithFrameLock(func() {
		rescanDirectory(s, s.root)
		worker = s.rescan
		rescanDirectory(s, s.root)
		if s.rescan != worker || s.root.Size != size || scanEntry(s.root, file) != oldEntry {
			t.Fatal("rescan overlaps or replaces results before completion")
		}
		cancelScan(s)
	})
	// Exercise the late publish path even if the worker notices cancellation
	// before reading its directory.
	publishDirDraft(worker, worker.root, nil, nil)
	WithFrameLock(func() {
		if !worker.cancelled.Load() || s.state != Stopped || s.root.Size != size || scanEntry(s.root, file) != oldEntry {
			t.Fatal("cancelled rescan publishes into the displayed tree")
		}
	})
}

func TestRescanExplicitSymlinkRoot(t *testing.T) {
	root := rescanFixture(t)
	file := filepath.Join(root, "real", "data.bin")
	writeScanFile(t, file, 4096)
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(filepath.Dir(file), alias); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	s := scanFixture(t, alias)
	size := writeScanFile(t, file, 32*1024)
	rescanAndWait(t, s, s.root)
	if s.err != nil || s.root.Size != size {
		t.Fatalf("explicit symlink root cannot be rescanned: size=%d err=%v", s.root.Size, s.err)
	}
}
