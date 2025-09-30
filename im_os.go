package slay

import (
	"os"
	"path/filepath"
	"sync"

	"github.com/fsnotify/fsnotify"
	"go.hasen.dev/generic"
)

// TODO: keep track of when the cahced content is being used or not so we can remove them from the caches!

// immediate-mode style OS functions

var imosLock sync.RWMutex

var direntries = make(map[string][]os.DirEntry)
var dirEntriesWatcher = generic.Must(fsnotify.NewWatcher())

func init() {
	go func() {
		for e := range dirEntriesWatcher.Events {
			switch e.Op {
			case fsnotify.Create, fsnotify.Remove, fsnotify.Rename:
				parent := filepath.Dir(e.Name)
				generic.WithWriteLock(&imosLock, func() {
					delete(direntries, parent) // invalidate it from cache!
				})
			}
		}
	}()
}

func DirListing(path string) []os.DirEntry {
	list, found := direntries[path]
	if found {
		return list
	}

	dirEntriesWatcher.Add(path)
	list, _ = os.ReadDir(path)

	generic.WithWriteLock(&imosLock, func() {
		direntries[path] = list
	})
	return list
}

var filecontent = make(map[string]map[string]any) // group content related to a file in a map so we can easily wipe all content cached based on the file
var filesWatcher = generic.Must(fsnotify.NewWatcher())

func _setFileCacheContent(fpath string, contentType string, value any) {
	imosLock.Lock()
	defer imosLock.Unlock()

	submap := filecontent[fpath]
	if submap == nil {
		submap = make(map[string]any)
	}
	submap[contentType] = value
	filecontent[fpath] = submap
}

func _getFileCacheContent[T any](fpath string, contentType string) (T, bool) {
	imosLock.RLock()
	defer imosLock.RUnlock()

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
				generic.WithWriteLock(&imosLock, func() {
					delete(filecontent, e.Name) // invalidate it from cache!
				})
			}
		}
	}()
}

func ReadFileContent(fpath string) []byte {
	const key = "content"
	content, found := _getFileCacheContent[[]byte](fpath, key)
	if found {
		return content
	}

	content, _ = os.ReadFile(fpath)
	filesWatcher.Add(filepath.Dir(fpath))
	_setFileCacheContent(fpath, key, content)
	return content
}
