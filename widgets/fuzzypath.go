package widgets

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	. "go.hasen.dev/shirei"
)

// FuzzyPathFinderAttrs configures FuzzyPathFinderExt — a Cmd+P-style
// fuzzy path picker (not a traditional file browser; see DirectoryBrowse).
type FuzzyPathFinderAttrs struct {
	Title string // modal title; zero → from Dirs/Files
	Width f32   // modal width; zero → 560

	// Dirs / Files select what the scanner includes. Independent flags —
	// set both for a mixed list. If neither is set, Dirs defaults to true.
	Dirs  bool
	Files bool

	// Root is the directory to scan from. Empty defaults to "/".
	Root string

	ShowHidden bool

	// Path field (outside the modal)
	NoAutoFocus bool
	MinWidth    f32
}

// DefaultFuzzyPathFinderAttrs returns directory-oriented fuzzy-finder defaults.
func DefaultFuzzyPathFinderAttrs() FuzzyPathFinderAttrs {
	return FuzzyPathFinderAttrs{
		Dirs: true,
	}
}

type fuzzyPathFinderState struct {
	active bool
	root   string
	query  string
}

// FuzzyPathFinder renders a path text field with a Find… button that opens
// a fuzzy path picker over a streaming BFS index. Prefer DirectoryBrowse for
// ordinary "choose a folder" UX.
func FuzzyPathFinder(text *string) {
	FuzzyPathFinderExt(text, DefaultFuzzyPathFinderAttrs())
}

// FuzzyPathFinderExt is FuzzyPathFinder with configuration.
func FuzzyPathFinderExt(text *string, attrs FuzzyPathFinderAttrs) {
	if attrs.Width == 0 {
		attrs.Width = 560
	}
	if !attrs.Dirs && !attrs.Files {
		attrs.Dirs = true
	}
	if attrs.Title == "" {
		switch {
		case attrs.Dirs && attrs.Files:
			attrs.Title = "Find path"
		case attrs.Files:
			attrs.Title = "Find file"
		default:
			attrs.Title = "Find folder"
		}
	}

	st := Use[fuzzyPathFinderState]("fuzzy-path-finder")

	Container(Attrs(Row, CrossMid, Gap(8), Expand), func() {
		input := DefaultTextInputAttrs()
		input.NoAutoFocus = attrs.NoAutoFocus
		if attrs.MinWidth > 0 {
			input.MinWidth = attrs.MinWidth
		} else {
			input.MinWidth = 280
		}
		if text != nil && *text != "" && attrs.Dirs && !attrs.Files && !pathIsDir(*text) {
			input.Accent = Vec4{5, 70, 50, 1}
		}
		Container(Attrs(Expand), func() {
			TextInputExt(text, input)
		})
		if CtrlButton(0, "Find…", true) {
			st.active = true
			st.query = ""
			st.root = resolveFuzzyRoot(attrs.Root)
			ensureFuzzyIndex(st.root, attrs.ShowHidden, attrs.Dirs, attrs.Files)
		}
	})

	if !st.active {
		return
	}

	closeDialog := func() {
		*st = fuzzyPathFinderState{}
	}

	Modal(attrs.Width, closeDialog, func() {
		Label(attrs.Title, FontSize(13), FontWeight(WeightBold), TextColor(220, 25, 25, 1))

		indexed, ready, scanErr := fuzzyIndexSnapshot(st.root)
		picked := ""
		accepted := FileSelector(FileSelectorAttrs{
			Selection:  &picked,
			Query:      &st.query,
			Candidates: indexed,
			Root:       st.root,
			Width:      attrs.Width - 40,
			Hint: func(matchCount int) string {
				return fuzzyHint(st.query, st.root, indexed, matchCount, ready, scanErr, attrs)
			},
		})

		if accepted {
			if text != nil && picked != "" {
				if attrs.Dirs && !attrs.Files {
					picked = appendPathSlash(picked)
				}
				*text = picked
			}
			closeDialog()
			return
		}

		if Button(0, "Cancel") {
			closeDialog()
		}
	})
}

func fuzzyHint(query, root string, indexed []string, matchCount int, ready bool, scanErr error, attrs FuzzyPathFinderAttrs) string {
	what := "paths"
	switch {
	case attrs.Dirs && !attrs.Files:
		what = "folders"
	case attrs.Files && !attrs.Dirs:
		what = "files"
	}
	switch {
	case !ready && len(indexed) == 0:
		return "indexing " + fileSelectorDisplay("", root) + "…"
	case scanErr != nil:
		return scanErr.Error()
	case matchCount == 0:
		if query == "" {
			return "no " + what + " in " + fileSelectorDisplay("", root)
		}
		return "no matches"
	default:
		hint := strconv.Itoa(matchCount) + " " + what
		if !ready {
			hint += " (still indexing…)"
		}
		return hint
	}
}

