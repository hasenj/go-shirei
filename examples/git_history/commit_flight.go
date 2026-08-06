package main

import (
	"context"
	"sync"
	"time"

	. "go.hasen.dev/shirei"
)

// Commit load flights: at most one job per (repo, hash). Multiple waiters join
// the same flight; wants only grow (stats → stats+diff). Cache owns the DiffDoc
// buffer; the flight writes into it and marks Ready when finished.
//
// Parallelism: many hashes may fly at once; flightSem bounds heavy work.

const maxConcurrentFlights = 6

type flightKey struct {
	repo string
	hash string
}

// statsSink is filled when the flight has CommitStats.
// apply runs under WithFrameLock from the flight.
type statsSink struct {
	apply  func(CommitStats)
	onDone func()
}

type commitFlight struct {
	key    flightKey
	ctx    context.Context
	cancel context.CancelFunc

	mu sync.Mutex

	wantStats bool
	wantDiff  bool

	// Cache-owned doc when diff is wanted (stable pointer for the flight's life).
	// Joins with a different *DiffDoc cancel and restart — never write into one
	// buffer while markReady settles another (tab close/reopen, cache invalidate).
	doc       *DiffDoc
	markReady func(err error)

	// Optional UI attachment (selection). Swapped on re-select; nil is fine —
	// flight still fills doc for the cache.
	uiNotify func(batch []DiffRow, done bool)

	statsSinks []statsSink
	onDones    []func()

	// streamingStarted avoids wiping partial rows on a second streamDiff call.
	streamingStarted bool

	// After finish:
	finished bool
	stats    CommitStats
	err      error

	// done is closed once finish has removed this flight from the map (or when
	// finish completes). Waiters use it to restart after cancel/replace.
	done chan struct{}
}

var (
	flightMu  sync.Mutex
	flights   = map[flightKey]*commitFlight{}
	flightSem = make(chan struct{}, maxConcurrentFlights)
)

// requestCommitDiff joins or starts a flight that fills ce (cache entry).
// markReady settles the cache slot; uiNotify may be nil (background fill).
// Flight does not stop if UI detaches — it keeps writing into ce.Doc.
//
// Joining with a different Doc than the flight's buffer cancels the old flight
// and starts a new one for ce (avoids markReady settling a new slot while rows
// were written into an orphaned buffer after tab close/reopen or invalidate).
//
// Background only: callers must not invoke this on the UI/frame path (it may
// take flightMu / eventually WithFrameLock via finish callbacks). Use
// requestLoad → go → requestCommitSelection.
func requestCommitDiff(
	repo, hash string,
	ce *CacheEntry,
	markReady func(err error),
	uiNotify func(batch []DiffRow, done bool),
	onDone func(),
) {
	if repo == "" || hash == "" || ce == nil || ce.Doc == nil {
		return
	}
	// Successful Ready: nothing to do. Failed Ready is reopened by the caller
	// (beginSelect / getReady policy); if we still see it, refuse instant-done.
	if ce.Ready && ce.Err == "" {
		if onDone != nil {
			onDone()
		}
		return
	}

	k := flightKey{repo: repo, hash: hash}
	for {
		// Race: previous flight may have settled this slot while we waited.
		if ce.Ready && ce.Err == "" {
			if onDone != nil {
				onDone()
			}
			return
		}

		flightMu.Lock()
		f := flights[k]
		if f == nil {
			ctx, cancel := context.WithCancel(context.Background())
			f = &commitFlight{
				key:       k,
				ctx:       ctx,
				cancel:    cancel,
				wantDiff:  true,
				wantStats: true,
				doc:       ce.Doc,
				markReady: markReady,
				uiNotify:  uiNotify,
				done:      make(chan struct{}),
			}
			if onDone != nil {
				f.onDones = append(f.onDones, onDone)
			}
			flights[k] = f
			flightMu.Unlock()
			go f.run()
			return
		}

		// Join existing in-flight job, or wait-and-restart if finished / wrong doc.
		f.mu.Lock()
		if f.finished {
			done := f.done
			f.mu.Unlock()
			flightMu.Unlock()
			if done != nil {
				<-done
			}
			// Map slot free (or another flight replaced us); retry.
			continue
		}
		// Different cache buffer for same (repo, hash): cancel and restart so
		// markReady never settles a slot the flight did not write into.
		if f.doc != nil && f.doc != ce.Doc {
			cancel := f.cancel
			done := f.done
			f.mu.Unlock()
			flightMu.Unlock()
			if cancel != nil {
				cancel()
			}
			if done != nil {
				<-done
			}
			continue
		}

		f.wantDiff = true
		f.wantStats = true
		if f.doc == nil {
			f.doc = ce.Doc
		}
		// Same doc only: safe to swap settlers / UI.
		if markReady != nil {
			f.markReady = markReady
		}
		if uiNotify != nil {
			f.uiNotify = uiNotify
		}
		if onDone != nil {
			f.onDones = append(f.onDones, onDone)
		}
		f.mu.Unlock()
		flightMu.Unlock()
		return
	}
}

