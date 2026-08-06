package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGlobFilters checks include/exclude filename globs over the fixture tree.
func TestGlobFilters(t *testing.T) {
	// include only .go files
	inc := searchSync(Params{Root: "testdata/sample", Query: "func", Include: "*.go"})
	if inc.filesMatched.Load() == 0 {
		t.Fatal("expected matches in .go files")
	}
	for _, m := range inc.matches {
		if !strings.HasSuffix(m.File.RelPath, ".go") {
			t.Errorf("include *.go matched a non-.go file: %s", m.File.RelPath)
		}
	}

	// exclude .go files
	exc := searchSync(Params{Root: "testdata/sample", Query: "hello", Exclude: "*.go"})
	if exc.matchCount.Load() == 0 {
		t.Fatal("expected non-.go matches for hello")
	}
	for _, m := range exc.matches {
		if strings.HasSuffix(m.File.RelPath, ".go") {
			t.Errorf("exclude *.go still searched: %s", m.File.RelPath)
		}
	}
}

// TestGitignore checks .gitignore handling through the haystack search path
// (engine coverage also lives in go.hasen.dev/textsearch).
func TestGitignore(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, content string) {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	write(".gitignore", "*.log\ndist/\n!keep.log\n")
	write("src/.gitignore", "b.txt\n")
	write("a.txt", "needle")
	write("app.log", "needle")  // ignored by *.log
	write("keep.log", "needle") // re-included by !keep.log
	write("dist/gen.txt", "needle")
	write("src/b.txt", "needle") // ignored by src/.gitignore
	write("src/c.txt", "needle")

	s := searchSync(Params{Root: dir, Query: "needle", Gitignore: true})
	got := map[string]bool{}
	for _, m := range s.matches {
		got[filepath.ToSlash(m.File.RelPath)] = true
	}

	for _, w := range []string{"a.txt", "keep.log", "src/c.txt"} {
		if !got[w] {
			t.Errorf("%s should have been searched", w)
		}
	}
	for _, nw := range []string{"app.log", "dist/gen.txt", "src/b.txt"} {
		if got[nw] {
			t.Errorf("%s should have been gitignored", nw)
		}
	}

	// without the flag, everything is searched
	all := searchSync(Params{Root: dir, Query: "needle"})
	if all.filesMatched.Load() <= s.filesMatched.Load() {
		t.Errorf("gitignore off should search more files than on (%d vs %d)",
			all.filesMatched.Load(), s.filesMatched.Load())
	}
}
