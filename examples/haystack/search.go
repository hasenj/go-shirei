package main

import (
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	g "go.hasen.dev/generic"
	"go.hasen.dev/textsearch"

	. "go.hasen.dev/shirei"
)

// Re-export core result types so the GUI and tests keep short names.
type (
	Params      = textsearch.Params
	FileResult  = textsearch.FileResult
	ContextLine = textsearch.ContextLine
	Match       = textsearch.Match
)

// Search holds everything about one run: its parameters, live counters, and
// the results as they stream in. Never copied (atomic.Bool inside); always
// referenced by pointer. UI-only fields stay here; matching lives in textsearch.
type Search struct {
	params    Params
	cancelled atomic.Bool

	started time.Time
	done    time.Time
	running bool
	err     error

	// matches is appended by worker goroutines only under WithFrameLock, so
	// the render (which holds the frame lock) sees a consistent snapshot. The
	// counters are atomic instead: they tick on every scanned file, and taking
	// the frame lock 2000+ times a second just to bump an int would serialize
	// the workers against each other and the UI.
	matches      []*Match
	filesScanned atomic.Int64
	filesMatched atomic.Int64
	matchCount   atomic.Int64

	// firstVis is this tab's first painted row index, mirrored each frame
	// (OutFirstVisible) and restored via ScrollToIndex when the tab is shown
	// again. Frame-goroutine-only; not touched by the scanning workers.
	firstVis int
}

// One shared worker pool for the whole app: a new search cancels the old one,
// but jobs already queued for the cancelled search still drain (they bail on
// the cancelled flag), so we reuse the pool rather than tearing it down.
var jobs = g.MakeJobQueue(max(4, runtime.NumCPU()))

// runNewSearch opens a new tab for params p and starts scanning in the
// background. Empty queries are ignored (no empty tab). Other tabs' searches
// keep running independently — a new search never cancels them; only closing a
// tab does.
func runNewSearch(p Params) {
	if p.Query == "" || p.Root == "" {
		return
	}

	s := &Search{params: p, started: time.Now()}
	if _, err := textsearch.NewMatcher(p); err != nil {
		s.err = err
		s.done = time.Now()
	} else {
		s.running = true
	}

	g.Append(&appData.searches, s)
	activateTab(s) // scrollY is 0 → the new tab starts at the top
	RequestNextFrame()

	if s.running {
		go runSearch(s)
	}
}

// runSearch walks the tree and fans every candidate file out to the worker
// pool, then waits for all of them and marks the search done. Concurrency is
// how we "finish as quickly as possible"; incremental publishing (each file
// under WithFrameLock) is how results appear as they are found.
func runSearch(s *Search) {
	m, err := textsearch.NewMatcher(s.params)
	if err != nil {
		WithFrameLock(func() {
			s.err = err
			s.running = false
			s.done = time.Now()
		})
		RequestNextFrame()
		return
	}

	var wg sync.WaitGroup
	textsearch.WalkFiles(s.params, s.cancelled.Load, func(path string) {
		wg.Add(1)
		jobs.Submit(func() {
			defer wg.Done()
			if s.cancelled.Load() {
				return
			}
			scanFile(s, m, path)
		})
	})
	wg.Wait()

	WithFrameLock(func() {
		if !s.cancelled.Load() {
			s.running = false
			s.done = time.Now()
		}
	})
	RequestNextFrame()
}

// searchSync runs the whole search on the calling goroutine, in lexical walk
// order. Used by tests and by --png so results are fully present (and in a
// deterministic order) before rendering.
func searchSync(p Params) *Search {
	s := &Search{params: p, started: time.Now()}
	r := textsearch.Search(p)
	s.err = r.Err
	s.matches = r.Matches
	s.filesScanned.Store(r.FilesScanned)
	s.filesMatched.Store(r.FilesMatched)
	s.matchCount.Store(r.MatchCount)
	s.done = time.Now()
	return s
}

// scanFile reads one file via textsearch and publishes its matches atomically.
func scanFile(s *Search, m *textsearch.Matcher, path string) {
	fileMatches, hitLines, ok := textsearch.ScanPath(path, s.params.Root, m, textsearch.ScanConfig{})
	if !ok {
		return
	}
	s.filesScanned.Add(1)
	if len(fileMatches) == 0 {
		return
	}

	WithFrameLock(func() {
		if s.cancelled.Load() {
			return
		}
		s.filesMatched.Add(1)
		s.matchCount.Add(int64(hitLines))
		g.Append(&s.matches, fileMatches...)
	})
	RequestNextFrame()
}
