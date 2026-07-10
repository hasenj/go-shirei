// "Why is this module in my binary?" — reverse-dependency chains.
//
// buildinfo gives the flat set of modules linked into the binary, but not the
// edges between them. The edges come from each dep's go.mod, which sits in the
// local module cache as a plain file:
//
//	$GOMODCACHE/cache/download/<escaped path>/@v/<version>.mod
//
// We parse just the require lines and keep only edges pointing at modules that
// are actually in the binary — no version resolution needed, the build already
// did MVS and buildinfo recorded the outcome.
//
// Known gaps, reported honestly in the output:
//   - the main module's go.mod is not embedded in the binary, so modules that
//     no cached go.mod requires are *inferred* to be direct dependencies
//   - "(devel)" / replaced modules have no cache entry, so their outgoing
//     requires are unknown
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Why prints the reverse-dependency chains for the module matching query.
func Why(info *ExeInfo, query string) {
	target := findModule(info, query)
	if target == nil {
		return
	}

	requiredBy, _, noModFile := depEdges(info)
	inBinary := map[string]*Module{}
	for _, m := range info.Deps {
		inBinary[m.Path] = m
	}

	fmt.Printf("%s %s — %.0f KB, %d functions\n\n",
		target.Path, target.Version, float64(target.CodeSize)/1e3, target.NumFuncs)

	// Shortest chain from the main module down to the target (à la `go mod
	// why`), found by BFS upward through the reverse edges. The link from the
	// main module to the chain's root is inferred (main's go.mod isn't in the
	// binary), hence the annotation.
	chain := shortestChain(target.Path, requiredBy)
	fmt.Printf("%s  (main module)\n", info.Main.Path)
	for i, p := range chain {
		note := ""
		if i == 0 {
			note = "  (direct dependency — inferred)"
		}
		fmt.Printf("%s%s %s%s\n", strings.Repeat("  ", i+1), p, inBinary[p].Version, note)
	}

	if direct := requiredBy[target.Path]; len(direct) > 1 {
		inChain := map[string]bool{}
		for _, p := range chain {
			inChain[p] = true
		}
		sort.Strings(direct)
		fmt.Printf("\nalso directly required by:\n")
		for _, p := range direct {
			if !inChain[p] {
				fmt.Printf("  %s\n", p)
			}
		}
	}

	if len(noModFile) > 0 {
		sort.Strings(noModFile)
		fmt.Printf("\nnote: requires of %d module(s) unknown (no module cache entry — local/replaced builds):\n", len(noModFile))
		for _, p := range noModFile {
			fmt.Printf("  %s\n", p)
		}
	}
}

// depEdges builds both directions of the require graph among the modules in
// the binary, from the direct require lines of each module's cached go.mod.
// Also returns the modules whose go.mod is not in the cache (local/replaced
// builds), whose outgoing edges are therefore unknown.
func depEdges(info *ExeInfo) (requiredBy, requires map[string][]string, noModFile []string) {
	requiredBy = map[string][]string{}
	requires = map[string][]string{}
	inBinary := map[string]bool{}
	for _, m := range info.Deps {
		inBinary[m.Path] = true
	}
	for _, m := range info.Deps {
		reqs, ok := cacheRequires(m.Path, m.Version)
		if !ok {
			noModFile = append(noModFile, m.Path)
			continue
		}
		for _, r := range reqs {
			if inBinary[r] {
				requiredBy[r] = append(requiredBy[r], m.Path)
				requires[m.Path] = append(requires[m.Path], r)
			}
		}
	}
	sort.Strings(noModFile)
	return requiredBy, requires, noModFile
}

