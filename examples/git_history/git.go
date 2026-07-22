package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// historyPageSize is how many commits to fetch per log page. First paint
// loads one page; scrolling/navigating near the end loads the next.
const historyPageSize = 50

// findRepo walks up from start looking for a .git directory or file (worktree).
// Returns the work-tree root (directory that contains .git), not the .git path.
func findRepo(start string) (string, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	for {
		gitPath := filepath.Join(dir, ".git")
		if st, err := os.Stat(gitPath); err == nil && (st.IsDir() || st.Mode().IsRegular()) {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("not a git repository (or any parent): %s", start)
		}
		dir = parent
	}
}

// Opened repositories (pure Go via go-git), one entry per work-tree path.
// Commit history / show use go-git. Dirty-slot status uses computeRepoStatusPure
// (index + lstat); see status_pure.go and BenchmarkStatus*.
//
// go-git's Repository and its object storage are not safe for concurrent use.
// Every reader/writer of a path's *git.Repository must hold that path's mutex
// for the whole duration of use (including Log iteration and Patch encode).
// Unsynchronized concurrent use produced intermittent "object not found"
// (loadHistory ran status + log in parallel on the same handle).
type repoGate struct {
	mu   sync.Mutex
	repo *git.Repository
}

var (
	repoMu sync.Mutex
	repos  = map[string]*repoGate{}

	statusMu    sync.Mutex
	statusCache *repoStatus
	statusAt    time.Time
	statusPath  string
)

const statusCacheTTL = 1 * time.Second

// lockRepo returns the cached go-git repo for path and an unlock function.
// Caller must unlock when finished with r (typically defer unlock()).
// Hold the lock for the entire use of r, including Log/CommitObject/Patch.
func lockRepo(path string) (r *git.Repository, unlock func(), err error) {
	repoMu.Lock()
	g, ok := repos[path]
	if !ok {
		opened, openErr := git.PlainOpen(path)
		if openErr != nil {
			repoMu.Unlock()
			return nil, nil, fmt.Errorf("open repo %s: %w", path, openErr)
		}
		g = &repoGate{repo: opened}
		repos[path] = g
	}
	repoMu.Unlock()
	g.mu.Lock()
	return g.repo, g.mu.Unlock, nil
}

func invalidateStatusCache() {
	statusMu.Lock()
	statusCache = nil
	statusPath = ""
	statusMu.Unlock()
}

// gitRun runs `git -C <repo> -c core.quotepath=false <args…>` and returns stdout.
// Dirty status and worktree/staging diffs go through the CLI so they stay near-
// instant; go-git's pure-Go Status walks the whole tree and was ~0.5–2s here.
func gitRun(repo string, args ...string) ([]byte, error) {
	full := make([]string, 0, 4+len(args))
	full = append(full, "-C", repo, "-c", "core.quotepath=false")
	full = append(full, args...)
	cmd := exec.Command("git", full...)
	out, err := cmd.Output()
	if err != nil {
		msg := err.Error()
		if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
			msg = strings.TrimSpace(string(ee.Stderr))
		}
		return nil, fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return out, nil
}

// porcelainLine is one `git status --porcelain=v1` entry.
// X = index (staging), Y = worktree; "??" is untracked.
type porcelainLine struct {
	X, Y byte
	Path string
	Orig string // rename/copy source when present
}

type repoStatus struct {
	lines []porcelainLine
}

func (s *repoStatus) worktreeDirty() bool {
	if s == nil {
		return false
	}
	for _, e := range s.lines {
		// Y != ' ' covers modified/deleted/untracked (?).
		if e.Y != ' ' {
			return true
		}
	}
	return false
}

func (s *repoStatus) stagingDirty() bool {
	if s == nil {
		return false
	}
	for _, e := range s.lines {
		// Untracked is ?? — X is '?' but not a staged change.
		if e.X != ' ' && e.X != '?' {
			return true
		}
	}
	return false
}

func (s *repoStatus) untrackedPaths() []string {
	if s == nil {
		return nil
	}
	var out []string
	for _, e := range s.lines {
		if e.X == '?' && e.Y == '?' && e.Path != "" {
			out = append(out, e.Path)
		}
	}
	sort.Strings(out)
	return out
}

