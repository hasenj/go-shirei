package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// MainPkg is a package main found under the scan root (or go.work uses).
type MainPkg struct {
	ImportPath string
	Dir        string // absolute
	Rel        string // path relative to scan root, for display / config
}

// ShireiPkg is an alias kept for call-site compatibility.
type ShireiPkg = MainPkg

type pkgCache struct {
	mu       sync.Mutex
	root     string
	pkgs     []MainPkg
	ready    bool
	scanning bool
	err      error
}

var pkgsCache pkgCache

func ensureShireiPackages(root string) (pkgs []MainPkg, ready bool, err error) {
	pkgsCache.mu.Lock()
	defer pkgsCache.mu.Unlock()
	if pkgsCache.root != root {
		pkgsCache.root = root
		pkgsCache.pkgs = nil
		pkgsCache.ready = false
		pkgsCache.scanning = true
		pkgsCache.err = nil
		go scanMainPackages(root)
	}
	return append([]MainPkg(nil), pkgsCache.pkgs...), pkgsCache.ready, pkgsCache.err
}

func invalidatePackages() {
	pkgsCache.mu.Lock()
	pkgsCache.root = ""
	pkgsCache.pkgs = nil
	pkgsCache.ready = false
	pkgsCache.scanning = false
	pkgsCache.err = nil
	pkgsCache.mu.Unlock()
}

func finishPkgScan(root string, pkgs []MainPkg, err error) {
	pkgsCache.mu.Lock()
	if pkgsCache.root == root {
		pkgsCache.pkgs = pkgs
		pkgsCache.err = err
		pkgsCache.ready = true
		pkgsCache.scanning = false
	}
	pkgsCache.mu.Unlock()
	wakeUI()
}

func scanMainPackages(root string) {
	if root == "" {
		finishPkgScan(root, nil, fmt.Errorf("no scan root"))
		return
	}
	seen := map[string]bool{}
	var out []MainPkg

	collect := func(modDir string) {
		list, err := listMainPackages(modDir)
		if err != nil {
			return
		}
		for _, p := range list {
			if seen[p.Dir] {
				continue
			}
			seen[p.Dir] = true
			rel := p.Dir
			if r, err := filepath.Rel(root, p.Dir); err == nil {
				rel = r
			}
			out = append(out, MainPkg{
				ImportPath: p.ImportPath,
				Dir:        p.Dir,
				Rel:        rel,
			})
		}
	}

	// Try root as a module / workspace first.
	collect(root)
	if len(out) == 0 {
		// go.work use entries (monorepo root).
		for _, u := range goWorkUses(root) {
			modDir := u
			if !filepath.IsAbs(modDir) {
				modDir = filepath.Join(root, u)
			}
			collect(modDir)
		}
	}
	finishPkgScan(root, out, nil)
}

type goListPkg struct {
	Name       string
	ImportPath string
	Dir        string
}

// listMainPackages lists package main under dir. Uses a narrow -json field set
// so the scan stays fast (no import graph).
func listMainPackages(dir string) ([]goListPkg, error) {
	cmd := exec.Command("go", "list", "-e", "-json=Name,ImportPath,Dir", "./...")
	cmd.Dir = dir
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	var out []goListPkg
	dec := json.NewDecoder(stdout)
	for dec.More() {
		var p goListPkg
		if err := dec.Decode(&p); err != nil {
			_ = cmd.Wait()
			return out, err
		}
		if p.Name != "main" {
			continue
		}
		out = append(out, p)
	}
	waitErr := cmd.Wait()
	if waitErr != nil && len(out) == 0 {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = waitErr.Error()
		}
		return nil, fmt.Errorf("go list: %s", firstLine(msg))
	}
	return out, nil
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

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
