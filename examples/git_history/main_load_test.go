package main

import (
	"context"
	"sync"
	"testing"
	"time"
)

// Regression: dirty-load cancel is per-tab. Starting a dirty load on tab B
// must not cancel tab A's in-flight context.
func TestDirtyLoadCancelIsPerTab(t *testing.T) {
	a := &RepoTab{path: "/tmp/a"}
	b := &RepoTab{path: "/tmp/b"}

	ctxA, cancelA := context.WithCancel(context.Background())
	a.replaceDirtyCancel(cancelA)

	ctxB, cancelB := context.WithCancel(context.Background())
	b.replaceDirtyCancel(cancelB)

	if ctxA.Err() != nil {
		t.Fatal("tab A canceled when only B was replaced")
	}
	if ctxB.Err() != nil {
		t.Fatal("ctxB should still be live after install")
	}

	// New dirty load on A cancels only A's previous cancel.
	ctxA2, cancelA2 := context.WithCancel(context.Background())
	a.replaceDirtyCancel(cancelA2)
	if ctxA.Err() == nil {
		t.Fatal("expected previous A cancel to fire")
	}
	if ctxB.Err() != nil {
		t.Fatal("tab B must stay live when A reloads")
	}
	cancelA2()
	cancelB()
	_ = ctxA2
}

// nextStatsBatch keeps history order and respects the concurrent window.
func TestNextStatsBatchOrderedWindow(t *testing.T) {
	ordered := []string{"a", "b", "c", "d", "e", "f"}
	ready := map[string]bool{}
	inflight := map[string]bool{}

	got := nextStatsBatch(ordered,
		func(id string) bool { return ready[id] },
		func(id string) bool { return inflight[id] },
		4,
	)
	if len(got) != 4 || got[0] != "a" || got[3] != "d" {
		t.Fatalf("cold start: %v", got)
	}

	// Two earliest in flight → only fill remaining slots with next ids.
	inflight["a"] = true
	inflight["b"] = true
	got = nextStatsBatch(ordered,
		func(id string) bool { return ready[id] },
		func(id string) bool { return inflight[id] },
		4,
	)
	if len(got) != 2 || got[0] != "c" || got[1] != "d" {
		t.Fatalf("partial inflight: %v", got)
	}

	// Window full of earlier work → do not start later commits.
	inflight["c"] = true
	inflight["d"] = true
	got = nextStatsBatch(ordered,
		func(id string) bool { return ready[id] },
		func(id string) bool { return inflight[id] },
		4,
	)
	if len(got) != 0 {
		t.Fatalf("full window should wait: %v", got)
	}

	// After a finishes, slot frees for e (still not f — only one free slot).
	delete(inflight, "a")
	ready["a"] = true
	// a ready, b,c,d inflight → slots=3 → start e
	got = nextStatsBatch(ordered,
		func(id string) bool { return ready[id] },
		func(id string) bool { return inflight[id] },
		4,
	)
	if len(got) != 1 || got[0] != "e" {
		t.Fatalf("after complete: %v", got)
	}

	// Skip ready holes in the middle.
	ready["b"] = true
	delete(inflight, "b")
	// a,b ready; c,d inflight; e not; → start e (and only e if one slot: slots=2, limit 4 → e,f)
	got = nextStatsBatch(ordered,
		func(id string) bool { return ready[id] },
		func(id string) bool { return inflight[id] },
		4,
	)
	if len(got) != 2 || got[0] != "e" || got[1] != "f" {
		t.Fatalf("skip ready: %v", got)
	}
}

// Regression: statsInflight mutations from background + paint must not race.
// Run with -race.
func TestStatsInflightConcurrent(t *testing.T) {
	tab := &RepoTab{
		path:          "/tmp/stats-race",
		statsInflight: map[string]bool{},
		commitStats:   map[string]CommitStats{},
	}
	var wg sync.WaitGroup
	ids := []string{"h1", "h2", "h3", "h4", "h5"}
	for n := 0; n < 50; n++ {
		for _, id := range ids {
			wg.Add(2)
			go func(id string) {
				defer wg.Done()
				_ = tab.tryMarkStatsInflight(id)
			}(id)
			go func(id string) {
				defer wg.Done()
				_ = tab.hasStatsInflight(id)
				tab.clearStatsInflight(id)
			}(id)
		}
	}
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("timeout")
	}
}