// getRepoStatus returns status, optionally using a short TTL cache.
// Implementation is pure Go (computeRepoStatusPure). Target: wall time ≤
// native `git status --porcelain=v1` on the same repo (see status_bench_test.go).
func getRepoStatus(repoPath string, force bool) (*repoStatus, error) {
	statusMu.Lock()
	if !force && statusCache != nil && statusPath == repoPath && time.Since(statusAt) < statusCacheTTL {
		st := statusCache
		statusMu.Unlock()
		return st, nil
	}
	statusMu.Unlock()

	st, err := computeRepoStatusPure(repoPath)
	if err != nil {
		return nil, err
	}
	statusMu.Lock()
	statusCache = st
	statusAt = time.Now()
	statusPath = repoPath
	statusMu.Unlock()
	return st, nil
}

// getRepoStatusNative is the stop-criterion oracle: real git porcelain.
// Used only by benchmarks/tests — not the app path.
func getRepoStatusNative(repoPath string) (*repoStatus, error) {
	out, err := gitRun(repoPath, "status", "--porcelain=v1")
	if err != nil {
		return nil, err
	}
	return &repoStatus{lines: parsePorcelain(string(out))}, nil
}

// parsePorcelain parses `git status --porcelain=v1` (non-z) output.
// Lines look like " M path", "M  path", "R  old -> new", "?? path".
func parsePorcelain(out string) []porcelainLine {
	var lines []porcelainLine
	for _, raw := range strings.Split(out, "\n") {
		line := strings.TrimRight(raw, "\r")
		if line == "" {
			continue
		}
		if len(line) < 3 {
			continue
		}
		x, y := line[0], line[1]
		// Porcelain v1: two status cols, then a space, then the path.
		rest := line[2:]
		if len(rest) > 0 && rest[0] == ' ' {
			rest = rest[1:]
		}
		rest = strings.TrimSpace(rest)
		if rest == "" {
			continue
		}
		pl := porcelainLine{X: x, Y: y}
		// Rename/copy: "old -> new" (optional score already absorbed into XY).
		if (x == 'R' || x == 'C' || y == 'R' || y == 'C') && strings.Contains(rest, " -> ") {
			old, neu, ok := strings.Cut(rest, " -> ")
			if ok {
				pl.Orig = strings.TrimSpace(old)
				pl.Path = strings.TrimSpace(neu)
			} else {
				pl.Path = rest
			}
		} else {
			pl.Path = rest
		}
		// Strip optional C-style quotes if present.
		pl.Path = unquotePorcelainPath(pl.Path)
		pl.Orig = unquotePorcelainPath(pl.Orig)
		lines = append(lines, pl)
	}
	return lines
}

func unquotePorcelainPath(p string) string {
	if len(p) >= 2 && p[0] == '"' && p[len(p)-1] == '"' {
		// Best-effort: go-git/core.quotepath=false usually avoids this.
		if u, err := strconv.Unquote(p); err == nil {
			return u
		}
	}
	return p
}

// loadDirtySlots builds Working tree / Staging sidebar rows from porcelain.
// Failures return an error so callers can skip slots without failing history.
func loadDirtySlots(repoPath string) ([]HistoryEntry, error) {
	st, err := getRepoStatus(repoPath, false)
	if err != nil {
		return nil, err
	}
	var out []HistoryEntry
	if st.worktreeDirty() {
		out = append(out, HistoryEntry{Kind: KindWorkingTree, ID: idWorkingTree})
	}
	if st.stagingDirty() {
		out = append(out, HistoryEntry{Kind: KindStaging, ID: idStaging})
	}
	return out, nil
}

func shortHash(full string) string {
	if len(full) >= 7 {
		return full[:7]
	}
	return full
}

func subjectOf(msg string) (subject, body string) {
	msg = strings.TrimRight(msg, "\n")
	subject, body, ok := strings.Cut(msg, "\n")
	if !ok {
		return msg, ""
	}
	return subject, strings.TrimLeft(body, "\n")
}

