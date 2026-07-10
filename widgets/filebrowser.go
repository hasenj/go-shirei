package widgets

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	. "go.hasen.dev/shirei"
)

const fileBrowserRowH f32 = 28

// FileBrowserAttrs configures the traditional file/directory browser.
type FileBrowserAttrs struct {
	Title string // zero → "Choose folder" / "Choose file" / "Choose path"
	Width f32   // modal / panel width; zero → 520

	// Dirs / Files control what can be chosen. Directories always appear so
	// the user can navigate; files appear only when Files is set.
	// If neither is set, Dirs defaults to true.
	Dirs  bool
	Files bool

	// Start is the directory shown when DirectoryBrowse opens the dialog.
	// Empty → home directory, else "/".
	Start string

	ShowHidden bool

	// Path field (DirectoryBrowse)
	NoAutoFocus bool
	MinWidth    f32
}

// DefaultFileBrowserAttrs returns a directory-picker configuration.
func DefaultFileBrowserAttrs() FileBrowserAttrs {
	return FileBrowserAttrs{Dirs: true}
}

type directoryBrowseState struct {
	active   bool
	cwd      string
	filter   string // filters the current directory listing only
	selected int    // -1 = nothing selected
}

// DirectoryBrowse renders a path text field with a Browse… button that opens
// a traditional click-to-navigate folder browser. "Choose" accepts the
// current directory; cancel / Escape / scrim leave the bound string unchanged.
func DirectoryBrowse(text *string) {
	DirectoryBrowseExt(text, DefaultFileBrowserAttrs())
}

// DirectoryBrowseExt is DirectoryBrowse with configuration.
func DirectoryBrowseExt(text *string, attrs FileBrowserAttrs) {
	normalizeFileBrowserAttrs(&attrs)

	st := Use[directoryBrowseState]("directory-browse")

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
		if CtrlButton(0, "Browse…", true) {
			draft := ""
			if text != nil {
				draft = *text
			}
			st.active = true
			st.filter = ""
			st.selected = -1
			st.cwd = resolveBrowserStart(attrs.Start, draft)
		}
	})

	if !st.active {
		return
	}

	closeDialog := func() {
		*st = directoryBrowseState{}
	}

	Modal(attrs.Width, closeDialog, func() {
		Label(attrs.Title, FontSize(13), FontWeight(WeightBold), TextColor(220, 25, 25, 1))

		if FileBrowserPanel(&st.cwd, &st.filter, &st.selected, text, attrs) {
			closeDialog()
			return
		}

		if Button(0, "Cancel") {
			closeDialog()
		}
	})
}

type fileBrowserPanelState struct {
	lastCwd    string
	lastFilter string
}

