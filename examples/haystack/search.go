package main

import (
	"bytes"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	g "go.hasen.dev/generic"

	. "go.hasen.dev/shirei"
)

const (
	maxFileSize = 4 << 20  // skip files larger than this (naive first iteration)
	sniffLen    = 16 << 10 // bytes read to decide "binary" before reading the rest
	ctxBefore   = 2        // context lines shown before a match
	ctxAfter    = 2        // context lines shown after a match
	maxLineLen  = 400      // truncate very long lines for display / shaping cost
)

// binaryExts are file types we never search. Skipping them by extension — the
// bulk of a real tree is images and other assets — avoids even opening the
// file. Anything not listed here still gets a content sniff (below) as a
// backstop, so an unknown binary with no extension is still kept out.
var binaryExts = map[string]bool{
	// images
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true,
	".bmp": true, ".ico": true, ".tif": true, ".tiff": true, ".heic": true,
	".svgz": true, ".psd": true,
	// video / audio
	".mp4": true, ".mov": true, ".avi": true, ".mkv": true, ".webm": true,
	".mp3": true, ".wav": true, ".flac": true, ".ogg": true, ".m4a": true,
	// fonts
	".ttf": true, ".otf": true, ".woff": true, ".woff2": true, ".eot": true,
	// archives / compressed
	".zip": true, ".gz": true, ".xz": true, ".bz2": true, ".zst": true,
	".tar": true, ".7z": true, ".rar": true, ".jar": true,
	// compiled / binary
	".so": true, ".dylib": true, ".dll": true, ".a": true, ".o": true,
	".obj": true, ".exe": true, ".bin": true, ".wasm": true, ".class": true,
	".pyc": true, ".pdf": true, ".db": true, ".sqlite": true, ".ds_store": true,
}

// Params is one search request. It is comparable (only strings and bools), so
// two searches (tabs) can be told apart cheaply.
type Params struct {
	Root      string
	Query     string
	MatchCase bool
	WholeWord bool
	Regex     bool

	// file filters
	Include   string // space/comma-separated filename globs; empty = all files
	Exclude   string // filename globs to skip
	Gitignore bool   // honour .gitignore files under the root
}

// FileResult is one file that produced at least one match. Shared by all of
// that file's Match rows so the header (relative path) and buttons are cheap.
type FileResult struct {
	Path    string // absolute path, for opening / copying
	RelPath string // path relative to the search root, for display
}

// ContextLine is one source line shown in a result row.
type ContextLine struct {
	Num     int
	Text    string
	IsMatch bool // true for the line the match is on (gets a highlight)
}

// Match is one matching line, with a few lines of surrounding context. One
// row in the results list. The open buttons target Line, which is why every
// matching line — not every file — is its own row.
type Match struct {
	File    *FileResult
	Line    int
	Context []ContextLine
}