// loadHistory is the first sidebar fill: dirty slots (pure-Go status) + first
// commit page (go-git). Status and the log page each take the per-repo lock;
// they may still run in parallel with each other only for non-go-git work —
// go-git access is serialized by lockRepo.
// after / hasMore drive infinite scroll via loadCommitPage.
func loadHistory(repoPath string) (entries []HistoryEntry, after string, hasMore bool, err error) {
	type dirtyResult struct {
		slots []HistoryEntry
		err   error
	}
	ch := make(chan dirtyResult, 1)
	go func() {
		slots, err := loadDirtySlots(repoPath)
		ch <- dirtyResult{slots, err}
	}()

	commits, after, hasMore, err := loadCommitPage(repoPath, "", historyPageSize)
	d := <-ch
	if d.err == nil {
		entries = append(entries, d.slots...)
	}
	// Dirty-slot failure is non-fatal: still show commits.

	if err != nil {
		// Empty repo (no commits) — dirty slots alone are fine.
		if err == plumbing.ErrReferenceNotFound {
			return entries, "", false, nil
		}
		return nil, "", false, err
	}
	entries = append(entries, commits...)
	return entries, after, hasMore, nil
}

// loadCommitPage walks the log starting after the given hash (exclusive).
// If after is empty, starts at HEAD. hasMore is true when a full page was
// returned (there may still be older commits).
func loadCommitPage(repoPath, after string, limit int) (entries []HistoryEntry, nextAfter string, hasMore bool, err error) {
	r, unlock, err := lockRepo(repoPath)
	if err != nil {
		return nil, "", false, err
	}
	defer unlock()

	if limit < 1 {
		limit = historyPageSize
	}
	var start plumbing.Hash
	skipFirst := false
	if after == "" {
		ref, err := r.Head()
		if err != nil {
			return nil, "", false, err
		}
		start = ref.Hash()
	} else {
		start = plumbing.NewHash(after)
		skipFirst = true
	}
	iter, err := r.Log(&git.LogOptions{From: start})
	if err != nil {
		return nil, "", false, err
	}
	defer iter.Close()

	skipped := false
	err = iter.ForEach(func(c *object.Commit) error {
		if skipFirst && !skipped {
			skipped = true
			if c.Hash == start {
				return nil
			}
		}
		subj, _ := subjectOf(c.Message)
		h := c.Hash.String()
		entries = append(entries, HistoryEntry{
			Kind:    KindCommit,
			ID:      h,
			Short:   shortHash(h),
			Subject: subj,
			Author:  c.Author.Name,
			When:    c.Author.When,
		})
		if len(entries) >= limit {
			nextAfter = h
			hasMore = true
			return fmt.Errorf("stop")
		}
		return nil
	})
	if err != nil && err.Error() != "stop" {
		return nil, "", false, err
	}
	if !hasMore {
		nextAfter = ""
	}
	return entries, nextAfter, hasMore, nil
}

// loadMoreHistory appends the next commit page after the given cursor.
func loadMoreHistory(repoPath, after string) (more []HistoryEntry, nextAfter string, hasMore bool, err error) {
	if after == "" {
		return nil, "", false, nil
	}
	return loadCommitPage(repoPath, after, historyPageSize)
}

func workingTreeDirty(repoPath string) (bool, error) {
	st, err := getRepoStatus(repoPath, false)
	if err != nil {
		return false, err
	}
	return st.worktreeDirty(), nil
}

func defaultSelection(entries []HistoryEntry) string {
	if len(entries) == 0 {
		return ""
	}
	return entries[0].ID
}

func selectionStillValid(entries []HistoryEntry, id string) bool {
	for _, e := range entries {
		if e.ID == id {
			return true
		}
	}
	return false
}

// loadDiffDoc loads message/stats/patch for a history entry.
// Commits use go-git; working tree / staging use git CLI diffs.
func loadDiffDoc(repoPath string, entry HistoryEntry) (*DiffDoc, error) {
	switch entry.Kind {
	case KindWorkingTree:
		return loadWorkingTreeDoc(repoPath)
	case KindStaging:
		return loadStagingDoc(repoPath)
	case KindCommit:
		return loadCommitDoc(repoPath, entry.ID)
	default:
		return nil, fmt.Errorf("unknown entry kind %d", entry.Kind)
	}
}

