package main

import (
	"fmt"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"go.hasen.dev/shirei/ext/darkmode"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

type f32 = float32

const (
	// Base row metrics; actual commit height varies with display options.
	historyRowLineH f32 = 16
	historyRowPad   f32 = 14 // Pad4 top+bottom
	historyRowGap   f32 = 2
	historyRowMinH  f32 = 32 // synthetic slots
	diffLineH       f32 = 18
	fileHeaderH     f32 = 48
	hunkHeaderH     f32 = 20
	// Fallback ImageWipe row height before DecodeConfig dims are ready.
	// Once dims land, height follows width-fit (capped) — same rule as paint.
	imageWipeRowH        f32 = 420
	imageWipePadX        f32 = 24  // Pad2 horizontal 12+12
	imageWipePadY        f32 = 16  // Pad2 vertical 8+8
	imageWipeMaxContentH f32 = 404 // imageWipeRowH - imageWipePadY
	imageWipeMinViewW    f32 = 120
	imageWipeMinViewH    f32 = 80
	sidebarMin           f32 = 180
	sidebarMax           f32 = 480
	splitterW            f32 = 4
	monoSize             f32 = 12
)

// Hard-coded ImageWipe look for git image diffs (not demo knobs).
var (
	gitImageWipeLeftAccent  = ImageWipeLeftAccent  // green = new
	gitImageWipeRightAccent = ImageWipeRightAccent // red = old
	// Purple @ 50% opacity when highlight is enabled.
	gitImageWipeHLOn  = Vec4{280, 50, 48, 0.5}
	gitImageWipeHLOff = Vec4{280, 50, 48, 0}
)

// historyRowHeight returns the fixed height for a sidebar history row.
// Synthetic slots are one line; commits have subject and metadata, plus optional stats.
func historyRowHeight(t *RepoTab, kind EntryKind) f32 {
	if kind != KindCommit {
		return historyRowMinH
	}
	showStats := t != nil && t.showStats
	lines := 2 // subject, then short hash and optional metadata
	if showStats {
		lines++
	}
	h := historyRowPad + f32(lines)*historyRowLineH
	if lines > 1 {
		h += f32(lines-1) * historyRowGap
	}
	if h < historyRowMinH {
		return historyRowMinH
	}
	return h
}

// formatHistoryTime is a compact local timestamp for the sidebar.
func formatHistoryTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Local().Format("2006-01-02 15:04")
}

var sidebarWidth f32 = 300

// findBarFocused is set while either find field has focus so Up/Down
// do not also move the commit selection. Per-bar flags drive Esc/Enter so an
// open-but-unfocused bar does not steal keys from the other.
var (
	findBarFocused     bool
	diffFindFocused    bool
	histFindFocused    bool
	diffToolbarFocused bool
)

// diffFileNav is filled while DiffStream paints; N/P use last frame's values
// (handleAppKeys runs before paint).
var diffFileNav struct {
	listKey any

	nextEnabled bool
	nextIndex   int  // file header to pin to top; -1 → ScrollToEnd when enabled
	nextUseEnd  bool // true when nextIndex < 0 but still can scroll down

	prevEnabled bool
	prevIndex   int // file header strictly above firstVis
}

// primaryMod is Cmd on macOS, Ctrl elsewhere — same rule as text editing.
func primaryMod() Modifiers {
	if runtime.GOOS == "darwin" {
		return ModCmd
	}
	return ModCtrl
}

func primaryModLabel() string {
	if runtime.GOOS == "darwin" {
		return "⌘"
	}
	return "Ctrl+"
}

func RootView() {
	SetDarkMode(darkmode.OSDarkMode())
	// findBarFocused still holds last frame's paint result so key handling
	// (before the bars re-draw) knows whether a find field owns focus.
	t := appData.active
	if t != nil && !appData.browseOpen {
		if t.diffFindOpen && (diffToolbarFocused || FocusedId() == nil) && GetFrameInput().Key == KeyEscape {
			t.diffFindOpen, t.diffFindFocusReq = false, false
			ClearFocus()
		}
		handleAppKeys(t)
	}
	findBarFocused = false
	diffFindFocused = false
	histFindFocused = false
	diffToolbarFocused = false
	ProfileButton("git_history")
	Container(Attrs(Viewport, UseSurface(SurfaceCanvas)), func() {
		TabBar()
		if t != nil {
			ToolBar(t)
			ContainerWithKey(t, Attrs(Row, Grow(1), Expand, Clip), func() {
				Sidebar(t)
				sidebarSplitter()
				MainContent(t)
			})
			StatusBar(t)
		} else {
			emptyNoTabs()
		}
		// Browser modal sits above content (popup layer). Toasts use ToastMessage.
		NewRepoBrowser()
	})
}

// StatusBar shows loading state and shortcuts without competing with the diff.
func StatusBar(t *RepoTab) {
	Container(Attrs(Row, Expand, CrossMid, FixHeight(26), Gap(12), Pad2(0, 12),
		UseSurface(SurfaceCanvas), Clip), func() {
		count := 0
		for _, e := range t.history {
			if e.Kind == KindCommit {
				count++
			}
		}
		note := fmt.Sprintf("Read only  ·  %d commits loaded", count)
		if t.listLoading || t.historyLoadingMore {
			note += "  ·  Loading history…"
		}
		if historyFiltering(t) {
			note += fmt.Sprintf("  ·  %d matching", len(t.histFindMatches))
		}
		Label(note, FontSize(10), TextColorVec(CurrentColorScheme.List.Muted))
		Filler(1)
		Label(primaryModLabel()+"L  Filter history     "+primaryModLabel()+"F  Find in diff     N / P  Next / previous file", FontSize(10), TextColorVec(CurrentColorScheme.List.Muted))
	})
}

// TabBar holds repository tabs and the Recent and Open controls.
func TabBar() {
	var closeReq *RepoTab
	NextAccessName("top_bar")
	Container(Attrs(Expand), func() {
		AssignAccess()
		TabStrip(func() {
			for _, tab := range appData.tabs {
				if RepoTabChrome(tab) {
					closeReq = tab
				}
			}
		}, func() {
			ModAttrs(Gap(8))

			// Recents menu (builtin MenuButton + keyboard filter).
			NextAccessName("recent")
			CtrlMenuButton(MenuIcon, "Recent", func() {
				NextAccessName("recent_menu")
				AssignAccess()
				MenuFilterQuery() // opt into typeahead
				if len(appData.recents) == 0 {
					Label("No recent repos", FontSize(11), FontStyle(StyleItalic))
					return
				}
				for _, path := range appData.recents {
					label := recentMenuLabel(path)
					if !MenuFilterMatches(label) && !MenuFilterMatches(path) {
						continue
					}
					p := path // capture
					if MenuItem(NoIcon, label) {
						openRecentRepo(p)
					}
				}
			})

			// Open directory browser.
			NextAccessName("files_browser")
			if CtrlButton(SymFolder, "Open…", true) {
				openNewRepoBrowser("")
			}

		})
	})
	if closeReq != nil {
		closeTab(closeReq)
	}
}

func recentMenuLabel(path string) string {
	base := filepath.Base(path)
	if base == "" || base == "." || base == string(filepath.Separator) {
		return path
	}
	return base + "  —  " + path
}

func openRecentRepo(path string) {
	tab, err := openRepoTab(path)
	if err != nil {
		// Drop dead recent; re-save list.
		dropRecent(path)
		ToastMessage(err.Error())
		return
	}
	ensureTabLoaded(tab)
}

func dropRecent(path string) {
	out := appData.recents[:0]
	for _, p := range appData.recents {
		if p != path {
			out = append(out, p)
		}
	}
	appData.recents = out
	scheduleSaveSession()
}

