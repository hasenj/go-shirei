package remote

import (
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"go.hasen.dev/shirei/examples/ferry/remote/remotetest"
)

func dialFixture(t *testing.T) *Conn {
	t.Helper()
	fx := remotetest.StartSSHD(t)
	host := Host{
		Alias:         "fixture",
		Hostname:      fx.Hostname,
		User:          fx.User,
		Port:          fx.Port,
		IdentityFiles: []string{fx.IdentityFile},
	}
	conn, err := Dial(host, DialOptions{
		KnownHostsPath: fx.KnownHosts,
		AcceptHostKey:  func(string, ssh.PublicKey) bool { return true },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

// --- tree helpers -----------------------------------------------------

// writeTree materializes rel-path → content. A "-> target" content makes
// a symlink.
func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if target, isLink := strings.CutPrefix(content, "-> "); isLink {
			if err := os.Symlink(target, p); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func readTree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		if d.Type()&fs.ModeSymlink != 0 {
			target, err := os.Readlink(p)
			if err != nil {
				return err
			}
			out[filepath.ToSlash(rel)] = "-> " + target
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = string(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func expectTree(t *testing.T, root string, want map[string]string) {
	t.Helper()
	got := readTree(t, root)
	if !maps.Equal(got, want) {
		t.Errorf("tree mismatch at %s:\n got: %#v\nwant: %#v", root, got, want)
	}
}

// expectClean asserts no ferry stage or aside directories were left behind.
func expectClean(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".ferry-stage-") || strings.HasPrefix(e.Name(), ".ferry-old-") {
			t.Errorf("leftover %s in %s", e.Name(), dir)
		}
	}
}
