//go:build !windows

package main

import (
	"os"
	"syscall"
)

type NodeId struct {
	device uint64
	inode  uint64
	nlinks uint64
}

// path is unused on unix: everything is already in the Stat_t behind the
// FileInfo (windows needs the path to open the entry — see node_windows.go).
func GetNodeId(path string, info os.FileInfo) NodeId {
	if info == nil {
		var st syscall.Stat_t
		if err := syscall.Stat(path, &st); err != nil {
			return NodeId{nlinks: 1}
		}
		return NodeId{
			device: uint64(st.Dev),
			inode:  uint64(st.Ino),
			nlinks: uint64(st.Nlink),
		}
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		// Exotic/FUSE FileInfo — degrade like Windows (no hard-link dedupe).
		return NodeId{nlinks: 1}
	}
	return NodeId{
		device: uint64(st.Dev),
		inode:  uint64(st.Ino),
		nlinks: uint64(st.Nlink),
	}
}

func NodeLinksCount(n NodeId) int {
	return int(n.nlinks)
}

// DirNodeId returns a stable directory identity for cycle detection.
// ok is false when the path cannot be identified.
func DirNodeId(path string, info os.FileInfo) (NodeId, bool) {
	id := GetNodeId(path, info)
	if id.device == 0 && id.inode == 0 {
		return id, false
	}
	// nlinks==1 with zero identity was the soft fallback above.
	if id.nlinks == 1 && id.device == 0 {
		return id, false
	}
	return id, true
}

func PhysicalSize(path string, fi os.FileInfo) int64 {
	if fi == nil {
		return 0
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return fi.Size()
	}
	return st.Blocks * 512
}
