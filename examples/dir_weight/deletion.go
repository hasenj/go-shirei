package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	. "go.hasen.dev/shirei"
)

// Tests can delay the filesystem operation without delaying the frame loop.
var removeScannedEntry = (*os.Root).Remove

func queuedAncestor(e *ScanEntry) *ScanEntry {
	for ; e != nil; e = e.Parent {
		if e.queued {
			return e
		}
	}
	return nil
}

func entryWithin(e, parent *ScanEntry) bool {
	for ; e != nil; e = e.Parent {
		if e == parent {
			return true
		}
	}
	return false
}

func attachedEntry(s *Scanner, e *ScanEntry) bool {
	if e == nil {
		return false
	}
	for e.Parent != nil {
		if !slices.Contains(e.Parent.Entries, e) {
			return false
		}
		e = e.Parent
	}
	return e == s.root
}

// queueDeletion only changes UI state. Callers hold the frame lock.
func queueDeletion(s *Scanner, e *ScanEntry) {
	if s.state != Done || s.deleteBusy || e == s.root || !attachedEntry(s, e) || queuedAncestor(e) != nil {
		return
	}
	queue := make([]*ScanEntry, 0, len(s.deleteQueue)+1)
	if s.deleteNodes == nil {
		s.deleteNodes = make(map[*ScanEntry]NodeId)
	}
	for _, q := range s.deleteQueue {
		if entryWithin(q, e) {
			q.queued, q.deleteErr = false, nil
			delete(s.deleteNodes, q)
		} else {
			queue = append(queue, q)
		}
	}
	e.queued, e.deleteErr = true, nil
	s.deleteNodes[e] = e.node
	s.deleteQueue = append(queue, e)
	s.deleteNotice = ""
	RequestNextFrame()
}

// A fresh slice keeps the review's current frame stable when a row is restored.
func restoreDeletion(s *Scanner, e *ScanEntry) {
	if s.deleteBusy {
		return
	}
	queue := make([]*ScanEntry, 0, len(s.deleteQueue))
	for _, q := range s.deleteQueue {
		if e == nil || q == e {
			q.queued, q.deleteErr = false, nil
			delete(s.deleteNodes, q)
		} else {
			queue = append(queue, q)
		}
	}
	s.deleteQueue = queue
	if len(queue) == 0 {
		s.deleteReview = false
	}
	s.deleteNotice = ""
	RequestNextFrame()
}

// A manual rescan can replace entries while their deletion intent remains.
// Missing or replaced entries require restoring and queueing the new item.
func refreshDeleteQueue(s *Scanner) {
	if len(s.deleteQueue) == 0 {
		return
	}
	paths := make(map[string]*ScanEntry)
	var index func(*ScanEntry)
	index = func(e *ScanEntry) {
		paths[e.Path] = e
		for _, child := range e.Entries {
			index(child)
		}
	}
	index(s.root)
	nodes := make(map[*ScanEntry]NodeId)
	for i, old := range s.deleteQueue {
		old.queued = false
		expected := s.deleteNodes[old]
		if e := paths[old.Path]; e != nil && e.node != (NodeId{}) && e.node == expected {
			s.deleteQueue[i] = e
			e.queued, e.deleteErr = true, nil
			nodes[e] = expected
		} else {
			old.queued = true
			old.deleteErr = fmt.Errorf("Item changed or is no longer in this scan. Restore it from the queue and review again.")
			nodes[old] = expected
		}
	}
	s.deleteNodes = nodes
}