// openNewRepoBrowser shows the folder modal. seedCwd empty → active tab path,
// else last browse cwd, else home-ish default via resolveBrowserStart path.
func openNewRepoBrowser(seedCwd string) {
	cwd := strings.TrimSpace(seedCwd)
	if cwd == "" {
		cwd = strings.TrimSpace(appData.browseCwd)
	}
	if cwd == "" && appData.active != nil {
		cwd = appData.active.path
	}
	if cwd == "" {
		cwd, _ = filepath.Abs(".")
	}
	if abs, err := filepath.Abs(cwd); err == nil {
		cwd = abs
	}
	appData.browseOpen = true
	appData.browseCwd = cwd
	appData.browseFilter = ""
	appData.browseSelected = -1
	appData.browsePick = ""
}

// NewRepoBrowser is the modal directory picker for Open.
func NewRepoBrowser() {
	if !appData.browseOpen {
		return
	}
	attrs := DefaultFileBrowserAttrs()
	attrs.Title = "Open git repository"
	attrs.Width = 560
	attrs.Start = appData.browseCwd

	closeDialog := func() {
		appData.browseOpen = false
	}

	Modal(attrs.Width, closeDialog, func() {
		NextAccessName("file_browser")
		AssignAccess()
		// Keep cwd non-empty for FileBrowserPanel.
		if appData.browseCwd == "" {
			appData.browseCwd, _ = filepath.Abs(".")
		}
		if FileBrowserPanel(&appData.browseCwd, &appData.browseFilter, &appData.browseSelected, &appData.browsePick, attrs) {
			tryOpenFromBrowser(appData.browsePick)
			return
		}
		if Button(NoIcon, "Cancel") {
			closeDialog()
		}
	})
}

