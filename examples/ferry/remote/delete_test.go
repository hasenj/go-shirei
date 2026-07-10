package remote

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDeleteRemote(t *testing.T) {
	conn := dialFixture(t)
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"plain.txt":            "goes away\n",
		"keep.txt":             "stays\n",
		"dir/inner.txt":        "recursive\n",
		"dir/deeper/leaf.txt":  "recursive too\n",
		"awk ward 'name'.txt":  "quoting\n",
		"unrelated/subtle.txt": "stays too\n",
	})

	err := conn.Delete([]string{
		filepath.Join(root, "plain.txt"),
		filepath.Join(root, "dir"),
		filepath.Join(root, "awk ward 'name'.txt"),
		filepath.Join(root, "already-gone.txt"), // idempotent: missing = deleted
	})
	if err != nil {
		t.Fatal(err)
	}

	expectTree(t, root, map[string]string{
		"keep.txt":             "stays\n",
		"unrelated/subtle.txt": "stays too\n",
	})
}

func TestDeleteRefusesAmbiguousPaths(t *testing.T) {
	conn := dialFixture(t)
	canary := filepath.Join(t.TempDir(), "canary.txt")
	if err := os.WriteFile(canary, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	// NOTE for future cases: never construct a "should be refused" path
	// from real directories — if the refusal logic has the very bug the
	// test hunts, the rm actually runs. Literals only.
	for _, bad := range [][]string{
		{"relative/path"},
		{"/"},
		{"/x/.."},   // cleans to "/"
		{"/var"},    // top-level: admin op, not file management
		{"/x/y/.."}, // cleans to top-level "/x"
		{""},
		{canary, "oops-relative"}, // one bad path poisons the whole batch
	} {
		if err := conn.Delete(bad); err == nil {
			t.Errorf("Delete(%q) should refuse", bad)
		}
	}
	if _, err := os.Stat(canary); err != nil {
		t.Fatalf("refused batches must delete NOTHING: %v", err)
	}
}
