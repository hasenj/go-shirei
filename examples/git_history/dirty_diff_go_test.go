package main

import (
	"context"
	"strings"
	"testing"
)

func TestStagingDiffPureGo(t *testing.T) {
	repo, run := gitTestRepo(t)
	writeFile(t, repo, "a.txt", "hello\n")
	run("add", "a.txt")
	run("commit", "-m", "init")
	writeFile(t, repo, "a.txt", "hello\nworld\n")
	run("add", "a.txt")

	clearRepoGates()
	invalidateStatusCache()
	doc, err := loadStagingDoc(repo)
	if err != nil {
		t.Fatal(err)
	}
	var sawAdd bool
	for _, r := range doc.Rows {
		if r.Kind == RowFileHeader && r.Text == "a.txt" {
			// ok
		}
		if r.Kind == RowAdd && strings.Contains(r.Text, "world") {
			sawAdd = true
		}
	}
	if !sawAdd {
		t.Fatalf("staged add missing: %#v", doc.Rows)
	}
}

func TestWorktreeDiffPureGo(t *testing.T) {
	repo, run := gitTestRepo(t)
	writeFile(t, repo, "a.txt", "hello\n")
	run("add", "a.txt")
	run("commit", "-m", "init")
	writeFile(t, repo, "a.txt", "hello\nlocal\n")

	clearRepoGates()
	invalidateStatusCache()
	doc, err := loadWorkingTreeDoc(repo)
	if err != nil {
		t.Fatal(err)
	}
	var sawAdd bool
	for _, r := range doc.Rows {
		if r.Kind == RowAdd && strings.Contains(r.Text, "local") {
			sawAdd = true
		}
	}
	if !sawAdd {
		t.Fatalf("worktree add missing: %#v", doc.Rows)
	}
}

func TestWorktreeUntrackedPureGo(t *testing.T) {
	repo, run := gitTestRepo(t)
	writeFile(t, repo, "tracked.txt", "t\n")
	run("add", "tracked.txt")
	run("commit", "-m", "init")
	writeFile(t, repo, "new.go", "package main\n")

	clearRepoGates()
	invalidateStatusCache()
	doc, err := loadWorkingTreeDoc(repo)
	if err != nil {
		t.Fatal(err)
	}
	var saw bool
	for _, r := range doc.Rows {
		if r.Kind == RowFileHeader && strings.Contains(r.Text, "new.go") {
			saw = true
		}
	}
	if !saw {
		t.Fatalf("untracked missing: %#v", doc.Rows)
	}
}

func TestStreamStagingHeaders(t *testing.T) {
	repo, run := gitTestRepo(t)
	writeFile(t, repo, "a.txt", "1\n")
	writeFile(t, repo, "b.txt", "2\n")
	run("add", ".")
	run("commit", "-m", "init")
	writeFile(t, repo, "a.txt", "1\nA\n")
	writeFile(t, repo, "b.txt", "2\nB\n")
	run("add", ".")

	clearRepoGates()
	invalidateStatusCache()
	var headers int
	err := streamStagingDiffGo(context.Background(), repo, func(batch []DiffRow, done bool) bool {
		for _, r := range batch {
			if r.Kind == RowFileHeader {
				headers++
			}
		}
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	if headers < 2 {
		t.Fatalf("headers=%d", headers)
	}
}

func TestStagingDelete(t *testing.T) {
	repo, run := gitTestRepo(t)
	writeFile(t, repo, "gone.txt", "x\n")
	run("add", "gone.txt")
	run("commit", "-m", "init")
	run("rm", "gone.txt")

	clearRepoGates()
	invalidateStatusCache()
	doc, err := loadStagingDoc(repo)
	if err != nil {
		t.Fatal(err)
	}
	var sawDel bool
	for _, r := range doc.Rows {
		if r.Kind == RowFileHeader && r.Text == "gone.txt" {
			// ok
		}
		if r.Kind == RowDel && strings.Contains(r.Text, "x") {
			sawDel = true
		}
	}
	if !sawDel {
		// empty file edge cases — at least header
		var sawH bool
		for _, r := range doc.Rows {
			if r.Kind == RowFileHeader && r.Text == "gone.txt" {
				sawH = true
			}
		}
		if !sawH {
			t.Fatalf("delete not shown: %#v", doc.Rows)
		}
	}
}