// requestCommitFlightStats joins or starts a flight for sidebar stats only
// (or attaches to an in-flight diff for the same hash).
// Background only (stats workers).
func requestCommitFlightStats(repo, hash string, sink statsSink) {
	if repo == "" || hash == "" || sink.apply == nil {
		return
	}

	flightMu.Lock()
	k := flightKey{repo: repo, hash: hash}
	f := flights[k]
	if f == nil {
		ctx, cancel := context.WithCancel(context.Background())
		f = &commitFlight{
			key:        k,
			ctx:        ctx,
			cancel:     cancel,
			wantStats:  true,
			statsSinks: []statsSink{sink},
			done:       make(chan struct{}),
		}
		flights[k] = f
		flightMu.Unlock()
		statsLog("flight new stats-only %s", shortHash(hash))
		go f.run()
		return
	}

	f.mu.Lock()
	if f.finished {
		st, err := f.stats, f.err
		done := f.done
		f.mu.Unlock()
		flightMu.Unlock()
		statsLog("flight join finished %s ready=%v err=%v", shortHash(hash), st.Ready, err)
		// Already off UI thread (stats worker); frame lock is OK here.
		if err == nil && st.Ready {
			WithFrameLock(func() { sink.apply(st) })
		}
		if sink.onDone != nil {
			sink.onDone()
		}
		// Ensure map is clear for any follow-up diff request.
		if done != nil {
			<-done
		}
		return
	}
	// Joining in-flight work (often the selected commit's full diff).
	statsLog("flight join live %s wantDiff=%v", shortHash(hash), f.wantDiff)
	f.wantStats = true
	f.statsSinks = append(f.statsSinks, sink)
	f.mu.Unlock()
	flightMu.Unlock()
}

// cancelCommitFlightsForRepo aborts in-flight jobs for a repo (tab close / hard refresh).
func cancelCommitFlightsForRepo(repo string) {
	if repo == "" {
		return
	}
	flightMu.Lock()
	var list []*commitFlight
	for k, f := range flights {
		if k.repo == repo {
			list = append(list, f)
		}
	}
	flightMu.Unlock()
	for _, f := range list {
		f.cancel()
	}
}

// attachFlightUI updates the UI notifier on an existing flight (re-select).
func attachFlightUI(repo, hash string, uiNotify func(batch []DiffRow, done bool)) {
	flightMu.Lock()
	f := flights[flightKey{repo: repo, hash: hash}]
	flightMu.Unlock()
	if f == nil {
		return
	}
	f.mu.Lock()
	if !f.finished {
		f.uiNotify = uiNotify
	}
	f.mu.Unlock()
}

