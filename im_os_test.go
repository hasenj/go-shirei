package shirei

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFileContentCacheEviction(t *testing.T) {
	savedN := contentCachePruneAfterFrames
	contentCachePruneAfterFrames = 2
	defer func() { contentCachePruneAfterFrames = savedN }()

	dir := t.TempDir()
	path := filepath.Join(dir, "hello.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	ui.FrameNumber = 5000
	got := ReadFileContent(path)
	if string(got) != "hello" {
		t.Fatalf("got %q", got)
	}
	if _, ok := res.filecontent[path]; !ok {
		t.Fatal("expected path in res.filecontent after read")
	}

	ui.FrameNumber = 5003 // lastUsed=5000, stale=5001 → prune
	maybeSweepContentCaches()
	if _, ok := res.filecontent[path]; ok {
		t.Fatal("expected path pruned from res.filecontent")
	}
	if _, ok := res.filecontentLastUsed[path]; ok {
		t.Fatal("expected lastUsed cleared")
	}

	// Reload after prune
	ui.FrameNumber = 5004
	got = ReadFileContent(path)
	if string(got) != "hello" {
		t.Fatalf("reload got %q", got)
	}
}

func TestFileContentTouchKeepsAlive(t *testing.T) {
	savedN := contentCachePruneAfterFrames
	contentCachePruneAfterFrames = 2
	defer func() { contentCachePruneAfterFrames = savedN }()

	dir := t.TempDir()
	path := filepath.Join(dir, "keep.txt")
	if err := os.WriteFile(path, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}

	ui.FrameNumber = 6000
	ReadFileContent(path)
	ui.FrameNumber = 6002
	ReadFileContent(path) // touch
	ui.FrameNumber = 6003
	maybeSweepContentCaches()
	if _, ok := res.filecontent[path]; !ok {
		t.Fatal("touched path should not be pruned")
	}
}

func TestDirListingCacheEviction(t *testing.T) {
	savedN := contentCachePruneAfterFrames
	contentCachePruneAfterFrames = 2
	defer func() { contentCachePruneAfterFrames = savedN }()

	dir := t.TempDir()
	ui.FrameNumber = 7000
	_ = DirListing(dir)
	if _, ok := res.direntries[dir]; !ok {
		t.Fatal("expected dir in res.direntries")
	}

	ui.FrameNumber = 7003
	maybeSweepContentCaches()
	if _, ok := res.direntries[dir]; ok {
		t.Fatal("expected dir pruned")
	}
}

// TestReadFileContentAsyncSmokeLargeFile exercises the real async threshold on a
// large many-newline file (text-view demo corpus). Skips unless the path exists:
//   SHIREI_SMOKE_LARGE_FILE=/path/to/file go test ./shirei/ -run SmokeLarge -count=1
// Default path: /tmp/shirei-textview-200mb.txt
func TestReadFileContentAsyncSmokeLargeFile(t *testing.T) {
	path := os.Getenv("SHIREI_SMOKE_LARGE_FILE")
	if path == "" {
		path = "/tmp/shirei-textview-200mb.txt"
	}
	st, err := os.Stat(path)
	if err != nil || st.Size() < fileContentAsyncThreshold {
		t.Skip("need a file >= fileContentAsyncThreshold (see SHIREI_SMOKE_LARGE_FILE)")
	}

	delete(res.filecontent, path)
	delete(res.filecontentLastUsed, path)
	delete(res.fileContentLoadID, path)
	ui.Host.NextFrame.Store(false)

	t0 := time.Now()
	var id0 uint64
	WithFrameLock(func() {
		if got := ReadFileContent(path); got != nil {
			t.Fatal("expected nil on first async miss")
		}
		var ok bool
		id0, ok = res.fileContentLoadID[path]
		if !ok || id0 == 0 {
			t.Fatal("expected in-flight load id")
		}
	})

	// Simulate many Loading frames (mouse / settle) without restarting the read.
	for i := 0; i < 60; i++ {
		WithFrameLock(func() {
			if got := ReadFileContent(path); got != nil {
				return
			}
			if id := res.fileContentLoadID[path]; id != id0 && id != 0 {
				t.Fatalf("load id restarted on miss frame %d: %d -> %d", i, id0, id)
			}
		})
		time.Sleep(16 * time.Millisecond)
	}

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		var installed []byte
		WithFrameLock(func() {
			installed = ReadFileContent(path)
		})
		if installed != nil {
			elapsed := time.Since(t0)
			if !FrameRequested() {
				t.Fatal("expected RequestNextFrame after async install")
			}
			if _, inflight := res.fileContentLoadID[path]; inflight {
				t.Fatal("load id should clear after install")
			}
			t.Logf("ready in %v size=%d", elapsed, len(installed))
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("timed out waiting for large async install")
}

func TestReadFileContentAsyncNoRestart(t *testing.T) {
	saved := fileContentAsyncThreshold
	fileContentAsyncThreshold = 1 // force async for tiny files
	defer func() { fileContentAsyncThreshold = saved }()

	dir := t.TempDir()
	path := filepath.Join(dir, "big.txt")
	if err := os.WriteFile(path, []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}

	delete(res.filecontent, path)
	delete(res.filecontentLastUsed, path)
	delete(res.fileContentLoadID, path)
	ui.Host.NextFrame.Store(false)

	// Hold the frame lock across both misses so the bg completion cannot
	// install between them (it also takes the frame lock).
	WithFrameLock(func() {
		got1 := ReadFileContent(path)
		if got1 != nil {
			t.Fatal("expected nil on first async miss")
		}
		id1, ok := res.fileContentLoadID[path]
		if !ok || id1 == 0 {
			t.Fatal("expected in-flight load id")
		}
		got2 := ReadFileContent(path)
		if got2 != nil {
			t.Fatal("expected nil while in-flight")
		}
		if id2 := res.fileContentLoadID[path]; id2 != id1 {
			t.Fatalf("load id restarted: %d -> %d", id1, id2)
		}
	})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var installed []byte
		var inflight bool
		WithFrameLock(func() {
			installed, _ = _getFileCacheContent[[]byte](path, "content")
			_, inflight = res.fileContentLoadID[path]
		})
		if installed != nil {
			if string(installed) != "payload" {
				t.Fatalf("got %q", installed)
			}
			if inflight {
				t.Fatal("load id should clear after install")
			}
			if !FrameRequested() {
				t.Fatal("expected RequestNextFrame after async install")
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("timed out waiting for async install")
}
