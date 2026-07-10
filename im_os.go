package shirei

import (
	"os"
	"path/filepath"

	"github.com/fsnotify/fsnotify"
	"go.hasen.dev/generic"
)

// TODO: keep track of when the cached content is being used or not so we can
// remove them from the caches!

// immediate-mode style OS functions.
//
// Synchronization is the frame lock, nothing else. DirListing / ReadFileContent
// are immediate-mode calls made while rendering, so they run under the frame
// lock and touch the caches directly. The fsnotify watcher goroutines run
// outside a frame, so they invalidate entries under WithFrameLock — which is
// the only thing that serializes them against a render.
//
// (They used to use an ad-hoc RWMutex that the read paths didn't take at all —
// DirListing read the map with no lock while a watcher deleted from it. That
// data race could tear a read mid-edit: a burst of fsnotify events, e.g. from
// another process writing files, would momentarily hand back a wrong/empty
// listing and flash the UI. All cache access goes through the frame lock now.)

var direntries = make(map[string][]os.DirEntry)
var dirEntriesWatcher = generic.Must(fsnotify.NewWatcher())

func init() {
	go func() {
		for e := range dirEntriesWatcher.Events {
			switch e.Op {
			case fsnotify.Create, fsnotify.Remove, fsnotify.Rename:
				parent := filepath.Dir(e.Name)
				WithFrameLock(func() {
					delete(direntries, parent) // invalidate it from cache!
					// the path itself may also be a cached listing: a
					// directory once listed while nonexistent (cached
					// empty, unwatchable) or just removed. DirListing
					// re-adds the watch on the next miss, so it heals.
					delete(direntries, e.Name)
				})
			}
		}
	}()
}

// DirListing returns the entries of a directory. Results are cached and kept
// fresh by a filesystem watcher, so it's cheap to call every frame. It is an
// immediate-mode call, meant to run during rendering (under the frame lock).
func DirListing(path string) []os.DirEntry {
	// called during a frame (frame lock held), so map access is safe
	if list, found := direntries[path]; found {
		return list
	}

	dirEntriesWatcher.Add(path)
	list, _ := os.ReadDir(path)
	direntries[path] = list
	return list
}

var filecontent = make(map[string]map[string]any) // group content related to a file in a map so we can easily wipe all content cached based on the file
var filesWatcher = generic.Must(fsnotify.NewWatcher())

// called during a frame (frame lock held) or from within WithFrameLock
func _setFileCacheContent(fpath string, contentType string, value any) {
	submap := filecontent[fpath]
	if submap == nil {
		submap = make(map[string]any)
		filecontent[fpath] = submap
	}
	submap[contentType] = value
}

// called during a frame (frame lock held)
func _getFileCacheContent[T any](fpath string, contentType string) (T, bool) {
	var zero T
	submap, ok := filecontent[fpath]
	if !ok {
		return zero, ok
	}
	content, ok := submap[contentType]
	if !ok {
		return zero, ok
	}
	typed, ok := content.(T)
	return typed, ok
}

func init() {
	go func() {
		for e := range filesWatcher.Events {
			switch e.Op {
			case fsnotify.Create, fsnotify.Remove, fsnotify.Rename:
				WithFrameLock(func() {
					delete(filecontent, e.Name) // invalidate it from cache!
				})
			}
		}
	}()
}

// ReadFileContent returns the bytes of a file, cached and invalidated when the
// file changes. A small file is read immediately; a large file is read on a
// background goroutine, so the first call returns nil and its content appears on
// a later frame.
func ReadFileContent(fpath string) []byte {
	const key = "content"
	content, found := _getFileCacheContent[[]byte](fpath, key)
	if found {
		return content
	}

	const threshold = 1024 * 1024 * 64 // read in bg if larger than this!
	s, _ := os.Stat(fpath)
	if s.Size() < threshold {
		content, _ = os.ReadFile(fpath)
		_setFileCacheContent(fpath, key, content)
		filesWatcher.Add(filepath.Dir(fpath))
	} else {
		go func() {
			data, _ := os.ReadFile(fpath)
			WithFrameLock(func() {
				_setFileCacheContent(fpath, key, data)
				filesWatcher.Add(filepath.Dir(fpath))
			})
		}()
	}
	return content
}
