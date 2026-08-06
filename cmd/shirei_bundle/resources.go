package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// packageResourcesDir is the conventional source-tree assets directory next to
// package main. Same name the runtime looks for (app.ResourcesDirName).
const packageResourcesDir = "Resources"

// defaultPackageIconRel returns a path relative to pkgDir for the package
// icon when one exists. Prefers Resources/icon.png (the app-resources
// convention); falls back to icon.png beside package main.
func defaultPackageIconRel(pkgDir string) string {
	pkgDir = strings.TrimSpace(pkgDir)
	if pkgDir == "" {
		return ""
	}
	for _, rel := range []string{
		filepath.Join(packageResourcesDir, "icon.png"),
		"icon.png",
	} {
		if st, err := os.Stat(filepath.Join(pkgDir, rel)); err == nil && !st.IsDir() {
			return rel
		}
	}
	return ""
}

// copyPackageResources flattens <pkgDir>/Resources/* into destRoot when the
// source directory exists. Missing Resources is not an error. destRoot must
// already exist (e.g. Contents/Resources or <stage>/Resources).
func copyPackageResources(pkgDir, destRoot string, logf func(string, ...any)) error {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	src := filepath.Join(pkgDir, packageResourcesDir)
	st, err := os.Stat(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if !st.IsDir() {
		return fmt.Errorf("%s exists but is not a directory", src)
	}
	if err := os.MkdirAll(destRoot, 0o755); err != nil {
		return err
	}
	n, err := copyDirContents(src, destRoot)
	if err != nil {
		return fmt.Errorf("copy %s → %s: %w", src, destRoot, err)
	}
	logf("copied %d file(s) from %s into %s", n, src, destRoot)
	return nil
}

// copyDirContents copies files and subdirectories under src into dst (dst is
// the destination root; src's basename is not created under dst).
func copyDirContents(src, dst string) (int, error) {
	count := 0
	err := filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		// Skip junk that should not ship in a release bundle.
		base := info.Name()
		if base == ".DS_Store" || strings.HasPrefix(base, "._") {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		out := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(out, 0o755)
		}
		if err := copyFileStream(path, out); err != nil {
			return err
		}
		count++
		return nil
	})
	return count, err
}

// copyFileStream copies a file without loading it entirely into memory (large
// release assets such as deploy tarballs).
func copyFileStream(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}
