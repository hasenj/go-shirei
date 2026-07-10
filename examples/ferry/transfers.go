package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync/atomic"
	"time"

	"go.hasen.dev/shirei"

	"go.hasen.dev/shirei/examples/ferry/remote"
)

type TransferStatus int

const (
	TransferPending TransferStatus = iota
	TransferRunning
	TransferAwaiting // conflict prompt is up
	TransferDone
	TransferFailed
	TransferSkipped
	TransferCancelled
)

type TransferDir int

const (
	DirUpload TransferDir = iota
	DirDownload
)

// Transfer is one queued copy — a whole selection moves as a single
// batch: one tar stream, one stage, one commit. An app-owned object; the
// queue strip renders these by pointer identity.
type Transfer struct {
	Dir     TransferDir
	Label   string          // single name, or "N items"
	Items   []string        // upload: local paths; download: entry names
	SrcDir  string          // download: the remote source directory
	itemDir map[string]bool // base name → is a directory (conflict wording)
	DstDir  string          // the other pane's cwd, captured at enqueue time
	DstDesc string

	// the transfer queue is global, but each transfer belongs to one
	// session: Conn is where it runs, Server is the alias shown in the
	// row, dstPane is the pane to refresh on success. Captured at enqueue
	// so a later tab switch (or the session closing) can't redirect it.
	Conn    *remote.Conn
	Server  string
	dstPane *Pane

	Status TransferStatus
	Err    error

	// progress is written from the transfer goroutine per chunk and read
	// by the UI every frame — atomics, no frame lock on the hot path
	done, total atomic.Int64
	cancel      context.CancelFunc
}

func (t *Transfer) Progress() (done, total int64) {
	return t.done.Load(), t.total.Load()
}

// conflictChoice is the batch conflict decision. Skip-existing reruns the
// transfer with only the fresh items (for a single-item batch that means
// nothing happens).
type conflictChoice int

const (
	choiceSkipExisting conflictChoice = iota
	choiceMerge
	choiceReplace
)

// ConflictRequest parks the transfer worker while the modal collects a
// decision.
type ConflictRequest struct {
	Transfer *Transfer
	Names    []string // the conflicted destination names
	HasDir   bool     // any of them a directory → merge/replace wording
	Answer   chan conflictChoice
}

// enqueueCopy queues the source pane's selection for copying into the
// other pane's cwd. Called from UI code (frame lock held).
func enqueueCopy(src, dst *Pane, dir TransferDir) {
	sel := src.selection()
	if len(sel) == 0 || !appData.hasRemote() {
		return
	}
	tr := &Transfer{
		Dir:     dir,
		SrcDir:  src.CWD,
		DstDir:  dst.CWD,
		DstDesc: dst.FS.Label + ":" + dst.CWD,
		itemDir: map[string]bool{},
		Label:   sel[0].Name,
		Conn:    appData.active.Conn,
		Server:  appData.active.Alias,
		dstPane: dst,
	}
	if len(sel) > 1 {
		tr.Label = fmt.Sprintf("%d items", len(sel))
	}
	for _, r := range sel {
		if dir == DirUpload {
			tr.Items = append(tr.Items, r.Path)
		} else {
			tr.Items = append(tr.Items, r.Name)
		}
		tr.itemDir[r.Name] = r.IsDir
	}
	appData.transfers = append(appData.transfers, tr)
	appData.transfersExpanded = true // a fresh transfer opens the panel
	if !appData.transferBusy {
		appData.transferBusy = true
		go transferWorker()
	}
}

// transferWorker drains the queue one transfer at a time — transfers
// share the browsing connection, and serial keeps progress readable.
func transferWorker() {
	for {
		var tr *Transfer
		shirei.WithFrameLock(func() {
			for _, t := range appData.transfers {
				if t.Status == TransferPending {
					tr = t
					break
				}
			}
			if tr == nil {
				appData.transferBusy = false
			} else {
				tr.Status = TransferRunning
			}
		})
		shirei.RequestNextFrame()
		if tr == nil {
			return
		}
		runTransfer(tr)
	}
}

