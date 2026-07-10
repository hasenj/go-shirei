//go:build windows

package main

import (
	"os"
	"syscall"
	"unsafe"
)

type NodeId struct {
	device uint64 // volume serial number
	inode  uint64 // NTFS file index
	nlinks uint64
}

// noIdentity is the NodeId for entries that can't recur in a scan
// (directories, reparse points, unopenable files): nlinks 1 keeps them out
// of the dedupe map, matching how main.go gates on NodeLinksCount > 1.
var noIdentity = NodeId{nlinks: 1}

// GetNodeId returns the file's identity and hard-link count. Windows has no
// way to learn the link count from directory enumeration alone, so this
// opens the file (attributes-only, no data access) and asks the handle —
// the per-file cost of correct hard-link dedupe (WinSxS under C:\Windows is
// full of hard links; without this a full-drive scan double-counts them).
//
// Directories are exempt: NTFS doesn't hard-link directories, and junctions
// or symlinks show up as reparse points (not IsRegular) which the scan
// doesn't follow.
func GetNodeId(path string, info os.FileInfo) NodeId {
	if !info.Mode().IsRegular() {
		return noIdentity
	}
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return noIdentity
	}
	h, err := syscall.CreateFile(p,
		0, // attribute access only
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE,
		nil, syscall.OPEN_EXISTING,
		syscall.FILE_FLAG_BACKUP_SEMANTICS|syscall.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return noIdentity
	}
	defer syscall.CloseHandle(h)
	var fi syscall.ByHandleFileInformation
	if err := syscall.GetFileInformationByHandle(h, &fi); err != nil {
		return noIdentity
	}
	return NodeId{
		device: uint64(fi.VolumeSerialNumber),
		inode:  uint64(fi.FileIndexHigh)<<32 | uint64(fi.FileIndexLow),
		nlinks: uint64(fi.NumberOfLinks),
	}
}

func NodeLinksCount(n NodeId) int {
	return int(n.nlinks)
}

var (
	modkernel32               = syscall.NewLazyDLL("kernel32.dll")
	procGetCompressedFileSize = modkernel32.NewProc("GetCompressedFileSizeW")
)

// PhysicalSize approximates on-disk size: GetCompressedFileSize reports the
// actual allocation for compressed and sparse files, and the logical size
// otherwise — the closest cheap analogue to unix's st.Blocks*512 (no handle
// needed, unlike FileStandardInfo's AllocationSize).
func PhysicalSize(path string, fi os.FileInfo) int64 {
	if !fi.Mode().IsRegular() {
		// don't follow reparse points to their target's size
		return fi.Size()
	}
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return fi.Size()
	}
	var high uint32
	low, _, callErr := procGetCompressedFileSize.Call(
		uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(&high)))
	// INVALID_FILE_SIZE in the low dword is only a failure if GetLastError
	// says so (a real size can end in 0xFFFFFFFF)
	const invalidFileSize = 0xFFFFFFFF
	if low == invalidFileSize {
		if errno, ok := callErr.(syscall.Errno); ok && errno != 0 {
			return fi.Size()
		}
	}
	return int64(high)<<32 | int64(low)
}
