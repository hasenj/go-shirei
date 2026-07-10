package main

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ignorePattern is one compiled .gitignore line.
type ignorePattern struct {
	re      *regexp.Regexp // matches paths relative to the .gitignore's directory
	negate  bool           // leading "!" — re-includes a previously ignored path
	dirOnly bool           // trailing "/" — only matches directories
}

// gitignoreMatcher evaluates paths (relative to the search root) against the
// .gitignore files found while walking. It honours per-directory scope,
// negation, dir-only patterns, and the common glob forms (* ? ** and /
// anchoring). It is a pragmatic subset of git's wildmatch — good for typical
// repos, not a bit-exact reimplementation (e.g. `a/**/b` matching zero
// directories, or global/core.excludesFile, are not handled).
type gitignoreMatcher struct {
	root  string
	cache map[string][]ignorePattern // dir relative to root ("" = root) -> its patterns
}

func newGitignore(root string) *gitignoreMatcher {
	return &gitignoreMatcher{root: root, cache: map[string][]ignorePattern{}}
}

func (gi *gitignoreMatcher) patternsFor(relDir string) []ignorePattern {
	if p, ok := gi.cache[relDir]; ok {
		return p
	}
	p := parseGitignoreFile(filepath.Join(gi.root, filepath.FromSlash(relDir), ".gitignore"))
	gi.cache[relDir] = p
	return p
}

// ignored reports whether rel (relative to root, "/"-separated) is excluded.
// Each ancestor directory's .gitignore is applied in turn, root first, and the
// last matching rule wins — so a deeper `!pattern` can re-include.
func (gi *gitignoreMatcher) ignored(rel string, isDir bool) bool {
	rel = filepath.ToSlash(rel)
	parts := strings.Split(rel, "/")
	ignored := false
	for i := range parts {
		relDir := strings.Join(parts[:i], "/")
		sub := strings.Join(parts[i:], "/")
		for _, p := range gi.patternsFor(relDir) {
			if p.dirOnly && !isDir {
				continue
			}
			if p.re.MatchString(sub) {
				ignored = !p.negate
			}
		}
	}
	return ignored
}

func parseGitignoreFile(path string) []ignorePattern {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var out []ignorePattern
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), " ")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		var p ignorePattern
		if strings.HasPrefix(line, "!") {
			p.negate = true
			line = line[1:]
		}
		if strings.HasSuffix(line, "/") {
			p.dirOnly = true
			line = strings.TrimSuffix(line, "/")
		}
		if p.re = compileGitignoreGlob(line); p.re != nil {
			out = append(out, p)
		}
	}
	return out
}

// compileGitignoreGlob turns a .gitignore pattern into a regexp over a path
// relative to the pattern's directory.
func compileGitignoreGlob(pattern string) *regexp.Regexp {
	// a leading "**/" means "at any depth" — the same as an unanchored pattern
	pattern = strings.TrimPrefix(pattern, "**/")

	// a slash anywhere anchors the pattern to the .gitignore's directory;
	// otherwise it matches by basename at any depth
	anchored := strings.HasPrefix(pattern, "/") || strings.Contains(pattern, "/")
	pattern = strings.TrimPrefix(pattern, "/")

	var b strings.Builder
	b.WriteString("^")
	if !anchored {
		b.WriteString("(?:.*/)?")
	}
	for i := 0; i < len(pattern); i++ {
		switch c := pattern[i]; c {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				b.WriteString(".*") // ** crosses directory separators
				i++
			} else {
				b.WriteString("[^/]*")
			}
		case '?':
			b.WriteString("[^/]")
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	// match the path itself or anything nested under it (so a dir pattern
	// covers its contents even before the walk prunes the directory)
	b.WriteString("(?:/.*)?$")

	re, err := regexp.Compile(b.String())
	if err != nil {
		return nil
	}
	return re
}
