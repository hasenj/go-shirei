//go:build !windows

package main

import (
	"fmt"
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
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		fmt.Println("info sys is not stat_t!!!", info.Sys())
		os.Exit(1)
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

func PhysicalSize(path string, fi os.FileInfo) int64 {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return fi.Size()
	}
	return st.Blocks * 512
}
