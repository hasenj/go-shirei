package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func gitTestRepo(t *testing.T) (repo string, run func(args ...string)) {
	t.Helper()
	repo = t.TempDir()
	run = func(args ...string) {
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
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "test")
	return repo, run
}

func writeFile(t *testing.T, repo, rel, content string) {
	t.Helper()
	path := filepath.Join(repo, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func headHash(t *testing.T, repo string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", repo, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

func loadGoPatch(t *testing.T, repo, hash string) *DiffDoc {
	t.Helper()
	clearRepoGates()
	doc, err := loadCommitMetaGo(repo, hash)
	if err != nil {
		t.Fatal(err)
	}
	if err := loadCommitPatchIntoGo(context.Background(), repo, hash, doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestPureGoPatchModify(t *testing.T) {
	repo, run := gitTestRepo(t)
	writeFile(t, repo, "a.txt", "one\ntwo\nthree\n")
	run("add", "a.txt")
	run("commit", "-m", "init")
	writeFile(t, repo, "a.txt", "one\nTWO\nthree\n")
	run("add", "a.txt")
	run("commit", "-m", "modify")

	doc := loadGoPatch(t, repo, headHash(t, repo))
	if len(doc.Segs) != 1 || doc.Segs[0].Path != "a.txt" {
		t.Fatalf("segs = %+v", doc.Segs)
	}
	var sawDel, sawAdd bool
	for _, r := range doc.Rows {
		if r.Kind == RowDel && strings.Contains(r.Text, "two") {
			sawDel = true
		}
		if r.Kind == RowAdd && strings.Contains(r.Text, "TWO") {
			sawAdd = true
		}
	}
	if !sawDel || !sawAdd {
		t.Fatalf("missing add/del: %#v", doc.Rows)
	}
}

func TestPureGoPatchAddDelete(t *testing.T) {
	repo, run := gitTestRepo(t)
	writeFile(t, repo, "keep.txt", "k\n")
	run("add", "keep.txt")
	run("commit", "-m", "init")
	writeFile(t, repo, "new.txt", "hello\n")
	run("add", "new.txt")
	run("rm", "keep.txt")
	run("commit", "-m", "add and delete")

	doc := loadGoPatch(t, repo, headHash(t, repo))
	paths := map[string]bool{}
	for _, s := range doc.Segs {
		paths[s.Path] = true
	}
	if !paths["new.txt"] || !paths["keep.txt"] {
		t.Fatalf("paths = %v", paths)
	}
}

func TestPureGoPatchRenameExact(t *testing.T) {
	repo, run := gitTestRepo(t)
	writeFile(t, repo, "old.txt", "same-content-bytes\n")
	run("add", "old.txt")
	run("commit", "-m", "init")
	run("mv", "old.txt", "new.txt")
	run("commit", "-m", "rename")

	doc := loadGoPatch(t, repo, headHash(t, repo))
	var renameHeader string
	for _, r := range doc.Rows {
		if r.Kind == RowFileHeader && strings.Contains(r.Text, "→") {
			renameHeader = r.Text
			break
		}
	}
	if renameHeader == "" {
		// exact rename may appear as delete+add if score path differs; accept either shape
		var sawOld, sawNew bool
		for _, r := range doc.Rows {
			if r.Kind == RowFileHeader {
				if r.Text == "old.txt" {
					sawOld = true
				}
				if r.Text == "new.txt" {
					sawNew = true
				}
			}
		}
		if !(sawOld && sawNew) {
			t.Fatalf("want rename header or old+new, rows=%#v", doc.Rows)
		}
		return
	}
	if !strings.Contains(renameHeader, "old.txt") || !strings.Contains(renameHeader, "new.txt") {
		t.Fatalf("rename header = %q", renameHeader)
	}
}

func TestPureGoPatchBinary(t *testing.T) {
	repo, run := gitTestRepo(t)
	writeFile(t, repo, "a.txt", "x\n")
	run("add", "a.txt")
	run("commit", "-m", "init")
	if err := os.WriteFile(filepath.Join(repo, "blob.bin"), []byte{0x00, 0x01, 0x02}, 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "blob.bin")
	run("commit", "-m", "bin")

	doc := loadGoPatch(t, repo, headHash(t, repo))
	var sawMeta bool
	for _, r := range doc.Rows {
		if r.Kind == RowMeta && strings.Contains(strings.ToLower(r.Text), "binary") {
			sawMeta = true
		}
	}
	if !sawMeta {
		t.Fatalf("want binary meta, rows=%#v", doc.Rows)
	}
}

func TestPureGoPatchLargeWasmSkipsPayload(t *testing.T) {
	repo, run := gitTestRepo(t)
	writeFile(t, repo, "keep.txt", "x\n")
	run("add", "keep.txt")
	run("commit", "-m", "init")

	wasm := make([]byte, 2<<20) // 2 MiB; old path would inflate this then Myers-skip
	copy(wasm, []byte{0x00, 0x61, 0x73, 0x6d})
	if err := os.WriteFile(filepath.Join(repo, "app.wasm"), wasm, 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "app.wasm")
	run("commit", "-m", "wasm")
	hash := headHash(t, repo)

	clearRepoGates()
	start := time.Now()
	snaps, err := snapshotFirstParentChanges(context.Background(), repo, hash)
	if err != nil {
		t.Fatal(err)
	}
	snapDur := time.Since(start)
	var wasmSnap fileSnap
	var found bool
	for _, s := range snaps {
		if strings.Contains(s.label, "app.wasm") {
			wasmSnap = s
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("no wasm snap in %#v", snaps)
	}
	if !wasmSnap.toBin {
		t.Fatalf("wasm not marked binary: %+v", wasmSnap)
	}
	if len(wasmSnap.to) != 0 || len(wasmSnap.from) != 0 {
		t.Fatalf("binary snapshot kept payload: from=%d to=%d", len(wasmSnap.from), len(wasmSnap.to))
	}

	doc := loadGoPatch(t, repo, hash)
	var sawMeta bool
	for _, r := range doc.Rows {
		if r.Kind == RowMeta && strings.Contains(strings.ToLower(r.Text), "binary") {
			sawMeta = true
		}
		if r.Kind == RowAdd || r.Kind == RowDel {
			t.Fatalf("wasm should not be line-diffed, got %#v", r)
		}
	}
	if !sawMeta {
		t.Fatalf("want binary meta, rows=%#v", doc.Rows)
	}
	if snapDur > 400*time.Millisecond {
		t.Fatalf("wasm snapshot took %v (payload still being read?)", snapDur)
	}
}

func TestPureGoPatchImageWithoutNul(t *testing.T) {
	repo, run := gitTestRepo(t)
	writeFile(t, repo, "keep.txt", "x\n")
	run("add", "keep.txt")
	run("commit", "-m", "init")
	// No NUL, almost no newlines — Myers on this as text would hit the 2s cap.
	jpeg := bytes.Repeat([]byte("JFIF"), 256*1024) // 1 MiB
	if err := os.WriteFile(filepath.Join(repo, "pic.jpg"), jpeg, 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "pic.jpg")
	run("commit", "-m", "jpeg")

	start := time.Now()
	doc := loadGoPatch(t, repo, headHash(t, repo))
	elapsed := time.Since(start)
	var sawImage bool
	for _, r := range doc.Rows {
		if r.Kind == RowImage {
			sawImage = true
		}
		if r.Kind == RowAdd || r.Kind == RowDel {
			t.Fatalf("jpeg should not be line-diffed, got %#v", r)
		}
	}
	if !sawImage {
		t.Fatalf("want RowImage, rows=%#v", doc.Rows)
	}
	if elapsed > 400*time.Millisecond {
		t.Fatalf("jpeg patch took %v (treated as text?)", elapsed)
	}
}

func TestPureGoPatchRootCommit(t *testing.T) {
	repo, run := gitTestRepo(t)
	writeFile(t, repo, "root.txt", "hello\n")
	run("add", "root.txt")
	run("commit", "-m", "root")

	doc := loadGoPatch(t, repo, headHash(t, repo))
	if len(doc.Segs) < 1 {
		t.Fatalf("want at least root.txt, segs=%+v rows=%#v", doc.Segs, doc.Rows)
	}
	var sawAdd bool
	for _, r := range doc.Rows {
		if r.Kind == RowAdd && strings.Contains(r.Text, "hello") {
			sawAdd = true
		}
	}
	if !sawAdd {
		t.Fatalf("root add missing: %#v", doc.Rows)
	}
}

func TestPureGoPatchEmptyFile(t *testing.T) {
	repo, run := gitTestRepo(t)
	writeFile(t, repo, "a.txt", "x\n")
	run("add", "a.txt")
	run("commit", "-m", "init")
	writeFile(t, repo, "empty.txt", "")
	run("add", "empty.txt")
	run("commit", "-m", "empty")

	doc := loadGoPatch(t, repo, headHash(t, repo))
	var sawHeader bool
	for _, r := range doc.Rows {
		if r.Kind == RowFileHeader && r.Text == "empty.txt" {
			sawHeader = true
		}
	}
	if !sawHeader {
		t.Fatalf("empty file header missing: %#v", doc.Rows)
	}
}

func TestFormatUnifiedFileBasic(t *testing.T) {
	u := formatUnifiedFile("f.go", "a\nb\n", "a\nc\n", perFileDiffTimeout)
	if !strings.Contains(u, "diff --git") || !strings.Contains(u, "@@") {
		t.Fatalf("unified = %q", u)
	}
	rows := parsePatch(u)
	if len(rows) == 0 {
		t.Fatal("parse empty")
	}
}

func TestFormatUnifiedFileHunkContextNotWholeFile(t *testing.T) {
	// 20 stable lines, one change in the middle — must not emit all 20 as context.
	var oldB, newB strings.Builder
	for i := 1; i <= 20; i++ {
		fmt.Fprintf(&oldB, "line %d\n", i)
		if i == 10 {
			fmt.Fprintf(&newB, "line %d CHANGED\n", i)
		} else {
			fmt.Fprintf(&newB, "line %d\n", i)
		}
	}
	u := formatUnifiedFile("big.txt", oldB.String(), newB.String(), perFileDiffTimeout)
	// Only one hunk expected; body lines should be ~1 change + 2*context, not 20+.
	hunks := strings.Count(u, "\n@@ ")
	if !strings.Contains(u, "@@ ") {
		t.Fatalf("no hunk:\n%s", u)
	}
	// header line is "@@ -…"; count of hunk starts
	nHunk := 0
	for _, line := range strings.Split(u, "\n") {
		if strings.HasPrefix(line, "@@ ") {
			nHunk++
		}
	}
	if nHunk != 1 {
		t.Fatalf("hunks=%d (raw @@ count=%d)\n%s", nHunk, hunks, u)
	}
	// Count prefixed body lines after the hunk header.
	bodyLines := 0
	inHunk := false
	for _, line := range strings.Split(u, "\n") {
		if strings.HasPrefix(line, "@@") {
			inHunk = true
			continue
		}
		if !inHunk || line == "" {
			continue
		}
		if line[0] == ' ' || line[0] == '+' || line[0] == '-' {
			bodyLines++
		}
	}
	// 3 context before + 1 del + 1 add + 3 context after = 8
	if bodyLines > 12 || bodyLines < 5 {
		t.Fatalf("bodyLines=%d (want ~8 with context=%d)\n%s", bodyLines, unifiedContext, u)
	}
	if strings.Contains(u, "line 1\n") && strings.Contains(u, "line 20\n") {
		// both ends only OK if context reaches them (change near edge); change is at 10
		if strings.Count(u, "line 1\n") > 0 && strings.Contains(u, "+line 1\n") {
			t.Fatalf("unexpected full-file style output:\n%s", u)
		}
	}
	// line 1 should not appear (far from change at 10 with ctx 3)
	if strings.Contains(u, " line 1\n") || strings.Contains(u, "\n line 1\n") {
		t.Fatalf("line 1 should be omitted as distant context:\n%s", u)
	}
}

func TestStreamCommitPatchGoOrder(t *testing.T) {
	repo, run := gitTestRepo(t)
	writeFile(t, repo, "a.txt", "1\n")
	writeFile(t, repo, "b.txt", "2\n")
	run("add", ".")
	run("commit", "-m", "init")
	writeFile(t, repo, "a.txt", "1\nA\n")
	writeFile(t, repo, "b.txt", "2\nB\n")
	run("add", ".")
	run("commit", "-m", "edit both")
	hash := headHash(t, repo)
	clearRepoGates()

	var batches [][]DiffRow
	var sawDone bool
	err := streamCommitPatchGo(context.Background(), repo, hash, func(batch []DiffRow, done bool) bool {
		if len(batch) > 0 {
			cp := append([]DiffRow(nil), batch...)
			batches = append(batches, cp)
		}
		if done {
			sawDone = true
		}
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	if !sawDone {
		t.Fatal("expected done")
	}
	// First non-empty batch of each file should start with a header.
	headers := 0
	for _, b := range batches {
		if len(b) == 1 && b[0].Kind == RowFileHeader {
			headers++
		}
	}
	if headers < 2 {
		t.Fatalf("want ≥2 header-only flushes, got %d batches=%#v", headers, batches)
	}
}
