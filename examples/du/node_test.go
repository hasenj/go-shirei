package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// TestNodeIdHardLinks exercises the per-platform node identity that backs
// hard-link dedupe: two links to the same file must share a NodeId and
// report a link count > 1; independent files must not collide. Runs on
// every platform; for windows, cross-compile and run under wine:
//
//	GOOS=windows GOARCH=amd64 go test -c -o /tmp/dutest.exe ./examples/du
//	wine /tmp/dutest.exe -test.run NodeId -test.v
func TestNodeIdHardLinks(t *testing.T) {
	dir := t.TempDir()
	writeFile := func(name string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, bytes.Repeat([]byte("du"), 32*1024), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	stat := func(p string) os.FileInfo {
		info, err := os.Lstat(p)
		if err != nil {
			t.Fatal(err)
		}
		return info
	}

	a := writeFile("a")
	c := writeFile("c")
	b := filepath.Join(dir, "b")
	if err := os.Link(a, b); err != nil {
		t.Skipf("hard links unsupported here: %v", err)
	}

	nodeA := GetNodeId(a, stat(a))
	nodeB := GetNodeId(b, stat(b))
	nodeC := GetNodeId(c, stat(c))

	if got := NodeLinksCount(nodeA); got < 2 {
		t.Errorf("link count of hard-linked file: got %d, want >= 2", got)
	}
	if nodeA != nodeB {
		t.Errorf("hard links have different NodeIds: %+v vs %+v", nodeA, nodeB)
	}
	if nodeA == nodeC {
		t.Errorf("independent files share a NodeId: %+v", nodeA)
	}
	if got := NodeLinksCount(nodeC); got != 1 {
		t.Errorf("link count of single-link file: got %d, want 1", got)
	}

	if size := PhysicalSize(a, stat(a)); size <= 0 {
		t.Errorf("PhysicalSize of a 64KB file: got %d, want > 0", size)
	}
}
