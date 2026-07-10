//go:build linux || (darwin && x11darwin)

package x11backend

import (
	"github.com/jezek/xgb/shm"
	"golang.org/x/sys/unix"
)

// MIT-SHM present path. Instead of serializing the whole BGRA buffer into a
// PutImage request every frame (a full-frame copy over the X connection), the
// renderer writes directly into a System V shared-memory segment that the X
// server has also attached; presenting then sends only a small ShmPutImage
// request that references the segment — no pixel data crosses the socket.
// Falls back to plain PutImage when the extension or the shm setup is
// unavailable (e.g. a remote X server).

var (
	useShm       bool    // MIT-SHM extension negotiated
	presentViaShm bool   // the current presentBuf is the attached shm segment

	shmAttached bool
	shmSeg      shm.Seg
	shmID       int
	shmData     []byte
)

// initShm negotiates the MIT-SHM extension; reports whether it is available.
func initShm() bool {
	return shm.Init(X) == nil
}

// ensureShm (re)creates a shared-memory segment of w*h*4 bytes, attaches it to
// both this process and the X server, and returns the shared buffer. Returns nil
// on any failure, so the caller can fall back to plain PutImage.
func ensureShm(w, h int) []byte {
	size := w * h * 4
	if shmAttached && len(shmData) == size {
		return shmData
	}
	releaseShm()

	id, err := unix.SysvShmGet(unix.IPC_PRIVATE, size, unix.IPC_CREAT|0o600)
	if err != nil {
		return nil
	}
	data, err := unix.SysvShmAttach(id, 0, 0)
	if err != nil {
		unix.SysvShmCtl(id, unix.IPC_RMID, nil)
		return nil
	}
	seg, err := shm.NewSegId(X)
	if err != nil {
		unix.SysvShmDetach(data)
		unix.SysvShmCtl(id, unix.IPC_RMID, nil)
		return nil
	}
	// Checked so we learn synchronously whether the server accepted the segment
	// (an unchecked failure would only surface as an async error event).
	if err := shm.AttachChecked(X, seg, uint32(id), false).Check(); err != nil {
		unix.SysvShmDetach(data)
		unix.SysvShmCtl(id, unix.IPC_RMID, nil)
		return nil
	}
	// Mark the segment for removal now: it is freed once both this process and
	// the server detach, so it can't leak even if we exit abnormally.
	unix.SysvShmCtl(id, unix.IPC_RMID, nil)

	shmAttached = true
	shmSeg = seg
	shmID = id
	shmData = data
	return shmData
}

func releaseShm() {
	if !shmAttached {
		return
	}
	shm.Detach(X, shmSeg)
	unix.SysvShmDetach(shmData)
	shmAttached = false
	shmData = nil
}
