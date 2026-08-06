package main

import (
	"bufio"
	"io"
	"io/fs"
	"os"
	"path/filepath"
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
	indexPaths, indexMtime, tracked, err := snapshotStatusGoGit(repoPath)
	if err != nil {
		return nil, err
	}

	var (
		errTrack error
		errUn    error
		untracked map[string]porcelainLine
		wg       sync.WaitGroup
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		for path, e := range indexPaths {
			y, werr := worktreeCode(repoPath, e, indexMtime)
			if werr != nil {
				errTrack = werr
				return
			}
			if y == ' ' {
				continue
			}
			pl := tracked[path]
			pl.Path = path
			if pl.X == 0 {
				pl.X = ' '
			}
			pl.Y = y
			tracked[path] = pl
		}
	}()
	go func() {
		defer wg.Done()
		untracked = make(map[string]porcelainLine, 8)
		base, ierr := loadBaseIgnorePatterns(repoPath)
		if ierr != nil {
			base = nil
		}
		keys := make(map[string]struct{}, len(indexPaths))
		for p := range indexPaths {
			keys[p] = struct{}{}
		}
		errUn = walkUntracked(repoPath, keys, base, untracked)
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

func worktreeCode(repoPath string, e indexEntrySnap, indexMtime time.Time) (byte, error) {
	if e.Mode == filemode.Submodule {
		return ' ', nil
	}
	abs, err := worktreeAbsPath(repoPath, e.Name)
	if err != nil {
		// Malformed index path: treat as missing from worktree.
		return 'D', nil
	}
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

// loadBaseIgnorePatterns loads excludes that apply from the repo root:
// global/system excludesfile, .git/info/exclude, and root .gitignore.
// Nested .gitignore files are applied during walkUntracked.
func loadBaseIgnorePatterns(repoPath string) ([]gitignore.Pattern, error) {
	var ps []gitignore.Pattern
	rootFS := osfs.New("/")
	if gps, err := gitignore.LoadGlobalPatterns(rootFS); err == nil {
		ps = append(ps, gps...)
	}
	if sps, err := gitignore.LoadSystemPatterns(rootFS); err == nil {
		ps = append(ps, sps...)
	}
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
// when entering a directory that has its own .gitignore).
// tracked is the set of index paths (slash form).
func walkUntracked(repoPath string, tracked map[string]struct{}, base []gitignore.Pattern, byPath map[string]porcelainLine) error {
	active := append([]gitignore.Pattern(nil), base...)
	matcher := gitignore.NewMatcher(active)

	return filepath.WalkDir(repoPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(repoPath, path)
		if err != nil {
			return nil
		}
		if rel == "." {
			return nil
		}
		if d.Name() == ".git" {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}

		relSlash := filepath.ToSlash(rel)
		parts := strings.Split(relSlash, "/")

		if d.IsDir() {
			if matcher != nil && matcher.Match(parts, true) {
				return fs.SkipDir
			}
			extra := readIgnoreFile(filepath.Join(path, ".gitignore"), parts)
			if len(extra) > 0 {
				active = append(active, extra...)
				matcher = gitignore.NewMatcher(active)
			}
			return nil
		}

		if matcher != nil && matcher.Match(parts, false) {
			return nil
		}
		if _, ok := tracked[relSlash]; ok {
			return nil
		}
		byPath[relSlash] = porcelainLine{X: '?', Y: '?', Path: relSlash}
		return nil
	})
}
