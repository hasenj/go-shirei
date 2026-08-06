package main

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/go-git/go-git/v6/plumbing"
)

// Benchmarks for load strategy. Run:
//
//	go test -bench='BenchmarkCommit(Meta|Numstat|Patch)' -benchtime=5x -run=^$
//	GO_GIT_PATCH_BENCH=1 go test -bench=BenchmarkCommitPatchGoGit -benchtime=1x -run=^$
//
// Takeaway: CommitObject (meta) is fine in pure Go. Full parent.Patch is where
// go-git spends seconds on large file rewrites; git CLI does the same job much
// faster. Process spawn (~5–10ms) only hurts for tiny meta loads on rapid key
// repeat — which is why meta is pure Go and the UI paints a stub first.

func benchRepo(b *testing.B) string {
	b.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		b.Fatal(err)
	}
	root, err := findRepo(cwd)
	if err != nil {
		b.Skip(err)
	}
	return root
}

func benchHeadHash(b *testing.B, repo string) string {
	b.Helper()
	r, unlock, err := lockRepo(repo)
	if err != nil {
		b.Fatal(err)
	}
	ref, err := r.Head()
	unlock()
	if err != nil {
		b.Fatal(err)
	}
	return ref.Hash().String()
}

func BenchmarkCommitMetaGo(b *testing.B) {
	repo := benchRepo(b)
	hash := benchHeadHash(b, repo)
	if _, err := loadCommitMetaGo(repo, hash); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := loadCommitMetaGo(repo, hash); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCommitNumstatCLI(b *testing.B) {
	repo := benchRepo(b)
	hash := benchHeadHash(b, repo)
	ctx := context.Background()
	if _, err := loadCommitNumstatCLI(ctx, repo, hash); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := loadCommitNumstatCLI(ctx, repo, hash); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCommitStatsGo(b *testing.B) {
	repo := benchRepo(b)
	hash := benchHeadHash(b, repo)
	ctx := context.Background()
	if _, err := loadCommitStatsCtx(ctx, repo, hash); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := loadCommitStatsCtx(ctx, repo, hash); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCommitPatchGo(b *testing.B) {
	repo := benchRepo(b)
	hash := benchHeadHash(b, repo)
	meta, err := loadCommitMetaGo(repo, hash)
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	d := *meta
	if err := loadCommitPatchIntoGo(ctx, repo, hash, &d); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d := *meta
		if err := loadCommitPatchIntoGo(ctx, repo, hash, &d); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkCommitPatchGoFirstBatch measures time until the first publish
// (typically the first file header) — the "feel instant" budget.
func BenchmarkCommitPatchGoFirstBatch(b *testing.B) {
	repo := benchRepo(b)
	hash := benchHeadHash(b, repo)
	ctx := context.Background()
	// Warm.
	_ = streamCommitPatchGo(ctx, repo, hash, func([]DiffRow, bool) bool { return true })
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		start := time.Now()
		var firstUS float64
		err := streamCommitPatchGo(ctx, repo, hash, func(batch []DiffRow, done bool) bool {
			if firstUS == 0 && len(batch) > 0 {
				firstUS = float64(time.Since(start).Microseconds())
			}
			return true
		})
		if err != nil {
			b.Fatal(err)
		}
		b.ReportMetric(firstUS, "us_to_first_batch")
	}
}

// BenchmarkCommitPatchGoGit is the old selection path (parent.Patch + Encode).
// Opt-in: can take a long time on large commits.
func BenchmarkCommitPatchGoGit(b *testing.B) {
	if os.Getenv("GO_GIT_PATCH_BENCH") == "" {
		b.Skip("set GO_GIT_PATCH_BENCH=1 to run go-git full Patch benchmark")
	}
	repo := benchRepo(b)
	hash := "855d415e"
	if _, err := loadCommitMetaGo(repo, hash); err != nil {
		hash = benchHeadHash(b, repo)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		start := time.Now()
		r, unlock, err := lockRepo(repo)
		if err != nil {
			b.Fatal(err)
		}
		c, err := r.CommitObject(plumbing.NewHash(hash))
		if err != nil {
			unlock()
			b.Fatal(err)
		}
		patch, err := commitPatch(c)
		if err != nil {
			unlock()
			b.Fatal(err)
		}
		doc := &DiffDoc{}
		fillDocFromPatch(doc, patch)
		unlock()
		b.ReportMetric(float64(time.Since(start).Milliseconds()), "ms_wall")
		if len(doc.Rows) == 0 {
			b.Fatal("empty patch rows")
		}
	}
}
