package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// revealInFileManager opens the OS file manager with path selected when possible
// (Finder on macOS, Explorer on Windows; FreeDesktop ShowItems on Linux).
// If path is a directory, it is opened (not "selected as a file").
func revealInFileManager(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if st, err := os.Stat(abs); err == nil && st.IsDir() {
		return openInFileManager(abs)
	}
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", "-R", abs).Run()
	case "windows":
		return exec.Command("explorer", "/select,", abs).Start()
	case "linux":
		uri := "file://" + abs
		return exec.Command(
			"gdbus", "call", "--session",
			"--dest", "org.freedesktop.FileManager1",
			"--object-path", "/org/freedesktop/FileManager1",
			"--method", "org.freedesktop.FileManager1.ShowItems",
			fmt.Sprintf("['%s']", uri), "",
		).Run()
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

// openInFileManager opens a directory in the OS file manager.
func openInFileManager(dir string) error {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", abs).Run()
	case "windows":
		return exec.Command("explorer", abs).Start()
	case "linux":
		return exec.Command("xdg-open", abs).Start()
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

// releaseEntryPaths returns primary + extra artifact paths (non-empty, de-duped).
func releaseEntryPaths(e ReleaseEntry) []string {
	var out []string
	seen := map[string]bool{}
	for _, p := range append([]string{e.Path}, e.Extra...) {
		p = strings.TrimSpace(p)
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}

// artifactFolderFor returns a directory to open for a release entry:
// parent of the primary path when it is a file, or the path itself if a dir.
func artifactFolderFor(e ReleaseEntry) string {
	p := strings.TrimSpace(e.Path)
	if p == "" {
		return ""
	}
	if st, err := os.Stat(p); err == nil && st.IsDir() {
		return p
	}
	return filepath.Dir(p)
}

func revealButtonLabel() string {
	switch runtime.GOOS {
	case "darwin":
		return "Reveal in Finder"
	case "windows":
		return "Reveal in File Explorer"
	default:
		return "Reveal in file manager"
	}
}
