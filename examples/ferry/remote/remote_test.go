package remote

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestEnumerateHosts(t *testing.T) {
	dir := t.TempDir()
	config := filepath.Join(dir, "config")
	os.WriteFile(config, []byte(`
Host web db-*
  User alice

Host db
  HostName 10.0.0.5
  Port 2222
`), 0o600)

	hosts, err := EnumerateHosts(config)
	if err != nil {
		t.Fatal(err)
	}
	if len(hosts) != 2 {
		t.Fatalf("want 2 hosts (wildcard skipped), got %+v", hosts)
	}
	web, db := hosts[0], hosts[1]
	if web.Alias != "web" || web.Hostname != "web" || web.User != "alice" || web.Port != "22" {
		t.Errorf("web resolved wrong: %+v", web)
	}
	if db.Alias != "db" || db.Hostname != "10.0.0.5" || db.Port != "2222" {
		t.Errorf("db resolved wrong: %+v", db)
	}
}

func TestBrowse(t *testing.T) {
	conn := dialFixture(t)
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"notes.txt":  "0123456789",
		"sub/x.bin":  "xx",
		"other/y.md": "yy",
	})

	entries, err := conn.List(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		name := e.Name
		if e.IsDir() {
			name += "/"
		}
		names = append(names, name)
	}
	want := []string{"notes.txt", "other/", "sub/"}
	if len(names) != 3 || names[0] != want[0] || names[1] != want[1] || names[2] != want[2] {
		t.Errorf("List = %v, want %v", names, want)
	}
	if entries[0].Size != 10 {
		t.Errorf("notes.txt size = %d, want 10", entries[0].Size)
	}

	head, err := conn.ReadHead(filepath.Join(dir, "notes.txt"), 4)
	if err != nil || string(head) != "0123" {
		t.Errorf("ReadHead(4) = %q, %v", head, err)
	}
	full, err := conn.ReadHead(filepath.Join(dir, "notes.txt"), 100)
	if err != nil || string(full) != "0123456789" {
		t.Errorf("ReadHead(100) = %q, %v", full, err)
	}
}

// srcTree is the standard upload/download payload: nesting, an empty-ish
// dir via sub, a symlink, and an executable.
var srcTree = map[string]string{
	"a.txt":      "alpha",
	"sub/b.txt":  "bravo",
	"sub/deep/c": "charlie",
	"link.txt":   "-> a.txt",
	"bin/run.sh": "#!/bin/sh\necho hi\n",
}

func setupSrc(t *testing.T) (parent, dir string) {
	parent = t.TempDir()
	dir = filepath.Join(parent, "data")
	writeTree(t, dir, srcTree)
	os.Chmod(filepath.Join(dir, "bin/run.sh"), 0o755)
	return
}

func prefixed(prefix string, tree map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range tree {
		out[prefix+k] = v
	}
	return out
}

func TestUploadFresh(t *testing.T) {
	conn := dialFixture(t)
	_, src := setupSrc(t)
	dest := t.TempDir()

	err := conn.Upload(context.Background(), []string{src}, dest, StrategyFail, nil)
	if err != nil {
		t.Fatal(err)
	}
	expectTree(t, dest, prefixed("data/", srcTree))
	expectClean(t, dest)

	info, err := os.Stat(filepath.Join(dest, "data/bin/run.sh"))
	if err != nil || info.Mode().Perm() != 0o755 {
		t.Errorf("exec bit lost: %v %v", info, err)
	}
}

func TestUploadConflictFail(t *testing.T) {
	conn := dialFixture(t)
	_, src := setupSrc(t)
	dest := t.TempDir()
	writeTree(t, dest, map[string]string{"data/old.txt": "old"})

	err := conn.Upload(context.Background(), []string{src}, dest, StrategyFail, nil)
	var conflict *ConflictError
	if !errors.As(err, &conflict) || len(conflict.Names) != 1 || conflict.Names[0] != "data" {
		t.Fatalf("want ConflictError on data, got %v", err)
	}
	expectTree(t, dest, map[string]string{"data/old.txt": "old"})
	expectClean(t, dest)
}

