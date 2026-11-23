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

	// matches is appended by worker goroutines via Post, so the render
	// (which holds the frame lock) sees a consistent snapshot. The counters
	// are atomic instead so workers never queue UI callbacks just to bump
	// scanned file counts.
	matches      []*Match
	filesScanned atomic.Int64
	filesMatched atomic.Int64
	matchCount   atomic.Int64

	// firstVis is this tab's first painted row index, mirrored each frame
	// (OutFirstVisible) and restored via ScrollToIndex when the tab is shown
	// again. Frame-goroutine-only; not touched by the scanning workers.
	firstVis       int
	rows           []resultRow
	groupedMatches int
	collapsed      map[*FileResult]bool
	selectedFile   *FileResult
	selectedLine   int
}

// One worker pool serves every tab. Jobs from closed tabs drain by checking
// their cancellation flag before reading or publishing a file.
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
	activateTab(s) // a new tab starts at the top
	RequestNextFrame()

	if s.running {
		go runSearch(s)
	}
}

// runSearch walks the tree and fans every candidate file out to the worker
// pool, then waits for all of them and marks the search done. Concurrency is
// how we "finish as quickly as possible"; incremental publishing (each file
// queued via Post) is how results appear as they are found.
func runSearch(s *Search) {
	m, err := textsearch.NewMatcher(s.params)
	if err != nil {
		Post(func() {
			s.err = err
			s.running = false
			s.done = time.Now()
		})
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

	Post(func() {
		if !s.cancelled.Load() {
			s.running = false
			s.done = time.Now()
		}
	})
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

	Post(func() {
		if s.cancelled.Load() {
			return
		}
		s.filesMatched.Add(1)
		s.matchCount.Add(int64(hitLines))
		g.Append(&s.matches, fileMatches...)
	})
}
