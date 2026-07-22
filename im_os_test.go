package shirei

import (
	"os"
	"path/filepath"
	"testing"
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
