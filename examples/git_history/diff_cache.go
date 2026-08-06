package main

// Diff document cache: owns the DiffDoc buffers that flights fill in place.
//
// Acquire hands out a stable *CacheEntry. Ready means the producer finished.
// Err non-empty means the finish was a failure — getReady does not treat those
// as hits so re-select can reload. Empty patches (Ready, Err="") are valid.
// Incomplete entries (Ready=false) are never evicted so an in-flight fill
// always has a home. Cap applies to Ready entries (~20 by default).

const defaultDiffCacheSize = 20

// CacheEntry is one cached (or reserved) diff slot.
type CacheEntry struct {
	Doc   *DiffDoc
	Ready bool
	Err   string // set when Ready after a failed load
}

// docCache is an LRU of commit/dirty DiffDocs keyed by HistoryEntry.ID.
type docCache struct {
	max   int
	order []string // oldest at [0]; touch moves to end
	m     map[string]*CacheEntry
}

func newDocCache(max int) *docCache {
	if max < 1 {
		max = defaultDiffCacheSize
	}
	return &docCache{max: max, m: make(map[string]*CacheEntry, max)}
}

// getReady returns a successfully completed doc, or nil. Failed slots
// (Ready with Err set) are not hits — callers re-acquire and reload.
// Touches LRU on hit.
func (c *docCache) getReady(id string) *DiffDoc {
	e := c.entry(id)
	if e == nil || !e.Ready || e.Doc == nil || e.Err != "" {
		return nil
	}
	c.touch(id)
	return e.Doc
}

// reopenFailed clears a Ready+Err slot so the next load can fill a fresh buffer.
// No-op when the slot is missing, still loading, or successfully ready.
// Returns the entry after reset, or nil if none.
func (c *docCache) reopenFailed(id string) *CacheEntry {
	if c == nil || id == "" {
		return nil
	}
	e := c.m[id]
	if e == nil || !e.Ready || e.Err == "" {
		return e
	}
	// New Doc pointer so a canceled flight still writing the old buffer cannot
	// join markReady onto this slot (see requestCommitDiff doc mismatch path).
	e.Doc = &DiffDoc{}
	e.Ready = false
	e.Err = ""
	c.touch(id)
	return e
}

// get is an alias for getReady (callers that only want finished docs).
func (c *docCache) get(id string) *DiffDoc {
	return c.getReady(id)
}

// has reports whether any slot exists (ready or loading).
func (c *docCache) has(id string) bool {
	return c.entry(id) != nil
}

func (c *docCache) entry(id string) *CacheEntry {
	if c == nil || id == "" {
		return nil
	}
	return c.m[id]
}

// acquire returns the slot for id, creating an empty !Ready entry if needed.
// created is true when a new slot was allocated.
func (c *docCache) acquire(id string) (e *CacheEntry, created bool) {
	if c == nil || id == "" {
		return &CacheEntry{Doc: &DiffDoc{}}, true
	}
	if e = c.m[id]; e != nil {
		c.touch(id)
		return e, false
	}
	e = &CacheEntry{Doc: &DiffDoc{}, Ready: false}
	c.m[id] = e
	c.order = append(c.order, id)
	c.evictIfNeeded()
	return e, true
}

// markReady settles a slot after the flight finishes writing into e.Doc.
func (c *docCache) markReady(id string, err error) {
	if c == nil || id == "" {
		return
	}
	e := c.m[id]
	if e == nil {
		return
	}
	e.Ready = true
	if err != nil {
		e.Err = err.Error()
	} else {
		e.Err = ""
	}
	c.touch(id)
	c.evictIfNeeded()
}

// put inserts or replaces a fully ready doc (sync loaders, dirty docs).
func (c *docCache) put(id string, doc *DiffDoc) {
	if c == nil || doc == nil || id == "" {
		return
	}
	if e, ok := c.m[id]; ok {
		e.Doc = doc
		e.Ready = true
		e.Err = ""
		c.touch(id)
		c.evictIfNeeded()
		return
	}
	c.m[id] = &CacheEntry{Doc: doc, Ready: true}
	c.order = append(c.order, id)
	c.evictIfNeeded()
}

// invalidate drops a slot so the next acquire starts fresh (dirty refresh).
func (c *docCache) invalidate(id string) {
	if c == nil || id == "" {
		return
	}
	if _, ok := c.m[id]; !ok {
		return
	}
	delete(c.m, id)
	for i, k := range c.order {
		if k == id {
			c.order = append(c.order[:i], c.order[i+1:]...)
			break
		}
	}
}

func (c *docCache) clear() {
	if c == nil {
		return
	}
	c.order = c.order[:0]
	clear(c.m)
}

func (c *docCache) touch(id string) {
	for i, k := range c.order {
		if k == id {
			c.order = append(c.order[:i], c.order[i+1:]...)
			break
		}
	}
	c.order = append(c.order, id)
}

// evictIfNeeded removes oldest Ready entries until len <= max.
// Loading (!Ready) slots are kept so in-flight writers keep a stable buffer.
func (c *docCache) evictIfNeeded() {
	if c == nil {
		return
	}
	for len(c.m) > c.max {
		evicted := false
		for i, id := range c.order {
			e := c.m[id]
			if e != nil && !e.Ready {
				continue
			}
			delete(c.m, id)
			c.order = append(c.order[:i], c.order[i+1:]...)
			evicted = true
			break
		}
		if !evicted {
			// All remaining are Loading — allow temporary over-cap.
			return
		}
	}
}

// filterDocCache keeps only entries for which keep(id) is true.
func filterDocCache(c *docCache, keep func(id string) bool) {
	if c == nil {
		return
	}
	n := c.order[:0]
	for _, id := range c.order {
		if keep(id) {
			n = append(n, id)
		} else {
			delete(c.m, id)
		}
	}
	c.order = n
}
