package shirei

import (
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// contentCachePruneAfterFrames drops unused content caches not touched for
// this many frame *passes* (FrameNumber steps; settle can advance twice per
// presented frame). Shared by:
//   - filecontent / direntries (this file)
//   - the image handle table (images.go)
//
// Tests may lower it.
var contentCachePruneAfterFrames int64 = 12

// dirListingRefreshInterval controls how often a cached DirListing checks disk
// for changes that fsnotify can miss (FUSE/Android MediaProvider, unwatched
// directories, symlinked or network mounts). Tests may lower it.
var dirListingRefreshInterval = time.Second

type dirListingMeta struct {
	modTime   time.Time
	checkedAt time.Time
	watched   bool
}

// Immediate-mode filesystem helpers.
//
// DirListing and ReadFileContent run during rendering under the frame lock and
// touch the caches on res directly. The fsnotify watcher goroutines started in
// NewResources post cache invalidations via Post so they run on the UI thread
// before the next frame build.
//
// Unused entries are dropped after contentCachePruneAfterFrames (see
// maybeSweepContentCaches).

// DirListing returns the entries of a directory. Results are cached and kept
// fresh by a filesystem watcher plus a periodic stat/re-read fallback for
// filesystems where watchers do not report external writes (such as Android
// FUSE / MediaProvider or unwatched mounts). It is an immediate-mode call,
// meant to run during rendering (under the frame lock).
func DirListing(path string) []os.DirEntry {
	now := time.Now()
	if list, found := res.direntries[path]; found {
		res.direntriesLastUsed[path] = ui.FrameNumber
		meta := res.direntriesMeta[path]
		if ui != nil && ui.Host.HeadlessRender {
			return list
		}
		if dirListingRefreshInterval <= 0 || now.Sub(meta.checkedAt) < dirListingRefreshInterval {
			return list
		}
		// On Android (FUSE/MediaProvider) or when fsnotify failed to watch path,
		// re-read the directory directly; otherwise stat and re-read only if
		// the directory's modification time changed.
		if runtime.GOOS != "android" && meta.watched {
			if st, err := os.Stat(path); err == nil && st.ModTime().Equal(meta.modTime) {
				meta.checkedAt = now
				res.direntriesMeta[path] = meta
				return list
			}
		}
	}

	watched := false
	if res.dirEntriesWatcher != nil {
		watched = res.dirEntriesWatcher.Add(path) == nil
	}
	var modTime time.Time
	if st, err := os.Stat(path); err == nil {
		modTime = st.ModTime()
	}
	list, _ := os.ReadDir(path)
	res.direntries[path] = list
	res.direntriesLastUsed[path] = ui.FrameNumber
	if res.direntriesMeta == nil {
		res.direntriesMeta = make(map[string]dirListingMeta)
	}
	res.direntriesMeta[path] = dirListingMeta{
		modTime:   modTime,
		checkedAt: now,
		watched:   watched,
	}
	return list
}

// DropDirListing removes path from the directory-listing cache so the next
// DirListing reads fresh entries from disk. Call during rendering (under the
// frame lock).
func DropDirListing(path string) {
	delete(res.direntries, path)
	delete(res.direntriesLastUsed, path)
	delete(res.direntriesMeta, path)
}

// file content load tokens and generation live on res.

func touchFileContent(fpath string) {
	res.filecontentLastUsed[fpath] = ui.FrameNumber
}

// called during a frame (frame lock held) or from within WithFrameLock
func _setFileCacheContent(fpath string, contentType string, value any) {
	submap := res.filecontent[fpath]
	if submap == nil {
		submap = make(map[string]any)
		res.filecontent[fpath] = submap
	}
	submap[contentType] = value
	res.fileContentGeneration++
	touchFileContent(fpath)
}

// called during a frame (frame lock held)
func _getFileCacheContent[T any](fpath string, contentType string) (T, bool) {
	var zero T
	submap, ok := res.filecontent[fpath]
	if !ok {
		return zero, ok
	}
	content, ok := submap[contentType]
	if !ok {
		return zero, ok
	}
	typed, ok := content.(T)
	if ok {
		touchFileContent(fpath)
	}
	return typed, ok
}

// called during a frame (frame lock held) or from within WithFrameLock
func _deleteFileCacheContent(fpath string, contentType string) {
	submap := res.filecontent[fpath]
	if submap == nil {
		return
	}
	delete(submap, contentType)
	if len(submap) == 0 {
		delete(res.filecontent, fpath)
		delete(res.filecontentLastUsed, fpath)
		delete(res.fileContentLoadID, fpath)
	}
}

// fileContentAsyncThreshold is the size at which ReadFileContent reads on a
// background goroutine instead of on the frame path. Tests may lower it.
var fileContentAsyncThreshold int64 = 1024 * 1024 * 64

// ReadFileContent returns the bytes of a file, cached and invalidated when the
// file changes. A small file is read immediately; a large file is read on a
// background goroutine, so the first call returns nil and its content appears on
// a later frame (via RequestNextFrame when the read finishes).
func ReadFileContent(fpath string) []byte {
	const key = "content"
	content, found := _getFileCacheContent[[]byte](fpath, key)
	if found {
		return content
	}

	s, _ := os.Stat(fpath)
	if s == nil {
		return nil
	}
	if s.Size() < fileContentAsyncThreshold {
		content, _ = os.ReadFile(fpath)
		_setFileCacheContent(fpath, key, content)
		if res.filesWatcher != nil {
			_ = res.filesWatcher.Add(filepath.Dir(fpath))
		}
	} else {
		// One in-flight read per path. A miss every Loading frame must not bump
		// the load id — that would cancel the previous read and leave a large
		// file stuck on Loading while input keeps the loop awake.
		if _, inflight := res.fileContentLoadID[fpath]; inflight {
			return nil
		}
		// Token so a prune discards this completion if the path was dropped.
		res.fileContentLoadSeq++
		loadID := res.fileContentLoadSeq
		res.fileContentLoadID[fpath] = loadID
		go func() {
			data, _ := os.ReadFile(fpath)
			Post(func() {
				if res.fileContentLoadID[fpath] != loadID {
					return
				}
				_setFileCacheContent(fpath, key, data)
				delete(res.fileContentLoadID, fpath)
				if res.filesWatcher != nil {
					_ = res.filesWatcher.Add(filepath.Dir(fpath))
				}
			})
		}()
	}
	return content
}

// DropFileContent removes one path from the file-content cache so the next
// ReadFileContent reads the file again. It is an immediate-mode call, meant
// to run during rendering (under the frame lock). An in-flight async read of
// the same path is discarded when it finishes.
func DropFileContent(fpath string) {
	_deleteFileCacheContent(fpath, "content")
	delete(res.fileContentLoadID, fpath)
}

// maybeSweepContentCaches drops filecontent and direntries paths not touched
// within contentCachePruneAfterFrames. Called after the final RunFrameFn pass
// (with maybeSweepImages). Cancels in-flight async file reads for pruned paths.
func maybeSweepContentCaches() {
	stale := ui.FrameNumber - contentCachePruneAfterFrames
	for path, last := range res.filecontentLastUsed {
		if last <= stale {
			delete(res.filecontent, path)
			delete(res.filecontentLastUsed, path)
			delete(res.fileContentLoadID, path)
		}
	}
	for path, last := range res.direntriesLastUsed {
		if last <= stale {
			delete(res.direntries, path)
			delete(res.direntriesLastUsed, path)
			delete(res.direntriesMeta, path)
		}
	}
}

// FileCacheStats is a snapshot of the IM file/dir caches for debug HUDs
// (parallel to ImageCacheStats — path-keyed rather than dense ImageIds).
type FileCacheStats struct {
	// FilePaths is the number of paths with any filecontent entry.
	FilePaths int
	// DirPaths is the number of cached DirListing paths.
	DirPaths int
	// Entries is the total number of content-type slots across all file paths
	// (e.g. "content", "image", "image-config").
	Entries int
	// ContentBytes is the approximate total size of cached []byte values
	// (raw file bodies). Other entry types are not included.
	ContentBytes int64
	// InFlight is the number of paths with an outstanding async load token.
	InFlight int
	// NextLoadID is the last async load id minted (fileContentLoadSeq).
	NextLoadID uint64
	// NextGeneration bumps on every cache write (set), like image generation.
	NextGeneration uint64
}

// DebugGetFileCacheStats returns a snapshot of filecontent / direntries.
// Intended for debug HUDs — not a stable performance API.
func DebugGetFileCacheStats() FileCacheStats {
	var s FileCacheStats
	s.FilePaths = len(res.filecontent)
	s.DirPaths = len(res.direntries)
	s.InFlight = len(res.fileContentLoadID)
	s.NextLoadID = res.fileContentLoadSeq
	s.NextGeneration = res.fileContentGeneration
	for _, sub := range res.filecontent {
		s.Entries += len(sub)
		for _, v := range sub {
			if b, ok := v.([]byte); ok {
				s.ContentBytes += int64(len(b))
			}
		}
	}
	return s
}