func loadCommitDoc(repoPath, hash string) (*DiffDoc, error) {
	r, unlock, err := lockRepo(repoPath)
	if err != nil {
		return nil, err
	}
	defer unlock()
	c, err := r.CommitObject(plumbing.NewHash(hash))
	if err != nil {
		return nil, err
	}
	doc := &DiffDoc{}
	doc.Author = c.Author.Name
	doc.Email = c.Author.Email
	doc.Date = c.Author.When.Format(time.RFC3339)
	doc.Subject, doc.Body = subjectOf(c.Message)
	for _, ph := range c.ParentHashes {
		doc.Parents = append(doc.Parents, ph.String())
	}
	// Patch encode reads objects through c's storer — stay under the lock.
	patch, err := commitPatch(c)
	if err != nil {
		return nil, err
	}
	fillDocFromPatch(doc, patch)
	return doc, nil
}

// loadCommitStats computes +/−/files only (no unified-diff encode) for the
// sidebar. Still needs a first-parent patch, but skips building DiffRows.
func loadCommitStats(repoPath, hash string) (CommitStats, error) {
	r, unlock, err := lockRepo(repoPath)
	if err != nil {
		return CommitStats{}, err
	}
	defer unlock()
	c, err := r.CommitObject(plumbing.NewHash(hash))
	if err != nil {
		return CommitStats{}, err
	}
	patch, err := commitPatch(c)
	if err != nil {
		return CommitStats{}, err
	}
	return statsFromObjectPatch(patch), nil
}

// commitPatch is first-parent (or empty→tree for root).
func commitPatch(c *object.Commit) (*object.Patch, error) {
	if c.NumParents() > 0 {
		parent, err := c.Parent(0)
		if err != nil {
			return nil, err
		}
		return parent.Patch(c)
	}
	toTree, err := c.Tree()
	if err != nil {
		return nil, err
	}
	return (&object.Tree{}).Patch(toTree)
}

func statsFromObjectPatch(patch *object.Patch) CommitStats {
	st := CommitStats{Ready: true}
	for _, fs := range patch.Stats() {
		st.Files++
		st.Added += fs.Addition
		st.Deleted += fs.Deletion
	}
	return st
}

func fillDocFromPatch(doc *DiffDoc, patch *object.Patch) {
	for _, fs := range patch.Stats() {
		doc.Stats = append(doc.Stats, FileStat{
			Path:    fs.Name,
			Added:   fs.Addition,
			Deleted: fs.Deletion,
		})
	}
	doc.recomputeTotals()
	var buf bytes.Buffer
	if err := patch.Encode(&buf); err != nil {
		return
	}
	doc.Rows = parsePatch(buf.String())
}

// loadWorkingTreeDoc: unstaged tracked changes via `git diff`, plus untracked
// files listed by porcelain (shown as full-file adds).
func loadWorkingTreeDoc(repoPath string) (*DiffDoc, error) {
	// Fresh status so untracked list matches the diff we show.
	st, err := getRepoStatus(repoPath, true)
	if err != nil {
		return nil, err
	}
	numOut, err := gitRun(repoPath, "diff", "--numstat")
	if err != nil {
		return nil, err
	}
	patchOut, err := gitRun(repoPath, "diff")
	if err != nil {
		return nil, err
	}
	doc := &DiffDoc{
		Stats: parseNumstat(string(numOut)),
		Rows:  parsePatch(string(patchOut)),
	}
	for _, path := range st.untrackedPaths() {
		stat, rows, err := untrackedFileRows(repoPath, path)
		if err != nil {
			doc.Stats = append(doc.Stats, FileStat{Path: path + " (untracked)"})
			doc.Rows = append(doc.Rows,
				DiffRow{Kind: RowFileHeader, Text: path + " (untracked)"},
				DiffRow{Kind: RowMeta, Text: "  (could not read: " + err.Error() + ")"},
			)
			continue
		}
		doc.Stats = append(doc.Stats, stat)
		doc.Rows = append(doc.Rows, rows...)
	}
	doc.recomputeTotals()
	return doc, nil
}

