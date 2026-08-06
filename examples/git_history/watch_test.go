package main

import (
	"path/filepath"
	"testing"

	"github.com/fsnotify/fsnotify"
)

func TestWatchPathNoisy(t *testing.T) {
	repo := "/tmp/repo"
	cases := []struct {
		name string
		want bool
	}{
		{filepath.Join(repo, ".git", "objects", "pack", "x.pack"), true},
		{filepath.Join(repo, ".git", "objects"), true},
		{filepath.Join(repo, ".git", "index.lock"), true},
		{filepath.Join(repo, ".git", "FETCH_HEAD"), true},
		{filepath.Join(repo, ".git", "HEAD"), false},
		{filepath.Join(repo, ".git", "index"), false},
		{filepath.Join(repo, ".git", "refs", "heads", "master"), false},
		{filepath.Join(repo, "main.go"), false},
		{filepath.Join(repo, ".DS_Store"), true},
		{filepath.Join(repo, "foo.go~"), true},
	}
	for _, tc := range cases {
		if got := watchPathNoisy(repo, tc.name); got != tc.want {
			t.Errorf("watchPathNoisy(%q)=%v want %v", tc.name, got, tc.want)
		}
	}
}

func TestWatchEventInterestingChmodOnly(t *testing.T) {
	repo := "/tmp/repo"
	ev := fsnotify.Event{Name: filepath.Join(repo, "a.go"), Op: fsnotify.Chmod}
	if watchEventInteresting(repo, ev) {
		t.Fatal("chmod-only should be ignored")
	}
	ev.Op = fsnotify.Write
	if !watchEventInteresting(repo, ev) {
		t.Fatal("write should be interesting")
	}
}

func TestResolveGitDir(t *testing.T) {
	// Integration: this monorepo when tests run inside it.
	cwd, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	repo, err := findRepo(cwd)
	if err != nil {
		t.Skip(err)
	}
	g, err := resolveGitDir(repo)
	if err != nil {
		t.Fatal(err)
	}
	if st, err := filepath.Abs(g); err != nil || st == "" {
		t.Fatalf("gitdir=%q err=%v", g, err)
	}
}