// commitDeletion processes the measured entries on a worker. While it runs,
// scans, queue edits and closing this tab are disabled; tree data stays stable.
func commitDeletion(s *Scanner) {
	if s.state != Done || s.deleteBusy || len(s.deleteQueue) == 0 {
		return
	}
	queue := slices.Clone(s.deleteQueue)
	valid := make([]*ScanEntry, 0, len(queue))
	failures := make(map[*ScanEntry]error)
	for _, e := range queue {
		e.deleteErr = nil
		if e == s.root || !attachedEntry(s, e) || e.node != s.deleteNodes[e] {
			failures[e] = fmt.Errorf("Item changed. Restore it from the queue and review again.")
		} else {
			valid = append(valid, e)
		}
	}
	s.deleteBusy, s.deleteReview = true, false
	s.deleteStarted = time.Now()
	s.deleteRemoved.Store(0)
	s.deleteNotice = ""
	remove := removeScannedEntry
	go func() {
		removed := deleteMeasuredEntries(s.root, valid, failures, func(parent *os.Root, name string) error {
			err := remove(parent, name)
			if err == nil {
				s.deleteRemoved.Add(1)
			}
			return err
		})
		// Build the cross-tab notification outside the frame lock. The measured
		// tree stays stable while the user browses, filters, or switches tabs.
		paths := make(map[string]NodeId)
		var record func(*ScanEntry)
		record = func(e *ScanEntry) {
			paths[deletionPath(e.Path)] = e.node
			for _, child := range e.Entries {
				record(child)
			}
		}
		for e := range removed {
			if !removed[e.Parent] {
				record(e)
			}
		}
		WithFrameLock(func() {
			// Other tabs can measure overlapping paths. Preserve these known
			// removals until each tab's current scan or deletion finishes.
			for _, other := range appData.scanners {
				if other == s {
					continue
				}
				rootPath := deletionPath(other.rootPath)
				prefix := strings.TrimSuffix(rootPath, string(filepath.Separator)) + string(filepath.Separator)
				for path, node := range paths {
					if path == rootPath || strings.HasPrefix(path, prefix) {
						if other.pendingDeletions == nil {
							other.pendingDeletions = make(map[string]NodeId)
						}
						other.pendingDeletions[path] = node
					}
				}
				applyKnownDeletions(other)
			}
			applyDeletions(s, removed)
			remaining := make([]*ScanEntry, 0, len(queue))
			for _, e := range queue {
				e.queued = false
				if err := failures[e]; err != nil {
					e.queued, e.deleteErr = true, err
					remaining = append(remaining, e)
				} else {
					delete(s.deleteNodes, e)
				}
			}
			s.deleteQueue = remaining
			s.deleteBusy = false
			s.deleteReview = s.deleteReview && len(remaining) > 0
			s.deleteNotice = "Deletion complete"
			if len(remaining) > 0 {
				s.deleteNotice = "Some items could not be deleted"
			}
			applyKnownDeletions(s)
			RequestNextFrame()
		})
	}()
	RequestNextFrame()
}

// Only names recorded by the scan are removed. Unmeasured additions leave a
// directory nonempty, and each successful removal has its own accounting result.
// Open directory handles keep traversal inside the validated parent directories.
func deleteMeasuredEntries(root *ScanEntry, queue []*ScanEntry, failures map[*ScanEntry]error, remove func(*os.Root, string) error) map[*ScanEntry]bool {
	removed := make(map[*ScanEntry]bool)
	if len(queue) == 0 {
		return removed
	}
	wanted, targets := make(map[*ScanEntry]bool), make(map[*ScanEntry]bool)
	for _, q := range queue {
		targets[q] = true
		for e := q; e != nil; e = e.Parent {
			wanted[e] = true
		}
	}
	fail := func(e, target *ScanEntry, err error) {
		for _, q := range queue {
			if q == target || (target == nil && entryWithin(q, e)) {
				if failures[q] == nil {
					failures[q] = fmt.Errorf("%s: %w", e.Path, err)
				}
			}
		}
	}
	check := func(e *ScanEntry, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		var id NodeId
		if e.IsDir {
			id, _ = DirNodeId(e.Path, info)
		} else {
			id = GetNodeId(e.Path, info)
		}
		id.nlinks = 0
		if e.node == (NodeId{}) || id != e.node || info.Mode().Type() != e.mode.Type() {
			return fmt.Errorf("Item changed since scanning. Rescan and review it again.")
		}
		if !e.IsDir && (info.Size() != e.logicalSize || info.ModTime().UnixNano() != e.modified) {
			return fmt.Errorf("File changed since scanning. Rescan and review it again.")
		}
		return nil
	}
	dir, err := os.OpenRoot(root.Path)
	if err == nil {
		defer dir.Close()
		info, statErr := dir.Stat(".")
		err = check(root, info, statErr)
	}
	if err != nil {
		fail(root, nil, err)
		return removed
	}
	var visit func(*os.Root, *ScanEntry, *ScanEntry) bool
	visit = func(parent *os.Root, e, target *ScanEntry) bool {
		if targets[e] {
			target = e
		}
		info, err := parent.Lstat(e.Name)
		if os.IsNotExist(err) {
			removed[e] = true
			return true
		}
		if err = check(e, info, err); err != nil {
			fail(e, target, err)
			return false
		}
		if e.IsDir {
			if e.readErr != nil {
				fail(e, target, fmt.Errorf("Folder is not fully scanned. Rescan it before deleting."))
				return false
			}
			childDir, err := parent.OpenRoot(e.Name)
			if err != nil {
				fail(e, target, err)
				return false
			}
			info, statErr := childDir.Stat(".")
			err = check(e, info, statErr)
			ok := err == nil
			if err != nil {
				fail(e, target, err)
			} else {
				for _, child := range e.Entries {
					if target != nil || wanted[child] {
						if !visit(childDir, child, target) {
							ok = false
						}
					}
				}
			}
			childDir.Close()
			if !ok {
				return false
			}
		}
		if target != nil {
			info, err := parent.Lstat(e.Name)
			if !os.IsNotExist(err) {
				err = check(e, info, err)
				if err == nil {
					err = remove(parent, e.Name)
				}
			}
			if err != nil && !os.IsNotExist(err) {
				fail(e, target, err)
				return false
			}
			removed[e] = true
		}
		return true
	}
	for _, child := range root.Entries {
		if wanted[child] {
			visit(dir, child, nil)
		}
	}
	return removed
}

