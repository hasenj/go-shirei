package main

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestPureGoStatsModify(t *testing.T) {
	repo, run := gitTestRepo(t)
	writeFile(t, repo, "a.txt", "one\ntwo\nthree\n")
	run("add", "a.txt")
	run("commit", "-m", "init")
	writeFile(t, repo, "a.txt", "one\nTWO\nthree\n")
	run("add", "a.txt")
	run("commit", "-m", "modify")

	clearRepoGates()
	st, err := loadCommitStatsCtx(context.Background(), repo, headHash(t, repo))
	if err != nil {
		t.Fatal(err)
	}
	if !st.Ready {
		t.Fatal("want Ready")
	}
	if st.Files != 1 {
		t.Fatalf("Files = %d, want 1", st.Files)
	}
	if st.Added != 1 || st.Deleted != 1 {
		t.Fatalf("stats = +%d −%d, want +1 −1", st.Added, st.Deleted)
	}
}

func TestPureGoStatsMultiFile(t *testing.T) {
	repo, run := gitTestRepo(t)
	writeFile(t, repo, "a.txt", "a\n")
	writeFile(t, repo, "b.txt", "b\n")
	run("add", ".")
	run("commit", "-m", "init")
	writeFile(t, repo, "a.txt", "a\nA\n")
	writeFile(t, repo, "c.txt", "c\n")
	run("rm", "b.txt")
	run("add", ".")
	run("commit", "-m", "multi")

	clearRepoGates()
	files, err := loadCommitFileStatsGo(context.Background(), repo, headHash(t, repo))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 3 {
		t.Fatalf("files = %#v, want 3 paths", files)
	}
	st := statsFromNumstat(files)
	if st.Files != 3 {
		t.Fatalf("Files = %d", st.Files)
	}
	// a: +1, c: +1, b: −1 → +2 −1
	if st.Added < 2 || st.Deleted < 1 {
		t.Fatalf("totals +%d −%d from %#v", st.Added, st.Deleted, files)
	}
}

func TestPureGoStatsBinary(t *testing.T) {
	repo, run := gitTestRepo(t)
	writeFile(t, repo, "t.txt", "text\n")
	run("add", "t.txt")
	run("commit", "-m", "init")
	// NUL byte forces binary detection.
	writeFile(t, repo, "blob.bin", "a\x00b\x00c")
	run("add", "blob.bin")
	run("commit", "-m", "binary")

	clearRepoGates()
	files, err := loadCommitFileStatsGo(context.Background(), repo, headHash(t, repo))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("files = %#v", files)
	}
	if !files[0].Binary {
		t.Fatalf("want binary: %#v", files[0])
	}
	st := statsFromNumstat(files)
	if st.Files != 1 || st.Added != 0 || st.Deleted != 0 {
		t.Fatalf("binary summary = %+v (binary must not inflate +/−)", st)
	}
}

func TestPureGoStatsRootCommit(t *testing.T) {
	repo, run := gitTestRepo(t)
	writeFile(t, repo, "root.txt", "hello\nworld\n")
	run("add", "root.txt")
	run("commit", "-m", "root")

	clearRepoGates()
	st, err := loadCommitStatsCtx(context.Background(), repo, headHash(t, repo))
	if err != nil {
		t.Fatal(err)
	}
	if st.Files != 1 || st.Added != 2 || st.Deleted != 0 {
		t.Fatalf("root stats = %+v, want +2 −0 · 1 file", st)
	}
}

func TestPureGoStatsCancel(t *testing.T) {
	repo, run := gitTestRepo(t)
	writeFile(t, repo, "a.txt", "x\n")
	run("add", "a.txt")
	run("commit", "-m", "init")
	// Several small files so cancel between files is meaningful.
	for i := 0; i < 8; i++ {
		writeFile(t, repo, strings.Repeat("f", i+1)+".txt", "line\n")
	}
	run("add", ".")
	run("commit", "-m", "many")

	clearRepoGates()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := loadCommitStatsCtx(ctx, repo, headHash(t, repo))
	if err == nil {
		t.Fatal("want error on canceled context")
	}
}

func TestPureGoStatsNoCLI(t *testing.T) {
	// Product loaders must not need git subprocess for stats.
	repo, run := gitTestRepo(t)
	writeFile(t, repo, "a.txt", "one\n")
	run("add", "a.txt")
	run("commit", "-m", "init")
	writeFile(t, repo, "a.txt", "two\n")
	run("add", "a.txt")
	run("commit", "-m", "chg")

	clearRepoGates()
	// Pure function path used by workers.
	st, err := loadCommitStatsGo(context.Background(), repo, headHash(t, repo))
	if err != nil {
		t.Fatal(err)
	}
	if !st.Ready || st.Files != 1 {
		t.Fatalf("%+v", st)
	}
}

func TestCountLineDeltaFastPaths(t *testing.T) {
	if a, d := countLineDelta("", "a\nb\n"); a != 2 || d != 0 {
		t.Fatalf("add: +%d −%d", a, d)
	}
	if a, d := countLineDelta("a\nb\n", ""); a != 0 || d != 2 {
		t.Fatalf("del: +%d −%d", a, d)
	}
	if a, d := countLineDelta("same\n", "same\n"); a != 0 || d != 0 {
		t.Fatalf("equal: +%d −%d", a, d)
	}
}

func TestPureGoStatsConcurrentWithHistory(t *testing.T) {
	repo, run := gitTestRepo(t)
	writeFile(t, repo, "a.txt", "1\n")
	run("add", "a.txt")
	run("commit", "-m", "c1")
	writeFile(t, repo, "a.txt", "2\n")
	run("add", "a.txt")
	run("commit", "-m", "c2")
	h1 := headHash(t, repo)
	writeFile(t, repo, "a.txt", "3\n")
	run("add", "a.txt")
	run("commit", "-m", "c3")
	h2 := headHash(t, repo)

	clearRepoGates()
	var wg sync.WaitGroup
	var mu sync.Mutex
	var errs []string
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, _, _, err := loadHistory(repo); err != nil {
				mu.Lock()
				errs = append(errs, "history: "+err.Error())
				mu.Unlock()
			}
		}()
		for _, h := range []string{h1, h2} {
			h := h
			wg.Add(1)
			go func() {
				defer wg.Done()
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				if _, err := loadCommitStatsCtx(ctx, repo, h); err != nil {
					mu.Lock()
					errs = append(errs, "stats: "+err.Error())
					mu.Unlock()
				}
			}()
		}
	}
	wg.Wait()
	for _, e := range errs {
		t.Error(e)
	}
}

func TestFileStatFromSnapSkipsModeOnly(t *testing.T) {
	_, ok := fileStatFromSnap(fileSnap{label: "x", modeOnly: true})
	if ok {
		t.Fatal("mode-only should be skipped")
	}
	_, ok = fileStatFromSnap(fileSnap{label: "x", skip: true})
	if ok {
		t.Fatal("skip should be skipped")
	}
}
