package main

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/go-git/go-billy/v6/osfs"
	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/filemode"
	formatcfg "github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/go-git/go-git/v6/plumbing/format/gitignore"
	"github.com/go-git/go-git/v6/plumbing/format/index"
	"github.com/go-git/go-git/v6/plumbing/object"
)

// indexEntrySnap is a copy of the index fields needed for worktree compare so
// we can release lockRepo before filesystem hashing / untracked walk.
type indexEntrySnap struct {
	Name       string
	Hash       plumbing.Hash
	Size       uint32
	ModifiedAt time.Time
	Mode       filemode.FileMode
}

// computeRepoStatusPure builds status the way git does for speed:
//
//  1. Staging: index cache-tree root hash vs HEAD tree (O(1) when clean).
//  2. Worktree: lstat each index entry; hash only on size/mtime mismatch or racy-git.
//  3. Untracked: directory walk with gitignore loaded lazily per directory.
//
// go-git work (index + HEAD) runs under a short lockRepo hold; worktree lstat/
// hash and untracked walk run unlocked in parallel afterward.
// Stop criterion: wall time ≤ native `git status --porcelain=v1`
// (see status_bench_test.go).
func computeRepoStatusPure(repoPath string) (*repoStatus, error) {
	repoAbs, err := filepath.Abs(repoPath)
	if err != nil {
		return nil, err
	}
	indexPaths, indexMtime, tracked, err := snapshotStatusGoGit(repoAbs)
	if err != nil {
		return nil, err
	}

	var (
		errTrack  error
		errUn     error
		untracked map[string]porcelainLine
		wg        sync.WaitGroup
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		errTrack = scanWorktree(repoAbs, indexPaths, indexMtime, tracked)
	}()
	go func() {
		defer wg.Done()
		untracked = make(map[string]porcelainLine, 8)
		base, ierr := loadBaseIgnorePatterns(repoAbs)
		if ierr != nil {
			base = nil
		}
		errUn = walkUntracked(repoAbs, indexPaths, base, untracked)
	}()
	wg.Wait()
	if errTrack != nil {
		return nil, errTrack
	}
	if errUn != nil {
		return nil, errUn
	}

	// Merge untracked into tracked map.
	for path, pl := range untracked {
		tracked[path] = pl
	}

	out := make([]porcelainLine, 0, len(tracked))
	for _, pl := range tracked {
		if pl.X == 0 {
			pl.X = ' '
		}
		if pl.Y == 0 {
			pl.Y = ' '
		}
		if pl.X == ' ' && pl.Y == ' ' {
			continue
		}
		out = append(out, pl)
	}
	return &repoStatus{lines: out}, nil
}

// snapshotStatusGoGit holds lockRepo only for index + HEAD reads and staging
// classification. Returns copied index entries and the staging-side map.
func snapshotStatusGoGit(repoPath string) (indexPaths map[string]indexEntrySnap, indexMtime time.Time, tracked map[string]porcelainLine, err error) {
	r, unlock, err := lockRepo(repoPath)
	if err != nil {
		return nil, time.Time{}, nil, err
	}
	defer unlock()

	idx, err := r.Storer.Index()
	if err != nil {
		return nil, time.Time{}, nil, err
	}

	indexPaths = make(map[string]indexEntrySnap, len(idx.Entries))
	for _, e := range idx.Entries {
		if e.Stage != 0 || e.SkipWorktree {
			continue
		}
		indexPaths[e.Name] = indexEntrySnap{
			Name:       e.Name,
			Hash:       e.Hash,
			Size:       e.Size,
			ModifiedAt: e.ModifiedAt,
			Mode:       e.Mode,
		}
	}
	indexMtime = idx.ModTime

	tracked = make(map[string]porcelainLine, 32)
	if err := fillStagingStatus(r, idx, indexPaths, tracked); err != nil {
		return nil, time.Time{}, nil, err
	}
	return indexPaths, indexMtime, tracked, nil
}

