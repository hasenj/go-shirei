//go:build !windows

package main

import (
	"fmt"
	"os"
	"syscall"
)

type NodeId struct {
	device int32
	inode  uint64
	nlinks uint16
}

func GetNodeId(info os.FileInfo) NodeId {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		fmt.Println("info sys is not stat_t!!!", info.Sys())
		os.Exit(1)
	}
	return NodeId{
		device: st.Dev,
		inode:  st.Ino,
		nlinks: st.Nlink,
	}
}

func NodeLinksCount(n NodeId) int {
	return int(n.nlinks)
}
