package x11backend

import "golang.org/x/sys/unix"

func threadID() int64 { return int64(unix.Gettid()) }