// applyDeletions changes only the in-memory snapshot. Surviving hard-link owners
// keep their allocation; a surviving alias inherits a deleted owner's allocation.
func applyDeletions(s *Scanner, removed map[*ScanEntry]bool) {
	if len(removed) == 0 {
		return
	}
	selectedPath := ""
	fallback := s.root
	if s.selected != nil {
		selectedPath = s.selected.Path
		fallback = s.selected
		for e := s.selected; e != nil; e = e.Parent {
			if removed[e] {
				fallback = e.Parent
			}
		}
	}
	owners := make(map[NodeId]*ScanEntry)
	var prune func(*ScanEntry)
	prune = func(e *ScanEntry) {
		kept := e.Entries[:0]
		for _, child := range e.Entries {
			if !removed[child] {
				kept = append(kept, child)
				prune(child)
			}
		}
		clear(e.Entries[len(kept):])
		e.Entries = kept
		if !e.IsDir && !e.Skip && e.node != (NodeId{}) {
			owners[e.node] = e
		}
	}
	if removed[s.root] {
		s.root.Entries = nil
		s.root.readErr = &os.PathError{Op: "deleted", Path: s.root.Path, Err: os.ErrNotExist}
		fallback = s.root
	} else {
		prune(s.root)
	}
	reconcileScan(s, selectedPath, fallback, owners, nil)
	s.scanned, s.submitted = s.root.subDone, s.root.subCount
}

func deletionPath(path string) string {
	path = filepath.Clean(path)
	if runtime.GOOS == "windows" {
		path = strings.ToLower(path)
	}
	return path
}

// Changes known from another tab are applied only when this tree is stable.
func applyKnownDeletions(s *Scanner) {
	if len(s.pendingDeletions) == 0 || s.root == nil || s.state == Running || s.deleteBusy {
		return
	}
	removed := make(map[*ScanEntry]bool)
	var visit func(*ScanEntry)
	visit = func(e *ScanEntry) {
		if node, ok := s.pendingDeletions[deletionPath(e.Path)]; ok && node == e.node {
			removed[e] = true
			return
		}
		for _, child := range e.Entries {
			visit(child)
		}
	}
	visit(s.root)
	s.pendingDeletions = nil
	remaining := make([]*ScanEntry, 0, len(s.deleteQueue))
	for _, q := range s.deleteQueue {
		deleted := false
		for e := q; e != nil; e = e.Parent {
			deleted = deleted || removed[e]
		}
		if deleted {
			q.queued, q.deleteErr = false, nil
			delete(s.deleteNodes, q)
		} else {
			remaining = append(remaining, q)
		}
	}
	s.deleteQueue = remaining
	if len(remaining) == 0 {
		s.deleteReview = false
	}
	applyDeletions(s, removed)
}
