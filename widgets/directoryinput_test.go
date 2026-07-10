package widgets

import (
	"os"
	"path/filepath"
	"testing"

	. "go.hasen.dev/shirei"
)

func TestPathStatus(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "Sub")
	if err := os.Mkdir(sub, 0755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "file.txt")
	if err := os.WriteFile(file, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	haveLink := os.Symlink(sub, link) == nil // symlinks may need privileges on Windows

	cases := []struct {
		path          string
		exists, isDir bool
		skip          bool
	}{
		{sub, true, true, false},
		{file, true, false, false},
		{filepath.Join(root, "nope"), false, false, false},
		// case-insensitive fallback (the macOS/Windows filesystem default)
		{filepath.Join(root, "sub"), true, true, false},
		// trailing separator is fine
		{sub + string(filepath.Separator), true, true, false},
		// a symlink to a directory counts as one
		{link, true, true, !haveLink},
		// filesystem root
		{filepath.VolumeName(root) + string(filepath.Separator), true, true, false},
		{"", false, false, false},
	}
	// DirListing state is shared with the watcher goroutine; tests take the
	// frame lock like a frame would
	for _, c := range cases {
		if c.skip {
			continue
		}
		var exists, isDir bool
		WithFrameLock(func() { exists, isDir = pathStatus(c.path) })
		if exists != c.exists || isDir != c.isDir {
			t.Errorf("pathStatus(%q): got (%v, %v), want (%v, %v)",
				c.path, exists, isDir, c.exists, c.isDir)
		}
	}
}