// fillStagingStatus records index↔HEAD differences into byPath.
// Fast path: cache-tree root hash == HEAD tree hash ⇒ staging clean.
// Caller holds lockRepo.
func fillStagingStatus(r *git.Repository, idx *index.Index, indexPaths map[string]indexEntrySnap, byPath map[string]porcelainLine) error {
	headTree, err := headTreeHash(r)
	if err != nil {
		return err
	}
	if headTree.IsZero() {
		for path := range indexPaths {
			byPath[path] = porcelainLine{X: 'A', Y: ' ', Path: path}
		}
		return nil
	}

	if idx.Cache != nil && len(idx.Cache.Entries) > 0 {
		root := idx.Cache.Entries[0]
		if root.Path == "" && root.Hash == headTree {
			return nil
		}
	}

	headBlobs, err := headBlobMap(r)
	if err != nil {
		return err
	}
	seenHead := make(map[string]bool, len(indexPaths))
	for path, e := range indexPaths {
		hh, inHead := headBlobs[path]
		if !inHead {
			byPath[path] = porcelainLine{X: 'A', Y: ' ', Path: path}
			continue
		}
		seenHead[path] = true
		if hh != e.Hash {
			byPath[path] = porcelainLine{X: 'M', Y: ' ', Path: path}
		}
	}
	for path := range headBlobs {
		if !seenHead[path] {
			if _, tracked := indexPaths[path]; !tracked {
				byPath[path] = porcelainLine{X: 'D', Y: ' ', Path: path}
			}
		}
	}
	return nil
}

func headTreeHash(r *git.Repository) (plumbing.Hash, error) {
	ref, err := r.Head()
	if err == plumbing.ErrReferenceNotFound {
		return plumbing.ZeroHash, nil
	}
	if err != nil {
		return plumbing.ZeroHash, err
	}
	c, err := r.CommitObject(ref.Hash())
	if err != nil {
		return plumbing.ZeroHash, err
	}
	return c.TreeHash, nil
}

func headBlobMap(r *git.Repository) (map[string]plumbing.Hash, error) {
	ref, err := r.Head()
	if err == plumbing.ErrReferenceNotFound {
		return map[string]plumbing.Hash{}, nil
	}
	if err != nil {
		return nil, err
	}
	c, err := r.CommitObject(ref.Hash())
	if err != nil {
		return nil, err
	}
	tree, err := c.Tree()
	if err != nil {
		return nil, err
	}
	m := make(map[string]plumbing.Hash, 1024)
	err = tree.Files().ForEach(func(f *object.File) error {
		m[f.Name] = f.Hash
		return nil
	})
	return m, err
}

// scanWorktree lstats index entries in parallel (git's threaded preload).
func scanWorktree(repoAbs string, indexPaths map[string]indexEntrySnap, indexMtime time.Time, tracked map[string]porcelainLine) error {
	n := len(indexPaths)
	if n == 0 {
		return nil
	}
	entries := make([]indexEntrySnap, 0, n)
	for _, e := range indexPaths {
		entries = append(entries, e)
	}
	type result struct {
		y   byte
		err error
	}
	out := make([]result, n)
	workers := runtime.GOMAXPROCS(0)
	if workers < 2 {
		workers = 2
	}
	var wg sync.WaitGroup
	chunk := (n + workers - 1) / workers
	for w := 0; w < workers; w++ {
		lo := w * chunk
		hi := lo + chunk
		if lo >= n {
			break
		}
		if hi > n {
			hi = n
		}
		wg.Add(1)
		go func(lo, hi int) {
			defer wg.Done()
			for i := lo; i < hi; i++ {
				y, err := worktreeCode(repoAbs, entries[i], indexMtime)
				out[i] = result{y, err}
			}
		}(lo, hi)
	}
	wg.Wait()
	for i, r := range out {
		if r.err != nil {
			return r.err
		}
		if r.y == ' ' {
			continue
		}
		path := entries[i].Name
		pl := tracked[path]
		pl.Path = path
		if pl.X == 0 {
			pl.X = ' '
		}
		pl.Y = r.y
		tracked[path] = pl
	}
	return nil
}

func worktreeCode(repoAbs string, e indexEntrySnap, indexMtime time.Time) (byte, error) {
	if e.Mode == filemode.Submodule {
		return ' ', nil
	}
	if e.Name == "" {
		return 'D', nil
	}
	// Index paths are repo-relative slash names; join once without Abs/escape
	// checks (those are for user-supplied paths in worktreeAbsPath).
	abs := repoAbs + string(filepath.Separator) + filepath.FromSlash(e.Name)
	fi, err := os.Lstat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return 'D', nil
		}
		return ' ', err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(abs)
		if err != nil {
			return ' ', err
		}
		h := hashBlobBytes([]byte(target))
		if h == e.Hash {
			return ' ', nil
		}
		return 'M', nil
	}
	if !fi.Mode().IsRegular() {
		return ' ', nil
	}

	sizeMatch := uint32(fi.Size()) == e.Size
	mtimeMatch := fi.ModTime().Unix() == e.ModifiedAt.Unix()
	racy := !indexMtime.IsZero() && !fi.ModTime().Before(indexMtime)

	if sizeMatch && mtimeMatch && !racy {
		return ' ', nil
	}

	h, err := hashFileBlob(abs, fi.Size())
	if err != nil {
		return ' ', err
	}
	if h == e.Hash {
		return ' ', nil
	}
	return 'M', nil
}

