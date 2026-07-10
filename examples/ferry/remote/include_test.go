package remote

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCollectAliasesFollowsIncludes exercises the enumeration walk over a
// realistic include layout: a glob include, a direct include, a missing
// include, an include cycle, a dedup, and skipped wildcard/negation
// patterns. It targets collectAliases (not EnumerateHosts) so it doesn't
// depend on `ssh -G` resolution.
func TestCollectAliasesFollowsIncludes(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, body string) {
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// conf.d/*.cfg — glob include, lexical order (10 before 20)
	write("conf.d/10-a.cfg", "Host inc-a\n  HostName 10.0.0.1\n")
	write("conf.d/20-b.cfg", "Host inc-b\n  HostName 10.0.0.2\nHost *\n  User z\n") // wildcard skipped
	// a direct include that re-declares inc-a (dedup) and includes back to
	// the top file (cycle guard must stop it)
	write("direct.cfg", "Host direct-one\n  HostName 10.0.0.3\nHost inc-a\n  User dup\nInclude top.cfg\n")
	// relative include path is taken against the including file's dir
	write("top.cfg", "Include conf.d/*.cfg\nInclude direct.cfg\nInclude missing.cfg\n"+
		"Host top-one\n  HostName 10.0.0.9\nHost !nope pat*\n  User w\n")

	var aliases []string
	err := collectAliases(filepath.Join(dir, "top.cfg"), true,
		map[string]bool{}, map[string]bool{}, &aliases)
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"inc-a", "inc-b", "direct-one", "top-one"}
	if len(aliases) != len(want) {
		t.Fatalf("aliases = %v, want %v", aliases, want)
	}
	for i := range want {
		if aliases[i] != want[i] {
			t.Fatalf("aliases = %v, want %v", aliases, want)
		}
	}
}

// TestCollectAliasesMissingTopIsError: a missing TOP-level file must
// error (unlike a missing include, which is skipped).
func TestCollectAliasesMissingTopIsError(t *testing.T) {
	var aliases []string
	err := collectAliases(filepath.Join(t.TempDir(), "nope.cfg"), true,
		map[string]bool{}, map[string]bool{}, &aliases)
	if err == nil {
		t.Fatal("a missing top-level config must be an error")
	}
}

// TestEnumerateHostsWithInclude is the end-to-end path (through ssh -G):
// an alias defined only in an included file must appear AND resolve.
func TestEnumerateHostsWithInclude(t *testing.T) {
	dir := t.TempDir()
	inc := filepath.Join(dir, "inc.cfg")
	top := filepath.Join(dir, "config")
	if err := os.WriteFile(inc, []byte("Host boxinc\n  HostName 127.0.0.1\n  Port 2299\n  User u\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(top, []byte("Include "+inc+"\nHost boxtop\n  HostName 127.0.0.1\n  Port 2298\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	hosts, err := EnumerateHosts(top)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]Host{}
	for _, h := range hosts {
		got[h.Alias] = h
	}
	if _, ok := got["boxtop"]; !ok {
		t.Error("top-level host missing")
	}
	inch, ok := got["boxinc"]
	if !ok {
		t.Fatal("included host was not enumerated")
	}
	// and ssh -G actually resolved the included host's settings
	if inch.Port != "2299" || inch.Hostname != "127.0.0.1" {
		t.Errorf("included host mis-resolved: %+v", inch)
	}
}
