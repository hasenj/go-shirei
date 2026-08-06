package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/index"
)

// Pure-Go working-tree and staging diffs.
//
// Like git: only open content for paths status already knows are dirty.
// Snapshot under lockRepo (index/HEAD blobs); worktree file reads outside
// the gate where possible. Line-diff unlocked via bodyFromSnapshot.

// streamStagingDiffGo publishes first-parent-style rows for index vs HEAD
// (equivalent to `git diff --cached`).
func streamStagingDiffGo(ctx context.Context, repoPath string, publish patchPublish) error {
	if publish == nil {
		return fmt.Errorf("nil publish")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	snaps, err := snapshotStagingChanges(ctx, repoPath)
	if err != nil {
		return err
	}
	if err := publishFileSnaps(ctx, snaps, publish); err != nil {
		return err
	}
	if !publish(nil, true) {
		return errStreamAbandoned
	}
	return nil
}

// streamWorkingTreeDiffGo publishes unstaged tracked changes (index vs disk)
// plus untracked files as full-file adds (equivalent to `git diff` + untracked).
func streamWorkingTreeDiffGo(ctx context.Context, repoPath string, publish patchPublish) error {
	if publish == nil {
		return fmt.Errorf("nil publish")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	snaps, untracked, err := snapshotWorktreeChanges(ctx, repoPath)
	if err != nil {
		return err
	}
	if err := publishFileSnaps(ctx, snaps, publish); err != nil {
		return err
	}
	// Untracked: filesystem only (already pure-Go helper).
	for _, path := range untracked {
		if err := ctx.Err(); err != nil {
			return err
		}
		_, rows, err := untrackedFileRows(repoPath, path)
		if err != nil {
			label := path + " (untracked)"
			if !publish([]DiffRow{
				{Kind: RowFileHeader, Text: label},
				{Kind: RowMeta, Text: "  (could not read: " + err.Error() + ")"},
			}, false) {
				return errStreamAbandoned
			}
			continue
		}
		if len(rows) == 0 {
			continue
		}
		// Header first for feel-instant, then body.
		if !publish(rows[:1], false) {
			return errStreamAbandoned
		}
		if len(rows) > 1 && !publishBodyBatched(publish, rows[1:]) {
			return errStreamAbandoned
		}
	}
	if !publish(nil, true) {
		return errStreamAbandoned
	}
	return nil
}

func publishFileSnaps(ctx context.Context, snaps []fileSnap, publish patchPublish) error {
	for _, s := range snaps {
		if err := ctx.Err(); err != nil {
			return err
		}
		header := DiffRow{Kind: RowFileHeader, Text: s.label}
		if !publish([]DiffRow{header}, false) {
			return errStreamAbandoned
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		body := bodyFromSnapshot(s)
		if !publishBodyBatched(publish, body) {
			return errStreamAbandoned
		}
	}
	return nil
}

// snapshotStagingChanges: only paths with staged (X) changes.
func snapshotStagingChanges(ctx context.Context, repoPath string) ([]fileSnap, error) {
	st, err := getRepoStatus(repoPath, true)
	if err != nil {
		return nil, err
	}
	var staged []porcelainLine
	for _, pl := range st.lines {
		if pl.X != ' ' && pl.X != '?' && pl.Path != "" {
			staged = append(staged, pl)
		}
	}
	sort.Slice(staged, func(i, j int) bool { return staged[i].Path < staged[j].Path })

	if len(staged) == 0 {
		return nil, nil
	}

	r, unlock, err := lockRepo(repoPath)
	if err != nil {
		return nil, err
	}
	defer unlock()

	idx, err := r.Storer.Index()
	if err != nil {
		return nil, err
	}
	indexPaths := indexPathMap(idx)
	headBlobs, err := headBlobMap(r)
	if err != nil {
		return nil, err
	}

	snaps := make([]fileSnap, 0, len(staged))
	for _, pl := range staged {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		path := pl.Path
		label := path
		if pl.Orig != "" && pl.Orig != path {
			label = pl.Orig + " → " + path
		}

		var fromHash, toHash plumbing.Hash
		var fromOK, toOK bool
		switch pl.X {
		case 'A':
			// Added to index: empty → index
			if e := indexPaths[path]; e != nil {
				toHash, toOK = e.Hash, true
			}
		case 'D':
			// Deleted from index: HEAD → empty
			if h, ok := headBlobs[path]; ok {
				fromHash, fromOK = h, true
			}
		case 'M', 'R', 'C', 'T':
			if h, ok := headBlobs[path]; ok {
				fromHash, fromOK = h, true
			}
			if pl.Orig != "" {
				if h, ok := headBlobs[pl.Orig]; ok {
					fromHash, fromOK = h, true
				}
			}
			if e := indexPaths[path]; e != nil {
				toHash, toOK = e.Hash, true
			}
		default:
			// U etc. — skip content diff
			snaps = append(snaps, fileSnap{label: label, skip: true, skipReason: fmt.Sprintf("staged %c", pl.X)})
			continue
		}

		snap, err := snapFromBlobHashes(r, label, fromHash, fromOK, toHash, toOK)
		if err != nil {
			return nil, err
		}
		snaps = append(snaps, snap)
	}
	return snaps, nil
}

// snapshotWorktreeChanges: unstaged tracked (Y) paths; untracked returned separately.
func snapshotWorktreeChanges(ctx context.Context, repoPath string) (snaps []fileSnap, untracked []string, err error) {
	st, err := getRepoStatus(repoPath, true)
	if err != nil {
		return nil, nil, err
	}
	var dirty []porcelainLine
	for _, pl := range st.lines {
		if pl.X == '?' && pl.Y == '?' {
			if pl.Path != "" {
				untracked = append(untracked, pl.Path)
			}
			continue
		}
		if pl.Y != ' ' && pl.Y != '?' && pl.Path != "" {
			dirty = append(dirty, pl)
		}
	}
	sort.Strings(untracked)
	sort.Slice(dirty, func(i, j int) bool { return dirty[i].Path < dirty[j].Path })

	if len(dirty) == 0 {
		return nil, untracked, nil
	}

	// Index blobs under lock; worktree bytes outside.
	type idxSide struct {
		path  string
		label string
		code  byte
		hash  plumbing.Hash
		has   bool
	}
	sides := make([]idxSide, 0, len(dirty))

	r, unlock, err := lockRepo(repoPath)
	if err != nil {
		return nil, nil, err
	}
	idx, err := r.Storer.Index()
	if err != nil {
		unlock()
		return nil, nil, err
	}
	indexPaths := indexPathMap(idx)
	for _, pl := range dirty {
		if err := ctx.Err(); err != nil {
			unlock()
			return nil, nil, err
		}
		s := idxSide{path: pl.Path, label: pl.Path, code: pl.Y}
		if e := indexPaths[pl.Path]; e != nil {
			s.hash, s.has = e.Hash, true
		}
		sides = append(sides, s)
	}
	// Read all index blobs while locked.
	fromBytes := make([][]byte, len(sides))
	fromBin := make([]bool, len(sides))
	fromTrunc := make([]bool, len(sides))
	for i, s := range sides {
		if !s.has {
			continue
		}
		data, bin, trunc, err := readBlobHashSnapshot(r, s.hash)
		if err != nil {
			unlock()
			return nil, nil, err
		}
		fromBytes[i], fromBin[i], fromTrunc[i] = data, bin, trunc
	}
	unlock()

	snaps = make([]fileSnap, 0, len(sides))
	for i, s := range sides {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		var to []byte
		var toBin, toTrunc bool
		switch s.code {
		case 'D':
			// deleted in worktree
			to = nil
		case 'M', 'T', 'U':
			data, bin, trunc, missing, err := readWorktreeSnapshot(repoPath, s.path)
			if err != nil {
				snaps = append(snaps, fileSnap{
					label: s.label, skip: true,
					skipReason: "could not read worktree: " + err.Error(),
				})
				continue
			}
			if missing {
				to = nil
			} else {
				to, toBin, toTrunc = data, bin, trunc
			}
		default:
			snaps = append(snaps, fileSnap{label: s.label, skip: true, skipReason: fmt.Sprintf("worktree %c", s.code)})
			continue
		}
		snaps = append(snaps, fileSnap{
			label:     s.label,
			from:      fromBytes[i],
			to:        to,
			fromBin:   fromBin[i],
			toBin:     toBin,
			fromTrunc: fromTrunc[i],
			toTrunc:   toTrunc,
		})
	}
	return snaps, untracked, nil
}

func indexPathMap(idx *index.Index) map[string]*index.Entry {
	m := make(map[string]*index.Entry, len(idx.Entries))
	for _, e := range idx.Entries {
		if e.Stage != 0 || e.SkipWorktree {
			continue
		}
		m[e.Name] = e
	}
	return m
}

func snapFromBlobHashes(r *git.Repository, label string, fromHash plumbing.Hash, fromOK bool, toHash plumbing.Hash, toOK bool) (fileSnap, error) {
	var s fileSnap
	s.label = label
	var err error
	if fromOK && !fromHash.IsZero() {
		s.from, s.fromBin, s.fromTrunc, err = readBlobHashSnapshot(r, fromHash)
		if err != nil {
			return s, err
		}
	}
	if toOK && !toHash.IsZero() {
		s.to, s.toBin, s.toTrunc, err = readBlobHashSnapshot(r, toHash)
		if err != nil {
			return s, err
		}
	}
	if fromOK && toOK && bytesEqual(s.from, s.to) && !s.fromBin && !s.toBin {
		s.modeOnly = true
	}
	return s, nil
}

func readBlobHashSnapshot(r *git.Repository, h plumbing.Hash) (data []byte, binary, truncated bool, err error) {
	if h.IsZero() {
		return nil, false, false, nil
	}
	b, err := r.BlobObject(h)
	if err != nil {
		return nil, false, false, err
	}
	rd, err := b.Reader()
	if err != nil {
		return nil, false, false, err
	}
	defer rd.Close()
	limited := io.LimitReader(rd, int64(commitPatchMaxBytes)+1)
	raw, err := io.ReadAll(limited)
	if err != nil {
		return nil, false, false, err
	}
	if len(raw) > commitPatchMaxBytes {
		raw = raw[:commitPatchMaxBytes]
		truncated = true
	}
	binary = isBinaryContent(raw)
	out := make([]byte, len(raw))
	copy(out, raw)
	return out, binary, truncated, nil
}

func readWorktreeSnapshot(repoPath, rel string) (data []byte, binary, truncated, missing bool, err error) {
	abs, err := worktreeAbsPath(repoPath, rel)
	if err != nil {
		return nil, false, false, false, err
	}
	fi, err := os.Lstat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, false, true, nil
		}
		return nil, false, false, false, err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(abs)
		if err != nil {
			return nil, false, false, false, err
		}
		b := []byte(target)
		out := make([]byte, len(b))
		copy(out, b)
		return out, false, false, false, nil
	}
	if !fi.Mode().IsRegular() {
		return nil, false, false, false, fmt.Errorf("not a regular file")
	}
	f, err := os.Open(abs)
	if err != nil {
		return nil, false, false, false, err
	}
	defer f.Close()
	limited := io.LimitReader(f, int64(commitPatchMaxBytes)+1)
	raw, err := io.ReadAll(limited)
	if err != nil {
		return nil, false, false, false, err
	}
	if len(raw) > commitPatchMaxBytes {
		raw = raw[:commitPatchMaxBytes]
		truncated = true
	}
	binary = isBinaryContent(raw)
	out := make([]byte, len(raw))
	copy(out, raw)
	return out, binary, truncated, false, nil
}

// loadStagingDoc / loadWorkingTreeDoc — pure-Go batch fill (tests / sync loaders).

func loadStagingDoc(repoPath string) (*DiffDoc, error) {
	return loadDirtyDoc(repoPath, true)
}

func loadWorkingTreeDoc(repoPath string) (*DiffDoc, error) {
	return loadDirtyDoc(repoPath, false)
}

func loadDirtyDoc(repoPath string, staging bool) (*DiffDoc, error) {
	doc := &DiffDoc{}
	var rows []DiffRow
	pub := func(batch []DiffRow, done bool) bool {
		if len(batch) > 0 {
			rows = append(rows, batch...)
		}
		if done {
			doc.Rows = rows
			doc.Segs = buildDiffFileSegs(doc)
			for _, s := range doc.Segs {
				doc.Stats = append(doc.Stats, FileStat{
					Path: s.Path, Added: s.Added, Deleted: s.Deleted, Binary: s.Binary,
				})
			}
			doc.recomputeTotals()
		}
		return true
	}
	var err error
	if staging {
		err = streamStagingDiffGo(context.Background(), repoPath, pub)
	} else {
		err = streamWorkingTreeDiffGo(context.Background(), repoPath, pub)
	}
	return doc, err
}