package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	. "go.hasen.dev/shirei"
)

func deleteAndWait(t *testing.T, s *Scanner) {
	t.Helper()
	WithFrameLock(func() { commitDeletion(s) })
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var busy bool
		WithFrameLock(func() { busy = s.deleteBusy })
		if !busy {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("deletion does not finish")
}

func TestDeletionQueueAndAccounting(t *testing.T) {
	root := rescanFixture(t)
	file := filepath.Join(root, "Cache", "nested", "old.bin")
	deletedSize := writeScanFile(t, file, 65536)
	survivor := filepath.Join(root, "Archive", "keep.bin")
	keptSize := writeScanFile(t, survivor, 8192)
	s := scanFixture(t, root)
	entry, folder := scanEntry(s.root, file), scanEntry(s.root, filepath.Dir(filepath.Dir(file)))
	archive := scanEntry(s.root, filepath.Dir(survivor))
	started, done := s.started, s.done
	s.selected, s.filter, s.minsize = entry, "bin", 1
	s.root.Expanded, archive.Expanded = true, true
	queueDeletion(s, s.root)
	queueDeletion(s, entry)
	queueDeletion(s, folder)
	queueDeletion(s, entry)
	if len(s.deleteQueue) != 1 || s.deleteQueue[0] != folder || entry.queued || queuedAncestor(entry) != folder {
		t.Fatal("queue does not collapse overlapping folder selections")
	}
	if s.root.Size != deletedSize+keptSize {
		t.Fatal("queueing changes measured sizes")
	}
	restoreDeletion(s, folder)
	if len(s.deleteQueue) != 0 || queuedAncestor(entry) != nil {
		t.Fatal("restore leaves deletion intent")
	}
	if _, err := os.Stat(file); err != nil {
		t.Fatal("staging/restoring touches files:", err)
	}
	// A rescan would observe these unrelated changes. Deletion must not.
	writeScanFile(t, survivor, 131072)
	unseen := filepath.Join(root, "Archive", "unseen.bin")
	writeScanFile(t, unseen, 32768)
	queueDeletion(s, folder)
	deleteAndWait(t, s)
	if len(s.deleteQueue) != 0 || s.root.Size != keptSize || archive.Size != keptSize || scanEntry(s.root, file) != nil || scanEntry(s.root, unseen) != nil {
		t.Fatalf("deletion accounting rescans or retains deleted entries: total=%d queue=%d", s.root.Size, len(s.deleteQueue))
	}
	if s.selected != s.root || s.root.Entries[0] != archive || !archive.Expanded || s.filter != "bin" || s.minsize != 1 {
		t.Fatal("deletion loses selection fallback, ordering, expansion, or filters")
	}
	if s.started != started || s.done != done || s.rescan != nil || s.state != Done || s.scanned != s.root.subDone || s.submitted != s.root.subCount {
		t.Fatal("deletion starts a scan or leaves stale progress")
	}
	if _, err := os.Stat(folder.Path); !os.IsNotExist(err) {
		t.Fatal("queued folder remains on disk:", err)
	}
	if proportionDenominator(archive, false, 0) != keptSize {
		t.Fatal("size bars retain an old denominator")
	}
}

func TestDeletionHardLinks(t *testing.T) {
	root := rescanFixture(t)
	a, b := filepath.Join(root, "A", "a.bin"), filepath.Join(root, "B", "b.bin")
	size := writeScanFile(t, a, 32768)
	if err := os.MkdirAll(filepath.Dir(b), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(a, b); err != nil {
		t.Skipf("hard links unavailable: %v", err)
	}
	s := scanFixture(t, root)
	owner, alias := scanEntry(s.root, a), scanEntry(s.root, b)
	if owner.Skip {
		owner, alias = alias, owner
	}
	queueDeletion(s, owner.Parent)
	deleteAndWait(t, s)
	if s.root.Size != size || alias.Skip || alias.Size != size || alias.Parent.Size != size {
		t.Fatal("deleting the counted name loses a surviving hard link's allocation")
	}
	queueDeletion(s, alias)
	deleteAndWait(t, s)
	if s.root.Size != 0 || alias.Parent.Size != 0 || len(s.deleteQueue) != 0 {
		t.Fatal("deleting the last known link leaves its allocation counted")
	}
}

func TestDeletionPartialFailureAndNewFiles(t *testing.T) {
	root := rescanFixture(t)
	folder := filepath.Join(root, "Old")
	old := filepath.Join(folder, "old.bin")
	writeScanFile(t, old, 8192)
	changed := filepath.Join(folder, "changed.bin")
	changedSize := writeScanFile(t, changed, 4096)
	other := filepath.Join(root, "other.bin")
	writeScanFile(t, other, 16384)
	s := scanFixture(t, root)
	target := scanEntry(s.root, folder)
	queueDeletion(s, target)
	queueDeletion(s, scanEntry(s.root, other))
	writeScanFile(t, changed, 32768)
	newFile := filepath.Join(folder, "new.bin")
	writeScanFile(t, newFile, 65536)
	deleteAndWait(t, s)
	if len(s.deleteQueue) != 1 || s.deleteQueue[0] != target || target.deleteErr == nil {
		t.Fatal("partial failure does not retain only the failed target")
	}
	if s.deleteReview || s.deleteNotice != "Some items could not be deleted" {
		t.Fatal("partial failure interrupts browsing or omits its status")
	}
	if s.root.Size != changedSize || target.Size != changedSize || scanEntry(s.root, old) != nil || scanEntry(s.root, other) != nil || scanEntry(s.root, newFile) != nil {
		t.Fatal("successful partial removals are not deducted from stored sizes")
	}
	for _, path := range []string{changed, newFile} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("unreviewed/changed file was removed: %s: %v", path, err)
		}
	}
	rescanAndWait(t, s, target)
	if len(s.deleteQueue) != 1 || s.deleteQueue[0] != target || target.deleteErr != nil {
		t.Fatal("manual rescan loses the queue or retains its resolved error")
	}
	// An addition alone makes the final directory removal fail.
	newer := filepath.Join(folder, "newer.bin")
	writeScanFile(t, newer, 4096)
	deleteAndWait(t, s)
	if s.root.Size != 0 || len(s.deleteQueue) != 1 || target.deleteErr == nil {
		t.Fatal("nonempty directory failure does not account for its removed children")
	}
	if _, err := os.Stat(newer); err != nil {
		t.Fatal("unscanned addition was deleted:", err)
	}
	restoreDeletion(s, nil)
	if len(s.deleteQueue) != 0 || target.queued {
		t.Fatal("restore all cannot clear a partial failure")
	}
}

