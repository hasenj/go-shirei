package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// statusBenchRepo is the monorepo root when tests run inside it.
func statusBenchRepo(t testing.TB) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	repo, err := findRepo(cwd)
	if err != nil {
		t.Skip("not inside a git repo:", err)
	}
	// Need git on PATH for the native baseline.
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH:", err)
	}
	return repo
}

func BenchmarkStatusNativeGit(b *testing.B) {
	repo := statusBenchRepo(b)
	// Warm once.
	if _, err := getRepoStatusNative(repo); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := getRepoStatusNative(repo); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkStatusPureGo(b *testing.B) {
	repo := statusBenchRepo(b)
	// Warm openRepoAt / packs.
	if _, err := computeRepoStatusPure(repo); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// No app-level cache: measure the real function.
		if _, err := computeRepoStatusPure(repo); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkStatusGoGitWorktree(b *testing.B) {
	repo := statusBenchRepo(b)
	r, unlock, err := lockRepo(repo)
	if err != nil {
		b.Fatal(err)
	}
	// go-git Worktree.Status is single-threaded here; hold the gate for the
	// whole benchmark so concurrent app paths cannot race this handle.
	defer unlock()
	wt, err := r.Worktree()
	if err != nil {
		b.Fatal(err)
	}
	if _, err := wt.Status(); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := wt.Status(); err != nil {
			b.Fatal(err)
		}
	}
}

// TestStatusPerfVsNative is the concrete stop criterion:
// pure-Go status wall time must be ≤ native `git status --porcelain=v1`
// (median of several runs, small absolute slack for timer noise).
func TestStatusPerfVsNative(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping status perf vs native in -short")
	}
	repo := statusBenchRepo(t)

	const (
		warmup = 2
		samples = 7
		// Slack absorbs scheduling noise; pure should still be in the same
		// ballpark as git, not "2s vs 20ms".
		slack = 5 * time.Millisecond
	)

	timeStatus := func(fn func() error) time.Duration {
		start := time.Now()
		if err := fn(); err != nil {
			t.Fatal(err)
		}
		return time.Since(start)
	}

	for i := 0; i < warmup; i++ {
		_ = timeStatus(func() error { _, err := getRepoStatusNative(repo); return err })
		_ = timeStatus(func() error { _, err := computeRepoStatusPure(repo); return err })
	}

	nativeSamples := make([]time.Duration, samples)
	pureSamples := make([]time.Duration, samples)
	for i := 0; i < samples; i++ {
		nativeSamples[i] = timeStatus(func() error { _, err := getRepoStatusNative(repo); return err })
		pureSamples[i] = timeStatus(func() error { _, err := computeRepoStatusPure(repo); return err })
	}

	nMed := medianDuration(nativeSamples)
	pMed := medianDuration(pureSamples)
	t.Logf("repo=%s  native median=%v  pure median=%v  samples=%d",
		filepath.Base(repo), nMed, pMed, samples)
	for i := 0; i < samples; i++ {
		t.Logf("  sample %d: native=%v pure=%v", i, nativeSamples[i], pureSamples[i])
	}

	if pMed > nMed+slack {
		t.Fatalf("pure-Go status slower than native git: pure median %v > native median %v + slack %v\n"+
			"Keep iterating on computeRepoStatusPure until pure ≤ native.",
			pMed, nMed, slack)
	}
}

// TestStatusAgreesWithNative checks dirty flags and untracked set match git.
// Staging/worktree codes may differ on renames; we compare path sets by side.
func TestStatusAgreesWithNative(t *testing.T) {
	repo := statusBenchRepo(t)
	native, err := getRepoStatusNative(repo)
	if err != nil {
		t.Fatal(err)
	}
	pure, err := computeRepoStatusPure(repo)
	if err != nil {
		t.Fatal(err)
	}

	if pure.worktreeDirty() != native.worktreeDirty() {
		t.Errorf("worktreeDirty: pure=%v native=%v", pure.worktreeDirty(), native.worktreeDirty())
	}
	if pure.stagingDirty() != native.stagingDirty() {
		t.Errorf("stagingDirty: pure=%v native=%v", pure.stagingDirty(), native.stagingDirty())
	}

	// Path sets with worktree changes (Y != ' ') and staging (X not space/?).
	nWT, nST, nUT := statusPathSets(native)
	pWT, pST, pUT := statusPathSets(pure)

	if !stringSetEqual(nWT, pWT) {
		t.Errorf("worktree paths differ\n  only native: %v\n  only pure: %v",
			setDiff(nWT, pWT), setDiff(pWT, nWT))
	}
	if !stringSetEqual(nST, pST) {
		t.Errorf("staging paths differ\n  only native: %v\n  only pure: %v",
			setDiff(nST, pST), setDiff(pST, nST))
	}
	if !stringSetEqual(nUT, pUT) {
		t.Errorf("untracked paths differ\n  only native: %v\n  only pure: %v",
			setDiff(nUT, pUT), setDiff(pUT, nUT))
	}
}

func statusPathSets(s *repoStatus) (worktree, staging, untracked map[string]bool) {
	worktree = map[string]bool{}
	staging = map[string]bool{}
	untracked = map[string]bool{}
	if s == nil {
		return
	}
	for _, e := range s.lines {
		if e.X == '?' && e.Y == '?' {
			untracked[e.Path] = true
			worktree[e.Path] = true
			continue
		}
		if e.Y != ' ' {
			worktree[e.Path] = true
		}
		if e.X != ' ' && e.X != '?' {
			staging[e.Path] = true
		}
	}
	return
}

func stringSetEqual(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}

func setDiff(a, b map[string]bool) []string {
	var out []string
	for k := range a {
		if !b[k] {
			out = append(out, k)
		}
	}
	return out
}

func medianDuration(ds []time.Duration) time.Duration {
	if len(ds) == 0 {
		return 0
	}
	// insertion sort copy
	c := append([]time.Duration(nil), ds...)
	for i := 1; i < len(c); i++ {
		j := i
		for j > 0 && c[j] < c[j-1] {
			c[j], c[j-1] = c[j-1], c[j]
			j--
		}
	}
	return c[len(c)/2]
}