func (f *commitFlight) run() {
	t0 := time.Now()
	semWait0 := time.Now()
	select {
	case flightSem <- struct{}{}:
		defer func() { <-flightSem }()
	case <-f.ctx.Done():
		f.finish(CommitStats{}, f.ctx.Err())
		return
	}
	semWait := time.Since(semWait0)

	if err := f.ctx.Err(); err != nil {
		f.finish(CommitStats{}, err)
		return
	}

	f.mu.Lock()
	wantDiff := f.wantDiff
	wantStats := f.wantStats
	doc := f.doc
	repo, hash := f.key.repo, f.key.hash
	f.mu.Unlock()

	statsLog("flight begin %s wantDiff=%v wantStats=%v semWait=%s",
		shortHash(hash), wantDiff, wantStats, semWait.Round(time.Microsecond))

	if wantDiff && doc != nil {
		meta0 := time.Now()
		if err := f.fillMeta(doc, repo, hash); err != nil {
			f.finish(CommitStats{}, err)
			return
		}
		statsLog("flight meta %s %s", shortHash(hash), time.Since(meta0).Round(time.Microsecond))
		f.nudgeUI()
	}

	snap0 := time.Now()
	snaps, err := snapshotFirstParentChanges(f.ctx, repo, hash)
	snapDur := time.Since(snap0)
	if err != nil {
		statsLog("flight snapshot FAIL %s %s err=%v", shortHash(hash), snapDur.Round(time.Microsecond), err)
		f.finish(CommitStats{}, err)
		return
	}
	statsLog("flight snapshot %s files=%d %s", shortHash(hash), len(snaps), snapDur.Round(time.Microsecond))

	f.mu.Lock()
	wantDiff = f.wantDiff
	wantStats = f.wantStats
	doc = f.doc
	f.mu.Unlock()

	var st CommitStats
	if wantDiff && doc != nil {
		diff0 := time.Now()
		st, err = f.streamDiff(snaps, doc)
		statsLog("flight streamDiff %s %s err=%v", shortHash(hash), time.Since(diff0).Round(time.Microsecond), err)
	} else if wantStats {
		stat0 := time.Now()
		files, ferr := fileStatsFromSnaps(f.ctx, snaps)
		if ferr != nil {
			statsLog("flight fileStats FAIL %s %s err=%v", shortHash(hash), time.Since(stat0).Round(time.Microsecond), ferr)
			f.finish(CommitStats{}, ferr)
			return
		}
		st = statsFromNumstat(files)
		statsLog("flight fileStats %s files=%d %s", shortHash(hash), len(files), time.Since(stat0).Round(time.Microsecond))

		// Upgrade: selection attached diff while we were stats-only.
		f.mu.Lock()
		wantDiff = f.wantDiff
		doc = f.doc
		f.mu.Unlock()
		if wantDiff && doc != nil {
			if err := f.fillMeta(doc, repo, hash); err != nil {
				f.finish(st, err)
				return
			}
			diff0 := time.Now()
			st2, derr := f.streamDiff(snaps, doc)
			statsLog("flight upgrade-streamDiff %s %s err=%v", shortHash(hash), time.Since(diff0).Round(time.Microsecond), derr)
			if derr != nil {
				f.finish(st, derr)
				return
			}
			st = st2
		}
	}

	statsLog("flight end %s total=%s wantDiff=%v", shortHash(hash), time.Since(t0).Round(time.Microsecond), wantDiff)
	f.finish(st, err)
}

func (f *commitFlight) fillMeta(doc *DiffDoc, repo, hash string) error {
	meta, err := loadCommitMetaGo(repo, hash)
	if err != nil {
		return err
	}
	if f.ctx.Err() != nil {
		return f.ctx.Err()
	}
	WithFrameLock(func() {
		// Keep stub sidebar totals until segs/stats overwrite them.
		a, d, fc := doc.TotalAdded, doc.TotalDeleted, doc.FileCount
		rows, segs := doc.Rows, doc.Segs
		*doc = *meta
		doc.Rows, doc.Segs = rows, segs
		if doc.TotalAdded == 0 && doc.TotalDeleted == 0 && doc.FileCount == 0 {
			doc.TotalAdded, doc.TotalDeleted, doc.FileCount = a, d, fc
		}
	})
	return nil
}