func resolveFuzzyRoot(attrRoot string) string {
	if attrRoot != "" {
		if abs, err := filepath.Abs(attrRoot); err == nil {
			return abs
		}
		return attrRoot
	}
	return "/"
}

func expandTilde(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, "~"+string(os.PathSeparator)) {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			return home + p[1:]
		}
	}
	return p
}

func appendPathSlash(dpath string) string {
	if dpath != "" && !strings.HasSuffix(dpath, string(os.PathSeparator)) {
		return dpath + string(os.PathSeparator)
	}
	return dpath
}

// ---------------------------------------------------------------------------
// streaming BFS index for FuzzyPathFinder
// ---------------------------------------------------------------------------

const fuzzyPublishEvery = 64

type fuzzyIndex struct {
	mu     sync.Mutex
	gen    uint64
	root   string
	hidden bool
	dirs   bool
	files  bool
	paths  []string
	err    error
	ready  bool
}

var globalFuzzyIndex fuzzyIndex

func ensureFuzzyIndex(root string, showHidden, dirs, files bool) {
	globalFuzzyIndex.mu.Lock()
	defer globalFuzzyIndex.mu.Unlock()
	same := globalFuzzyIndex.root == root &&
		globalFuzzyIndex.hidden == showHidden &&
		globalFuzzyIndex.dirs == dirs &&
		globalFuzzyIndex.files == files
	if same {
		if !globalFuzzyIndex.ready {
			RequestNextFrame()
		}
		return
	}
	globalFuzzyIndex.gen++
	gen := globalFuzzyIndex.gen
	globalFuzzyIndex.root = root
	globalFuzzyIndex.hidden = showHidden
	globalFuzzyIndex.dirs = dirs
	globalFuzzyIndex.files = files
	globalFuzzyIndex.paths = nil
	globalFuzzyIndex.err = nil
	globalFuzzyIndex.ready = false
	go scanFuzzyIndexBFS(gen, root, showHidden, dirs, files)
	RequestNextFrame()
}

func fuzzyIndexSnapshot(root string) (paths []string, ready bool, err error) {
	globalFuzzyIndex.mu.Lock()
	defer globalFuzzyIndex.mu.Unlock()
	if globalFuzzyIndex.root != root {
		return nil, false, nil
	}
	if !globalFuzzyIndex.ready {
		RequestNextFrame()
	}
	return append([]string(nil), globalFuzzyIndex.paths...), globalFuzzyIndex.ready, globalFuzzyIndex.err
}

func fuzzyPublish(gen uint64, batch []string, done bool, scanErr error) bool {
	globalFuzzyIndex.mu.Lock()
	defer globalFuzzyIndex.mu.Unlock()
	if globalFuzzyIndex.gen != gen {
		return false
	}
	if len(batch) > 0 {
		globalFuzzyIndex.paths = append(globalFuzzyIndex.paths, batch...)
	}
	if done {
		globalFuzzyIndex.err = scanErr
		globalFuzzyIndex.ready = true
	}
	RequestNextFrame()
	return true
}

var fuzzySkipDirs = map[string]bool{
	".git": true, ".hg": true, ".svn": true, ".jj": true,
	"node_modules": true, "vendor": true,
	".idea": true, ".vscode": true, ".cursor": true,
}

func scanFuzzyIndexBFS(gen uint64, root string, showHidden, wantDirs, wantFiles bool) {
	queue := []string{root}
	batch := make([]string, 0, fuzzyPublishEvery)

	flush := func(done bool, scanErr error) bool {
		if len(batch) == 0 && !done {
			return true
		}
		ok := fuzzyPublish(gen, batch, done, scanErr)
		batch = batch[:0]
		return ok
	}

	for len(queue) > 0 {
		dir := queue[0]
		queue = queue[1:]

		entries, err := os.ReadDir(dir)
		if err != nil {
			if !flush(false, nil) {
				return
			}
			continue
		}
		for _, e := range entries {
			name := e.Name()
			hidden := strings.HasPrefix(name, ".") && name != "." && name != ".."
			if !showHidden && hidden {
				continue
			}
			path := filepath.Join(dir, name)
			if e.IsDir() {
				if fuzzySkipDirs[name] {
					continue
				}
				if wantDirs {
					batch = append(batch, path)
				}
				queue = append(queue, path)
			} else if wantFiles {
				batch = append(batch, path)
			}
			if len(batch) >= fuzzyPublishEvery {
				if !flush(false, nil) {
					return
				}
			}
		}
	}
	flush(true, nil)
}
