package widgets

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"
	"unsafe"

	. "go.hasen.dev/shirei"
)

const fileBrowserRowH f32 = 28

// FileBrowserSort controls entry ordering in FileBrowserPanel.
type FileBrowserSort int

const (
	FileBrowserSortName FileBrowserSort = iota
	FileBrowserSortModified
	FileBrowserSortSize
)

// FileBrowserAttrs configures the traditional file/directory browser.
type FileBrowserAttrs struct {
	Title string // zero → "Choose folder" / "Choose file" / "Choose path"
	Width f32    // modal / panel width; zero → 560

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

	// Start is the directory shown when DirectoryBrowse or FileBrowserModal
	// opens the dialog. Empty → home directory, else "/".
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
		Container(Attrs(Row, CrossMid, Gap(8), Expand), func() {
			Label(attrs.Title, FontSize(13), FontWeight(WeightBold), TextColorVec(scheme.List.Surface.Text))
			Element(Attrs(Grow(1)))
			if ButtonStyled("Cancel", ButtonAttrs{}, DefaultButtonLook(), scheme.Buttons.Default, scheme.FocusRing) {
				closeDialog()
			}
		})

		if fileBrowserPanel(&st.cwd, &st.filter, &st.selected, text, attrs, scheme, scrollBar) {
			closeDialog()
			return
		}
	})
}

type fileBrowserModalState struct {
	wasOpen       bool
	cwd           string
	filter        string
	selected      int
	pendingAccept bool
	pendingPath   string
}

// FileBrowserModal renders a modal file/directory picker controlled by *open.
// It retains the last browsed directory internally, draws a title bar and
// Cancel button around FileBrowserPanel, and closes (*open = false) on accept,
// Cancel, Escape, or scrim click. Returns true on the frame a path is accepted
// (writing it to *selection when non-nil).
func FileBrowserModal(open *bool, selection *string, attrs FileBrowserAttrs) bool {
	return fileBrowserModal(open, selection, attrs, CurrentColorScheme, ScrollBars)
}

// FileBrowserModalStyled is FileBrowserModal with an explicit ColorScheme.
func FileBrowserModalStyled(open *bool, selection *string, attrs FileBrowserAttrs, scheme ColorScheme) bool {
	return fileBrowserModal(open, selection, attrs, scheme, scrollBarWithStyle(scheme.ScrollBar))
}

func fileBrowserModal(open *bool, selection *string, attrs FileBrowserAttrs, scheme ColorScheme, scrollBar ScrollBarFn) bool {
	normalizeFileBrowserAttrs(&attrs)
	st := Use[fileBrowserModalState]("file-browser-modal")
	if st.pendingAccept {
		st.pendingAccept = false
		path := st.pendingPath
		st.pendingPath = ""
		st.wasOpen = false
		st.filter = ""
		st.selected = -1
		if open != nil {
			*open = false
		}
		if selection != nil {
			*selection = path
		}
		return true
	}
	if open == nil || !*open {
		st.wasOpen = false
		return false
	}
	if !st.wasOpen {
		st.wasOpen = true
		st.filter = ""
		st.selected = -1
		if st.cwd == "" || !pathIsDir(st.cwd) {
			cur := ""
			if selection != nil {
				cur = *selection
			}
			st.cwd = resolveBrowserStart(attrs.Start, cur)
		}
	}

	closeDialog := func() {
		*open = false
		st.wasOpen = false
		st.filter = ""
		st.selected = -1
	}

	panelAttrs := attrs
	panelAttrs.Width = attrs.Width - 32
	if panelAttrs.Width < 260 {
		panelAttrs.Width = 260
	}

	ModalStyled(attrs.Width, closeDialog, ModalStyleForScheme(scheme), func() {
		Container(Attrs(Row, CrossMid, Gap(8), Expand), func() {
			Label(attrs.Title, FontSize(13), FontWeight(WeightBold), TextColorVec(scheme.List.Surface.Text))
			Element(Attrs(Grow(1)))
			if ButtonStyled("Cancel", ButtonAttrs{}, DefaultButtonLook(), scheme.Buttons.Default, scheme.FocusRing) {
				closeDialog()
			}
		})

		var picked string
		if fileBrowserPanel(&st.cwd, &st.filter, &st.selected, &picked, panelAttrs, scheme, scrollBar) {
			// ModalStyled runs its body in the overlay pass after fileBrowserModal
			// has returned; deliver the accepted path and close on the next frame.
			st.pendingAccept = true
			st.pendingPath = picked
			RequestNextFrame()
			return
		}
	})
	return false
}

