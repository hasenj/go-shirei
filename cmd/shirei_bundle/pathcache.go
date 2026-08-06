package main

import (
	"os"
	"strings"
	"sync"
	"time"
)

// Short-lived path existence cache for immediate-mode UI.
// Frame code must not hammer the filesystem every paint; Stat is cheap
// once but multiplies across apps × fields × 60fps.
const pathCacheTTL = 2 * time.Second

type pathCacheEntry struct {
	exists bool
	isDir  bool
	at     time.Time
}

var pathCache struct {
	mu sync.Mutex
	m  map[string]pathCacheEntry
}

// cachedPathInfo returns whether path exists and whether it is a directory.
// Empty path → false, false. Results are cached for pathCacheTTL.
func cachedPathInfo(path string) (exists, isDir bool) {
	path = strings.TrimSpace(path)
	if path == "" {
		return false, false
	}
	now := time.Now()
	pathCache.mu.Lock()
	if e, ok := pathCache.m[path]; ok && now.Sub(e.at) < pathCacheTTL {
		pathCache.mu.Unlock()
		return e.exists, e.isDir
	}
	pathCache.mu.Unlock()

	st, err := os.Stat(path)
	exists = err == nil
	if exists {
		isDir = st.IsDir()
	}
	pathCache.mu.Lock()
	if pathCache.m == nil {
		pathCache.m = make(map[string]pathCacheEntry)
	}
	pathCache.m[path] = pathCacheEntry{exists: exists, isDir: isDir, at: now}
	pathCache.mu.Unlock()
	return exists, isDir
}

func invalidatePathCache() {
	pathCache.mu.Lock()
	pathCache.m = nil
	pathCache.mu.Unlock()
}
