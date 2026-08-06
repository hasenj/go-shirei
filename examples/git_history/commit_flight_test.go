package main

import (
	"sync"
	"testing"
	"time"
)

func TestCommitFlightStatsAndDiffJoin(t *testing.T) {
	repo, run := gitTestRepo(t)
	writeFile(t, repo, "a.txt", "one\ntwo\n")
	run("add", "a.txt")
	run("commit", "-m", "init")
	writeFile(t, repo, "a.txt", "one\nTWO\n")
	run("add", "a.txt")
	run("commit", "-m", "mod")
	hash := headHash(t, repo)
	clearRepoGates()

	cache := newDocCache(10)
	ce, _ := cache.acquire(hash)

	var statsMu sync.Mutex
	var gotStats CommitStats
	var statsDone, diffDone sync.WaitGroup
	statsDone.Add(1)
	diffDone.Add(1)

	// Stats request first (may start flight).
	requestCommitFlightStats(repo, hash, statsSink{
		apply: func(st CommitStats) {
			statsMu.Lock()
			gotStats = st
			statsMu.Unlock()
		},
		onDone: statsDone.Done,
	})

	// Diff joins same flight; writes into cache slot.
	requestCommitDiff(repo, hash, ce, func(err error) {
		cache.markReady(hash, err)
		diffDone.Done()
	}, nil, nil)

	wait := make(chan struct{})
	go func() {
		statsDone.Wait()
		diffDone.Wait()
		close(wait)
	}()
	select {
	case <-wait:
	case <-time.After(30 * time.Second):
		t.Fatal("timeout waiting for flight")
	}

	statsMu.Lock()
	st := gotStats
	statsMu.Unlock()
	if !st.Ready || st.Files != 1 || st.Added != 1 || st.Deleted != 1 {
		t.Fatalf("stats = %+v", st)
	}
	if !ce.Ready || ce.Doc == nil || len(ce.Doc.Rows) == 0 {
		t.Fatalf("diff not filled: ready=%v rows=%d", ce.Ready, len(ce.Doc.Rows))
	}
}

func TestCommitFlightIdempotentDiffRequests(t *testing.T) {
	repo, run := gitTestRepo(t)
	writeFile(t, repo, "a.txt", "x\n")
	run("add", "a.txt")
	run("commit", "-m", "init")
	writeFile(t, repo, "a.txt", "y\n")
	run("add", "a.txt")
	run("commit", "-m", "mod")
	hash := headHash(t, repo)
	clearRepoGates()

	cache := newDocCache(10)
	ce, _ := cache.acquire(hash)

	done := make(chan struct{})
	requestCommitDiff(repo, hash, ce, func(err error) {
		cache.markReady(hash, err)
		close(done)
	}, nil, nil)

	// Extra joins — same flight, no second job.
	for i := 0; i < 4; i++ {
		requestCommitDiff(repo, hash, ce, nil, nil, nil)
	}

	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("timeout")
	}
	if !ce.Ready || len(ce.Doc.Rows) == 0 {
		t.Fatal("expected filled doc")
	}
}

func TestCommitFlightStatsOnly(t *testing.T) {
	repo, run := gitTestRepo(t)
	writeFile(t, repo, "a.txt", "a\n")
	run("add", "a.txt")
	run("commit", "-m", "init")
	writeFile(t, repo, "a.txt", "b\n")
	run("add", "a.txt")
	run("commit", "-m", "mod")
	hash := headHash(t, repo)
	clearRepoGates()

	done := make(chan CommitStats, 1)
	requestCommitFlightStats(repo, hash, statsSink{
		apply: func(st CommitStats) { done <- st },
		onDone: func() {},
	})
	select {
	case st := <-done:
		if !st.Ready || st.Files != 1 {
			t.Fatalf("%+v", st)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("timeout")
	}
}

// Regression: joining a flight with a different cache Doc (tab close/reopen or
// reopenFailed) must not mark the new slot Ready with an empty/orphan buffer.
func TestCommitFlightDifferentDocRestarts(t *testing.T) {
	repo, run := gitTestRepo(t)
	writeFile(t, repo, "a.txt", "one\n")
	run("add", "a.txt")
	run("commit", "-m", "init")
	writeFile(t, repo, "a.txt", "two\n")
	run("add", "a.txt")
	run("commit", "-m", "mod")
	hash := headHash(t, repo)
	clearRepoGates()

	cache1 := newDocCache(10)
	ce1, _ := cache1.acquire(hash)

	// Start a flight into ce1, then cancel (tab close / hard refresh).
	started := make(chan struct{})
	requestCommitDiff(repo, hash, ce1, func(err error) {
		cache1.markReady(hash, err)
	}, func(batch []DiffRow, done bool) {
		select {
		case <-started:
		default:
			close(started)
		}
	}, nil)

	// Give the flight a moment to register, then cancel and open a new slot.
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		// Still OK — cancel before/after stream start both exercise the path.
	}
	cancelCommitFlightsForRepo(repo)

	cache2 := newDocCache(10)
	ce2, _ := cache2.acquire(hash)
	if ce2.Doc == ce1.Doc {
		t.Fatal("expected distinct cache buffers")
	}

	done := make(chan error, 1)
	requestCommitDiff(repo, hash, ce2, func(err error) {
		cache2.markReady(hash, err)
		done <- err
	}, nil, nil)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("second flight err: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("timeout waiting for restarted flight")
	}

	if !ce2.Ready || ce2.Err != "" {
		t.Fatalf("ce2 ready=%v err=%q", ce2.Ready, ce2.Err)
	}
	if len(ce2.Doc.Rows) == 0 {
		t.Fatal("new slot marked Ready but empty — orphan join bug")
	}
	// Old slot may be canceled empty; must not be confused with success.
	if ce1.Ready && ce1.Err == "" && len(ce1.Doc.Rows) == 0 {
		// Canceled finish can mark Ready with err; empty success would be wrong.
		t.Fatal("old slot Ready success with empty doc")
	}
}