type browserPlace struct {
	label string
	path  string
	icon  IconGlyph
}

type fileBrowserPanelState struct {
	lastCwd    string
	lastFilter string

	initedToggles bool
	showHidden    bool
	showAllFiles  bool
	sortBy        FileBrowserSort
	sortDesc      bool

	placesLoaded bool
	places       []browserPlace

	// Cached sorted listing for the current directory.
	cacheCwd       string
	cacheRawPtr    *os.DirEntry
	cacheRawLen    int
	cacheHidden    bool
	cacheAllFiles  bool
	cacheDirs      bool
	cacheFiles     bool
	cacheExtsKey   string
	cacheSortBy    FileBrowserSort
	cacheSortDesc  bool
	cacheEntries   []browserEntry
	cacheReadErr   string
	cacheRawNonDir int // files present before extension filtering
}

// FileBrowserPanel draws a one-level listing for *cwd (DirListing), with
// clickable breadcrumbs, quick-jump places, sorting, show-hidden and all-files
// toggles, and a filter/path input box.
//
// Selection (*selected, -1 = none): empty filter → nothing selected; typing a
// filter selects the first match. Up/Down move the highlight (from none:
// Down → first, Up → last). Enter activates the selected directory or file, or
// jumps to a direct path typed in the filter box; Cmd/Ctrl+Enter accepts the
// current directory when nothing is selected; Cmd/Ctrl+Up goes to the parent;
// Cmd/Ctrl+Shift+. toggles hidden files.
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
	if !st.initedToggles {
		st.initedToggles = true
		st.showHidden = attrs.ShowHidden
		DropDirListing(*cwd)
	}
	if !st.placesLoaded {
		st.placesLoaded = true
		st.places = discoverBrowserPlaces()
	}

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

	refresh := func() {
		DropDirListing(*cwd)
		st.cacheCwd = ""
	}

	navigate := func(path string) {
		*cwd = path
		*filter = ""
		*selected = -1
		st.lastCwd = path
		st.lastFilter = ""
		DropDirListing(path)
		st.cacheCwd = ""
	}

	canChoose := attrs.Dirs && pathIsDir(*cwd)
	cmdEnterActive := canChoose && *selected < 0

	// Header: clickable path breadcrumbs on the left, Refresh (and Choose when Dirs) on the right.
	Container(Attrs(Row, CrossMid, Gap(8), Expand), func() {
		crumbMaxW := attrs.Width - 84
		if attrs.Dirs {
			crumbMaxW = attrs.Width - 190
		}
		if crumbMaxW < 120 {
			crumbMaxW = 120
		}
		Container(Attrs(Row, Wrap, CrossMid, Gap(2), Grow(1), MaxWidth(crumbMaxW)), func() {
			crumbs := pathBreadcrumbs(*cwd)
			for i, cr := range crumbs {
				isLast := i == len(crumbs)-1
				if i > 0 {
					Icon(SymRight, FontSize(10), TextColorVec(scheme.List.Muted))
				}
				Container(Attrs(Pad2(2, 5), Corners(4), NoAnimate), func() {
					if IsHovered() {
						ModAttrs(BackgroundVec(scheme.List.Hovered.Background))
					}
					if IsClicked() {
						if isLast {
							refresh()
						} else {
							navigate(cr.path)
						}
					}
					weight := WeightNormal
					clr := scheme.List.Muted
					if isLast {
						weight = WeightBold
						clr = scheme.List.Surface.Text
					} else if IsHovered() {
						clr = scheme.List.Hovered.Text
					}
					Label(cr.label, FontSize(12), FontWeight(weight), TextColorVec(clr))
				})
			}
		})
		if browserMiniPill(SymRefresh, "Refresh", false, scheme) {
			refresh()
		}
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

	// Quick-jump places bar (Home, Desktop, Downloads, Documents, Movies, drives/volumes).
	if len(st.places) > 0 {
		Container(Attrs(Row, Wrap, CrossMid, Gap(4), Expand), func() {
			for _, pl := range st.places {
				active := pathsEqual(*cwd, pl.path)
				if browserMiniPill(pl.icon, pl.label, active, scheme) {
					navigate(pl.path)
				}
			}
		})
	}

	// Filter / direct path input.
	fAttrs := DefaultTextInputAttrs()
	fAttrs.FontSize = 13
	fAttrs.Placeholder = "Filter or path…"
	fAttrs.NoAutoFocus = attrs.NoAutoFocus || isMobileBrowser()
	fAttrs.MinWidth = attrs.Width - 20
	if fAttrs.MinWidth < 180 {
		fAttrs.MinWidth = 180
	}
	fAttrs.NoUpDownLineEdges = true
	TextInputStyled(filter, fAttrs, scheme.TextInput, scheme.FocusRing)
	filterId := GetLastId()

	// Build/fetch cached listing for *cwd.
	effAttrs := attrs
	effAttrs.ShowHidden = st.showHidden
	if st.showAllFiles {
		effAttrs.Exts = nil
	}
	allEntries, readErr, hiddenByExt := cachedBrowserListing(st, *cwd, effAttrs, st.sortBy, st.sortDesc)
	entries := filterBrowserListing(allEntries, *filter)

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

	tryDirectPath := func(raw string) bool {
		raw = strings.TrimSpace(raw)
		if !looksLikeDirectPath(raw) {
			return false
		}
		p := expandTilde(raw)
		if !filepath.IsAbs(p) {
			p = filepath.Join(*cwd, p)
		}
		fi, err := os.Stat(p)
		if err != nil {
			return false
		}
		if abs, err := filepath.Abs(p); err == nil {
			p = abs
		}
		if fi.IsDir() {
			navigate(p)
			return true
		}
		if attrs.Files {
			if selection != nil {
				*selection = p
			}
			accepted = true
			return true
		}
		return false
	}

	mods := GetInputState().Modifiers
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
		if mods&editPrimaryMod() != 0 {
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
	case KeyCode('.'), KeyH:
		if mods&editPrimaryMod() != 0 && mods&ModShift != 0 {
			st.showHidden = !st.showHidden
			GetFrameInput().Key = 0
		}
	case KeyEnter:
		primary := mods&editPrimaryMod() != 0
		if primary {
			if *selected < 0 {
				acceptCwd()
			}
		} else if *selected >= 0 {
			activate(entries[*selected])
		} else if filtering {
			tryDirectPath(*filter)
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

	// Toolbar row: sort pills, Hidden toggle, All-files toggle, and right-aligned Esc hint.
	escHint := fileBrowserEscHint(*selected, *filter, IdHasFocus(filterId))
	if escHint == "" {
		escHint = " "
	}
	Container(Attrs(Row, Wrap, Expand, CrossMid, Gap(6)), func() {
		sortIcon := SymSortAsc
		if st.sortDesc {
			sortIcon = SymSortDesc
		}
		for _, opt := range []struct {
			mode        FileBrowserSort
			label       string
			defaultDesc bool
		}{
			{FileBrowserSortName, "Name", false},
			{FileBrowserSortModified, "Date", true},
			{FileBrowserSortSize, "Size", true},
		} {
			active := st.sortBy == opt.mode
			var ic IconGlyph
			if active {
				ic = sortIcon
			}
			if browserMiniPill(ic, opt.label, active, scheme) {
				if active {
					st.sortDesc = !st.sortDesc
				} else {
					st.sortBy = opt.mode
					st.sortDesc = opt.defaultDesc
				}
			}
		}

		hidIcon := SymHide
		if st.showHidden {
			hidIcon = SymShow
		}
		if browserMiniPill(hidIcon, "Hidden", st.showHidden, scheme) {
			st.showHidden = !st.showHidden
		}

		if attrs.Files && len(attrs.Exts) > 0 {
			if browserMiniPill(IconGlyph{}, "All files", st.showAllFiles, scheme) {
				st.showAllFiles = !st.showAllFiles
			}
		}

		Element(Attrs(Grow(1)))
		if !isMobileBrowser() {
			Label(escHint, FontSize(10), TextColorVec(scheme.List.Muted))
		}
	})

	// Entry list viewport (adapts height to mobile / small windows).
	rowH := browserRowHeight()
	viewportH := browserListHeight(rowH)
	showMeta := attrs.Width >= 380
	now := time.Now()
	Container(Attrs(Expand, FixHeight(viewportH), Clip, BackgroundVec(scheme.List.Surface.Background), Corners(4)), func() {
		if readErr != "" {
			Container(Attrs(Expand, Grow(1), Center, Gap(8), Pad(16)), func() {
				Label("Cannot open folder: "+readErr, FontSize(12), TextColorVec(scheme.List.Error))
				parent := filepath.Dir(*cwd)
				if parent != *cwd {
					if ButtonStyled("Go to parent", ButtonAttrs{}, DefaultButtonLook(), scheme.Buttons.Default, scheme.FocusRing) {
						navigate(parent)
					}
				}
			})
			return
		}

		hasOnlyUp := len(entries) == 0 || (len(entries) == 1 && entries[0].up)
		if len(entries) > 0 {
			listH := viewportH
			if hasOnlyUp {
				listH = rowH
			}
			Container(Attrs(Expand, FixHeight(listH), Clip), func() {
				virtualListView(st, VirtualListAttrs{
					ItemCount: len(entries),
					ItemKey:   func(i int) any { return entries[i].key },
					ItemHeight: func(i int, _ f32) f32 {
						return rowH
					},
					ItemView: func(i int, _ f32) {
						e := entries[i]
						Container(Attrs(Row, Expand, CrossMid, Gap(8), Pad2(5, 10), FixHeight(rowH), NoAnimate), func() {
							if *selected >= 0 && i == *selected {
								ModAttrs(BackgroundVec(scheme.List.Selected.Background))
							} else if IsHovered() {
								ModAttrs(BackgroundVec(scheme.List.Hovered.Background))
							}
							if IsClicked() {
								activate(e)
							}
							icon, iconClr := browserEntryIcon(e, scheme)
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
							Container(Attrs(Row, CrossMid, Grow(1), Expand, Clip, Extrinsic), func() {
								Label(name, FontSize(12), TextColorVec(clr))
							})

							if showMeta && !e.up {
								metaClr := scheme.List.Muted
								if i == *selected {
									metaClr = scheme.List.Selected.Text
								}
								if !e.dir && e.hasInfo {
									Container(Attrs(Row, CrossMid, MainAlign(AlignEnd), FixWidth(62)), func() {
										Label(formatByteSize(e.size), FontSize(11), TextColorVec(metaClr))
									})
								}
								if !e.modTime.IsZero() {
									Container(Attrs(Row, CrossMid, MainAlign(AlignEnd), FixWidth(76)), func() {
										Label(formatShortModTime(e.modTime, now), FontSize(11), TextColorVec(metaClr))
									})
								}
							}
						})
					},
				}, scrollBar)
			})
		}

		if hasOnlyUp {
			Container(Attrs(Expand, Grow(1), Center, Gap(6), Pad(16)), func() {
				var msg string
				switch {
				case filtering:
					msg = fmt.Sprintf("No matches for %q", strings.TrimSpace(*filter))
				case hiddenByExt > 0 && attrs.Files && len(attrs.Exts) > 0 && !st.showAllFiles:
					msg = fmt.Sprintf("No matching files (%s) — %d other file(s) hidden", formatExtsSummary(attrs.Exts), hiddenByExt)
				default:
					msg = "Empty folder"
				}
				Label(msg, FontSize(12), TextColorVec(scheme.List.Muted))
			})
		}
	})

	return accepted
}

func isMobileBrowser() bool {
	return runtime.GOOS == "android" || runtime.GOOS == "ios" || !GetHost().HardwareKeyboard
}

func browserRowHeight() f32 {
	if isMobileBrowser() {
		if cs := GetHost().ComfortScale; cs > 1 {
			h := fileBrowserRowH * cs
			if h > 36 {
				return 36
			}
			return h
		}
		return 34
	}
	return fileBrowserRowH
}

func browserListHeight(rowH f32) f32 {
	maxH := f32(14) * rowH
	winH := GetHost().WindowSize[1]
	if winH <= 0 {
		return maxH
	}
	if isMobileBrowser() {
		// Leave room for modal chrome and a soft keyboard on mobile screens.
		capH := min(winH-260, winH*0.32)
		if capH > 260 {
			capH = 260
		}
		minH := max(4*rowH, 120)
		if capH < minH {
			capH = minH
		}
		if capH > winH-180 && winH > 260 {
			capH = winH - 180
		}
		return min(maxH, capH)
	}
	if avail := winH - 260; avail < maxH {
		return max(4*rowH, avail)
	}
	return maxH
}

func browserMiniPill(icon IconGlyph, label string, active bool, scheme ColorScheme) bool {
	clicked := false
	Container(Attrs(Row, CrossMid, Gap(4), Pad2(3, 7), Corners(5), NoAnimate), func() {
		if active {
			ModAttrs(BackgroundVec(scheme.List.Selected.Background))
		} else if IsHovered() {
			ModAttrs(BackgroundVec(scheme.List.Hovered.Background))
		} else {
			ModAttrs(BackgroundVec(scheme.List.Surface.Background))
		}
		if IsClicked() {
			clicked = true
		}
		clr := scheme.List.Muted
		if active {
			clr = scheme.List.Selected.Text
		} else if IsHovered() {
			clr = scheme.List.Hovered.Text
		}
		if icon != (IconGlyph{}) {
			Icon(icon, FontSize(11), TextColorVec(clr))
		}
		Label(label, FontSize(11), TextColorVec(clr))
	})
	return clicked
}

func browserEntryIcon(e browserEntry, scheme ColorScheme) (IconGlyph, Vec4) {
	switch {
	case e.up:
		return SymArrowUp, scheme.List.Muted
	case e.dir:
		return SymFolder, scheme.List.Folder
	case fileMatchesExts(e.name, []string{".mp4", ".webm", ".mkv", ".mov", ".m4v", ".avi", ".ts", ".flv", ".ogv"}):
		return SymVideo, scheme.List.File
	case fileMatchesExts(e.name, []string{".mp3", ".wav", ".flac", ".ogg", ".opus", ".m4a", ".aac"}):
		return SymAudio, scheme.List.File
	case fileMatchesExts(e.name, []string{".png", ".jpg", ".jpeg", ".webp", ".gif", ".svg", ".ico"}):
		return SymImage, scheme.List.File
	default:
		return SymFile, scheme.List.Muted
	}
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

type pathCrumb struct {
	label string
	path  string
}

func pathBreadcrumbs(abs string) []pathCrumb {
	clean := filepath.Clean(abs)
	if clean == "" {
		return nil
	}

	home, _ := os.UserHomeDir()
	if home != "" {
		home = filepath.Clean(home)
		if clean == home {
			return []pathCrumb{{label: "~", path: home}}
		}
		prefix := home + string(os.PathSeparator)
		if strings.HasPrefix(clean, prefix) {
			crumbs := []pathCrumb{{label: "~", path: home}}
			rest := clean[len(prefix):]
			cur := home
			for _, part := range strings.Split(rest, string(os.PathSeparator)) {
				if part == "" {
					continue
				}
				cur = filepath.Join(cur, part)
				crumbs = append(crumbs, pathCrumb{label: part, path: cur})
			}
			return crumbs
		}
	}

	vol := filepath.VolumeName(clean)
	rest := strings.TrimPrefix(clean, vol)
	sep := string(os.PathSeparator)
	var crumbs []pathCrumb
	cur := vol
	if strings.HasPrefix(rest, sep) {
		rootLabel := sep
		if vol != "" {
			rootLabel = vol + sep
		}
		cur = vol + sep
		crumbs = append(crumbs, pathCrumb{label: rootLabel, path: cur})
		rest = strings.TrimPrefix(rest, sep)
	}
	for _, part := range strings.Split(rest, sep) {
		if part == "" {
			continue
		}
		cur = filepath.Join(cur, part)
		crumbs = append(crumbs, pathCrumb{label: part, path: cur})
	}
	if len(crumbs) == 0 {
		crumbs = append(crumbs, pathCrumb{label: clean, path: clean})
	}
	return crumbs
}

func discoverBrowserPlaces() []browserPlace {
	var places []browserPlace
	addIfDir := func(label, path string, icon IconGlyph) {
		if path == "" {
			return
		}
		if st, err := os.Stat(path); err == nil && st.IsDir() {
			if abs, err := filepath.Abs(path); err == nil {
				path = abs
			}
			for _, existing := range places {
				if pathsEqual(existing.path, path) {
					return
				}
			}
			places = append(places, browserPlace{label: label, path: path, icon: icon})
		}
	}

	if home, err := os.UserHomeDir(); err == nil && home != "" {
		addIfDir("Home", home, SymHome)
		addIfDir("Desktop", filepath.Join(home, "Desktop"), SymFolder)
		addIfDir("Downloads", filepath.Join(home, "Downloads"), SymDownload)
		addIfDir("Documents", filepath.Join(home, "Documents"), SymFolder)
		if runtime.GOOS == "darwin" {
			addIfDir("Movies", filepath.Join(home, "Movies"), SymVideo)
		} else {
			addIfDir("Videos", filepath.Join(home, "Videos"), SymVideo)
		}
	}

	if runtime.GOOS == "windows" {
		for drv := byte('A'); drv <= 'Z'; drv++ {
			root := fmt.Sprintf("%c:\\", drv)
			if st, err := os.Stat(root); err == nil && st.IsDir() {
				addIfDir(fmt.Sprintf("%c:", drv), root, SymFolder)
			}
		}
	} else if runtime.GOOS == "android" {
		addIfDir("Movies", "/sdcard/Movies", SymVideo)
		addIfDir("DCIM", "/sdcard/DCIM", SymFolder)
		addIfDir("Download", "/sdcard/Download", SymDownload)
	} else if runtime.GOOS == "darwin" {
		if ents, err := os.ReadDir("/Volumes"); err == nil {
			for _, e := range ents {
				if strings.HasPrefix(e.Name(), ".") {
					continue
				}
				full := filepath.Join("/Volumes", e.Name())
				if target, err := os.Readlink(full); err == nil && target == "/" {
					continue
				}
				addIfDir(e.Name(), full, SymFolder)
			}
		}
	}
	return places
}

func pathsEqual(a, b string) bool {
	ca := filepath.Clean(a)
	cb := filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(ca, cb)
	}
	return ca == cb
}

func looksLikeDirectPath(s string) bool {
	if s == "~" || strings.HasPrefix(s, "~/") || strings.HasPrefix(s, "~\\") || strings.HasPrefix(s, "/") {
		return true
	}
	if len(s) >= 3 && ((s[0] >= 'A' && s[0] <= 'Z') || (s[0] >= 'a' && s[0] <= 'z')) && s[1] == ':' && (s[2] == '\\' || s[2] == '/') {
		return true
	}
	return false
}

type browserEntry struct {
	key     string
	name    string
	path    string
	dir     bool
	up      bool
	size    int64
	modTime time.Time
	hasInfo bool
}

func cachedBrowserListing(st *fileBrowserPanelState, cwd string, attrs FileBrowserAttrs, sortBy FileBrowserSort, sortDesc bool) ([]browserEntry, string, int) {
	raw := DirListing(cwd)
	rawPtr := unsafe.SliceData(raw)
	extsKey := strings.Join(attrs.Exts, "\x00")
	if st.cacheCwd == cwd &&
		st.cacheRawPtr == rawPtr &&
		st.cacheRawLen == len(raw) &&
		st.cacheHidden == attrs.ShowHidden &&
		st.cacheDirs == attrs.Dirs &&
		st.cacheFiles == attrs.Files &&
		st.cacheExtsKey == extsKey &&
		st.cacheSortBy == sortBy &&
		st.cacheSortDesc == sortDesc {
		return st.cacheEntries, st.cacheReadErr, st.cacheRawNonDir
	}

	readErr := ""
	if len(raw) == 0 {
		// Distinguish genuinely empty directory from permission/IO error.
		if _, err := os.ReadDir(cwd); err != nil {
			readErr = err.Error()
		}
	}

	entries, hiddenByExt := buildBrowserListing(cwd, raw, attrs, sortBy, sortDesc)
	st.cacheCwd = cwd
	st.cacheRawPtr = rawPtr
	st.cacheRawLen = len(raw)
	st.cacheHidden = attrs.ShowHidden
	st.cacheDirs = attrs.Dirs
	st.cacheFiles = attrs.Files
	st.cacheExtsKey = extsKey
	st.cacheSortBy = sortBy
	st.cacheSortDesc = sortDesc
	st.cacheEntries = entries
	st.cacheReadErr = readErr
	st.cacheRawNonDir = hiddenByExt
	return entries, readErr, hiddenByExt
}

func browserListing(cwd string, attrs FileBrowserAttrs) []browserEntry {
	entries, _ := buildBrowserListing(cwd, DirListing(cwd), attrs, FileBrowserSortName, false)
	return entries
}

func buildBrowserListing(cwd string, raw []os.DirEntry, attrs FileBrowserAttrs, sortBy FileBrowserSort, sortDesc bool) ([]browserEntry, int) {
	var out []browserEntry
	parent := filepath.Dir(cwd)
	hasUp := parent != cwd
	if hasUp {
		out = append(out, browserEntry{key: "..", name: "..", path: parent, dir: true, up: true})
	}
	hiddenByExt := 0
	for _, e := range raw {
		name := e.Name()
		if name == "." || name == ".." {
			continue
		}
		if !attrs.ShowHidden && strings.HasPrefix(name, ".") {
			continue
		}
		path := filepath.Join(cwd, name)
		isDir := e.IsDir()
		var fi os.FileInfo
		if !isDir && e.Type()&os.ModeSymlink != 0 {
			if st, err := os.Stat(path); err == nil {
				fi = st
				isDir = st.IsDir()
			}
		}
		if isDir {
			be := browserEntry{key: path, name: name, path: path, dir: true}
			if fi == nil {
				fi, _ = e.Info()
			}
			if fi != nil {
				be.modTime = fi.ModTime()
				be.hasInfo = true
			}
			out = append(out, be)
		} else if attrs.Files {
			if !fileMatchesExts(name, attrs.Exts) {
				hiddenByExt++
				continue
			}
			be := browserEntry{key: path, name: name, path: path, dir: false}
			if fi == nil {
				fi, _ = e.Info()
			}
			if fi != nil {
				be.size = fi.Size()
				be.modTime = fi.ModTime()
				be.hasInfo = true
			}
			out = append(out, be)
		}
	}

	sortStart := 0
	if hasUp {
		sortStart = 1
	}
	if len(out) > sortStart+1 {
		sortBrowserEntries(out[sortStart:], sortBy, sortDesc)
	}
	return out, hiddenByExt
}

func sortBrowserEntries(entries []browserEntry, sortBy FileBrowserSort, desc bool) {
	slices.SortStableFunc(entries, func(a, b browserEntry) int {
		// Directories always stay grouped above files.
		if a.dir != b.dir {
			if a.dir {
				return -1
			}
			return 1
		}
		var cmp int
		switch sortBy {
		case FileBrowserSortModified:
			switch {
			case a.modTime.Before(b.modTime):
				cmp = -1
			case a.modTime.After(b.modTime):
				cmp = 1
			}
		case FileBrowserSortSize:
			switch {
			case a.size < b.size:
				cmp = -1
			case a.size > b.size:
				cmp = 1
			}
		}
		if cmp != 0 {
			if desc {
				return -cmp
			}
			return cmp
		}
		nameCmp := strings.Compare(strings.ToLower(a.name), strings.ToLower(b.name))
		if sortBy == FileBrowserSortName && desc {
			return -nameCmp
		}
		return nameCmp
	})
}

func formatByteSize(n int64) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%d B", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	case n < 1024*1024*1024:
		return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
	default:
		return fmt.Sprintf("%.2f GB", float64(n)/(1024*1024*1024))
	}
}

func formatShortModTime(t, now time.Time) string {
	if t.Year() == now.Year() && t.YearDay() == now.YearDay() {
		return t.Format("15:04")
	}
	if t.Year() == now.Year() {
		return t.Format("Jan 2")
	}
	return t.Format("2006-01-02")
}

func formatExtsSummary(exts []string) string {
	parts := make([]string, 0, min(len(exts), 4))
	for i, e := range exts {
		if i == 4 {
			parts = append(parts, "…")
			break
		}
		e = strings.TrimSpace(e)
		if e != "" && !strings.HasPrefix(e, ".") {
			e = "." + e
		}
		parts = append(parts, e)
	}
	return strings.Join(parts, ", ")
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
		a.Width = 560
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