func TestUploadMerge(t *testing.T) {
	conn := dialFixture(t)
	_, src := setupSrc(t)
	dest := t.TempDir()
	writeTree(t, dest, map[string]string{
		"data/a.txt":    "stale alpha",
		"data/keep.txt": "keep me",
	})

	err := conn.Upload(context.Background(), []string{src}, dest, StrategyMerge, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := prefixed("data/", srcTree)
	want["data/keep.txt"] = "keep me"
	expectTree(t, dest, want)
	expectClean(t, dest)
}

func TestUploadReplace(t *testing.T) {
	conn := dialFixture(t)
	_, src := setupSrc(t)
	dest := t.TempDir()
	writeTree(t, dest, map[string]string{
		"data/a.txt":    "stale alpha",
		"data/keep.txt": "gone after replace",
	})

	err := conn.Upload(context.Background(), []string{src}, dest, StrategyReplace, nil)
	if err != nil {
		t.Fatal(err)
	}
	expectTree(t, dest, prefixed("data/", srcTree))
	expectClean(t, dest)
}

func TestUploadCancelLeavesDestUntouched(t *testing.T) {
	conn := dialFixture(t)
	srcParent := t.TempDir()
	big := filepath.Join(srcParent, "big.dat")
	f, err := os.Create(big)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(48 << 20); err != nil { // 48MB sparse: reads as zeros
		t.Fatal(err)
	}
	f.Close()
	dest := t.TempDir()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var called bool
	progress := func(done, total int64) {
		called = true
		if done > total/4 {
			cancel()
		}
	}
	err = conn.Upload(ctx, []string{big}, dest, StrategyFail, progress)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
	if !called {
		t.Error("progress callback never ran")
	}
	expectTree(t, dest, map[string]string{})
	expectClean(t, dest)
}

func TestDownloadFresh(t *testing.T) {
	conn := dialFixture(t)
	srcParent, _ := setupSrc(t)
	dest := t.TempDir()

	err := conn.Download(context.Background(), srcParent, []string{"data"}, dest, StrategyFail, nil)
	if err != nil {
		t.Fatal(err)
	}
	expectTree(t, dest, prefixed("data/", srcTree))
	expectClean(t, dest)
}

func TestDownloadConflictFail(t *testing.T) {
	conn := dialFixture(t)
	srcParent, _ := setupSrc(t)
	dest := t.TempDir()
	writeTree(t, dest, map[string]string{"data/old.txt": "old"})

	err := conn.Download(context.Background(), srcParent, []string{"data"}, dest, StrategyFail, nil)
	var conflict *ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("want ConflictError, got %v", err)
	}
	expectTree(t, dest, map[string]string{"data/old.txt": "old"})
	expectClean(t, dest)
}

func TestDownloadMerge(t *testing.T) {
	conn := dialFixture(t)
	srcParent, _ := setupSrc(t)
	dest := t.TempDir()
	writeTree(t, dest, map[string]string{
		"data/a.txt":    "stale alpha",
		"data/keep.txt": "keep me",
	})

	err := conn.Download(context.Background(), srcParent, []string{"data"}, dest, StrategyMerge, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := prefixed("data/", srcTree)
	want["data/keep.txt"] = "keep me"
	expectTree(t, dest, want)
	expectClean(t, dest)
}

func TestDownloadReplace(t *testing.T) {
	conn := dialFixture(t)
	srcParent, _ := setupSrc(t)
	dest := t.TempDir()
	writeTree(t, dest, map[string]string{
		"data/keep.txt": "gone after replace",
	})

	err := conn.Download(context.Background(), srcParent, []string{"data"}, dest, StrategyReplace, nil)
	if err != nil {
		t.Fatal(err)
	}
	expectTree(t, dest, prefixed("data/", srcTree))
	expectClean(t, dest)
}

func TestDownloadCancelLeavesDestUntouched(t *testing.T) {
	conn := dialFixture(t)
	srcParent := t.TempDir()
	big := filepath.Join(srcParent, "big.dat")
	f, err := os.Create(big)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(48 << 20); err != nil {
		t.Fatal(err)
	}
	f.Close()
	dest := t.TempDir()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	progress := func(done, total int64) {
		if done > total/4 {
			cancel()
		}
	}
	err = conn.Download(ctx, srcParent, []string{"big.dat"}, dest, StrategyFail, progress)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
	expectTree(t, dest, map[string]string{})
	expectClean(t, dest)
}

// Filenames with spaces and quotes must survive the commit scripts.
func TestUploadAwkwardNames(t *testing.T) {
	conn := dialFixture(t)
	parent := t.TempDir()
	src := filepath.Join(parent, "my stuff")
	tree := map[string]string{
		"it's here.txt":   "quoted",
		"two  spaces/x y": "spaced",
	}
	writeTree(t, src, tree)
	dest := t.TempDir()
	writeTree(t, dest, map[string]string{"my stuff/keep.txt": "keep"})

	err := conn.Upload(context.Background(), []string{src}, dest, StrategyMerge, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := prefixed("my stuff/", tree)
	want["my stuff/keep.txt"] = "keep"
	expectTree(t, dest, want)
	expectClean(t, dest)
}
