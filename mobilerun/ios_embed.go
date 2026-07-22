package main

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

//go:embed all:embed/*
var runtimeEmbed embed.FS

// RuntimeDirs are absolute paths after materializing the embedded host+script.
type RuntimeDirs struct {
	Root     string // extract root (contains ios-run.sh + ioshost/)
	Script   string // path to ios-run.sh
	HostDir  string // path to ioshost template
	BuildDir string // scratch for archives / .app / host-work
}

// ensureRuntime extracts the embedded ios-run assets into the user cache if
// needed and returns paths. The template is never written back to the module.
func ensureRuntime() (RuntimeDirs, error) {
	var zero RuntimeDirs
	sum, err := hashEmbed()
	if err != nil {
		return zero, err
	}
	cacheRoot, err := os.UserCacheDir()
	if err != nil {
		return zero, err
	}
	root := filepath.Join(cacheRoot, "shirei", "mobilerun-ios-runtime", sum)
	script := filepath.Join(root, "ios-run.sh")
	host := filepath.Join(root, "ioshost")
	build := filepath.Join(cacheRoot, "shirei", "ios-build")
	marker := filepath.Join(root, ".ok")

	if st, err := os.Stat(marker); err != nil || !st.Mode().IsRegular() {
		if err := os.RemoveAll(root); err != nil {
			return zero, err
		}
		if err := os.MkdirAll(root, 0o755); err != nil {
			return zero, err
		}
		if err := extractEmbed(root); err != nil {
			_ = os.RemoveAll(root)
			return zero, err
		}
		if err := os.Chmod(script, 0o755); err != nil {
			return zero, err
		}
		if err := os.WriteFile(marker, []byte(sum+"\n"), 0o644); err != nil {
			return zero, err
		}
	}

	if err := os.MkdirAll(build, 0o755); err != nil {
		return zero, err
	}
	return RuntimeDirs{Root: root, Script: script, HostDir: host, BuildDir: build}, nil
}

func hashEmbed() (string, error) {
	h := sha256.New()
	err := fs.WalkDir(runtimeEmbed, "embed", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if _, err := io.WriteString(h, path+"\n"); err != nil {
			return err
		}
		f, err := runtimeEmbed.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		if _, err := io.Copy(h, f); err != nil {
			return err
		}
		_, _ = io.WriteString(h, "\n")
		return nil
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil))[:16], nil
}

func extractEmbed(destRoot string) error {
	return fs.WalkDir(runtimeEmbed, "embed", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel("embed", path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		out := filepath.Join(destRoot, rel)
		if d.IsDir() {
			return os.MkdirAll(out, 0o755)
		}
		base := filepath.Base(path)
		if base == "libshirei.a" || filepath.Ext(base) == ".a" {
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return err
		}
		src, err := runtimeEmbed.Open(path)
		if err != nil {
			return err
		}
		defer src.Close()
		dst, err := os.OpenFile(out, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(dst, src)
		closeErr := dst.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
}

// findShireiModule looks for a local go.hasen.dev/shirei module near start
// (for SHIREI_MODULE / go.work). Optional — published shirei with iOS works without it.
func findShireiModule(start string) string {
	d := start
	for i := 0; i < 12; i++ {
		modFile := filepath.Join(d, "go.mod")
		if data, err := os.ReadFile(modFile); err == nil {
			if modulePath(string(data)) == "go.hasen.dev/shirei" {
				if _, err := os.Stat(filepath.Join(d, "iosbackend")); err == nil {
					return d
				}
				if filepath.Base(d) == "shirei" {
					return d
				}
			}
		}
		cand := filepath.Join(d, "shirei")
		if data, err := os.ReadFile(filepath.Join(cand, "go.mod")); err == nil {
			if modulePath(string(data)) == "go.hasen.dev/shirei" {
				return cand
			}
		}
		parent := filepath.Dir(d)
		if parent == d {
			break
		}
		d = parent
	}
	return ""
}

func modulePath(goMod string) string {
	for _, line := range strings.Split(goMod, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module "))
		}
	}
	return ""
}
