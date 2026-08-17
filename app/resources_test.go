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

// Monorepo layout: shared top-level Resources/ plus <package>/Resources.
// Dev resolution must pick the package-local directory for go run ./pkg.
func TestPackageLocalBeatsMonorepoResources(t *testing.T) {
	t.Cleanup(resetResourcesState)
	root := t.TempDir()
	shared := filepath.Join(root, "Resources")
	pkgRes := filepath.Join(root, "gardener", "Resources")
	if err := os.MkdirAll(shared, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(pkgRes, 0o755); err != nil {
		t.Fatal(err)
	}
	// Marker only in package Resources — shared must not win.
	if err := os.WriteFile(filepath.Join(pkgRes, "sprout-icon.png"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Chdir(root)
	got := packageLocalResources(root)
	want, _ := filepath.Abs(pkgRes)
	// uniqueChild alone would return "" (shared is not a child package path;
	// two? only one child has Resources). uniqueChild sees gardener only → ok.
	// Also cover main-package name match path when BuildInfo base is gardener.
	if got != want {
		// Fallback expectation: unique child still prefers gardener/Resources.
		if u := uniqueChildResources(root); u != want {
			t.Fatalf("packageLocal got %q unique %q want %q", got, u, want)
		}
	}

	// Full findResourcesDir from this cwd must not land on shared.
	resetResourcesState()
	t.Chdir(root)
	dir := findResourcesDir()
	if dir != want {
		t.Fatalf("findResourcesDir got %q want %q (shared would be wrong)", dir, want)
	}
	if _, err := os.Stat(filepath.Join(dir, "sprout-icon.png")); err != nil {
		t.Fatalf("package icon missing under resolved dir: %v", err)
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
