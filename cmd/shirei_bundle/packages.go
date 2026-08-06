package main

import (
	"fmt"
	"sync"

	"go.hasen.dev/textsearch"
)

// MainPkg is a candidate package directory (func main in a .go file).
type MainPkg struct {
	Dir string // absolute
	Rel string // relative to scan root
}

type pkgCache struct {
	mu       sync.Mutex
	root     string
	pkgs     []MainPkg
	ready    bool
	scanning bool
	err      error
}

var pkgsCache pkgCache

func ensurePackages(root string) (pkgs []MainPkg, ready bool, err error) {
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
	pkgs, err := listMainPackages(root)
	finishPkgScan(root, pkgs, err)
}

func listMainPackages(root string) ([]MainPkg, error) {
	dirs, err := textsearch.MatchingDirs(textsearch.Params{
		Root:      root,
		Query:     "func main",
		MatchCase: true,
		Include:   "*.go",
		Gitignore: true,
	})
	if err != nil {
		return nil, err
	}
	out := make([]MainPkg, 0, len(dirs))
	for _, d := range dirs {
		out = append(out, MainPkg{Dir: d.Dir, Rel: d.Rel})
	}
	return out, nil
}
