//go:build darwin && x11darwin

package x11backend

import "syscall"

func threadID() int64 {
	id, _, _ := syscall.RawSyscall(syscall.SYS_THREAD_SELFID, 0, 0, 0)
	return int64(id)
}
