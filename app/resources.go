package app

import (
	"os"
	"path"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
)

// ResourcesDirName is the conventional directory for packaged app assets.
// In the source tree it lives next to package main (<package>/Resources).
// Desktop packages place the same directory next to the executable
// (macOS: Contents/Resources inside the .app).
const ResourcesDirName = "Resources"

// SHIREI_RESOURCES overrides automatic resolution when set (tests / odd layouts).
const resourcesEnvVar = "SHIREI_RESOURCES"

var (
	resourcesMu       sync.Mutex
	resourcesOverride string
	resourcesCached   string
	resourcesResolved bool
)

// SetResourcesDir pins the resources directory for this process. Pass an empty
// string to clear the pin and return to automatic resolution. Relative paths
// are made absolute against the process working directory.
func SetResourcesDir(dir string) {
	dir = strings.TrimSpace(dir)
	if dir != "" {
		if abs, err := filepath.Abs(dir); err == nil {
			dir = abs
		}
	}
	resourcesMu.Lock()
	resourcesOverride = dir
	resourcesCached = ""
	resourcesResolved = false
	resourcesMu.Unlock()
}

// ResourcesDir returns the absolute directory that holds packaged app assets,
// or "" if none can be found. Resolution order:
//
//  1. SetResourcesDir pin
//  2. SHIREI_RESOURCES environment variable
//  3. macOS .app: Contents/Resources next to the executable
//  4. <exeDir>/Resources when that directory exists
//  5. Dev probe from the working directory and executable directory (see below)
//
// Dev probe (covers `go run ./pkg` from a module or monorepo root):
// prefer a package-local Resources under the cwd (main package name match, or
// a unique child package), then walk parents for a Resources directory. The
// package-local step runs first so a shared monorepo-level Resources/ does not
// shadow <package>/Resources.
func ResourcesDir() string {
	resourcesMu.Lock()
	defer resourcesMu.Unlock()
	if resourcesOverride != "" {
		return resourcesOverride
	}
	if env := strings.TrimSpace(os.Getenv(resourcesEnvVar)); env != "" {
		if abs, err := filepath.Abs(env); err == nil {
			return abs
		}
		return env
	}
	if resourcesResolved {
		return resourcesCached
	}
	resourcesCached = findResourcesDir()
	resourcesResolved = true
	return resourcesCached
}

// ResourcePath joins name under ResourcesDir. When ResourcesDir is empty it
// returns name cleaned (caller typically still fails on open).
func ResourcePath(name string) string {
	root := ResourcesDir()
	if root == "" {
		return filepath.Clean(name)
	}
	return filepath.Join(root, name)
}

func findResourcesDir() string {
	exe, err := os.Executable()
	exeDir := ""
	if err == nil {
		exe, _ = filepath.EvalSymlinks(exe)
		exeDir = filepath.Dir(exe)
		if dir, ok := macOSAppResources(exeDir); ok {
			return dir
		}
		if dir := existingDir(filepath.Join(exeDir, ResourcesDirName)); dir != "" {
			return dir
		}
	}

	cwd, _ := os.Getwd()
	if cwd != "" {
		if dir := packageLocalResources(cwd); dir != "" {
			return dir
		}
	}
	for _, base := range []string{cwd, exeDir} {
		if base == "" {
			continue
		}
		if dir := walkParentsForResources(base); dir != "" {
			return dir
		}
	}
	return ""
}

// packageLocalResources finds Resources under an immediate child of parent that
// belongs to the running main package (go run ./pkg from a monorepo root), or
// the sole child package that has a Resources directory.
func packageLocalResources(parent string) string {
	if dir := mainPackageChildResources(parent); dir != "" {
		return dir
	}
	return uniqueChildResources(parent)
}

// mainPackageChildResources uses debug.BuildInfo's main package path base name
// (e.g. "gardener" from go.hasen.dev/gardener) to pick parent/<base>/Resources.
func mainPackageChildResources(parent string) string {
	bi, ok := debug.ReadBuildInfo()
	if !ok || bi.Path == "" {
		return ""
	}
	base := path.Base(bi.Path)
	if base == "" || base == "." || base == "/" {
		return ""
	}
	return existingDir(filepath.Join(parent, base, ResourcesDirName))
}

// macOSAppResources reports Contents/Resources when exeDir is .../App.app/Contents/MacOS.
func macOSAppResources(exeDir string) (string, bool) {
	// filepath uses OS separators; also accept forward slashes from odd mounts.
	norm := filepath.ToSlash(exeDir)
	if !strings.HasSuffix(norm, ".app/Contents/MacOS") {
		return "", false
	}
	contents := filepath.Dir(exeDir) // .../Contents
	dir := filepath.Join(contents, ResourcesDirName)
	if st, err := os.Stat(dir); err == nil && st.IsDir() {
		return dir, true
	}
	// Bundle layout is authoritative even if Resources is not created yet.
	return dir, true
}

func walkParentsForResources(start string) string {
	dir := start
	for {
		if found := existingDir(filepath.Join(dir, ResourcesDirName)); found != "" {
			return found
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func uniqueChildResources(parent string) string {
	entries, err := os.ReadDir(parent)
	if err != nil {
		return ""
	}
	var hits []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		cand := filepath.Join(parent, e.Name(), ResourcesDirName)
		if found := existingDir(cand); found != "" {
			hits = append(hits, found)
		}
	}
	if len(hits) == 1 {
		return hits[0]
	}
	return ""
}

func existingDir(path string) string {
	st, err := os.Stat(path)
	if err != nil || !st.IsDir() {
		return ""
	}
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return path
}
