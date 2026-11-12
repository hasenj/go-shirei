package main

import (
	"os"
	"slices"

	. "go.hasen.dev/shirei"
)

// rescanDirectory starts a replacement scan while retaining the displayed tree.
// The caller holds the frame lock, as with other UI actions.
func rescanDirectory(s *Scanner, target *ScanEntry) {
	if s == nil || s.state != Done || s.deleteBusy || target == nil || !target.IsDir {
		return
	}
	root := target
	for root.Parent != nil {
		root = root.Parent
	}
	if root != s.root {
		return
	}
	worker := newScanner()
	worker.rescanOf, worker.rescanTarget = s, target
	s.rescan = worker
	s.deleteNotice = ""
	s.state = Running
	s.showReadErrors = false
	startScan(worker, target.Path)
	RequestNextFrame()
}

// finishRescan reconciles a completed replacement under the frame lock.
func finishRescan(worker *Scanner) {
	s, target := worker.rescanOf, worker.rescanTarget
	if s.cancelled.Load() || s.rescan != worker {
		return
	}
	selectedPath := ""
	if s.selected != nil {
		selectedPath = s.selected.Path
	}
	expanded := make(map[string]bool)
	owners := make(map[NodeId]*ScanEntry)
	var remember func(*ScanEntry, bool)
	remember = func(e *ScanEntry, replacing bool) {
		replacing = replacing || e == target
		if replacing {
			expanded[e.Path] = e.Expanded
		} else if !e.IsDir && !e.Skip && e.node != (NodeId{}) {
			owners[e.node] = e
		}
		for _, child := range e.Entries {
			remember(child, replacing)
		}
	}
	remember(s.root, false)

	// A fresh observation also updates other known names for that inode.
	freshSizes := make(map[NodeId]int)
	var collect func(*ScanEntry)
	collect = func(e *ScanEntry) {
		e.Expanded = expanded[e.Path]
		if !e.IsDir && e.node != (NodeId{}) {
			freshSizes[e.node] = e.physicalSize
		}
		for _, child := range e.Entries {
			collect(child)
		}
	}
	collect(worker.root)

	parent := target.Parent
	fallback := target
	if parent != nil && os.IsNotExist(worker.err) {
		index := slices.Index(parent.Entries, target)
		parent.Entries = slices.Delete(parent.Entries, index, index+1)
		fallback = parent
	} else {
		*target = *worker.root
		target.Parent = parent
	}

	reconcileScan(s, selectedPath, fallback, owners, freshSizes)
	s.started, s.done = worker.started, worker.done
	s.scanned, s.submitted = worker.scanned, worker.submitted
	s.state = Done
	s.rescan = nil
	refreshDeleteQueue(s)
	applyKnownDeletions(s)
	RequestNextFrame()
}

// reconcileScan updates the measured tree without reading the filesystem.
func reconcileScan(s *Scanner, selectedPath string, fallback *ScanEntry, owners map[NodeId]*ScanEntry, freshSizes map[NodeId]int) {
	s.selected = nil
	s.readErrors = nil
	var reconcile func(*ScanEntry, *ScanEntry)
	reconcile = func(e, parent *ScanEntry) {
		e.Parent = parent
		e.Depth = 0
		if parent != nil {
			e.Depth = parent.Depth + 1
		}
		if e.Path == selectedPath {
			s.selected = e
		}
		if e.readErr != nil {
			s.readErrors = append(s.readErrors, e.readErr)
		}
		if !e.IsDir {
			if size, ok := freshSizes[e.node]; ok {
				e.physicalSize = size
			}
			e.Size, e.Skip = e.physicalSize, false
			if e.node != (NodeId{}) {
				if owners[e.node] == nil {
					owners[e.node] = e
				}
				if owners[e.node] != e {
					e.Size, e.Skip = 0, true
				}
			}
			return
		}
		for _, child := range e.Entries {
			reconcile(child, e)
		}
		updateEntryTotals(e)
	}
	reconcile(s.root, nil)
	if selectedPath != "" && s.selected == nil {
		s.selected = fallback
	}
	s.err = s.root.readErr
	// The completed scan's claim maps can retain replaced subtrees.
	s.links.Clear()
	s.seenDirs.Clear()
}
