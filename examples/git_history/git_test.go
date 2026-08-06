package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParseNumstat(t *testing.T) {
	in := "12\t3\tfoo.go\n-\t-\tblob.bin\n1\t0\tpath/with space.txt\n"
	got := parseNumstat(in)
	want := []FileStat{
		{Path: "foo.go", Added: 12, Deleted: 3},
		{Path: "blob.bin", Added: -1, Deleted: -1, Binary: true},
		{Path: "path/with space.txt", Added: 1, Deleted: 0},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseNumstat:\n got %#v\nwant %#v", got, want)
	}
}

func TestParsePatchBasic(t *testing.T) {
	in := `diff --git a/foo.go b/foo.go
index 111..222 100644
--- a/foo.go
+++ b/foo.go
@@ -1,3 +1,4 @@
 package main
-old
+new
 context
`
	rows := parsePatch(in)
	kinds := make([]DiffRowKind, len(rows))
	texts := make([]string, len(rows))
	for i, r := range rows {
		kinds[i] = r.Kind
		texts[i] = r.Text
	}
	if kinds[0] != RowFileHeader || texts[0] != "foo.go" {
		t.Fatalf("first row = %v %q, want file header foo.go", kinds[0], texts[0])
	}
	if kinds[1] != RowHunkHeader {
		t.Fatalf("second row kind = %v, want hunk", kinds[1])
	}
	// find add/del
	var sawAdd, sawDel bool
	for _, r := range rows {
		if r.Kind == RowAdd && r.Text == "+new" {
			sawAdd = true
		}
		if r.Kind == RowDel && r.Text == "-old" {
			sawDel = true
		}
	}
	if !sawAdd || !sawDel {
		t.Fatalf("missing add/del rows: %#v", rows)
	}
}

func TestParsePatchRename(t *testing.T) {
	in := `diff --git a/old.txt b/new.txt
similarity index 100%
rename from old.txt
rename to new.txt
`
	rows := parsePatch(in)
	if len(rows) == 0 || rows[0].Kind != RowFileHeader {
		t.Fatalf("want file header, got %#v", rows)
	}
	if rows[0].Text != "old.txt → new.txt" {
		t.Fatalf("rename label = %q", rows[0].Text)
	}
}

func TestParsePatchBinary(t *testing.T) {
	in := `diff --git a/x.bin b/x.bin
index 111..222 100644
Binary files a/x.bin and b/x.bin differ
`
	rows := parsePatch(in)
	var sawMeta bool
	for _, r := range rows {
		if r.Kind == RowMeta {
			sawMeta = true
		}
	}
	if !sawMeta {
		t.Fatalf("expected binary meta row, got %#v", rows)
	}
}

func TestParsePatchBinaryImage(t *testing.T) {
	in := `diff --git a/shot.png b/shot.png
index 111..222 100644
Binary files a/shot.png and b/shot.png differ
`
	rows := parsePatch(in)
	var sawImg bool
	for _, r := range rows {
		if r.Kind == RowImage && r.Text == "shot.png" {
			sawImg = true
		}
		if r.Kind == RowMeta {
			t.Fatalf("image path should not use RowMeta: %#v", rows)
		}
	}
	if !sawImg {
		t.Fatalf("expected RowImage for shot.png, got %#v", rows)
	}
}

func TestSplitNumstatAndPatch(t *testing.T) {
	in := "12\t3\tfoo.go\n1\t0\tbar.go\n\ndiff --git a/foo.go b/foo.go\n@@ -1 +1 @@\n-old\n+new\n"
	num, patch := splitNumstatAndPatch(in)
	stats := parseNumstat(num)
	if len(stats) != 2 || stats[0].Path != "foo.go" {
		t.Fatalf("numstat parse: %#v from %q", stats, num)
	}
	rows := parsePatch(patch)
	if len(rows) == 0 || rows[0].Kind != RowFileHeader {
		t.Fatalf("patch rows: %#v", rows)
	}
}