// FileBrowserPanel draws a one-level listing for *cwd (DirListing), with an
// optional filter box that only narrows the current directory's entries.
//
// Selection (*selected, -1 = none): empty filter → nothing selected; typing a
// filter selects the first match. Up/Down move the highlight (from none:
// Down → first, Up → last). Enter navigates the selected directory;
// Cmd/Ctrl+Enter accepts the current directory when nothing is selected;
// Cmd/Ctrl+Up goes to the parent.
//
// Escape ladder (consumes the key until the last step): clear selection →
// clear filter → blur filter input → leave Escape for the modal to dismiss.
func FileBrowserPanel(cwd *string, filter *string, selected *int, selection *string, attrs FileBrowserAttrs) bool {
	normalizeFileBrowserAttrs(&attrs)
	if cwd == nil || *cwd == "" {
		return false
	}
	if filter == nil {
		empty := ""
		filter = &empty
	}
	if selected == nil {
		none := -1
		selected = &none
	}

	st := Use[fileBrowserPanelState]("file-browser-panel")

	accepted := false
	acceptCwd := func() {
		if !attrs.Dirs || !pathIsDir(*cwd) {
			return
		}
		if selection != nil {
			*selection = appendPathSlash(*cwd)
		}
		accepted = true
	}

	canChoose := attrs.Dirs && pathIsDir(*cwd)
	// Hint brightness follows selection from the previous frame; typing that
	// newly selects the first match dims ⌘⏎ on the next frame.
	cmdEnterActive := canChoose && *selected < 0

	// Path is the value Choose / ⌘⏎ will accept. Choose is pinned to the
	// right via Grow on the path cell; long paths wrap inside the leftover
	// width instead of shoving the button.
	Container(Attrs(Row, CrossMid, Gap(12), Expand), func() {
		Container(Attrs(Grow(1), Expand, Clip, Extrinsic), func() {
			w := GetResolvedSize()[0]
			if w < 1 {
				w = attrs.Width - 100
			}
			Label(fileSelectorDisplay("", *cwd), FontSize(13), FontWeight(WeightBold), TextColor(220, 25, 22, 1), TextWidth(w))
		})
		if attrs.Dirs {
			Container(Attrs(CrossAlign(AlignEnd), Gap(2)), func() {
				if ButtonExt("Choose", ButtonAttrs{Accent: AccentMeadow, Disabled: !canChoose}) && canChoose {
					acceptCwd()
				}
				hintClr := Vec4{0, 0, 55, 1}
				if !cmdEnterActive {
					hintClr = Vec4{0, 0, 75, 1}
				}
				Label(primaryEnterHint()+" accept", FontSize(10), TextColorVec(hintClr))
			})
		}
	})

	fAttrs := DefaultTextInputAttrs()
	fAttrs.FontSize = 13
	fAttrs.MinWidth = attrs.Width - 40
	if fAttrs.MinWidth < 200 {
		fAttrs.MinWidth = 200
	}
	fAttrs.NoUpDownLineEdges = true
	TextInputExt(filter, fAttrs)
	filterId := GetLastId()

	entries := browserListing(*cwd, attrs)
	entries = filterBrowserListing(entries, *filter)

	filtering := strings.TrimSpace(*filter) != ""
	if *cwd != st.lastCwd || *filter != st.lastFilter {
		if filtering && len(entries) > 0 {
			*selected = 0
		} else {
			*selected = -1
		}
		st.lastCwd = *cwd
		st.lastFilter = *filter
	}
	if *selected >= len(entries) {
		*selected = -1
	}

	navigate := func(path string) {
		*cwd = path
		*filter = ""
		*selected = -1
		st.lastCwd = path
		st.lastFilter = ""
	}

	activate := func(e browserEntry) {
		if e.up {
			parent := filepath.Dir(*cwd)
			if parent != *cwd {
				navigate(parent)
			}
		} else if e.dir {
			navigate(e.path)
		} else if attrs.Files {
			if selection != nil {
				*selection = e.path
			}
			accepted = true
		}
	}

	switch FrameInput.Key {
	case KeyDown:
		if len(entries) == 0 {
			break
		}
		if *selected < 0 {
			*selected = 0
		} else if *selected+1 < len(entries) {
			*selected++
		}
		VirtualListScrollIntoView(st, entries[*selected].key)
	case KeyUp:
		if InputState.Modifiers&editPrimaryMod != 0 {
			parent := filepath.Dir(*cwd)
			if parent != *cwd {
				navigate(parent)
			}
			break
		}
		if len(entries) == 0 {
			break
		}
		if *selected < 0 {
			*selected = len(entries) - 1
		} else if *selected > 0 {
			*selected--
		}
		VirtualListScrollIntoView(st, entries[*selected].key)
	case KeyEnter:
		primary := InputState.Modifiers&editPrimaryMod != 0
		if primary {
			if *selected < 0 {
				acceptCwd()
			}
		} else if *selected >= 0 {
			e := entries[*selected]
			if e.dir {
				activate(e)
			}
		}
	case KeyEscape:
		switch {
		case *selected >= 0:
			*selected = -1
			FrameInput.Key = 0
		case strings.TrimSpace(*filter) != "":
			*filter = ""
			*selected = -1
			st.lastFilter = ""
			FrameInput.Key = 0
		case IdHasFocus(filterId):
			ClearFocus()
			FrameInput.Key = 0
		}
	}

	if escHint := fileBrowserEscHint(*selected, *filter, IdHasFocus(filterId)); escHint != "" {
		Container(Attrs(Row, Expand, CrossMid), func() {
			Element(Attrs(Grow(1)))
			Label(escHint, FontSize(10), TextColor(0, 0, 55, 1))
		})
	}

	const maxRows = 14
	Container(Attrs(Expand, FixHeight(f32(maxRows)*fileBrowserRowH), Clip, Background(220, 8, 98, 1), Corners(4)), func() {
		VirtualListView(st, len(entries),
			func(i int) any { return entries[i].key },
			func(i int, _ f32) f32 { return fileBrowserRowH },
			func(i int, _ f32) {
				e := entries[i]
				Container(Attrs(Row, Expand, CrossMid, Gap(8), Pad2(5, 10), FixHeight(fileBrowserRowH), NoAnimate), func() {
					if *selected >= 0 && i == *selected {
						ModAttrs(Background(220, 45, 88, 1))
					} else if IsHovered() {
						ModAttrs(Background(220, 15, 93, 1))
					}
					if IsClicked() {
						activate(e)
					}
					name := e.name
					if e.dir && !e.up {
						name += string(os.PathSeparator)
					}
					clr := Vec4{220, 20, 22, 1}
					if e.up {
						clr = Vec4{0, 0, 45, 1}
					}
					Label(name, FontSize(12), TextColorVec(clr))
				})
			},
		)
	})

	return accepted
}