func TestDeletionReplacedPathsAndSymlinks(t *testing.T) {
	root := rescanFixture(t)
	path := filepath.Join(root, "Old", "file.bin")
	size := writeScanFile(t, path, 8192)
	s := scanFixture(t, root)
	entry := scanEntry(s.root, path)
	queueDeletion(s, entry)
	// Keep the original inode allocated, so replacement is deterministic.
	if err := os.Rename(path, path+".original"); err != nil {
		t.Fatal(err)
	}
	writeScanFile(t, path, 8192)
	deleteAndWait(t, s)
	if entry.deleteErr == nil || s.root.Size != size {
		t.Fatal("replacement is accepted or failed deletion changes totals")
	}
	rescanAndWait(t, s, entry.Parent)
	deleteAndWait(t, s)
	if len(s.deleteQueue) != 1 || entry.deleteErr == nil {
		t.Fatal("rescan silently retargets a queued deletion to a different file")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("replacement file removed:", err)
	}
	restoreDeletion(s, nil)
	out := rescanFixture(t)
	outside := filepath.Join(out, "file.bin")
	writeScanFile(t, outside, 4096)
	dir := scanEntry(s.root, filepath.Dir(path))
	queueDeletion(s, scanEntry(s.root, path))
	if err := os.Rename(dir.Path, dir.Path+".original"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(out, dir.Path); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	deleteAndWait(t, s)
	if len(s.deleteQueue) != 1 || s.deleteQueue[0].deleteErr == nil {
		t.Fatal("replacement symlink ancestor is accepted")
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatal("deletion escapes into replacement symlink:", err)
	}
	// A scanned link is itself removable, without visiting its target.
	restoreDeletion(s, nil)
	rescanAndWait(t, s, s.root)
	queueDeletion(s, scanEntry(s.root, dir.Path))
	deleteAndWait(t, s)
	if len(s.deleteQueue) != 0 {
		t.Fatal("scanned symlink cannot be removed:", s.deleteQueue[0].deleteErr)
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatal("removing a link deletes its target:", err)
	}
}

func TestDeletionRescanAndBusyLifecycle(t *testing.T) {
	root := rescanFixture(t)
	folder := filepath.Join(root, "Old")
	file := filepath.Join(folder, "file.bin")
	writeScanFile(t, file, 4096)
	s := scanFixture(t, root)
	target := scanEntry(s.root, folder)
	queueDeletion(s, target)
	if err := os.Rename(folder, folder+".original"); err != nil {
		t.Fatal(err)
	}
	writeScanFile(t, file, 8192)
	rescanAndWait(t, s, target)
	deleteAndWait(t, s)
	if len(s.deleteQueue) != 1 || target.deleteErr == nil {
		t.Fatal("rescanning the queued folder silently accepts its replacement")
	}
	if _, err := os.Stat(file); err != nil {
		t.Fatal("replacement folder is deleted:", err)
	}
	restoreDeletion(s, target)
	queueDeletion(s, target)
	WithFrameLock(func() {
		commitDeletion(s)
		rescanDirectory(s, s.root)
		restoreDeletion(s, nil)
		closeScanner(s)
		if !s.deleteBusy || s.rescan != nil || len(s.deleteQueue) != 1 || s.cancelled.Load() {
			t.Fatal("deletion permits a conflicting scan, restore, or tab close")
		}
	})
	deleteAndWait(t, s)
	if len(s.deleteQueue) != 0 || s.root.Size != 0 {
		t.Fatal("explicitly requeued folder cannot be deleted")
	}
}

func TestDeletionOverlappingTabs(t *testing.T) {
	root := rescanFixture(t)
	folder := filepath.Join(root, "Old")
	file := filepath.Join(folder, "file.bin")
	writeScanFile(t, file, 32768)
	kept := writeScanFile(t, filepath.Join(root, "keep.bin"), 4096)
	s, other, nested := scanFixture(t, root), scanFixture(t, root), scanFixture(t, folder)
	previous := appData
	appData = &DiskUsageAnalyzer{scanners: []*Scanner{s, other, nested}, activeScanner: s}
	t.Cleanup(func() { appData = previous })
	queueDeletion(other, scanEntry(other.root, file))
	queueDeletion(s, scanEntry(s.root, folder))
	started, done := other.started, other.done
	// Hold the second tab in its in-progress state to exercise deferred updates.
	other.state = Running
	deleteAndWait(t, s)
	if s.root.Size != kept || len(other.pendingDeletions) == 0 || nested.root.Size != 0 || !os.IsNotExist(nested.err) {
		t.Fatal("known deletions do not reach overlapping scans")
	}
	other.state = Done
	applyKnownDeletions(other)
	if other.root.Size != kept || len(other.deleteQueue) != 0 || other.started != started || other.done != done {
		t.Fatal("deferred deletion starts a scan, retains queued deletions, or loses accounting")
	}
	queueDeletion(s, scanEntry(s.root, filepath.Join(root, "keep.bin")))
	queueDeletion(other, scanEntry(other.root, filepath.Join(root, "keep.bin")))
	WithFrameLock(func() {
		commitDeletion(s)
		commitDeletion(other)
	})
	deleteAndWait(t, s)
	deleteAndWait(t, other)
	if s.root.Size != 0 || other.root.Size != 0 || len(s.deleteQueue) != 0 || len(other.deleteQueue) != 0 {
		t.Fatal("overlapping deletion workers retain deleted files or queue entries")
	}
}