func TestParsePorcelain(t *testing.T) {
	in := "" +
		" M tracked.go\n" +
		"M  staged.go\n" +
		"MM both.go\n" +
		"R  old.txt -> new.txt\n" +
		"?? untracked.md\n" +
		"D  gone.go\n"
	got := parsePorcelain(in)
	if len(got) != 6 {
		t.Fatalf("lines = %d, want 6: %#v", len(got), got)
	}
	st := &repoStatus{lines: got}
	if !st.worktreeDirty() {
		t.Fatal("expected worktree dirty")
	}
	if !st.stagingDirty() {
		t.Fatal("expected staging dirty")
	}
	ut := st.untrackedPaths()
	if len(ut) != 1 || ut[0] != "untracked.md" {
		t.Fatalf("untracked = %v", ut)
	}
	// Staging-only should still be staging dirty, worktree clean.
	onlyStaged := parsePorcelain("M  only-staged.go\n")
	st2 := &repoStatus{lines: onlyStaged}
	if st2.worktreeDirty() {
		t.Fatal("staging-only should not mark worktree dirty")
	}
	if !st2.stagingDirty() {
		t.Fatal("staging-only should mark staging dirty")
	}
}

func TestUntrackedInWorkingTree(t *testing.T) {
	repo := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test",
			"GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=test",
			"GIT_COMMITTER_EMAIL=test@example.com",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init")
	// Need at least one commit so the repo is usable; tracked empty tree is fine.
	if err := os.WriteFile(filepath.Join(repo, "tracked.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "tracked.txt")
	run("commit", "-m", "init")

	// Untracked only — no unstaged tracked changes.
	if err := os.WriteFile(filepath.Join(repo, "new.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "blob.bin"), []byte{0x00, 0x01, 0x02}, 0o644); err != nil {
		t.Fatal(err)
	}

	dirty, err := workingTreeDirty(repo)
	if err != nil || !dirty {
		t.Fatalf("workingTreeDirty = %v, %v; want true", dirty, err)
	}
	entries, _, _, err := loadHistory(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 || entries[0].Kind != KindWorkingTree {
		t.Fatalf("want Working tree first, got %#v", entries)
	}

	doc, err := loadWorkingTreeDoc(repo)
	if err != nil {
		t.Fatal(err)
	}
	var sawText, sawBin bool
	for _, r := range doc.Rows {
		if r.Kind == RowFileHeader && strings.Contains(r.Text, "new.go") && strings.Contains(r.Text, "untracked") {
			sawText = true
		}
		if r.Kind == RowMeta && strings.Contains(r.Text, "Binary") {
			sawBin = true
		}
		if r.Kind == RowAdd && r.Text == "+package main" {
			// text content present as add
		}
	}
	if !sawText {
		t.Fatalf("missing untracked text file header in rows: %#v", doc.Rows)
	}
	if !sawBin {
		t.Fatalf("missing binary untracked notice in rows: %#v", doc.Rows)
	}
	// Stats should include both paths.
	if len(doc.Stats) < 2 {
		t.Fatalf("stats = %#v; want ≥2 untracked entries", doc.Stats)
	}
}

// Integration against this monorepo when tests run inside it.
func TestLoadHistoryAndHEAD(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	repo, err := findRepo(cwd)
	if err != nil {
		t.Skip("not inside a git repo:", err)
	}
	entries, after, hasMore, err := loadHistory(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("expected at least one history entry")
	}
	// Prefer a commit entry for the doc load (dirty slots may be empty of message).
	var commit *HistoryEntry
	for i := range entries {
		if entries[i].Kind == KindCommit {
			commit = &entries[i]
			break
		}
	}
	if commit == nil {
		t.Skip("no commits")
	}
	doc, err := loadDiffDoc(repo, *commit)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Subject == "" {
		t.Fatal("expected commit subject")
	}
	// Root commits and empty commits can have no rows; still expect stats or rows.
	if len(doc.Stats) == 0 && len(doc.Rows) == 0 {
		t.Log("empty patch for HEAD — unusual but allowed")
	}

	// Pagination: a full first page should advertise more; loading more should
	// not repeat the last hash of the first page.
	if hasMore {
		if after == "" {
			t.Fatal("hasMore but empty after cursor")
		}
		more, _, _, err := loadMoreHistory(repo, after)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range more {
			if e.ID == after {
				t.Fatalf("next page should not re-include cursor %s", after)
			}
		}
	}
}