// tryOpenFromBrowser accepts a chosen directory: open as tab, or toast + stay
// in the browser at the same folder if it is not a git work tree.
func tryOpenFromBrowser(path string) {
	path = strings.TrimSpace(path)
	if path == "" {
		path = appData.browseCwd
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	// Remember where the browser was for re-open / stay.
	appData.browseCwd = path

	tab, err := openRepoTab(path)
	if err != nil {
		ToastMessage(err.Error())
		// Keep browser open at the same place (re-open if something closed it).
		appData.browseOpen = true
		appData.browseFilter = ""
		appData.browseSelected = -1
		appData.browsePick = ""
		return
	}
	appData.browseOpen = false
	ensureTabLoaded(tab)
}

// RepoTabChrome draws one tab; returns true if × was clicked this frame.
func RepoTabChrome(t *RepoTab) (closeClicked bool) {
	NextAccessName("repo_tab")
	NextAccessValue(t.path)
	if TabItem(t, t.label, t == appData.active, func() {
		if t.listLoading {
			Label("…", FontSize(11))
		}
		NextAccessName("close")
		closeClicked = TabCloseButton()
	}) {
		appData.active = t
		ensureTabLoaded(t)
		scheduleSaveSession()
	}
	return closeClicked
}

// ToolBar: refresh for the active tab only.
func ToolBar(t *RepoTab) {
	Container(Attrs(Row, CrossMid, Expand, Gap(10), FixHeight(36), Pad2(0, 12),
		UseSurface(SurfaceCanvas), Clip), func() {
		Icon(SymFolder, FontSize(13), TextColorVec(CurrentColorScheme.List.Muted))
		Container(Attrs(Grow(1), Expand, Extrinsic, Clip, MainAlign(AlignMiddle)), func() {
			Label(t.path, FontSize(11), TextColorVec(CurrentColorScheme.List.Muted))
		})
		if CtrlButton(SymRefresh, "Refresh", !t.listLoading) {
			go refreshHistory(t, true)
		}
	})
}

func emptyNoTabs() {
	Container(Attrs(Grow(1), Expand, Center, Gap(12), Pad(40)), func() {
		Label("Open a git repository", FontSize(16), FontWeight(WeightBold))
		Label("Choose Open in the tab bar, or pass a path on the command line.",
			FontSize(12))
		if CtrlButton(NoIcon, "Open repository…", true) {
			openNewRepoBrowser("")
		}
	})
}

// handleAppKeys: optional find shortcuts (⌘/Ctrl+F, ⌘/Ctrl+L), file jump
// (n/p), and history Up/Down.
func handleAppKeys(t *RepoTab) {
	if handleFindShortcuts(t) {
		return
	}
	// Up/Down and n/p are defaults for the commit/diff view, not hotkeys
	// that steal from a focused control. Menus live on the popup layer
	// (after this function), so last frame's focus is the signal — same
	// idea as findBarFocused.
	if FocusedId() != nil {
		return
	}
	if handleFileNavKeys(t) {
		return
	}
	handleHistoryKeys(t)
}

// handleFileNavKeys: N next file / P previous file. Only when
// that direction is enabled; ignored while a find field has focus.
func handleFileNavKeys(t *RepoTab) bool {
	if findBarFocused || t.diffFindOpen {
		return false
	}
	if GetInputState().Modifiers != 0 {
		return false
	}
	switch GetFrameInput().Key {
	case KeyN:
		if diffFileNav.nextEnabled && diffFileNav.listKey != nil {
			jumpDiffNextFile()
		}
		return true
	case KeyP:
		if diffFileNav.prevEnabled && diffFileNav.listKey != nil {
			jumpDiffPrevFile()
		}
		return true
	default:
		return false
	}
}

func jumpDiffNextFile() {
	if diffFileNav.listKey == nil {
		return
	}
	if diffFileNav.nextUseEnd || diffFileNav.nextIndex < 0 {
		VirtualListView_ScrollToEnd(diffFileNav.listKey, 0)
	} else {
		VirtualListView_ScrollToIndex(diffFileNav.listKey, diffFileNav.nextIndex)
	}
	RequestNextFrame()
}

func jumpDiffPrevFile() {
	if diffFileNav.listKey == nil || diffFileNav.prevIndex < 0 {
		return
	}
	VirtualListView_ScrollToIndex(diffFileNav.listKey, diffFileNav.prevIndex)
	RequestNextFrame()
}

// handleFindShortcuts opens or re-focuses the optional find bars.
// Returns true when a shortcut was consumed (so arrow navigation stays quiet).
// Esc / Enter while a bar is focused are handled inside the bar during paint.
func handleFindShortcuts(t *RepoTab) bool {
	key := GetFrameInput().Key
	if key == KeyCodeNone {
		return false
	}
	if GetInputState().Modifiers != primaryMod() {
		return false
	}
	switch key {
	case KeyF:
		// Diff find only when a ready doc is on screen.
		if t.doc == nil || t.docID != t.selected {
			return true
		}
		t.diffFindOpen = true
		t.diffFindFocusReq = true
		// Drop history-find focus so keys go to the diff field.
		t.histFindFocusReq = false
		return true
	case KeyL:
		t.histFindFocusReq = true
		t.diffFindFocusReq = false
		return true
	}
	return false
}

// handleHistoryKeys moves the active tab's selection with Up/Down over the
// visible history list (full log, or the filtered subset when filtering).
func handleHistoryKeys(t *RepoTab) {
	if findBarFocused {
		return
	}
	mods := GetInputState().Modifiers
	if mods&(ModCmd|ModCtrl|ModAlt) != 0 {
		return
	}
	var delta int
	switch GetFrameInput().Key {
	case KeyUp:
		delta = -1
	case KeyDown:
		delta = 1
	default:
		return
	}

	syncHistFind(t)
	filtering := historyFiltering(t)
	// visible: full history indices, or histFindMatches when filtering.
	var n int
	histAt := func(pos int) int { return pos } // pos → history index
	if filtering {
		n = len(t.histFindMatches)
		histAt = func(pos int) int { return t.histFindMatches[pos] }
	} else {
		n = len(t.history)
	}
	if n == 0 {
		return
	}

	// Position within the visible list.
	pos := -1
	if filtering {
		sel := selectedHistoryIndex(t)
		for i, hi := range t.histFindMatches {
			if hi == sel {
				pos = i
				break
			}
		}
	} else {
		pos = selectedHistoryIndex(t)
	}
	if pos < 0 {
		if delta > 0 {
			pos = 0
		} else {
			pos = n - 1
		}
	} else {
		pos += delta
		if pos < 0 {
			pos = 0
		}
		if pos >= n {
			pos = n - 1
		}
	}
	hi := histAt(pos)
	id := t.history[hi].ID
	if id != t.selected {
		selectEntry(t, id)
	}
	// Same list key as Sidebar's VirtualListView.
	VirtualListScrollIntoView(t, id)
	if pos >= n-8 || hi >= len(t.history)-8 {
		maybeLoadMoreHistory(t)
	}
}

func selectedHistoryIndex(t *RepoTab) int {
	for i, e := range t.history {
		if e.ID == t.selected {
			return i
		}
	}
	return -1
}

// historyFiltering is true when the sidebar is narrowed by a non-empty query.
func historyFiltering(t *RepoTab) bool {
	return t != nil && strings.TrimSpace(t.histFindQuery) != ""
}

func sidebarSplitter() {
	Container(Attrs(FixWidth(splitterW), Expand, BackgroundVec(CurrentColorScheme.Surfaces.Panel.Border)), func() {
		if IsHovered() {
			ModAttrs(BackgroundVec(CurrentColorScheme.FocusRing))
		}
		PressAction()
		if IsActive() {
			sidebarWidth = clampF32(sidebarWidth+GetFrameInput().Motion[0], sidebarMin, sidebarMax)
		}
	})
}

func Sidebar(t *RepoTab) {
	// Path lives in ToolBar (full width + Refresh); don't repeat it here.
	// Header with list options, history filter (⌘/Ctrl+L), then list.
	Container(Attrs(FixWidth(sidebarWidth), Expand, Clip, UseSurface(SurfaceCanvas)), func() {
		if t.repoErr != "" {
			Container(Attrs(Expand, Pad(12)), func() {
				Label(t.repoErr, FontSize(12), TextColorVec(CurrentColorScheme.List.Error))
			})
			return
		}

		if t.listErr != "" && len(t.history) == 0 {
			Container(Attrs(Expand, Pad(12)), func() {
				Label(t.listErr, FontSize(12), TextColorVec(CurrentColorScheme.List.Error))
			})
			return
		}

		HistoryListHeader(t)

		HistoryFindBar(t)

		// Ordered sidebar +/− fill (batched); do not per-row stampede.
		if t.showStats {
			pumpHistoryStats(t)
		}

		Container(Attrs(Viewport), func() {
			if len(t.history) == 0 {
				Container(Attrs(Expand, Pad(12)), func() {
					if t.listLoading {
						Label("Loading history…", FontStyle(StyleItalic))
					} else {
						Label("No commits", FontStyle(StyleItalic))
					}
				})
				return
			}
			syncHistFind(t)
			filtering := historyFiltering(t)
			// When filtering, list only match indices; otherwise the full history.
			var n int
			histAt := func(i int) int { return i }
			if filtering {
				n = len(t.histFindMatches)
				histAt = func(i int) int { return t.histFindMatches[i] }
				// Keep paging so rare terms can reach older commits.
				if t.historyHasMore && n < historyPageSize {
					maybeLoadMoreHistory(t)
				}
				if n == 0 {
					Container(Attrs(Expand, Pad(12), Gap(6)), func() {
						if t.historyLoadingMore || t.historyHasMore {
							Label("Searching older commits…", FontStyle(StyleItalic))
						} else {
							Label("No matching commits", FontStyle(StyleItalic))
						}
					})
					return
				}
			} else {
				n = len(t.history)
			}
			count := n
			if t.historyLoadingMore {
				count = n + 1
			}
			// Key list by tab so each repo keeps its own scroll offset.
			VirtualListView(t, count,
				func(i int) any {
					if i >= n {
						return "history-loading-more"
					}
					return t.history[histAt(i)].ID
				},
				func(i int, w f32) f32 {
					if i >= n {
						return historyRowMinH
					}
					return historyRowHeight(t, t.history[histAt(i)].Kind)
				},
				func(i int, w f32) {
					if i >= n {
						Container(Attrs(Expand, FixHeight(historyRowMinH), MaxWidth(w), CrossMid, Pad2(0, 10)), func() {
							Label("Loading older commits…", FontSize(11), FontStyle(StyleItalic))
						})
						return
					}
					hi := histAt(i)
					if i >= n-8 || hi >= len(t.history)-8 {
						maybeLoadMoreHistory(t)
					}
					historyRow(t, hi, w)
				},
			)
		})
	})
}

// HistoryListHeader is a thin strip above the commit list with display toggles.
func HistoryListHeader(t *RepoTab) {
	Container(Attrs(Row, CrossMid, Expand, Gap(6), Pad2(4, 10),
		UseSurface(SurfaceCanvas), BorderColorVec(CurrentColorScheme.Surfaces.Panel.Border), BorderWidth(1)), func() {
		Label("History", FontSize(13), FontWeight(WeightSemibold))
		Filler(1)
		if t == nil {
			return
		}
		// CheckBoxes inside the menu stay open for multi-toggle; MenuItem would
		// dismiss after each click. Toggles are per-repo and persisted.
		//
		// Detection must run *inside* the menu builder: MenuButton queues its
		// body via Popup, so it runs after this function returns. Comparing
		// before/after MenuButtonExt always saw no change and never saved.
		CtrlMenuButton(SymOptsV, "", func() {
			// Menu shell only pads vertically (Pad2(6,0)); wrap content so the
			// panel has even inset around the title and checkboxes.
			Container(Attrs(Pad2(5, 10), Gap(10), MinWidth(148)), func() {
				Label("Show in list", FontSize(10), FontStyle(StyleItalic))
				CheckBox(&t.showAuthor, "Author name")
				CheckBox(&t.showTime, "Timestamp")
				CheckBox(&t.showStats, "Diff stats")
			})
			if tabDisplayDirty(t) {
				rememberTabDisplay(t)
				// Immediate write: debounced save can miss a quick quit after
				// the last toggle (popup body runs late; prefs must stick).
				_ = saveSessionNow()
			}
		})
	})
}

func historyRow(t *RepoTab, i int, width f32) {
	if i < 0 || i >= len(t.history) {
		return
	}
	e := t.history[i]
	selected := t.selected == e.ID
	rowH := historyRowHeight(t, e.Kind)

	// When filtering, every visible row is a match — highlight the query
	// substring on short hash / subject / author / synthetic label.
	q := ""
	if historyFiltering(t) {
		q = t.histFindQuery
	}

	NextAccessName("history_entry")
	NextAccessValue(e.ID)
	ContainerWithKey(e.ID, Attrs(Expand, FixHeight(rowH), MaxWidth(width), Clip, Gap(historyRowGap), Pad4(7, 14, 7, 14), BorderWidth(0.5), BorderColorVec(CurrentColorScheme.Surfaces.Panel.Border)), func() {
		AssignAccess()
		ModAttrs(UnsetMaxCross)
		if selected {
			color := CurrentColorScheme.List.Selected.Background
			color[3] = 0.28
			ModAttrs(BackgroundVec(color))
		} else if IsHovered() {
			ModAttrs(BackgroundVec(CurrentColorScheme.List.Hovered.Background), AmendTextStyle(TextColorVec(CurrentColorScheme.List.Hovered.Text)))
		}
		if PressAction() {
			selectEntry(t, e.ID)
		}

		textColor := CurrentColorScheme.List.Surface.Text
		muteColor := CurrentColorScheme.List.Muted
		if selected {
			textColor = CurrentColorScheme.Surfaces.Panel.Text
			muteColor = CurrentColorScheme.List.Muted
			Element(Attrs(Float(0, 0), FixSize(3, rowH), BackgroundVec(CurrentColorScheme.FocusRing)))
		}

		switch e.Kind {
		case KindWorkingTree, KindStaging:
			Container(Attrs(Row, CrossMid, Expand, Grow(1), Gap(8)), func() {
				Icon(SymFile, FontSize(12), TextColorVec(muteColor))
				historyText(e.SidebarLabel(), q,
					FontSize(12), FontWeight(WeightSemibold), TextColorVec(textColor))
			})
		default:
			Container(Attrs(Row, Expand, Clip), func() {
				ModAttrs(UnsetMaxCross)
				historyText(e.Subject, q, FontSize(12), FontWeight(WeightSemibold), TextColorVec(textColor))
			})
			Container(Attrs(Row, CrossMid, Expand, Gap(6), Clip), func() {
				ModAttrs(UnsetMaxCross)
				historyText(e.Short, q, FontSize(10), Fonts(Monospace...), TextColorVec(muteColor))
				if t.showAuthor && e.Author != "" {
					Label("·", FontSize(10), TextColorVec(muteColor))
					historyText(e.Author, q, FontSize(10), TextColorVec(muteColor))
				}
				if t.showTime && !e.When.IsZero() {
					Label("·", FontSize(10), TextColorVec(muteColor))
					Label(formatHistoryTime(e.When), FontSize(10), TextColorVec(muteColor))
				}
			})
			// Optional stats line (filled in history order by pumpHistoryStats).
			if t.showStats {
				if st, ok := t.commitStats[e.ID]; ok && st.Ready {
					Container(Attrs(Row, CrossMid, Gap(8)), func() {
						Label(fmt.Sprintf("+%d", st.Added), FontSize(10), Fonts(Monospace...), TextColorVec(diffStatColor(true)))
						Label(fmt.Sprintf("−%d", st.Deleted), FontSize(10), Fonts(Monospace...), TextColorVec(diffStatColor(false)))
						files := "files"
						if st.Files == 1 {
							files = "file"
						}
						Label(fmt.Sprintf("· %d %s", st.Files, files), FontSize(10),
							Fonts(Monospace...), TextColorVec(muteColor))
					})
				} else if t.hasStatsInflight(e.ID) {
					// Per-row progress so a slow pure-Go job is visible (R18–R19).
					Label("…", FontSize(10), Fonts(Monospace...), TextColorVec(muteColor))
				}
			}
		}
	})
}

// historyText is Label with optional filter substring highlights.
func historyText(text, query string, mods ...TextStyleFn) {
	if query == "" || text == "" {
		Label(text, mods...)
		return
	}
	ranges := findSubstringRanges(text, query)
	if len(ranges) == 0 {
		Label(text, mods...)
		return
	}
	bg := CurrentColorScheme.List.Selected.Background
	spans := make([]TextSpan, 0, len(ranges))
	for _, r := range ranges {
		spans = append(spans, Span(r[0], r[1], TextBackgroundVec(bg), TextColorVec(CurrentColorScheme.List.Selected.Text)))
	}
	Text(text, TextStyle(mods...), spans...)
}

func MainContent(t *RepoTab) {
	// Extrinsic+Clip: width comes from the pane, not from long header/find/diff
	// lines (otherwise the find bar and scrollbar get pushed off-screen).
	Container(Attrs(Grow(1), Expand, Extrinsic, Clip, UseSurface(SurfacePanel)), func() {
		if t.repoErr != "" {
			centeredMessage(t.repoErr)
			return
		}
		if t.selected == "" {
			if t.listLoading {
				centeredMessage("Loading…")
			} else {
				centeredMessage("Select a commit from the history")
			}
			return
		}
		// Always paint header from the sidebar entry (subject/author/time are
		// already on HistoryEntry). Full message + diff fill in as they load;
		// never blank the whole pane while a huge prior patch is still running.
		DiffHeader(t)
		DiffToolbar(t)
		DiffStream(t)
	})
}

// DiffToolbar reserves one row for file controls or the active search.
func DiffToolbar(t *RepoTab) {
	NextAccessName("diff_toolbar")
	Container(Attrs(Row, Expand, Clip, CrossMid, Gap(6), Pad2(5, 10), FixHeight(38),
		UseSurface(SurfaceCanvas)), func() {
		AssignAccess()
		diffToolbarFocused = HasFocusWithin()
		if t.diffFindOpen {
			DiffFindBar(t)
			return
		}
		ready := t.doc != nil && t.docID == t.selected
		Container(Attrs(Grow(1), Expand, Extrinsic, Clip, MainAlign(AlignMiddle)), func() {
			if ready {
				stats := statsFromDoc(t.doc)
				Container(Attrs(Row, CrossMid, Gap(10)), func() {
					Label(fmt.Sprintf("%d files", stats.Files), FontSize(11), FontWeight(WeightSemibold))
					Label(fmt.Sprintf("+%d", stats.Added), FontSize(11), TextColorVec(diffStatColor(true)))
					Label(fmt.Sprintf("−%d", stats.Deleted), FontSize(11), TextColorVec(diffStatColor(false)))
				})
			}
		})
		v := syncDiffView(t)
		label, collapse := "Collapse all", true
		if v != nil && v.AllCollapsed() {
			label, collapse = "Expand all", false
		}
		NextAccessName("collapse_all")
		if CtrlButton(NoIcon, label, ready && !t.docLoading && diffViewFoldable(v)) {
			setDiffAllCollapsed(t, collapse)
		}
		navReady := ready && diffFileNav.listKey == [2]any{t, t.docID}
		NextAccessName("previous_file")
		if CtrlButton(SymArrowUp, "Previous file", navReady && diffFileNav.prevEnabled) {
			jumpDiffPrevFile()
		}
		NextAccessName("next_file")
		if CtrlButton(SymArrowDown, "Next file", navReady && diffFileNav.nextEnabled) {
			jumpDiffNextFile()
		}
		NextAccessName("find_diff")
		if CtrlButton(SymSearch, "Find", ready) {
			t.diffFindOpen, t.diffFindFocusReq = true, true
			t.histFindFocusReq = false
			RequestNextFrame()
		}
	})
}

// DiffFindBar occupies the file toolbar while searching. Closing preserves the query.
func DiffFindBar(t *RepoTab) {
	Icon(SymSearch, FontSize(12), TextColorVec(CurrentColorScheme.List.Muted))
	Container(Attrs(Grow(1), Expand, Extrinsic, Clip, MainAlign(AlignMiddle)), func() {
		sz := GetAvailableSize()
		if sz[0] < 1 || sz[1] < 1 {
			RequestNextFrame()
			return
		}
		attrs := DefaultTextInputAttrs()
		attrs.FontSize = 12
		attrs.MinWidth, attrs.MaxWidth = sz[0], sz[0]
		attrs.FixedWidth, attrs.NoAutoFocus = true, true
		attrs.Placeholder = "Find in diff…"
		NextAccessName("diff_query")
		TextInputExt(&t.findQuery, attrs)
		if t.diffFindFocusReq {
			FocusImmediateOn(GetLastId())
			t.diffFindFocusReq = false
		}
		if HasFocusWithin() {
			findBarFocused, diffFindFocused = true, true
		}
	})
	syncDiffFind(t)
	n := len(t.findMatches)
	note := "0 matches"
	if n > 0 {
		note = fmt.Sprintf("%d of %d", t.findIdx+1, n)
	}
	NextAccessName("diff_matches")
	NextAccessValue(note)
	Container(Attrs(MinWidth(64), Center), func() { AssignAccess(); Label(note, FontSize(11), TextColorVec(CurrentColorScheme.List.Muted)) })
	NextAccessName("previous_match")
	NextAccessLabel("Previous match")
	if CtrlButton(SymArrowUp, "", n > 0) {
		diffFindStep(t, -1)
	}
	NextAccessName("next_match")
	NextAccessLabel("Next match")
	if CtrlButton(SymArrowDown, "", n > 0) {
		diffFindStep(t, 1)
	}
	NextAccessName("close_find")
	NextAccessLabel("Close find")
	close := CtrlButton(SymICross, "", true)
	if diffFindFocused && GetFrameInput().Key == KeyEnter {
		if GetInputState().Modifiers&ModShift != 0 {
			diffFindStep(t, -1)
		} else {
			diffFindStep(t, 1)
		}
	}
	if close {
		t.diffFindOpen, t.diffFindFocusReq = false, false
		ClearFocus()
		RequestNextFrame()
	}
}

// HistoryFindBar filters the commit list; ⌘/Ctrl+L focuses the field.
func HistoryFindBar(t *RepoTab) {
	Container(Attrs(Row, Expand, Clip, CrossMid, Gap(4), Pad2(4, 8),
		MinHeight(36),
		UseSurface(SurfaceCanvas), BorderColorVec(CurrentColorScheme.Surfaces.Panel.Border), BorderWidth(1)), func() {
		Icon(SymSearch, FontSize(11))

		Container(Attrs(Grow(1), Expand, Extrinsic, Clip), func() {
			sz := GetAvailableSize()
			if sz[0] < 1 || sz[1] < 1 {
				RequestNextFrame()
				return
			}
			attrs := DefaultTextInputAttrs()
			attrs.FontSize = 11
			attrs.MinWidth = sz[0]
			attrs.MaxWidth = sz[0]
			attrs.FixedWidth = true
			attrs.NoAutoFocus = true
			attrs.Placeholder = "Filter commits…"
			NextAccessName("history_query")
			TextInputExt(&t.histFindQuery, attrs)
			if t.histFindFocusReq {
				FocusImmediateOn(GetLastId())
				t.histFindFocusReq = false
			}
			if HasFocusWithin() {
				findBarFocused = true
				histFindFocused = true
			}
		})

		syncHistFind(t)
		if strings.TrimSpace(t.histFindQuery) != "" {
			if findClearButton() {
				t.histFindQuery = ""
				syncHistFind(t)
			} else {
				n := len(t.histFindMatches)
				note := "0"
				if n > 0 {
					note = fmt.Sprintf("%d", n)
				}
				if t.historyHasMore {
					note += "+"
				}
				Label(note, FontSize(9))
			}
		}

		if histFindFocused {
			switch GetFrameInput().Key {
			case KeyEscape:
				// Clear the query and return focus to the list.
				t.histFindQuery = ""
				syncHistFind(t)
				t.histFindFocusReq = false
				ClearFocus()
			}
		}
	})
}

// findClearButton is a compact × control for find/filter bars. Returns true
// when clicked (caller clears the query).
func findClearButton() bool {
	clicked := false
	Container(Attrs(Pad(3), Corners(3), Center), func() {
		if IsHovered() {
			ModAttrs(BackgroundVec(CurrentColorScheme.Table.Hovered))
		}
		if PressAction() {
			clicked = true
		}
		Icon(SymICross, FontSize(11))
	})
	return clicked
}

// syncDiffFind rebuilds the match list when the query or displayed doc changes.
func syncDiffFind(t *RepoTab) {
	q := t.findQuery
	docID := t.docID
	if q == "" || t.doc == nil {
		if t.findQ != "" || len(t.findMatches) > 0 {
			t.findQ, t.findDocID = "", ""
			t.findMatches, t.findIdx = nil, -1
		}
		return
	}
	if t.findDocID == docID && t.findQ == q {
		return
	}
	matches := findMatchesInDoc(t.doc, q)
	t.findDocID = docID
	t.findQ = q
	t.findMatches = matches
	t.findIdx = -1
	if len(matches) == 0 {
		return
	}
	t.findIdx = 0
	focusDiffFindMatch(t, 0)
}

func diffFindStep(t *RepoTab, delta int) {
	n := len(t.findMatches)
	if n == 0 {
		return
	}
	if t.findIdx < 0 {
		t.findIdx = 0
	} else {
		t.findIdx = (t.findIdx + delta%n + n) % n
	}
	focusDiffFindMatch(t, delta)
}

// focusDiffFindMatch scrolls the match row into the list via ScrollToIndexAt.
// delta chooses a comfortable vertical placement: next (down) sits between
// middle and bottom; prev (up) between middle and top; first match (0) is
// near the middle. Collapsed files containing the hit are auto-expanded.
func focusDiffFindMatch(t *RepoTab, delta int) {
	if t.findIdx < 0 || t.findIdx >= len(t.findMatches) {
		return
	}
	row := t.findMatches[t.findIdx].row
	frac := f32(0.45) // first open / re-query: a bit above true center
	if delta > 0 {
		frac = 0.62 // scrolling down → mid–bottom
	} else if delta < 0 {
		frac = 0.32 // scrolling up → mid–top
	}
	listKey := [2]any{t, t.docID}
	vis := row
	if v := syncDiffView(t); v.HasSegs() {
		if v.EnsureExpandedSource(row) {
			rememberDiffCollapse(t, v)
		}
		if vi, ok := v.VisOf(row); ok {
			vis = vi
		}
	}
	VirtualListView_ScrollToIndexAt(listKey, vis, frac)
}

// syncDiffView returns the collapse projection for the current doc, creating
// or resetting it when the selected doc changes. On create, restores any
// session-remembered collapsed paths for this docID. While a patch streams,
// Grow catches up if segs lag behind Rows.
func syncDiffView(t *RepoTab) *DiffView {
	if t == nil || t.doc == nil {
		if t != nil {
			t.diffView = nil
		}
		return nil
	}
	var remembered map[string]bool
	if t.collapsedByDoc != nil {
		remembered = t.collapsedByDoc[t.docID]
	}
	if t.diffView != nil && t.diffView.docID == t.docID {
		// Streaming: extend segs if rows grew past the last segment.
		if t.diffView.HasSegs() {
			last := t.diffView.segs[len(t.diffView.segs)-1]
			if last.End < len(t.doc.Rows) {
				t.diffView.Grow(t.doc, last.End, remembered)
				t.doc.Segs = cloneSegs(t.diffView.segs)
			}
		} else if len(t.doc.Segs) > 0 || len(t.doc.Rows) > 0 {
			t.diffView.Grow(t.doc, 0, remembered)
			t.doc.Segs = cloneSegs(t.diffView.segs)
		}
		return t.diffView
	}
	// Prefer live.Segs when already grown before first paint.
	segs := t.doc.Segs
	if len(segs) == 0 && len(t.doc.Rows) > 0 {
		segs = buildDiffFileSegs(t.doc)
		t.doc.Segs = segs
	}
	t.diffView = newDiffView(t.docID, segs)
	t.diffView.ApplyCollapsedPaths(remembered)
	return t.diffView
}

// rememberDiffCollapse stores the current view's collapsed paths for this doc
// (session-only). Empty sets clear the entry so default-expand stays cheap.
func rememberDiffCollapse(t *RepoTab, v *DiffView) {
	if t == nil || v == nil || v.docID == "" {
		return
	}
	if t.collapsedByDoc == nil {
		t.collapsedByDoc = map[string]map[string]bool{}
	}
	paths := v.CollapsedPaths()
	if len(paths) == 0 {
		delete(t.collapsedByDoc, v.docID)
		return
	}
	t.collapsedByDoc[v.docID] = paths
}

// scrollDiffToSource pins a source row (or its file header if still hidden)
// at the top of the virtual list. Used for find jumps and expand/collapse-all,
// not for single-file toggles (those must keep the viewport stable).
func scrollDiffToSource(t *RepoTab, source int) {
	if t == nil {
		return
	}
	listKey := [2]any{t, t.docID}
	v := syncDiffView(t)
	if !v.HasSegs() {
		VirtualListView_ScrollToIndex(listKey, source)
		RequestNextFrame()
		return
	}
	vis, ok := v.VisOf(source)
	if !ok {
		fi := v.fileOfSource(source)
		if fi >= 0 {
			vis = v.prefix[fi]
			ok = true
		}
	}
	if ok {
		VirtualListView_ScrollToIndex(listKey, vis)
	}
	RequestNextFrame()
}

// toggleDiffFile flips collapse for one file without re-pinning scroll.
// Indices above the header are unchanged, so keeping scrollY leaves the
// header (and everything above it) in place; only the body below disappears
// or reappears. If the viewport top was deep in this file's body when
// collapsing, re-anchor to the header so we do not land on a renumbered row.
func toggleDiffFile(t *RepoTab, fileIdx int) {
	v := syncDiffView(t)
	if v == nil || fileIdx < 0 || fileIdx >= len(v.segs) {
		return
	}
	headerSrc := v.segs[fileIdx].Header
	pin := t.diffPinSource
	// Viewport top sits strictly inside this file's body (below its header).
	wasInBody := pin > headerSrc && v.fileOfSource(pin) == fileIdx
	collapsing := !v.IsCollapsed(fileIdx)

	if !v.ToggleFile(fileIdx) {
		return
	}
	rememberDiffCollapse(t, v)
	if collapsing && wasInBody {
		// Body rows are gone; pin the header without a frac so it stays visible.
		scrollDiffToSource(t, headerSrc)
		return
	}
	RequestNextFrame()
}

// setDiffAllCollapsed expands or collapses every file; keeps the current
// viewport pin (or first file header) in view.
func setDiffAllCollapsed(t *RepoTab, collapsed bool) {
	v := syncDiffView(t)
	if v == nil || !v.HasSegs() {
		return
	}
	pin := t.diffPinSource
	if pin < 0 || (v.fileOfSource(pin) < 0 && len(v.segs) > 0) {
		pin = v.segs[0].Header
	}
	// If collapsing, pin may land on a hidden body row — use its file header.
	if collapsed {
		if fi := v.fileOfSource(pin); fi >= 0 {
			pin = v.segs[fi].Header
		}
	}
	v.SetAllCollapsed(collapsed)
	rememberDiffCollapse(t, v)
	scrollDiffToSource(t, pin)
}

// syncHistFind rebuilds the filtered index list when the query or loaded
// history length changes. If the current selection falls out of the filter,
// selects the first match (when any).
func syncHistFind(t *RepoTab) {
	q := t.histFindQuery
	nHist := len(t.history)
	if strings.TrimSpace(q) == "" {
		if t.histFindQ != "" || len(t.histFindMatches) > 0 {
			t.histFindQ, t.histFindN = "", 0
			t.histFindMatches = nil
		}
		return
	}
	if t.histFindQ == q && t.histFindN == nHist {
		return
	}
	matches := findMatchesInHistory(t.history, q)
	t.histFindQ = q
	t.histFindN = nHist
	t.histFindMatches = matches
	if len(matches) == 0 {
		return
	}
	sel := selectedHistoryIndex(t)
	if historyIndexHasMatch(matches, sel) {
		return
	}
	id := t.history[matches[0]].ID
	if id != t.selected {
		selectEntry(t, id)
	}
	VirtualListScrollIntoView(t, id)
}

func DiffHeader(t *RepoTab) {
	doc := t.doc
	entry := selectedEntry(t)
	docReady := doc != nil && t.docID == t.selected
	// Mirror history-filter highlights in the commit header (subject / body / hash).
	q := ""
	if historyFiltering(t) {
		q = t.histFindQuery
	}

	// Expand+Clip only — height from content; width capped by MainContent Extrinsic.
	Container(Attrs(Expand, Clip, MaxWidth(GetResolvedWidth()), Pad4(12, 14, 10, 14), Gap(5), UseSurface(SurfacePanel)), func() {
		switch {
		case entry != nil && entry.Kind == KindWorkingTree:
			historyText("Working tree changes", q, FontWeight(WeightSemibold), FontSize(17))
		case entry != nil && entry.Kind == KindStaging:
			historyText("Staged changes", q, FontWeight(WeightSemibold), FontSize(17))
		case entry != nil && entry.Kind == KindCommit && entry.Subject != "":
			historyText(entry.Subject, q, FontWeight(WeightSemibold), FontSize(17))
		case docReady && doc.Subject != "":
			historyText(doc.Subject, q, FontWeight(WeightSemibold), FontSize(17))
		case entry != nil && entry.Short != "":
			historyText(entry.Short, q, FontWeight(WeightBold), FontSize(14), Fonts(Monospace...))
		default:
			historyText(t.selected, q, FontWeight(WeightBold), FontSize(14), Fonts(Monospace...))
		}

		// Author / time: prefer full doc meta when ready; else sidebar HistoryEntry.
		if entry != nil && entry.Kind == KindCommit {
			if docReady {
				meta := strings.TrimSpace(fmt.Sprintf("%s <%s>  ·  %s", doc.Author, doc.Email, doc.Date))
				if meta != "<>  ·" && meta != "" {
					Label(meta+"  ·  "+entry.Short, FontSize(11), TextColorVec(CurrentColorScheme.List.Muted))
				}
				if len(doc.Parents) > 1 {
					Label(fmt.Sprintf("merge commit (%d parents) — showing first-parent diff", len(doc.Parents)),
						FontSize(11), FontStyle(StyleItalic), TextColor(30, 60, CurrentColorScheme.List.Muted[2], 1))
				}
				if body := strings.TrimSpace(doc.Body); body != "" {
					preview := body
					if lines := strings.Split(preview, "\n"); len(lines) > 8 {
						preview = strings.Join(lines[:8], "\n") + "\n…"
					}
					historyText(preview, q, FontSize(12))
				}
			} else if entry.Author != "" || !entry.When.IsZero() {
				parts := []string{}
				if entry.Author != "" {
					parts = append(parts, entry.Author)
				}
				if ts := formatHistoryTime(entry.When); ts != "" {
					parts = append(parts, ts)
				}
				if len(parts) > 0 {
					Label(strings.Join(parts, "  ·  "), FontSize(11))
				}
			}
		}

		if t.docLoading {
			Label(diffLoadingNote(doc), FontSize(11), TextColorVec(CurrentColorScheme.List.Muted))
		}
		if docReady && !t.docLoading && docHasImageRows(doc) {
			CheckBox(&t.showImageDiffHL, "Highlight image diffs")
		}
		if t.docErr != "" && (t.docID == t.selected || t.docID == "") {
			Label(t.docErr, FontSize(11), TextColorVec(CurrentColorScheme.List.Error))
		}
	})
}

// diffLoadingNote is the in-header status while a patch stream is still open.
// n = headers published so far; total from numstat when known (may exceed n when
// pure-Go tree diff collapses renames, or lag while Myers runs on one file).
func diffLoadingNote(doc *DiffDoc) string {
	if doc == nil {
		return "Loading diff…"
	}
	n := len(doc.Segs)
	if n == 0 {
		for _, r := range doc.Rows {
			if r.Kind == RowFileHeader {
				n++
			}
		}
	}
	total := len(doc.Stats)
	if total == 0 && doc.FileCount > 0 {
		total = doc.FileCount
	}
	switch {
	case n == 0 && total == 0:
		return "Loading diff…"
	case n == 0 && total > 0:
		return fmt.Sprintf("Loading diff… · 0 / %d files", total)
	case total > 0:
		return fmt.Sprintf("Loading diff… · %d / %d files", n, total)
	case n == 1:
		return "Loading diff… · 1 file so far"
	default:
		return fmt.Sprintf("Loading diff… · %d files so far", n)
	}
}

func DiffStream(t *RepoTab) {
	doc := t.doc
	docReady := doc != nil && t.docID == t.selected
	// Reset each paint; re-enabled below when jumps are possible.
	diffFileNav = struct {
		listKey     any
		nextEnabled bool
		nextIndex   int
		nextUseEnd  bool
		prevEnabled bool
		prevIndex   int
	}{}

	NextAccessName("diff_viewport")
	Container(Attrs(Viewport, Expand, Clip, UseSurface(SurfacePanel)), func() {
		AssignAccess()
		type diffSelState struct {
			entryID string
			sel     LineSelection
		}
		st := Use[diffSelState]("diff-sel")

		if doc == nil {
			if t.docLoading {
				Container(Attrs(Expand, Center, Pad(30)), func() {
					Label("Loading…", FontStyle(StyleItalic), FontSize(13))
				})
			}
			return
		}
		if st.entryID != t.docID {
			st.entryID = t.docID
			st.sel.Clear()
		}
		if len(doc.Rows) == 0 {
			Container(Attrs(Expand, Center, Pad(30)), func() {
				// Meta can arrive before the patch; keep showing Loading until
				// docLoading clears (empty patch → "No changes").
				if t.docLoading || !docReady {
					Label("Loading diff…", FontStyle(StyleItalic), FontSize(13))
				} else {
					Label("No changes", FontStyle(StyleItalic), FontSize(13))
				}
			})
			return
		}
		nSource := len(doc.Rows)
		lineText := func(i int) string {
			if i < 0 || i >= nSource {
				return ""
			}
			return doc.Rows[i].Text
		}
		if docReady {
			// Selection is addressed in source row space (stable under collapse).
			LineSelectionFrame(&st.sel, IsHovered(), nSource, lineText)
			syncDiffFind(t)
		}

		v := syncDiffView(t)
		useView := v.HasSegs()
		itemCount := nSource
		if useView {
			itemCount = v.ItemCount()
		}

		// Key by tab+doc so each repo keeps independent scroll.
		listKey := [2]any{t, t.docID}
		var scrollY, maxScroll f32
		var firstVis, lastVis int
		var listContentW f32 // last row width from ItemView (for image MaxSize bake)
		// Row heights are O(1) (fixed line metrics / image dims). Cover the
		// whole list so TotalHeight is an exact mean × n (no tall-head
		// scrollbar snap). Odd counts: top gets the extra middle row.
		avgTop, avgBot := 0, 0
		if itemCount > 0 {
			avgTop = (itemCount + 1) / 2
			avgBot = itemCount / 2
		}
		toggleFile := -1
		VirtualListViewExt(listKey, VirtualListAttrs{
			ItemCount: itemCount,
			// Source-row keys keep file headers stable while folding.
			ItemKey: func(i int) any {
				if useView {
					return v.SourceOf(i)
				}
				return i
			},
			ItemHeight: func(i int, w f32) f32 {
				src := i
				if useView {
					src = v.SourceOf(i)
				}
				return rowHeight(t, doc.Rows[src], w)
			},
			ItemView: func(i int, w f32) {
				listContentW = w
				src := i
				if useView {
					src = v.SourceOf(i)
				}
				var sel *LineSelection
				if docReady {
					sel = &st.sel
				}
				diffRowView(t, src, doc.Rows[src], w, sel, v, &toggleFile)
			},
			AvgSampleTop:       avgTop,
			AvgSampleBottom:    avgBot,
			OutScrollOffset:    &scrollY,
			OutMaxScrollOffset: &maxScroll,
			OutFirstVisible:    &firstVis,
			OutLastVisible:     &lastVis,
		})

		// Background image window: current page ± one page; bake at list width.
		scheduleImagePrefetch(t, doc, firstVis, lastVis, useView, v, listContentW)

		// File-header jumps use visible indices (headers always stay in the list).
		var headers []int
		if useView {
			headers = v.HeadersVis()
		} else {
			headers = fileHeaderIndices(doc.Rows)
		}
		lastH := lastFileHeaderInRange(headers, firstVis, lastVis)
		nextIdx := nextFileHeaderAfter(headers, lastH)
		prevIdx := prevFileHeaderBefore(headers, firstVis)
		// Prev enabled only when there is a header above firstVis (implies
		// we can move). Next when the stream can still scroll down.
		diffFileNav.listKey = listKey
		diffFileNav.nextIndex = nextIdx
		diffFileNav.nextUseEnd = nextIdx < 0
		diffFileNav.nextEnabled = fileNavCanScrollDown(scrollY, maxScroll)
		diffFileNav.prevIndex = prevIdx
		diffFileNav.prevEnabled = prevIdx >= 0

		// Remember top-of-view source row for collapse-all scroll pin.
		if firstVis >= 0 && firstVis < itemCount {
			if useView {
				t.diffPinSource = v.SourceOf(firstVis)
			} else {
				t.diffPinSource = firstVis
			}
		}

		// Apply a row click after painting: the visible-to-source map stays
		// consistent for every item in this pass.
		if toggleFile >= 0 {
			toggleDiffFile(t, toggleFile)
		}
	})
}

func rowHeight(t *RepoTab, r DiffRow, width f32) f32 {
	switch r.Kind {
	case RowFileHeader:
		return fileHeaderH
	case RowHunkHeader:
		return hunkHeaderH
	case RowImage:
		return imageRowHeight(t, r.Text, width)
	default:
		return diffLineH
	}
}

// imageRowHeight is the virtual-list / paint height for a RowImage.
// Fixed slot so async dim/decode never reflows the list (reflow felt like stall).
// ImageWipe still letterboxes inside the content box.
func imageRowHeight(t *RepoTab, path string, width f32) f32 {
	_ = t
	_ = path
	_ = width
	return imageWipeRowH
}

func diffRowTextStyle(r DiffRow) TextStyleAttrs {
	st := DefaultTextStyle()
	st.SetFontFamilies(Monospace...)
	switch r.Kind {
	case RowFileHeader:
		st.FontSize = 12
		st.Weight = WeightBold
		st.TextColor = CurrentColorScheme.Surfaces.Panel.Text
	case RowHunkHeader:
		st.FontSize = 11
		st.TextColor = CurrentColorScheme.FocusRing
	case RowAdd:
		st.FontSize = monoSize
		st.TextColor = CurrentColorScheme.Surfaces.Panel.Text
	case RowDel:
		st.FontSize = monoSize
		st.TextColor = CurrentColorScheme.Surfaces.Panel.Text
	case RowMeta:
		st.FontSize = 11
		st.Style = StyleItalic
		st.TextColor = CurrentColorScheme.List.Muted
	default:
		st.FontSize = monoSize
		st.TextColor = CurrentColorScheme.Surfaces.Panel.Text
	}
	return st
}

func diffRowView(t *RepoTab, idx int, r DiffRow, width f32, sel *LineSelection, view *DiffView, toggleFile *int) {
	h := rowHeight(t, r, width)

	if r.Kind == RowImage {
		diffImageRowView(t, r, width, h)
		return
	}

	if r.Kind == RowFileHeader {
		if diffFileHeaderView(idx, r, width, h, view) {
			*toggleFile = view.fileOfSource(idx)
		}
		return
	}

	style := diffRowTextStyle(r)
	shaped := ShapeText(r.Text, style)

	// Find highlights only while the optional bar is open (query persists for reopen).
	var findSpans []StyleSpan
	if t != nil && t.diffFindOpen && t.findQuery != "" && len(t.findMatches) > 0 {
		cur := diffMatch{}
		if t.findIdx >= 0 && t.findIdx < len(t.findMatches) {
			cur = t.findMatches[t.findIdx]
		}
		for _, m := range matchesOnRow(t.findMatches, idx) {
			bg := Vec4{43, 65, 76, 1}
			if CurrentColorScheme.Surfaces.Panel.Background[2] < 50 {
				bg = Vec4{43, 45, 30, 1}
			}
			fg := CurrentColorScheme.Surfaces.Panel.Text
			if m.row == cur.row && m.from == cur.from && m.to == cur.to {
				bg, fg = Vec4{43, 85, 57, 1}, Vec4{0, 0, 10, 1}
			}
			findSpans = append(findSpans, ResolveSpan(m.from, m.to, style, TextBackgroundVec(bg), TextColorVec(fg)))
		}
	}

	Container(Attrs(Expand, MainAlign(AlignMiddle), FixHeight(h), MaxWidth(width), Clip, Pad2(0, 10)), func() {
		switch r.Kind {
		case RowHunkHeader:
			ModAttrs(UseSurface(SurfaceCanvas))
		case RowAdd:
			ModAttrs(BackgroundVec(diffRowColor(true)))
		case RowDel:
			ModAttrs(BackgroundVec(diffRowColor(false)))
		}
		// Whole-row tint for the line that holds the current match.
		if t != nil && t.diffFindOpen && t.findIdx >= 0 && t.findIdx < len(t.findMatches) && t.findMatches[t.findIdx].row == idx {
			if r.Kind == RowContext || r.Kind == RowMeta {
				ModAttrs(Background(55, 35, (CurrentColorScheme.Surfaces.Panel.Background[2]*0.94 + CurrentColorScheme.Surfaces.Panel.Text[2]*0.06), 1))
			}
		}

		var selFrom, selTo int
		if sel != nil {
			if IsHovered() {
				sel.Hit(idx, shaped)
			}
			selFrom, selTo = sel.LineRange(idx, len(shaped.Runes))
		}

		Container(Attrs(Row, Expand), func() {
			ShapedTextLayoutStyled(shaped, style, selFrom, selTo, CurrentColorScheme.TextInput.Selection, findSpans...)
		})
	})
}

// diffViewFoldable is true when at least one file has body rows to hide.
func diffViewFoldable(v *DiffView) bool {
	if !v.HasSegs() {
		return false
	}
	for _, s := range v.segs {
		if s.End-s.Header > 1 {
			return true
		}
	}
	return false
}

// diffFileHeaderView renders one clickable row in both expanded and collapsed states.
func diffFileHeaderView(srcIdx int, r DiffRow, width, h f32, view *DiffView) (clicked bool) {
	fileIdx := view.fileOfSource(srcIdx)
	collapsed := view.IsCollapsed(fileIdx)
	filePath := strings.TrimSuffix(r.Text, " (untracked)")
	name, directory := path.Base(filePath), path.Dir(filePath)
	if oldPath, newPath, renamed := strings.Cut(filePath, " → "); renamed {
		filePath = newPath
		name, directory = path.Base(newPath), path.Dir(newPath)
		if path.Base(oldPath) != name {
			name = path.Base(oldPath) + " → " + name
		}
		if path.Dir(oldPath) != directory {
			directory = path.Dir(oldPath) + " → " + directory
		}
	}
	if directory == "." {
		directory = "Repository root"
	}
	if strings.HasSuffix(r.Text, " (untracked)") {
		name += " (untracked)"
	}

	NextAccessName("diff_file")
	NextAccessRole("button")
	NextAccessLabel(r.Text)
	NextAccessValue(r.Text)
	description := "Collapse file"
	if collapsed {
		description = "Expand file"
	}
	NextAccessDescription(description)
	Container(Attrs(Row, CrossMid, Expand, FixHeight(h), MaxWidth(width), Clip, Pad2(0, 10), Gap(10),
		UseSurface(SurfaceCanvas)), func() {
		AssignAccess()
		st := ProcessButtonEvents(fileIdx < 0)
		clicked = st.Clicked
		if st.Hovered {
			ModAttrs(BackgroundVec(CurrentColorScheme.List.Hovered.Background))
		}
		if st.FocusVisible {
			ModAttrs(BorderWidth(1), BorderColorVec(CurrentColorScheme.FocusRing))
		}
		Element(Attrs(Float(0, h-0.5), FixSize(width, 0.5), BackgroundVec(CurrentColorScheme.Surfaces.Panel.Border)))
		Container(Attrs(FixSize(22, 28), Center), func() {
			chevron := SymDown
			if collapsed {
				chevron = SymRight
			}
			Icon(chevron, FontSize(14))
		})
		icon := SymFile
		if isImagePath(filePath) {
			icon = SymImage
		}
		Icon(icon, FontSize(16), TextColorVec(CurrentColorScheme.List.Muted))
		Container(Attrs(Grow(1), Expand, Extrinsic, Clip, MainAlign(AlignMiddle), Gap(2)), func() {
			// Keep both lines unwrapped while leaving room for counts.
			Container(Attrs(Row, Expand, Clip), func() {
				ModAttrs(UnsetMaxCross)
				Label(name, FontSize(12), FontWeight(WeightSemibold))
			})
			Container(Attrs(Row, Expand, Clip), func() {
				ModAttrs(UnsetMaxCross)
				Label(directory, FontSize(10), TextColorVec(CurrentColorScheme.List.Muted))
			})
		})
		if fileIdx >= 0 {
			seg := view.segs[fileIdx]
			Container(Attrs(Row, CrossMid, Gap(12)), func() {
				if seg.Binary || seg.Added < 0 || seg.Deleted < 0 {
					Label("binary", FontSize(11), TextColorVec(CurrentColorScheme.List.Muted))
				} else {
					Label(fmt.Sprintf("+%d", seg.Added), FontSize(11), Fonts(Monospace...), TextColorVec(diffStatColor(true)))
					Label(fmt.Sprintf("−%d", seg.Deleted), FontSize(11), Fonts(Monospace...), TextColorVec(diffStatColor(false)))
				}
			})
		}
	})
	return clicked
}

// diffImageRowView paints an ImageWipe for a binary image change.
// Left = new, right = old; green/red 6px outline; purple highlight @ 50% when enabled.
// height must match imageRowHeight (fixed). Images are pre-baked to MaxSize×scale.
func diffImageRowView(t *RepoTab, r DiffRow, width, height f32) {
	Container(Attrs(Expand, FixHeight(height), MaxWidth(width), Clip, Pad2(8, 12),
		UseSurface(SurfaceCanvas)), func() {
		if t == nil {
			Label("image diff", FontSize(12))
			return
		}
		pair := ensureImagePair(t, r.Text, width)
		if !pair.ready {
			Label("Loading image…", FontSize(12), FontStyle(StyleItalic))
			return
		}
		if pair.err != "" && pair.old == nil && pair.new == nil {
			Label(pair.err, FontSize(12), FontStyle(StyleItalic), TextColorVec(CurrentColorScheme.List.Error))
			return
		}

		// Per-path wipe position (stable identity under the list item).
		type wipePos struct {
			T    float32
			Init bool
		}
		st := Use[wipePos](r.Text)
		if !st.Init {
			st.T = 0.5
			st.Init = true
		}

		// Keys include list width + scale so UseImage does not reuse a stale bake.
		bakeTag := fmt.Sprintf("%.0f@%.2f", pair.listWidth, pair.windowScale)
		var leftId, rightId ImageId
		if pair.new != nil {
			leftId = UseImage("gh-img-new:"+t.docID+":"+r.Text+":"+bakeTag, pair.new)
		}
		if pair.old != nil {
			rightId = UseImage("gh-img-old:"+t.docID+":"+r.Text+":"+bakeTag, pair.old)
		}

		hl := gitImageWipeHLOff
		if t.showImageDiffHL {
			hl = gitImageWipeHLOn
		}

		maxBox := pair.logicalSize
		if maxBox[0] < 1 || maxBox[1] < 1 {
			maxBox = wipeContentLogicalSize(width)
		}

		ImageWipe(ImageWipeAttrs{
			LeftImage:          leftId,
			RightImage:         rightId,
			OutSlider:          &st.T,
			LeftAccentColor:    gitImageWipeLeftAccent,
			RightAccentColor:   gitImageWipeRightAccent,
			OutlineThickness:   6,
			LeftLabel:          "new",
			RightLabel:         "old",
			DiffHighlightColor: hl,
			MaxSize:            maxBox,
		})
	})
}

func selectedEntry(t *RepoTab) *HistoryEntry {
	for i := range t.history {
		if t.history[i].ID == t.selected {
			return &t.history[i]
		}
	}
	return nil
}

func centeredMessage(msg string) {
	Container(Attrs(Grow(1), Expand, Center, Pad(24)), func() {
		Label(msg, FontSize(13), FontStyle(StyleItalic))
	})
}

func clampF32(v, lo, hi f32) f32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// Diff colors stay readable against the active scheme's panel.
func diffStatColor(added bool) Vec4 {
	hue := f32(8)
	if added {
		hue = 140
	}
	light := f32(34)
	if CurrentColorScheme.Surfaces.Panel.Background[2] < 50 {
		light = 72
	}
	return Vec4{hue, 48, light, 1}
}

func diffRowColor(added bool) Vec4 {
	hue := f32(8)
	if added {
		hue = 140
	}
	bg := CurrentColorScheme.Surfaces.Panel.Background[2]
	fg := CurrentColorScheme.Surfaces.Panel.Text[2]
	mix := f32(0.07)
	if bg < 50 {
		mix = 0.025
	}
	return Vec4{hue, 20, bg*(1-mix) + fg*mix, 1}
}