func primaryEnterHint() string {
	if editPrimaryMod == ModCmd {
		return "⌘⏎"
	}
	return "Ctrl+Enter"
}

// fileBrowserEscHint is the next Escape step under the filter box.
func fileBrowserEscHint(selected int, filter string, filterFocused bool) string {
	switch {
	case selected >= 0:
		return "Esc clear selection"
	case strings.TrimSpace(filter) != "":
		return "Esc clear filter"
	case filterFocused:
		return "Esc blur"
	default:
		return ""
	}
}

type browserEntry struct {
	key  string
	name string
	path string
	dir  bool
	up   bool
}

func browserListing(cwd string, attrs FileBrowserAttrs) []browserEntry {
	var out []browserEntry
	parent := filepath.Dir(cwd)
	if parent != cwd {
		out = append(out, browserEntry{key: "..", name: "..", path: parent, dir: true, up: true})
	}
	for _, e := range DirListing(cwd) {
		name := e.Name()
		if name == "." || name == ".." {
			continue
		}
		if !attrs.ShowHidden && strings.HasPrefix(name, ".") {
			continue
		}
		path := filepath.Join(cwd, name)
		if e.IsDir() {
			out = append(out, browserEntry{key: path, name: name, path: path, dir: true})
		} else if attrs.Files {
			out = append(out, browserEntry{key: path, name: name, path: path, dir: false})
		}
	}
	if len(out) > 1 {
		rest := out[1:]
		sort.SliceStable(rest, func(i, j int) bool {
			if rest[i].dir != rest[j].dir {
				return rest[i].dir
			}
			return strings.ToLower(rest[i].name) < strings.ToLower(rest[j].name)
		})
	}
	return out
}

// filterBrowserListing keeps entries whose name contains query
// (case-insensitive), including ".." only when it matches. Empty query
// returns the full listing.
func filterBrowserListing(entries []browserEntry, query string) []browserEntry {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return entries
	}
	out := entries[:0:0]
	for _, e := range entries {
		if strings.Contains(strings.ToLower(e.name), q) {
			out = append(out, e)
		}
	}
	return out
}

func normalizeFileBrowserAttrs(a *FileBrowserAttrs) {
	if a.Width == 0 {
		a.Width = 520
	}
	if !a.Dirs && !a.Files {
		a.Dirs = true
	}
	if a.Title == "" {
		switch {
		case a.Dirs && a.Files:
			a.Title = "Choose path"
		case a.Files:
			a.Title = "Choose file"
		default:
			a.Title = "Choose folder"
		}
	}
}

func resolveBrowserStart(attrStart, currentPath string) string {
	try := func(p string) (string, bool) {
		if p == "" {
			return "", false
		}
		p = expandTilde(p)
		if fi, err := os.Stat(p); err == nil && fi.IsDir() {
			if abs, err := filepath.Abs(p); err == nil {
				return abs, true
			}
			return p, true
		}
		parent := filepath.Dir(p)
		if fi, err := os.Stat(parent); err == nil && fi.IsDir() {
			if abs, err := filepath.Abs(parent); err == nil {
				return abs, true
			}
			return parent, true
		}
		return "", false
	}
	if attrStart != "" {
		if p, ok := try(attrStart); ok {
			return p
		}
	}
	if p, ok := try(currentPath); ok {
		return p
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return home
	}
	return "/"
}
