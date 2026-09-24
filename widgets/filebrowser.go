package widgets

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	. "go.hasen.dev/shirei"
)

const fileBrowserRowH f32 = 28

// FileBrowserAttrs configures the traditional file/directory browser.
type FileBrowserAttrs struct {
	Title string // zero → "Choose folder" / "Choose file" / "Choose path"
	Width f32    // modal / panel width; zero → 520

	// Dirs / Files control what can be chosen. Directories always appear so
	// the user can navigate; files appear only when Files is set.
	// If neither is set, Dirs defaults to true.
	Dirs  bool
	Files bool

	// Exts, when non-empty and Files is set, limits listed files to those
	// whose extension matches (case-insensitive). Entries may be with or
	// without a leading dot (".png" and "png" are the same). Directories
	// are never filtered by Exts. Empty Exts → all files.
	Exts []string

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
	directoryBrowse(text, attrs, CurrentColorScheme, ScrollBars)
}

// DirectoryBrowseStyled supplies explicit colors for the composite and its stock children.
func DirectoryBrowseStyled(text *string, attrs FileBrowserAttrs, scheme ColorScheme) {
	directoryBrowse(text, attrs, scheme, scrollBarWithStyle(scheme.ScrollBar))
}

func directoryBrowse(text *string, attrs FileBrowserAttrs, scheme ColorScheme, scrollBar ScrollBarFn) {
	normalizeFileBrowserAttrs(&attrs)

	st := Use[directoryBrowseState]("directory-browse")

	Container(Attrs(Row, CrossMid, Gap(8), Expand), func() {
		NextAccessRole("group")
		AssignAccess()
		input := DefaultTextInputAttrs()
		input.NoAutoFocus = attrs.NoAutoFocus
		if attrs.MinWidth > 0 {
			input.MinWidth = attrs.MinWidth
		} else {
			input.MinWidth = 280
		}
		focus := scheme.FocusRing
		if text != nil && *text != "" && attrs.Dirs && !attrs.Files && !pathIsDir(*text) {
			focus = scheme.List.Error
		}
		Container(Attrs(Expand), func() {
			TextInputStyled(text, input, scheme.TextInput, focus)
		})
		if ButtonStyled("Browse…", ButtonAttrs{}, DefaultButtonLook(), scheme.Buttons.Default, scheme.FocusRing) {
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

	ModalStyled(attrs.Width, closeDialog, ModalStyleForScheme(scheme), func() {
		Label(attrs.Title, FontSize(13), FontWeight(WeightBold), TextColorVec(scheme.List.Surface.Text))

		if fileBrowserPanel(&st.cwd, &st.filter, &st.selected, text, attrs, scheme, scrollBar) {
			closeDialog()
			return
		}

		if ButtonStyled("Cancel", ButtonAttrs{}, DefaultButtonLook(), scheme.Buttons.Default, scheme.FocusRing) {
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
	return fileBrowserPanel(cwd, filter, selected, selection, attrs, CurrentColorScheme, ScrollBars)
}

// FileBrowserPanelStyled uses an explicit scheme for its list, fields, and buttons.
func FileBrowserPanelStyled(cwd *string, filter *string, selected *int, selection *string, attrs FileBrowserAttrs, scheme ColorScheme) bool {
	return fileBrowserPanel(cwd, filter, selected, selection, attrs, scheme, scrollBarWithStyle(scheme.ScrollBar))
}

func fileBrowserPanel(cwd *string, filter *string, selected *int, selection *string, attrs FileBrowserAttrs, scheme ColorScheme, scrollBar ScrollBarFn) bool {
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
			w := GetResolvedWidth()
			if w < 1 {
				w = attrs.Width - 100
			}
			Container(Attrs(MaxWidth(w)), func() {
				Label(fileSelectorDisplay("", *cwd), FontSize(13), FontWeight(WeightBold), TextColorVec(scheme.List.Surface.Text))
			})
		})
		if attrs.Dirs {
			Container(Attrs(CrossAlign(AlignEnd), Gap(2)), func() {
				if ButtonStyled("Choose", ButtonAttrs{Disabled: !canChoose}, DefaultButtonLook(), scheme.Buttons.Primary, scheme.FocusRing) && canChoose {
					acceptCwd()
				}
				hintClr := scheme.List.Muted
				if !cmdEnterActive {
					hintClr = scheme.List.Disabled
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
	TextInputStyled(filter, fAttrs, scheme.TextInput, scheme.FocusRing)
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

	switch GetFrameInput().Key {
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
		if GetInputState().Modifiers&editPrimaryMod() != 0 {
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
		primary := GetInputState().Modifiers&editPrimaryMod() != 0
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
			GetFrameInput().Key = 0
		case strings.TrimSpace(*filter) != "":
			*filter = ""
			*selected = -1
			st.lastFilter = ""
			GetFrameInput().Key = 0
		case IdHasFocus(filterId):
			ClearFocus()
			GetFrameInput().Key = 0
		}
	}

	// Keep the hint row's height stable when focus changes, so controls stay
	// under the pointer throughout a click in a centered dialog.
	escHint := fileBrowserEscHint(*selected, *filter, IdHasFocus(filterId))
	if escHint == "" {
		escHint = " "
	}
	Container(Attrs(Row, Expand, CrossMid), func() {
		Element(Attrs(Grow(1)))
		Label(escHint, FontSize(10), TextColorVec(scheme.List.Muted))
	})

	const maxRows = 14
	Container(Attrs(Expand, FixHeight(f32(maxRows)*fileBrowserRowH), Clip, BackgroundVec(scheme.List.Surface.Background), Corners(4)), func() {
		virtualListView(st, VirtualListAttrs{ItemCount: len(entries), ItemKey: func(i int) any { return entries[i].key }, ItemHeight: func(i int, _ f32) f32 { return fileBrowserRowH }, ItemView: func(i int, _ f32) {
			e := entries[i]
			Container(Attrs(Row, Expand, CrossMid, Gap(8), Pad2(5, 10), FixHeight(fileBrowserRowH), NoAnimate), func() {
				if *selected >= 0 && i == *selected {
					ModAttrs(BackgroundVec(scheme.List.Selected.Background))
				} else if IsHovered() {
					ModAttrs(BackgroundVec(scheme.List.Hovered.Background))
				}
				if IsClicked() {
					activate(e)
				}
				// Glyph from bundled Microns (folder vs file) so dirs read at a glance.
				icon := SymFile
				iconClr := scheme.List.Muted
				switch {
				case e.up:
					icon = SymArrowUp
					iconClr = scheme.List.Muted
				case e.dir:
					icon = SymFolder
					iconClr = scheme.List.Folder // muted folder tint
				case fileMatchesExts(e.name, []string{".png", ".jpg", ".jpeg", ".webp", ".gif", ".svg", ".ico"}):
					icon = SymImage
					iconClr = scheme.List.File
				}
				if i == *selected {
					iconClr = scheme.List.Selected.Text
				}
				Icon(icon, FontSize(14), TextColorVec(iconClr))
				name := e.name
				if e.dir && !e.up {
					name += string(os.PathSeparator)
				}
				clr := scheme.List.Surface.Text
				if e.up {
					clr = scheme.List.Muted
				}
				if i == *selected {
					clr = scheme.List.Selected.Text
				} else if IsHovered() {
					clr = scheme.List.Hovered.Text
				}
				Label(name, FontSize(12), TextColorVec(clr))
			})
		},
		}, scrollBar)
	})

	return accepted
}

func primaryEnterHint() string {
	if editPrimaryMod() == ModCmd {
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
		} else if attrs.Files && fileMatchesExts(name, attrs.Exts) {
			out = append(out, browserEntry{key: path, name: name, path: path, dir: false})
		}
	}
	if len(out) > 1 {
		rest := out[1:]
		slices.SortStableFunc(rest, func(a, b browserEntry) int {
			if a.dir != b.dir {
				if a.dir {
					return -1
				}
				return 1
			}
			return strings.Compare(strings.ToLower(a.name), strings.ToLower(b.name))
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

// fileMatchesExts reports whether name's extension is in exts (or exts is empty).
func fileMatchesExts(name string, exts []string) bool {
	if len(exts) == 0 {
		return true
	}
	ext := strings.ToLower(filepath.Ext(name))
	if ext == "" {
		return false
	}
	for _, want := range exts {
		want = strings.ToLower(strings.TrimSpace(want))
		if want == "" {
			continue
		}
		if !strings.HasPrefix(want, ".") {
			want = "." + want
		}
		if ext == want {
			return true
		}
	}
	return false
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
