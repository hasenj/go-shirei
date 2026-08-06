package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// releaseLdflags omits the symbol table and DWARF from production binaries.
const releaseLdflags = "-s -w"

// buildDesktopBinary cross-compiles a normal Go main for desktop platforms.
// Uses CGO_ENABLED=0 so packaging works from any host (shirei backends are purego).
// Always strips symbols (-s -w). Windows release builds also use -H windowsgui so
// the PE is a GUI app (no console window on double-click). Linux has no linker
// equivalent; the .desktop entry uses Terminal=false instead. Dev builds (plain
// go build) stay console-attached and keep symbols.
func buildDesktopBinary(moduleRoot, pkgSpec, binPath, goos, goarch string,
	logf func(string, ...any), cancelled func() bool, setCmd func(*exec.Cmd)) error {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	ldflags := releaseLdflags
	if goos == "windows" {
		// GUI subsystem: no console window when launched from Explorer.
		ldflags += " -H windowsgui"
	}
	args := []string{"build", "-o", binPath, "-ldflags=" + ldflags, pkgSpec}
	logf("— go build GOOS=%s GOARCH=%s CGO_ENABLED=0 (%s)", goos, goarch, pkgSpec)
	cmd := exec.Command("go", args...)
	cmd.Dir = moduleRoot
	cmd.Env = append(os.Environ(),
		"CGO_ENABLED=0",
		"GOOS="+goos,
		"GOARCH="+goarch,
	)
	if err := runCmdLog(cmd, logf, cancelled, setCmd); err != nil {
		if cancelled != nil && cancelled() {
			return fmt.Errorf("cancelled")
		}
		return fmt.Errorf("go build %s/%s: %w", goos, goarch, err)
	}
	return nil
}

// resolvePlatformReleaseDir returns <releaseDir>/<product>/<version>/<platform>.
// releaseDir should be absolute (callers join with working directory first).
// Version groups all platforms for a release; platform groups multi-arch outputs.
func resolvePlatformReleaseDir(dir, product, version, platform string) (string, error) {
	outDir := strings.TrimSpace(dir)
	if outDir == "" {
		outDir = "releases"
	}
	if !filepath.IsAbs(outDir) {
		if abs, err := filepath.Abs(outDir); err == nil {
			outDir = abs
		}
	}
	product = sanitizeProductName(strings.TrimSpace(product))
	if product == "" {
		product = "App"
	}
	version = strings.TrimSpace(version)
	if version == "" {
		version = "0.0.0"
	}
	// Keep path segments filesystem-safe.
	version = strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', ':', 0:
			return '-'
		default:
			return r
		}
	}, version)
	platform = strings.TrimSpace(platform)
	if platform == "" {
		platform = "unknown"
	}
	destRoot := filepath.Join(outDir, product, version, platform)
	if err := os.MkdirAll(destRoot, 0o755); err != nil {
		return "", err
	}
	return destRoot, nil
}

// writeLinuxDesktopFile writes a freedesktop.org .desktop entry for a portable
// tarball (relative Exec/Icon so it works when extracted anywhere).
func writeLinuxDesktopFile(path, displayName, execName, iconName, comment string) error {
	displayName = strings.TrimSpace(displayName)
	if displayName == "" {
		displayName = execName
	}
	execName = strings.TrimSpace(execName)
	iconName = strings.TrimSpace(iconName)
	if iconName == "" {
		iconName = execName
	}
	// Escape backslash and quotes for desktop-entry string values.
	esc := func(s string) string {
		s = strings.ReplaceAll(s, `\`, `\\`)
		s = strings.ReplaceAll(s, `"`, `\"`)
		s = strings.ReplaceAll(s, "\n", `\n`)
		return s
	}
	var b strings.Builder
	b.WriteString("[Desktop Entry]\n")
	b.WriteString("Type=Application\n")
	b.WriteString("Version=1.0\n")
	fmt.Fprintf(&b, "Name=%s\n", esc(displayName))
	if c := strings.TrimSpace(comment); c != "" {
		fmt.Fprintf(&b, "Comment=%s\n", esc(c))
	}
	// Relative path: run from the extracted directory.
	fmt.Fprintf(&b, "Exec=./%s\n", esc(execName))
	fmt.Fprintf(&b, "Icon=%s\n", esc(iconName))
	b.WriteString("Terminal=false\n")
	b.WriteString("Categories=Utility;\n")
	b.WriteString("StartupNotify=true\n")
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

// copyIconIfPresent copies src icon into destDir as destBase + ext.
// Returns the basename written (without path), or "" if no icon.
func copyIconIfPresent(src, destDir, destBase string) (string, error) {
	src = strings.TrimSpace(src)
	if src == "" {
		return "", nil
	}
	st, err := os.Stat(src)
	if err != nil || st.IsDir() {
		return "", nil
	}
	ext := strings.ToLower(filepath.Ext(src))
	if ext == "" {
		ext = ".png"
	}
	name := destBase + ext
	dest := filepath.Join(destDir, name)
	if err := copyFile(src, dest); err != nil {
		return "", err
	}
	return name, nil
}

// tarGzipDir creates destTarGz containing the directory dir as a single top-level folder.
func tarGzipDir(dir, destTarGz string) error {
	_ = os.Remove(destTarGz)
	f, err := os.Create(destTarGz)
	if err != nil {
		return err
	}
	defer f.Close()
	gz := gzip.NewWriter(f)
	defer gz.Close()
	tw := tar.NewWriter(gz)
	defer tw.Close()

	base := filepath.Base(dir)
	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(filepath.Join(base, rel))
		if rel == "." {
			name = base + "/"
		}
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = name
		hdr.ModTime = info.ModTime()
		if info.IsDir() {
			if !strings.HasSuffix(hdr.Name, "/") {
				hdr.Name += "/"
			}
			return tw.WriteHeader(hdr)
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		rf, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(tw, rf)
		rf.Close()
		return copyErr
	})
}

// zipDir creates destZip containing the directory dir as a single top-level folder.
func zipDir(dir, destZip string) error {
	_ = os.Remove(destZip)
	f, err := os.Create(destZip)
	if err != nil {
		return err
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	defer zw.Close()

	base := filepath.Base(dir)
	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(filepath.Join(base, rel))
		if info.IsDir() {
			if rel == "." {
				return nil
			}
			if !strings.HasSuffix(name, "/") {
				name += "/"
			}
			_, err := zw.Create(name)
			return err
		}
		w, err := zw.Create(name)
		if err != nil {
			return err
		}
		rf, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(w, rf)
		rf.Close()
		return copyErr
	})
}

// desktopCacheBuildDir returns a unique cache dir for one desktop package job.
func desktopCacheBuildDir(product string) (string, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(cache, "shirei", "bundle-desktop",
		product+"-"+fmt.Sprintf("%d", time.Now().UnixNano()))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}