func hashBlobBytes(content []byte) plumbing.Hash {
	hw := plumbing.NewHasher(formatcfg.SHA1, plumbing.BlobObject, int64(len(content)))
	_, _ = hw.Write(content)
	return hw.Sum()
}

func hashFileBlob(path string, size int64) (plumbing.Hash, error) {
	f, err := os.Open(path)
	if err != nil {
		return plumbing.ZeroHash, err
	}
	defer f.Close()
	hw := plumbing.NewHasher(formatcfg.SHA1, plumbing.BlobObject, size)
	if _, err := io.Copy(hw, f); err != nil {
		return plumbing.ZeroHash, err
	}
	return hw.Sum(), nil
}

// sharedGlobalIgnore is process-wide (global/system excludesfile); repo
// .gitignore and info/exclude are loaded per call.
var (
	sharedIgnoreOnce sync.Once
	sharedIgnore     []gitignore.Pattern
)

func sharedGlobalIgnore() []gitignore.Pattern {
	sharedIgnoreOnce.Do(func() {
		rootFS := osfs.New("/")
		if gps, err := gitignore.LoadGlobalPatterns(rootFS); err == nil {
			sharedIgnore = append(sharedIgnore, gps...)
		}
		if sps, err := gitignore.LoadSystemPatterns(rootFS); err == nil {
			sharedIgnore = append(sharedIgnore, sps...)
		}
	})
	return sharedIgnore
}

// loadBaseIgnorePatterns loads excludes that apply from the repo root:
// global/system excludesfile, .git/info/exclude, and root .gitignore.
// Nested .gitignore files are applied during walkUntracked.
func loadBaseIgnorePatterns(repoPath string) ([]gitignore.Pattern, error) {
	ps := append([]gitignore.Pattern(nil), sharedGlobalIgnore()...)
	ps = append(ps, readIgnoreFile(filepath.Join(repoPath, ".git", "info", "exclude"), nil)...)
	ps = append(ps, readIgnoreFile(filepath.Join(repoPath, ".gitignore"), nil)...)
	return ps, nil
}

func readIgnoreFile(path string, domain []string) []gitignore.Pattern {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var ps []gitignore.Pattern
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		s := sc.Text()
		if s == "" || strings.HasPrefix(s, "#") {
			continue
		}
		ps = append(ps, gitignore.ParsePattern(s, domain))
	}
	return ps
}

// walkUntracked walks the worktree, applying gitignore lazily (push patterns
// when entering a directory that has its own .gitignore, pop on the way out).
// tracked is index paths (slash form).
func walkUntracked(repoAbs string, tracked map[string]indexEntrySnap, base []gitignore.Pattern, byPath map[string]porcelainLine) error {
	return walkUntrackedDir(repoAbs, "", nil, base, tracked, byPath)
}

func walkUntrackedDir(abs, relSlash string, parts []string, active []gitignore.Pattern, tracked map[string]indexEntrySnap, byPath map[string]porcelainLine) error {
	ents, err := os.ReadDir(abs)
	if err != nil {
		return nil
	}
	if relSlash != "" {
		for _, e := range ents {
			if e.Name() == ".gitignore" && !e.IsDir() {
				extra := readIgnoreFile(filepath.Join(abs, ".gitignore"), parts)
				if len(extra) > 0 {
					active = append(active, extra...)
				}
				break
			}
		}
	}
	matcher := gitignore.NewMatcher(active)
	for _, e := range ents {
		name := e.Name()
		if name == ".git" {
			continue
		}
		childParts := make([]string, len(parts)+1)
		copy(childParts, parts)
		childParts[len(parts)] = name
		childRel := name
		if relSlash != "" {
			childRel = relSlash + "/" + name
		}
		if e.IsDir() {
			if matcher.Match(childParts, true) {
				continue
			}
			childAbs := abs + string(filepath.Separator) + name
			if err := walkUntrackedDir(childAbs, childRel, childParts, active, tracked, byPath); err != nil {
				return err
			}
			continue
		}
		if matcher.Match(childParts, false) {
			continue
		}
		if _, ok := tracked[childRel]; ok {
			continue
		}
		byPath[childRel] = porcelainLine{X: '?', Y: '?', Path: childRel}
	}
	return nil
}