// loadStagingDoc: staged changes via `git diff --cached`.
func loadStagingDoc(repoPath string) (*DiffDoc, error) {
	numOut, err := gitRun(repoPath, "diff", "--cached", "--numstat")
	if err != nil {
		return nil, err
	}
	patchOut, err := gitRun(repoPath, "diff", "--cached")
	if err != nil {
		return nil, err
	}
	doc := &DiffDoc{
		Stats: parseNumstat(string(numOut)),
		Rows:  parsePatch(string(patchOut)),
	}
	doc.recomputeTotals()
	return doc, nil
}

// untrackedMaxBytes caps how much of an untracked file we read into the stream.
const untrackedMaxBytes = 256 << 10 // 256 KiB

func untrackedFileRows(repo, rel string) (FileStat, []DiffRow, error) {
	label := rel + " (untracked)"
	abs := filepath.Join(repo, rel)
	info, err := os.Stat(abs)
	if err != nil {
		return FileStat{}, nil, err
	}
	if info.IsDir() {
		return FileStat{Path: label}, []DiffRow{
			{Kind: RowFileHeader, Text: label},
			{Kind: RowMeta, Text: "  (directory)"},
		}, nil
	}

	f, err := os.Open(abs)
	if err != nil {
		return FileStat{}, nil, err
	}
	defer f.Close()

	size := info.Size()
	head := make([]byte, 8192)
	n, _ := f.Read(head)
	head = head[:n]
	if isBinaryContent(head) {
		return FileStat{Path: label, Added: -1, Deleted: -1, Binary: true}, []DiffRow{
			{Kind: RowFileHeader, Text: label},
			{Kind: RowMeta, Text: "Binary file (untracked)"},
		}, nil
	}

	if _, err := f.Seek(0, 0); err != nil {
		return FileStat{}, nil, err
	}
	buf, err := io.ReadAll(io.LimitReader(f, int64(untrackedMaxBytes)+1))
	if err != nil {
		return FileStat{}, nil, err
	}
	truncated := len(buf) > untrackedMaxBytes
	if truncated {
		buf = buf[:untrackedMaxBytes]
	}
	content := string(buf)
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")
	lines := strings.Split(content, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	st := FileStat{Path: label, Added: len(lines), Deleted: 0}
	rows := make([]DiffRow, 0, len(lines)+3)
	rows = append(rows, DiffRow{Kind: RowFileHeader, Text: label})
	rows = append(rows, DiffRow{
		Kind: RowHunkHeader,
		Text: fmt.Sprintf("@@ -0,0 +1,%d @@ untracked", len(lines)),
	})
	for _, line := range lines {
		rows = append(rows, DiffRow{Kind: RowAdd, Text: "+" + line})
	}
	if truncated || size > int64(untrackedMaxBytes) {
		rows = append(rows, DiffRow{
			Kind: RowMeta,
			Text: fmt.Sprintf("  … truncated (showing first %d bytes of %d)", untrackedMaxBytes, size),
		})
	}
	return st, rows, nil
}

func isBinaryContent(b []byte) bool {
	return bytes.IndexByte(b, 0) >= 0
}

// ---- unified-diff text parsers (still used for go-git Patch.Encode output) ----

func parseNumstat(out string) []FileStat {
	var stats []FileStat
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		fields := strings.SplitN(line, "\t", 3)
		if len(fields) < 3 {
			continue
		}
		st := FileStat{Path: fields[2]}
		if fields[0] == "-" || fields[1] == "-" {
			st.Binary = true
			st.Added = -1
			st.Deleted = -1
		} else {
			st.Added, _ = strconv.Atoi(fields[0])
			st.Deleted, _ = strconv.Atoi(fields[1])
		}
		stats = append(stats, st)
	}
	return stats
}