// shortestChain returns the shortest root→target path through the reverse
// edges, where a root is a module nothing (known) requires — i.e. an inferred
// direct dependency of the main module.
func shortestChain(target string, requiredBy map[string][]string) []string {
	type node struct {
		path string
		next *node // toward the target
	}
	seen := map[string]bool{target: true}
	frontier := []*node{{path: target}}
	for len(frontier) > 0 {
		var nextFrontier []*node
		for _, n := range frontier {
			parents := requiredBy[n.path]
			if len(parents) == 0 {
				// Root found; unwind into a root-first slice.
				var chain []string
				for c := n; c != nil; c = c.next {
					chain = append(chain, c.path)
				}
				return chain
			}
			sorted := append([]string(nil), parents...)
			sort.Strings(sorted)
			for _, p := range sorted {
				if !seen[p] {
					seen[p] = true
					nextFrontier = append(nextFrontier, &node{path: p, next: n})
				}
			}
		}
		frontier = nextFrontier
	}
	return []string{target} // cycle with no root; degenerate but possible
}

// findModule resolves a user query to a module in the binary: exact path,
// then prefix (a package path finds its owning module), then substring.
func findModule(info *ExeInfo, query string) *Module {
	for _, m := range info.Deps {
		if m.Path == query {
			return m
		}
	}
	match := func(pred func(*Module) bool) []*Module {
		var out []*Module
		for _, m := range info.Deps {
			if pred(m) {
				out = append(out, m)
			}
		}
		return out
	}
	// package path -> owning module (longest prefix wins)
	prefixed := match(func(m *Module) bool { return strings.HasPrefix(query, m.Path+"/") })
	if len(prefixed) > 0 {
		sort.Slice(prefixed, func(i, j int) bool { return len(prefixed[i].Path) > len(prefixed[j].Path) })
		return prefixed[0]
	}
	subs := match(func(m *Module) bool { return strings.Contains(m.Path, query) })
	switch len(subs) {
	case 1:
		return subs[0]
	case 0:
		fmt.Fprintf(os.Stderr, "no module in this binary matches %q\n", query)
	default:
		fmt.Fprintf(os.Stderr, "%q is ambiguous, matches:\n", query)
		for _, m := range subs {
			fmt.Fprintf(os.Stderr, "  %s\n", m.Path)
		}
	}
	return nil
}

// cacheRequires reads the require list of module m@version from the module cache.
func cacheRequires(modPath, version string) ([]string, bool) {
	file := filepath.Join(modCacheDir(), "cache", "download",
		filepath.FromSlash(cacheEscape(modPath)), "@v", cacheEscape(version)+".mod")
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, false
	}
	return parseRequires(string(data)), true
}

func modCacheDir() string {
	if d := os.Getenv("GOMODCACHE"); d != "" {
		return d
	}
	if g := os.Getenv("GOPATH"); g != "" {
		return filepath.Join(g, "pkg", "mod")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "go", "pkg", "mod")
}

// cacheEscape applies the module cache's path escaping: uppercase letters
// become '!' + lowercase ("github.com/Azure" -> "github.com/!azure").
func cacheEscape(s string) string {
	var b strings.Builder
	for _, c := range []byte(s) {
		if c >= 'A' && c <= 'Z' {
			b.WriteByte('!')
			b.WriteByte(c + ('a' - 'A'))
		} else {
			b.WriteByte(c)
		}
	}
	return b.String()
}

// parseRequires extracts required module paths from go.mod text, skipping
// "// indirect" entries: a module that actually imports another must list it
// un-annotated, so direct entries are the true import-level edges. Handles
// both single-line requires and require blocks.
func parseRequires(gomod string) []string {
	var out []string
	inBlock := false
	for _, line := range strings.Split(gomod, "\n") {
		indirect := false
		if i := strings.Index(line, "//"); i >= 0 {
			indirect = strings.Contains(line[i:], "indirect")
			line = line[:i]
		}
		line = strings.TrimSpace(line)
		switch {
		case line == "require (":
			inBlock = true
		case inBlock && line == ")":
			inBlock = false
		case inBlock && !indirect:
			if f := strings.Fields(line); len(f) >= 2 {
				out = append(out, strings.Trim(f[0], `"`))
			}
		case strings.HasPrefix(line, "require ") && !indirect:
			if f := strings.Fields(line); len(f) >= 3 {
				out = append(out, strings.Trim(f[1], `"`))
			}
		}
	}
	return out
}
