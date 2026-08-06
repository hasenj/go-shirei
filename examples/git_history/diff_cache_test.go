package main

import "testing"

func TestDocCacheAcquireAndReady(t *testing.T) {
	c := newDocCache(3)
	e, created := c.acquire("a")
	if !created || e == nil || e.Doc == nil || e.Ready {
		t.Fatalf("acquire: created=%v entry=%+v", created, e)
	}
	e2, created2 := c.acquire("a")
	if created2 || e2 != e {
		t.Fatal("second acquire must return same slot")
	}
	if c.getReady("a") != nil {
		t.Fatal("not ready yet")
	}
	e.Doc.Subject = "hi"
	c.markReady("a", nil)
	if !e.Ready || c.getReady("a") != e.Doc {
		t.Fatal("ready miss")
	}
}

func TestDocCacheEvictsReadyNotLoading(t *testing.T) {
	c := newDocCache(2)
	a, _ := c.acquire("a")
	c.markReady("a", nil)
	b, _ := c.acquire("b")
	c.markReady("b", nil)
	// loading slot should not be evicted preferentially
	load, _ := c.acquire("load")
	if load.Ready {
		t.Fatal("load should be incomplete")
	}
	// force over-cap with another ready
	c.put("c", &DiffDoc{Subject: "c"})
	c.put("d", &DiffDoc{Subject: "d"})
	if !c.has("load") {
		t.Fatal("loading entry must survive eviction")
	}
	if c.has("a") && c.has("b") && c.has("c") && c.has("d") {
		t.Fatal("expected some ready eviction")
	}
	_ = a
	_ = b
}

func TestDocCacheInvalidate(t *testing.T) {
	c := newDocCache(5)
	c.put("x", &DiffDoc{Subject: "x"})
	c.invalidate("x")
	if c.getReady("x") != nil {
		t.Fatal("invalidate failed")
	}
}

// Regression: failed loads must not be permanent getReady hits.
func TestDocCacheFailedNotGetReady(t *testing.T) {
	c := newDocCache(5)
	e, _ := c.acquire("a")
	e.Doc.Subject = "partial"
	c.markReady("a", errString("boom"))
	if c.getReady("a") != nil {
		t.Fatal("failed Ready must not be a getReady hit")
	}
	if e := c.entry("a"); e == nil || !e.Ready || e.Err == "" {
		t.Fatal("slot should remain Ready+Err until reopen")
	}
	oldDoc := e.Doc
	re := c.reopenFailed("a")
	if re == nil || re.Ready || re.Err != "" {
		t.Fatalf("reopenFailed: %+v", re)
	}
	if re.Doc == nil || re.Doc == oldDoc {
		t.Fatal("reopenFailed must allocate a new Doc buffer")
	}
	if c.getReady("a") != nil {
		t.Fatal("still loading after reopen")
	}
}

type errString string

func (e errString) Error() string { return string(e) }

// Regression: beginSelect reloads after a Ready+Err cache slot.
func TestBeginSelectRetriesFailedCache(t *testing.T) {
	prevTabs := appData.tabs
	defer func() { appData.tabs = prevTabs }()

	tab := &RepoTab{
		path:     "/tmp/retry-repo",
		docCache: newDocCache(5),
		history: []HistoryEntry{
			{Kind: KindCommit, ID: "abc", Short: "abc", Subject: "s"},
		},
	}
	appData.tabs = []*RepoTab{tab}

	ce, _ := tab.docCache.acquire("abc")
	tab.docCache.markReady("abc", errString("transient"))
	// Simulate sticky error selection state.
	tab.doc = ce.Doc
	tab.docID = "abc"
	tab.docLoading = false
	tab.docErr = "transient"

	entry, _, _, ok, cached := beginSelect(tab, "abc")
	if !ok {
		t.Fatal("expected ok")
	}
	if cached {
		t.Fatal("failed cache must not short-circuit as cached")
	}
	if entry.ID != "abc" {
		t.Fatal(entry.ID)
	}
	if tab.docLoading != true {
		t.Fatal("expected docLoading for reload")
	}
	if tab.docErr != "" {
		t.Fatalf("docErr should clear for retry, got %q", tab.docErr)
	}
	if e := tab.docCache.entry("abc"); e == nil || e.Ready {
		t.Fatalf("slot should be !Ready for new load: %+v", e)
	}
}
