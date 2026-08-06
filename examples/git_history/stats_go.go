package main

import (
	"context"
	"fmt"
	"strings"

	gdiff "github.com/go-git/go-git/v6/utils/diff"
)

// Pure-Go first-parent commit stats for the history sidebar (+/− · N files).
// Reuses snapshotFirstParentChanges (short lockRepo) then unlocked Myers counts
// only — no DiffRows, no unified text, no parent.Patch.

// loadCommitFileStatsGo returns per-path stats for a first-parent commit patch
// (same semantic target as git log -1 --numstat, directional parity).
func loadCommitFileStatsGo(ctx context.Context, repoPath, hash string) ([]FileStat, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if repoPath == "" || hash == "" {
		return nil, fmt.Errorf("repoPath and hash required")
	}
	snaps, err := snapshotFirstParentChanges(ctx, repoPath, hash)
	if err != nil {
		return nil, err
	}
	return fileStatsFromSnaps(ctx, snaps)
}

// loadCommitStatsGo is the pure-Go sidebar summary.
func loadCommitStatsGo(ctx context.Context, repoPath, hash string) (CommitStats, error) {
	files, err := loadCommitFileStatsGo(ctx, repoPath, hash)
	if err != nil {
		return CommitStats{}, err
	}
	return statsFromNumstat(files), nil
}

// fileStatsFromSnaps counts +/− per snap without building DiffRows.
// Checks ctx between files so cancel can abandon mid-commit.
func fileStatsFromSnaps(ctx context.Context, snaps []fileSnap) ([]FileStat, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	out := make([]FileStat, 0, len(snaps))
	for _, s := range snaps {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		st, ok := fileStatFromSnap(s)
		if !ok {
			continue
		}
		out = append(out, st)
	}
	return out, nil
}

// fileStatFromSnap returns false for non-file / mode-only changes (not counted
// as paths, matching typical git log --numstat).
func fileStatFromSnap(s fileSnap) (FileStat, bool) {
	if s.skip || s.modeOnly {
		return FileStat{}, false
	}
	label := s.label
	if label == "" {
		label = "(unknown)"
	}
	if s.fromBin || s.toBin {
		return FileStat{Path: label, Added: -1, Deleted: -1, Binary: true}, true
	}
	added, deleted := countLineDelta(string(s.from), string(s.to))
	return FileStat{Path: label, Added: added, Deleted: deleted}, true
}

// countLineDelta is unlocked Myers line counts (inserts / deletes only).
// Uses the same timeout cap as the patch streamer.
func countLineDelta(from, to string) (added, deleted int) {
	// Fast paths: pure add / pure delete / identical.
	if from == to {
		return 0, 0
	}
	if from == "" {
		return countContentLines(to), 0
	}
	if to == "" {
		return 0, countContentLines(from)
	}
	diffs := gdiff.DoWithTimeout(from, to, perFileDiffTimeout)
	for _, o := range expandLineOps(diffs) {
		switch o.typ {
		case '+':
			added++
		case '-':
			deleted++
		}
	}
	return added, deleted
}

// countContentLines counts lines the same way expandLineOps would for a pure
// insert/delete block (split on '\n', including a final non-empty fragment).
func countContentLines(s string) int {
	if s == "" {
		return 0
	}
	n := 0
	for len(s) > 0 {
		i := strings.IndexByte(s, '\n')
		if i < 0 {
			n++
			break
		}
		n++
		s = s[i+1:]
	}
	return n
}
