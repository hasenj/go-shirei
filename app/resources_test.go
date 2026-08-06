package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResourcePathJoin(t *testing.T) {
	t.Cleanup(resetResourcesState)
	SetResourcesDir("/tmp/Resources")
	got := ResourcePath("icon.png")
	want := filepath.Join("/tmp/Resources", "icon.png")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestSetResourcesDirOverridesEnv(t *testing.T) {
	t.Cleanup(resetResourcesState)
	t.Setenv(resourcesEnvVar, filepath.Join(t.TempDir(), "from-env"))
	pin := t.TempDir()
	SetResourcesDir(pin)
	if got := ResourcesDir(); got != pin {
		t.Fatalf("pin should win: got %q want %q", got, pin)
	}
}

func TestResourcesDirFromEnv(t *testing.T) {
	t.Cleanup(resetResourcesState)
	dir := t.TempDir()
	t.Setenv(resourcesEnvVar, dir)
	if got := ResourcesDir(); got != dir {
		t.Fatalf("got %q want %q", got, dir)
	}
}

func TestMacOSAppResources(t *testing.T) {
	root := t.TempDir()
	appMacOS := filepath.Join(root, "MyApp.app", "Contents", "MacOS")
	res := filepath.Join(root, "MyApp.app", "Contents", "Resources")
	if err := os.MkdirAll(appMacOS, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(res, 0o755); err != nil {
		t.Fatal(err)
	}
	got, ok := macOSAppResources(appMacOS)
	if !ok {
		t.Fatal("expected macOS app layout")
	}
	if got != res {
		t.Fatalf("got %q want %q", got, res)
	}
}

func TestWalkParentsForResources(t *testing.T) {
	root := t.TempDir()
	res := filepath.Join(root, "Resources")
	deep := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(res, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	got := walkParentsForResources(deep)
	want, _ := filepath.Abs(res)
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestUniqueChildResources(t *testing.T) {
	root := t.TempDir()
	only := filepath.Join(root, "gardener", "Resources")
	if err := os.MkdirAll(only, 0o755); err != nil {
		t.Fatal(err)
	}
	got := uniqueChildResources(root)
	want, _ := filepath.Abs(only)
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}

	// Ambiguous: two children with Resources → empty
	other := filepath.Join(root, "other", "Resources")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := uniqueChildResources(root); got != "" {
		t.Fatalf("expected empty when ambiguous, got %q", got)
	}
}

func TestExeDirResources(t *testing.T) {
	t.Cleanup(resetResourcesState)
	// Pin via finding an existing sibling layout using SetResourcesDir after
	// constructing the tree — automatic exe probing needs a real executable
	// path; cover the helper instead.
	root := t.TempDir()
	res := filepath.Join(root, "Resources")
	if err := os.MkdirAll(res, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := existingDir(res); got == "" {
		t.Fatal("expected existing Resources dir")
	}
}

func resetResourcesState() {
	resourcesMu.Lock()
	resourcesOverride = ""
	resourcesCached = ""
	resourcesResolved = false
	resourcesMu.Unlock()
}