// parsePatch turns unified-diff text into a flat DiffRow stream.
func parsePatch(out string) []DiffRow {
	var rows []DiffRow
	var currentFile string
	var pendingOld string
	var headerEmitted bool

	emitFileHeader := func(path string) {
		if path == "" {
			path = currentFile
		}
		if path == "" || headerEmitted {
			return
		}
		rows = append(rows, DiffRow{Kind: RowFileHeader, Text: path})
		currentFile = path
		headerEmitted = true
	}

	for _, raw := range strings.Split(out, "\n") {
		line := strings.TrimRight(raw, "\r")

		switch {
		case strings.HasPrefix(line, "diff --git "):
			rest := strings.TrimPrefix(line, "diff --git ")
			a, b, ok := strings.Cut(rest, " ")
			path := ""
			if ok {
				path = strings.TrimPrefix(b, "b/")
				if path == b {
					path = strings.TrimPrefix(a, "a/")
				}
			} else {
				path = strings.TrimPrefix(rest, "a/")
			}
			currentFile = path
			pendingOld = ""
			headerEmitted = false

		case strings.HasPrefix(line, "rename from "):
			pendingOld = strings.TrimPrefix(line, "rename from ")
		case strings.HasPrefix(line, "rename to "):
			pendingNew := strings.TrimPrefix(line, "rename to ")
			label := pendingNew
			if pendingOld != "" {
				label = pendingOld + " → " + pendingNew
			}
			emitFileHeader(label)

		case strings.HasPrefix(line, "--- "):
		case strings.HasPrefix(line, "+++ "):
			p := strings.TrimPrefix(line, "+++ ")
			if strings.HasPrefix(p, "b/") {
				p = p[2:]
			}
			if p == "/dev/null" {
				emitFileHeader(currentFile)
			} else {
				emitFileHeader(p)
			}

		case strings.HasPrefix(line, "Binary files ") || strings.HasPrefix(line, "BIN "):
			emitFileHeader(currentFile)
			rows = append(rows, DiffRow{Kind: RowMeta, Text: line})

		case strings.HasPrefix(line, "@@"):
			emitFileHeader(currentFile)
			rows = append(rows, DiffRow{Kind: RowHunkHeader, Text: line})

		case strings.HasPrefix(line, "+"):
			rows = append(rows, DiffRow{Kind: RowAdd, Text: line})
		case strings.HasPrefix(line, "-"):
			rows = append(rows, DiffRow{Kind: RowDel, Text: line})
		case strings.HasPrefix(line, "\\"):
			rows = append(rows, DiffRow{Kind: RowMeta, Text: line})
		case strings.HasPrefix(line, " "):
			rows = append(rows, DiffRow{Kind: RowContext, Text: line})
		case line == "":
			if len(rows) > 0 {
				last := rows[len(rows)-1].Kind
				if last == RowContext || last == RowAdd || last == RowDel || last == RowHunkHeader {
					rows = append(rows, DiffRow{Kind: RowContext, Text: " "})
				}
			}
		default:
		}
	}
	return rows
}

// splitNumstatAndPatch kept for unit tests of the old CLI splitter.
func splitNumstatAndPatch(out string) (numstat, patch string) {
	lines := strings.Split(out, "\n")
	i := 0
	for ; i < len(lines); i++ {
		line := strings.TrimRight(lines[i], "\r")
		if line == "" {
			if i+1 < len(lines) && strings.HasPrefix(lines[i+1], "diff ") {
				i++
				break
			}
			continue
		}
		if strings.HasPrefix(line, "diff ") {
			break
		}
		if strings.Count(line, "\t") < 2 {
			break
		}
	}
	numstat = strings.Join(lines[:i], "\n")
	if i < len(lines) {
		patch = strings.Join(lines[i:], "\n")
	}
	return numstat, patch
}

func formatStatsLine(doc *DiffDoc) string {
	if doc == nil {
		return ""
	}
	n := len(doc.Stats)
	files := "files"
	if n == 1 {
		files = "file"
	}
	return fmt.Sprintf("+%d −%d · %d %s", doc.TotalAdded, doc.TotalDeleted, n, files)
}