func runTransfer(tr *Transfer) {
	conn := tr.Conn
	if conn == nil {
		finishTransfer(tr, TransferFailed, errors.New("not connected"))
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	shirei.WithFrameLock(func() { tr.cancel = cancel })

	var lastFrame time.Time
	progress := func(done, total int64) {
		tr.done.Store(done)
		tr.total.Store(total)
		if time.Since(lastFrame) > 50*time.Millisecond {
			lastFrame = time.Now()
			shirei.RequestNextFrame()
		}
	}

	attempt := func(items []string, strat remote.Strategy) error {
		if tr.Dir == DirUpload {
			return conn.Upload(ctx, items, tr.DstDir, strat, progress)
		}
		return conn.Download(ctx, tr.SrcDir, items, tr.DstDir, strat, progress)
	}

	// optimistic runs: the engine checks conflicts before any data moves,
	// so a ConflictError costs nothing. Skip-existing filters and retries
	// (the fresh set can conflict again if the destination races us).
	items := tr.Items
	var err error
	for {
		err = attempt(items, remote.StrategyFail)
		var conflict *remote.ConflictError
		if !errors.As(err, &conflict) {
			break
		}
		switch askConflict(tr, conflict.Names) {
		case choiceSkipExisting:
			items = withoutNames(items, tr.Dir, conflict.Names)
			if len(items) == 0 {
				finishTransfer(tr, TransferSkipped, nil)
				return
			}
			continue
		case choiceMerge:
			err = attempt(items, remote.StrategyMerge)
		case choiceReplace:
			err = attempt(items, remote.StrategyReplace)
		}
		break
	}

	switch {
	case err == nil:
		finishTransfer(tr, TransferDone, nil)
		refreshDest(tr)
	case errors.Is(err, context.Canceled):
		finishTransfer(tr, TransferCancelled, nil)
	default:
		finishTransfer(tr, TransferFailed, err)
	}
}

// withoutNames drops the items whose destination name is in conflicted.
func withoutNames(items []string, dir TransferDir, conflicted []string) []string {
	skip := map[string]bool{}
	for _, n := range conflicted {
		skip[n] = true
	}
	var out []string
	for _, item := range items {
		name := item
		if dir == DirUpload {
			name = filepath.Base(item)
		}
		if !skip[name] {
			out = append(out, item)
		}
	}
	return out
}

func finishTransfer(tr *Transfer, status TransferStatus, err error) {
	shirei.WithFrameLock(func() {
		tr.Status = status
		tr.Err = err
		tr.cancel = nil
	})
	shirei.RequestNextFrame()
}

// askConflict publishes the prompt and parks until the modal answers.
func askConflict(tr *Transfer, names []string) conflictChoice {
	req := &ConflictRequest{
		Transfer: tr,
		Names:    names,
		Answer:   make(chan conflictChoice, 1),
	}
	for _, n := range names {
		if tr.itemDir[n] {
			req.HasDir = true
		}
	}
	shirei.WithFrameLock(func() {
		tr.Status = TransferAwaiting
		appData.conflictReq = req
	})
	shirei.RequestNextFrame()
	choice := <-req.Answer
	shirei.WithFrameLock(func() {
		if appData.conflictReq == req {
			appData.conflictReq = nil
		}
		tr.Status = TransferRunning
	})
	shirei.RequestNextFrame()
	return choice
}

// refreshDest reloads the transfer's destination pane if it still shows
// the destination directory, keeping the current anchor by name. The pane
// was captured at enqueue, so this works even if that session isn't the
// active tab anymore.
func refreshDest(tr *Transfer) {
	shirei.WithFrameLock(func() {
		dst := tr.dstPane
		if dst == nil || dst.CWD != tr.DstDir {
			return // user navigated away; nothing to refresh
		}
		keep := ""
		if dst.Anchor != nil {
			keep = dst.Anchor.Name
		}
		dst.reload(keep)
	})
	shirei.RequestNextFrame()
}

func cancelTransfer(tr *Transfer) {
	if tr.cancel != nil {
		tr.cancel()
	}
}

// cancelSessionTransfers cancels every queued or running transfer bound
// to s — called when its tab closes so nothing keeps writing to a
// connection that is going away.
func cancelSessionTransfers(s *Session) {
	for _, tr := range appData.transfers {
		if tr.Conn != s.Conn {
			continue
		}
		switch tr.Status {
		case TransferPending:
			tr.Status = TransferCancelled
		case TransferRunning, TransferAwaiting:
			if tr.cancel != nil {
				tr.cancel()
			}
		}
	}
}