func (f *commitFlight) streamDiff(snaps []fileSnap, doc *DiffDoc) (CommitStats, error) {
	f.mu.Lock()
	first := !f.streamingStarted
	f.streamingStarted = true
	f.mu.Unlock()

	if first {
		WithFrameLock(func() {
			// Fresh stream into this buffer (partial rows only if re-entry bug).
			if len(doc.Rows) > 0 && len(doc.Segs) == 0 && doc.Subject != "" {
				// Stub/meta only — clear any accidental body.
			}
		})
	}

	for _, s := range snaps {
		if err := f.ctx.Err(); err != nil {
			return CommitStats{}, err
		}
		header := DiffRow{Kind: RowFileHeader, Text: s.label}
		f.emit(doc, []DiffRow{header}, false)
		body := bodyFromSnapshot(s)
		const batch = 64
		for i := 0; i < len(body); i += batch {
			if err := f.ctx.Err(); err != nil {
				return CommitStats{}, err
			}
			end := i + batch
			if end > len(body) {
				end = len(body)
			}
			f.emit(doc, body[i:end], false)
		}
	}
	f.emit(doc, nil, true)

	var st CommitStats
	WithFrameLock(func() {
		st = statsFromDoc(doc)
	})
	return st, nil
}

func (f *commitFlight) emit(doc *DiffDoc, batch []DiffRow, done bool) {
	WithFrameLock(func() {
		if doc != nil && len(batch) > 0 {
			prev := len(doc.Rows)
			doc.Rows = append(doc.Rows, batch...)
			growDocSegs(&doc.Segs, doc.Rows, prev, len(doc.Rows), doc.Stats)
		}
		if doc != nil && done {
			doc.Segs = buildDiffFileSegs(doc)
			if len(doc.Stats) == 0 && len(doc.Segs) > 0 {
				for _, s := range doc.Segs {
					doc.Stats = append(doc.Stats, FileStat{
						Path: s.Path, Added: s.Added, Deleted: s.Deleted, Binary: s.Binary,
					})
				}
				doc.recomputeTotals()
			}
		}
	})

	f.mu.Lock()
	ui := f.uiNotify
	f.mu.Unlock()
	if ui != nil {
		ui(batch, done)
	}
}

func (f *commitFlight) nudgeUI() {
	f.mu.Lock()
	ui := f.uiNotify
	f.mu.Unlock()
	if ui != nil {
		ui(nil, false)
	}
}

func (f *commitFlight) finish(st CommitStats, err error) {
	f.mu.Lock()
	if f.finished {
		f.mu.Unlock()
		return
	}
	f.finished = true
	if err == nil {
		if !st.Ready {
			st.Ready = true
		}
		f.stats = st
	}
	f.err = err
	sinks := append([]statsSink(nil), f.statsSinks...)
	onDones := append([]func(){}, f.onDones...)
	markReady := f.markReady
	doc := f.doc
	wantDiff := f.wantDiff
	cancel := f.cancel
	f.mu.Unlock()

	if err == nil && st.Ready {
		WithFrameLock(func() {
			for _, s := range sinks {
				if s.apply != nil {
					s.apply(st)
				}
			}
			if doc != nil {
				doc.TotalAdded = st.Added
				doc.TotalDeleted = st.Deleted
				if doc.FileCount == 0 {
					doc.FileCount = st.Files
				}
			}
		})
	}

	// Settle cache whenever we own a doc (diff path or upgraded).
	if markReady != nil && (wantDiff || doc != nil) {
		markReady(err)
	}

	for _, s := range sinks {
		if s.onDone != nil {
			s.onDone()
		}
	}
	for _, d := range onDones {
		if d != nil {
			d()
		}
	}

	flightMu.Lock()
	if cur := flights[f.key]; cur == f {
		delete(flights, f.key)
	}
	flightMu.Unlock()

	if cancel != nil {
		cancel()
	}
	// Signal waiters only after the map slot is free so a restart can insert.
	if f.done != nil {
		select {
		case <-f.done:
		default:
			close(f.done)
		}
	}
}