// Search holds everything about one run: its parameters, the compiled matcher,
// live counters, and the results as they stream in. Never copied (atomic.Bool
// inside); always referenced by pointer.
type Search struct {
	params    Params
	matcher   *Matcher
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

	// scrollY is this tab's saved list scroll offset, mirrored from the list
	// each frame it's active (via VirtualListAttrs.OutScrollOffset) and restored
	// when the tab is selected again. Frame-goroutine-only; not touched by the
	// scanning workers.
	scrollY f32
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
	if m, err := buildMatcher(p); err != nil {
		s.err = err
		s.done = time.Now()
	} else {
		s.matcher = m
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
	var wg sync.WaitGroup
	walkFiles(s.params, s.cancelled.Load, func(path string) {
		wg.Add(1)
		jobs.Submit(func() {
			defer wg.Done()
			if s.cancelled.Load() {
				return
			}
			scanFile(s, path)
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
	m, err := buildMatcher(p)
	if err != nil {
		s.err = err
		s.done = time.Now()
		return s
	}
	s.matcher = m
	walkFiles(p, func() bool { return false }, func(path string) {
		scanFile(s, path)
	})
	s.done = time.Now()
	return s
}

// walkFiles visits every regular text-sized file under p.Root, skipping noisy
// directories, oversized files, and anything the filters (globs, .gitignore)
// exclude. cancelled lets the async walk stop early.
func walkFiles(p Params, cancelled func() bool, process func(path string)) {
	root := p.Root
	include := parseGlobs(p.Include)
	exclude := parseGlobs(p.Exclude)
	var gi *gitignoreMatcher
	if p.Gitignore {
		gi = newGitignore(root)
	}

	filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if cancelled() {
			return fs.SkipAll
		}
		if err != nil {
			return nil // unreadable entry: skip it, keep going
		}
		if d.IsDir() {
			if skipDir(path, d, root) {
				return fs.SkipDir
			}
			if gi != nil && path != root && gi.ignored(relTo(root, path), true) {
				return fs.SkipDir
			}
			return nil
		}
		name := d.Name()
		if binaryExts[strings.ToLower(filepath.Ext(name))] {
			return nil // images/media/binaries: don't even open them
		}
		if !matchesInclude(include, name) || matchesAny(exclude, name) {
			return nil
		}
		if gi != nil && gi.ignored(relTo(root, path), false) {
			return nil
		}
		// d.Info() is only valid inside the callback, so read size here and
		// hand the worker just the path string.
		info, err := d.Info()
		if err != nil || info.Size() > maxFileSize {
			return nil
		}
		process(path)
		return nil
	})
}

func relTo(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return rel
}

// parseGlobs splits a filter field ("*.go, *.md") into individual globs.
func parseGlobs(s string) []string {
	fields := strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t'
	})
	return fields
}

// matchesInclude is true when name matches at least one glob, or when there are
// no include globs at all (no filter → everything is included).
func matchesInclude(globs []string, name string) bool {
	if len(globs) == 0 {
		return true
	}
	return matchesAny(globs, name)
}

func matchesAny(globs []string, name string) bool {
	for _, g := range globs {
		if ok, err := filepath.Match(g, name); err == nil && ok {
			return true
		}
	}
	return false
}

// skipDir prunes version-control and dependency directories (and hidden
// directories generally), but never the root itself — the user may point the
// search straight at a hidden folder.
func skipDir(path string, d fs.DirEntry, root string) bool {
	if path == root {
		return false
	}
	name := d.Name()
	switch name {
	case ".git", ".hg", ".svn", "node_modules":
		return true
	}
	return strings.HasPrefix(name, ".")
}

// scanFile reads one file, skips it if it looks binary, collects every
// matching line with context, and publishes the file's matches atomically.
func scanFile(s *Search, path string) {
	data, ok := readTextFile(path)
	if !ok {
		return
	}
	s.filesScanned.Add(1)

	// Whole-file prefilter — the big win. Almost no file contains the term,
	// yet splitting each into lines and testing each is the bulk of the work.
	// One SIMD bytes.Index scan of the buffer bails on non-matching files
	// without allocating a []string. Sound because our patterns are
	// line-oriented and never need '.' to cross a newline, so any per-line
	// match implies a whole-buffer match (the reverse can false-positive across
	// a newline — harmless, the line scan below just finds nothing).
	if !s.matcher.MatchBuffer(data) {
		return
	}

	lines := strings.Split(string(data), "\n")
	var fr *FileResult
	var fileMatches []*Match
	for i, raw := range lines {
		raw = strings.TrimRight(raw, "\r")
		if !s.matcher.MatchLine([]byte(raw)) {
			continue
		}
		if fr == nil {
			rel, relErr := filepath.Rel(s.params.Root, path)
			if relErr != nil {
				rel = path
			}
			fr = &FileResult{Path: path, RelPath: rel}
		}
		fileMatches = append(fileMatches, &Match{
			File:    fr,
			Line:    i + 1,
			Context: buildContext(lines, i),
		})
	}
	if len(fileMatches) == 0 {
		return // prefilter false positive (a match straddling a newline)
	}

	WithFrameLock(func() {
		if s.cancelled.Load() {
			return
		}
		s.filesMatched.Add(1)
		s.matchCount.Add(int64(len(fileMatches)))
		g.Append(&s.matches, fileMatches...)
	})
	RequestNextFrame()
}

// buildContext gathers the lines around the match at index i (0-based) into
// display-ready ContextLines (1-based numbers, tabs expanded, truncated).
func buildContext(lines []string, i int) []ContextLine {
	start := max(0, i-ctxBefore)
	end := min(len(lines)-1, i+ctxAfter)
	out := make([]ContextLine, 0, end-start+1)
	for j := start; j <= end; j++ {
		out = append(out, ContextLine{
			Num:     j + 1,
			Text:    cleanLine(lines[j]),
			IsMatch: j == i,
		})
	}
	return out
}

func cleanLine(s string) string {
	s = strings.TrimRight(s, "\r")
	s = strings.ReplaceAll(s, "\t", "    ")
	if len(s) > maxLineLen {
		r := []rune(s)
		if len(r) > maxLineLen {
			r = r[:maxLineLen]
		}
		s = string(r) + "…"
	}
	return s
}

// readTextFile reads a file for searching, returning ok=false if it is binary
// or unreadable. Crucially it sniffs only the first sniffLen bytes for a NUL
// before committing to the rest: a binary that slipped past the extension
// filter costs one small read, not a full-file read of (potentially) megabytes
// we would immediately discard.
func readTextFile(path string) ([]byte, bool) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer f.Close()

	head := make([]byte, sniffLen)
	n, err := io.ReadFull(f, head)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return nil, false
	}
	head = head[:n]
	if bytes.IndexByte(head, 0) >= 0 {
		return nil, false // binary: NUL in the sniff window
	}
	if n < sniffLen {
		return head, true // whole file fit in the sniff buffer
	}
	// Larger text file: read the remainder and stitch it on.
	rest, err := io.ReadAll(f)
	if err != nil {
		return nil, false
	}
	return append(head, rest...), true
}
