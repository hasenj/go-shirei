package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Text markers in *_test.go (simple bytes.Contains — not AST).
// Primary convention for apps: TestSnapshot*. Also accept shirei harness
// call sites that use other test names (layout_tests, softrender).
var snapshotTestMarkers = [][]byte{
	[]byte("TestSnapshot"),
	[]byte("ReportSnap"),
	[]byte("layoutSnapshot"),
}

// findScanRoot finds a module or workspace root from start (or an explicit path).
// Walks up for go.work, then go.mod. Not shirei-specific.
func findScanRoot(start string) (string, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	for {
		if st, err := os.Stat(filepath.Join(dir, "go.work")); err == nil && !st.IsDir() {
			return dir, nil
		}
		if st, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil && !st.IsDir() {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no go.mod or go.work above %s", start)
		}
		dir = parent
	}
}

// moduleDirs returns directories to walk: either root itself or each go.work
// use entry (absolute). No go list — pure filesystem.
func moduleDirs(root string) []string {
	uses := goWorkUses(root)
	if len(uses) == 0 {
		return []string{root}
	}
	var out []string
	for _, u := range uses {
		if filepath.IsAbs(u) {
			out = append(out, u)
		} else {
			out = append(out, filepath.Join(root, u))
		}
	}
	return out
}

func goWorkUses(root string) []string {
	data, err := os.ReadFile(filepath.Join(root, "go.work"))
	if err != nil {
		return nil
	}
	var uses []string
	inUse := false
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}
		if strings.HasPrefix(line, "use ") {
			rest := strings.TrimSpace(strings.TrimPrefix(line, "use "))
			if rest == "(" {
				inUse = true
				continue
			}
			uses = append(uses, strings.Trim(rest, `"`))
			continue
		}
		if inUse {
			if line == ")" {
				inUse = false
				continue
			}
			uses = append(uses, strings.Trim(line, `"`))
		}
	}
	return uses
}

// readModulePath returns the module path from go.mod, or "" if missing/unparseable.
func readModulePath(modDir string) string {
	data, err := os.ReadFile(filepath.Join(modDir, "go.mod"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module "))
		}
	}
	return ""
}

// skipWalkDir names that are never Go packages we care about.
func skipWalkDir(name string) bool {
	switch name {
	case ".git", "vendor", "node_modules", "testdata":
		return true
	}
	return strings.HasPrefix(name, ".")
}

// pkgHasSnapshotMarker is a cheap source text search over *_test.go in the
// package directory (non-recursive). Avoids needing goldens on disk. False
// positives are OK; missing report JSON just means no image diff pane.
func pkgHasSnapshotMarker(pkgDir string) bool {
	ents, err := os.ReadDir(pkgDir)
	if err != nil {
		return false
	}
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, "_test.go") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(pkgDir, name))
		if err != nil {
			continue
		}
		for _, m := range snapshotTestMarkers {
			if bytes.Contains(data, m) {
				return true
			}
		}
	}
	return false
}

// importPathFor joins the module path with a path relative to the module root.
func importPathFor(modPath, relFromMod string) string {
	relFromMod = filepath.ToSlash(relFromMod)
	if modPath == "" {
		return relFromMod
	}
	if relFromMod == "" || relFromMod == "." {
		return modPath
	}
	return modPath + "/" + relFromMod
}

// discoverPackages finds packages under root that look like snapshot hosts by
// walking the filesystem (no go list). Import paths come from go.mod module
// path + directory relative to the module root.
func discoverPackages(root string) ([]*PackageItem, error) {
	seen := map[string]bool{}
	var pkgs []*PackageItem

	for _, modDir := range moduleDirs(root) {
		st, err := os.Stat(modDir)
		if err != nil || !st.IsDir() {
			continue
		}
		modPath := readModulePath(modDir)
		_ = filepath.WalkDir(modDir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if !d.IsDir() {
				return nil
			}
			if path != modDir {
				if skipWalkDir(d.Name()) {
					return filepath.SkipDir
				}
				// Nested module not listed as its own walk root.
				if _, err := os.Stat(filepath.Join(path, "go.mod")); err == nil {
					return filepath.SkipDir
				}
			}
			if seen[path] || !pkgHasSnapshotMarker(path) {
				return nil
			}
			tests, err := listTests(path)
			if err != nil || len(tests) == 0 {
				return nil
			}
			seen[path] = true
			// Rel is the path used for `go test ./…` under the scan root.
			// Keep "." for the module-root package (never rewrite to base(root)).
			rel, err := filepath.Rel(root, path)
			if err != nil {
				rel = path
			}
			relFromMod, err := filepath.Rel(modDir, path)
			if err != nil {
				relFromMod = "."
			}
			ip := importPathFor(modPath, relFromMod)
			pkg := &PackageItem{Dir: path, ImportPath: ip, Rel: rel}
			for _, name := range tests {
				pkg.Tests = append(pkg.Tests, &TestItem{
					PkgDir:     path,
					ImportPath: ip,
					Name:       name,
					Status:     statusUnknown,
				})
			}
			pkgs = append(pkgs, pkg)
			return nil
		})
	}
	if len(pkgs) == 0 {
		return nil, fmt.Errorf("no packages with snapshot markers in *_test.go under %s", root)
	}
	// Stable tree order (WalkDir is already lexical per module; multi-module
	// workspaces may interleave — sort by Rel).
	sort.Slice(pkgs, func(i, j int) bool {
		return pkgs[i].Rel < pkgs[j].Rel
	})
	return pkgs, nil
}

// listTests reads Test* function names from *_test.go sources (no compile).
// The package already passed the marker filter; we do not re-filter names.
// Source listing avoids the multi-second go test -list compile per package
// that blocked startup when done for every snapshot package.
func listTests(pkgDir string) ([]string, error) {
	ents, err := os.ReadDir(pkgDir)
	if err != nil {
		return nil, err
	}
	fset := token.NewFileSet()
	seen := map[string]bool{}
	var names []string
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, "_test.go") {
			continue
		}
		path := filepath.Join(pkgDir, name)
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			// Skip unparseable files; other files may still list tests.
			continue
		}
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || fn.Name == nil {
				continue
			}
			n := fn.Name.Name
			if !strings.HasPrefix(n, "Test") || n == "TestMain" {
				continue
			}
			if seen[n] {
				continue
			}
			seen[n] = true
			names = append(names, n)
		}
	}
	sort.Strings(names)
	return names, nil
}
