package main

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

type f32 = float32

const (
	// Base row metrics; actual commit height varies with display options.
	historyRowLineH f32 = 16
	historyRowPad   f32 = 8 // Pad4 top+bottom
	historyRowGap   f32 = 2
	historyRowMinH  f32 = 28 // synthetic / single-line commits
	diffLineH       f32 = 18
	fileHeaderH     f32 = 26
	hunkHeaderH     f32 = 20
	sidebarMin      f32 = 180
	sidebarMax      f32 = 480
	splitterW       f32 = 6
	monoSize        f32 = 12
)

// historyRowHeight returns the fixed height for a sidebar history row.
// Synthetic slots are one line; commits grow with author / time / stats toggles.
func historyRowHeight(t *RepoTab, kind EntryKind) f32 {
	if kind != KindCommit {
		return historyRowMinH
	}
	showAuthor, showTime, showStats := false, false, false
	if t != nil {
		showAuthor, showTime, showStats = t.showAuthor, t.showTime, t.showStats
	}
	lines := 1 // short hash + subject
	if showAuthor || showTime {
		lines++
	}
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

var sidebarWidth f32 = 280

// findBarFocused is set while either optional find field has focus so Up/Down
// do not also move the commit selection. Per-bar flags drive Esc/Enter so an
// open-but-unfocused bar does not steal keys from the other.
var (
	findBarFocused  bool
	diffFindFocused bool
	histFindFocused bool
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
	// findBarFocused still holds last frame's paint result so key handling
	// (before the bars re-draw) knows whether a find field owns focus.
	t := appData.active
	if t != nil && !appData.browseOpen {
		handleAppKeys(t)
	}
	findBarFocused = false
	diffFindFocused = false
	histFindFocused = false
	Container(Attrs(Viewport, Background(220, 12, 96, 1)), func() {
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
		// Browser modal and toast sit above content (popup / float).
		NewRepoBrowser()
		ToastView()
	})
}

// StatusBar is a single bottom strip of shortcut hints for the active tab.
func StatusBar(t *RepoTab) {
	mod := primaryModLabel()
	Container(Attrs(Row, Expand, CrossMid, Gap(10), Pad2(4, 12),
		Background(220, 12, 88, 1), BorderColor(0, 0, 78, 1), BorderWidth(1)), func() {
		hint := func(s string) {
			Label(s, FontSize(10), FontStyle(StyleItalic), TextColor(0, 0, 45, 1))
		}
		hint(mod + "L filter history")
		hint("·")
		hint(mod + "F find in diff")
		hint("·")
		hint("N/P next/prev file")
		if strings.TrimSpace(t.histFindQuery) != "" {
			Filler(1)
			n := len(t.histFindMatches)
			note := "history: 0 matches"
			if n > 0 {
				note = fmt.Sprintf("history: %d matches", n)
			}
			if t.historyHasMore {
				note += "+"
			}
			hint(note)
		} else if t.diffFindOpen && t.findQuery != "" {
			Filler(1)
			n := len(t.findMatches)
			note := "diff: 0 matches"
			if n > 0 && t.findIdx >= 0 {
				note = fmt.Sprintf("diff: %d/%d", t.findIdx+1, n)
			} else if n > 0 {
				note = fmt.Sprintf("diff: %d matches", n)
			}
			hint(note)
		}
	})
}

// TabBar is a horizontal strip of open repos + a special "New" tab-like button.
func TabBar() {
	var closeReq *RepoTab
	Container(Attrs(Row, Extrinsic, Clip, Expand, FixHeight(40), Pad2(6, 10),
		Background(220, 12, 84, 1), BorderColor(0, 0, 75, 1), BorderWidth(1)), func() {
		ScrollOnInput()
		ScrollBars()
		Container(Attrs(Row, CrossMid, Gap(6)), func() {
			for _, tab := range appData.tabs {
				if RepoTabChrome(tab) {
					closeReq = tab
				}
			}
			NewTabButton()
		})
	})
	if closeReq != nil {
		closeTab(closeReq)
	}
}

// NewTabButton is a split control: main area opens the folder browser; the
// chevron opens a filterable menu of recently opened repos.
func NewTabButton() {
	// Shared inactive-tab chrome around both segments.
	Container(Attrs(Row, CrossMid, MinHeight(26), Corners(5), Background(220, 10, 92, 1)), func() {
		if IsHovered() {
			ModAttrs(Background(220, 14, 95, 1))
		}
		// Primary: open directory browser.
		Container(Attrs(Row, CrossMid, Gap(4), Pad2(4, 10)), func() {
			if PressAction() {
				openNewRepoBrowser("")
			}
			Label("+ New", FontWeight(WeightBold), FontSize(12), TextColor(0, 0, 20, 1))
		})
		// Divider
		Element(Attrs(FixWidth(1), FixHeight(16), Background(0, 0, 0, 0.08)))
		// Recents menu (builtin MenuButton + keyboard filter).
		MenuButtonExt("", ButtonAttrs{
			Icon:     TypArrowSortedDown,
			TextSize: 11,
		}, func() {
			_ = MenuFilterQuery() // opt into typeahead
			if len(appData.recents) == 0 {
				Label("No recent repos", FontSize(11), FontStyle(StyleItalic), TextColor(0, 0, 50, 1))
				return
			}
			for _, path := range appData.recents {
				label := recentMenuLabel(path)
				if !MenuFilterMatches(label) && !MenuFilterMatches(path) {
					continue
				}
				p := path // capture
				if MenuItem(0, label) {
					openRecentRepo(p)
				}
			}
		})
	})
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
		showToast(err.Error())
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

// NewRepoBrowser is the modal directory picker for + New.
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
		// Keep cwd non-empty for FileBrowserPanel.
		if appData.browseCwd == "" {
			appData.browseCwd, _ = filepath.Abs(".")
		}
		if FileBrowserPanel(&appData.browseCwd, &appData.browseFilter, &appData.browseSelected, &appData.browsePick, attrs) {
			tryOpenFromBrowser(appData.browsePick)
			return
		}
		if Button(0, "Cancel") {
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
		showToast(err.Error())
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

const toastDuration = 2 * time.Second

func showToast(msg string) {
	appData.toastMsg = msg
	appData.toastUntil = time.Now().Add(toastDuration)
	RequestNextFrame()
}

// ToastView draws a single bottom-right notification with × and auto-dismiss.
func ToastView() {
	if appData.toastMsg == "" {
		return
	}
	if !appData.toastUntil.IsZero() && time.Now().After(appData.toastUntil) {
		appData.toastMsg = ""
		appData.toastUntil = time.Time{}
		return
	}
	// Keep the frame loop alive until auto-dismiss.
	RequestNextFrame()

	ws := GetHost().WindowSize
	const tw f32 = 360
	// Content-sized height; reserve enough for 12px type + padding so Float
	// anchor stays above the window edge (descenders need room under baseline).
	const reserveH f32 = 64
	pad := f32(16)
	x := ws[0] - tw - pad
	y := ws[1] - reserveH - pad
	if x < pad {
		x = pad
	}
	if y < pad {
		y = pad
	}

	// Clip on the outer padded card only — keeps long paths inside the
	// rounded rect without a tight clip around the label (which ate descenders).
	Container(Attrs(NoAnimate, InFront, Float(x, y), FixWidth(tw), Clip,
		Row, CrossMid, Gap(10), Pad2(14, 14), Corners(8),
		Background(0, 0, 18, 0.92), BoxShadow(12)), func() {
		Container(Attrs(Grow(1)), func() {
			Label(appData.toastMsg, FontSize(12), TextColor(0, 0, 98, 1))
		})
		Container(Attrs(Pad(4), Corners(4)), func() {
			if IsHovered() {
				ModAttrs(Background(0, 0, 100, 0.15))
			}
			if PressAction() {
				appData.toastMsg = ""
				appData.toastUntil = time.Time{}
			}
			Icon(TypTimes, FontSize(12), TextColor(0, 0, 90, 1))
		})
	})
}

// RepoTabChrome draws one tab; returns true if × was clicked this frame.
func RepoTabChrome(t *RepoTab) (closeClicked bool) {
	active := t == appData.active
	ContainerWithKey(t, Attrs(Row, CrossMid, Gap(6), Pad2(4, 10), Corners(5),
		MinHeight(26), MaxWidth(200), Background(220, 10, 92, 1)), func() {
		if !active && IsHovered() {
			ModAttrs(Background(220, 14, 95, 1))
		}
		if active {
			ModAttrs(Background(0, 0, 100, 1), BorderColor(210, 40, 55, 1), BorderWidth(1))
		}
		if PressAction() {
			appData.active = t
			ensureTabLoaded(t)
			scheduleSaveSession()
		}
		Container(Attrs(MaxWidth(140), Clip), func() {
			Label(t.label, FontWeight(WeightBold), FontSize(12), TextColor(0, 0, 20, 1))
		})
		if t.listLoading {
			Label("…", FontSize(11), TextColor(0, 0, 45, 1))
		}
		Container(Attrs(Pad(2), Corners(3)), func() {
			if IsHovered() {
				ModAttrs(Background(0, 0, 55, 0.4))
			}
			if PressAction() {
				closeClicked = true
			}
			Icon(TypTimes, FontSize(11), TextColor(0, 0, 35, 1))
		})
	})
	return closeClicked
}

// ToolBar: refresh for the active tab only.
func ToolBar(t *RepoTab) {
	Container(Attrs(Row, CrossMid, Expand, Gap(10), Pad2(6, 12),
		Background(220, 14, 90, 1), BorderColor(0, 0, 78, 1), BorderWidth(1)), func() {
		Label(t.path, FontSize(11), TextColor(0, 0, 40, 1))
		Filler(1)
		if CtrlButton(SymRefresh, "Refresh", !t.listLoading) {
			go refreshHistory(t, true)
		}
	})
}

func emptyNoTabs() {
	Container(Attrs(Grow(1), Expand, Center, Gap(12), Pad(40)), func() {
		Label("Open a git repository", FontSize(16), FontWeight(WeightBold), TextColor(0, 0, 30, 1))
		Label("Click + New in the tab bar, or pass a path on the command line.",
			FontSize(12), TextColor(0, 0, 45, 1))
		if CtrlButton(0, "Open repository…", true) {
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
	if handleFileNavKeys(t) {
		return
	}
	handleHistoryKeys(t)
}

// handleFileNavKeys: N next file / P previous file (float buttons). Only when
// that direction is enabled; ignored while a find field has focus.
func handleFileNavKeys(t *RepoTab) bool {
	if findBarFocused || diffFindFocused {
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
		t.histFindOpen = true
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
	Container(Attrs(FixWidth(splitterW), Expand, Background(0, 0, 80, 1)), func() {
		if IsHovered() {
			ModAttrs(Background(210, 60, 60, 1))
		}
		PressAction()
		if IsActive() {
			sidebarWidth = clampF32(sidebarWidth+GetFrameInput().Motion[0], sidebarMin, sidebarMax)
		}
	})
}

func Sidebar(t *RepoTab) {
	// Path lives in ToolBar (full width + Refresh); don't repeat it here.
	// Header with list options, optional History find bar (⌘/Ctrl+L), then list.
	Container(Attrs(FixWidth(sidebarWidth), Expand, Clip, Background(0, 0, 95, 1)), func() {
		if t.repoErr != "" {
			Container(Attrs(Expand, Pad(12)), func() {
				Label(t.repoErr, FontSize(12), TextColor(0, 70, 45, 1))
			})
			return
		}

		if t.listErr != "" && len(t.history) == 0 {
			Container(Attrs(Expand, Pad(12)), func() {
				Label(t.listErr, FontSize(12), TextColor(0, 70, 45, 1))
			})
			return
		}

		HistoryListHeader(t)

		if t.histFindOpen {
			HistoryFindBar(t)
		}

		Container(Attrs(Viewport), func() {
			if len(t.history) == 0 {
				Container(Attrs(Expand, Pad(12)), func() {
					if t.listLoading {
						Label("Loading history…", FontStyle(StyleItalic), TextColor(0, 0, 50, 1))
					} else {
						Label("No commits", FontStyle(StyleItalic), TextColor(0, 0, 50, 1))
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
							Label("Searching older commits…", FontStyle(StyleItalic), TextColor(0, 0, 50, 1))
						} else {
							Label("No matching commits", FontStyle(StyleItalic), TextColor(0, 0, 50, 1))
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
							Label("Loading older commits…", FontSize(11), FontStyle(StyleItalic), TextColor(0, 0, 50, 1))
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
		Background(0, 0, 93, 1), BorderColor(0, 0, 85, 1), BorderWidth(1)), func() {
		Label("Commits", FontSize(11), FontWeight(WeightSemibold), TextColor(0, 0, 35, 1))
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
		MenuButtonExt("", ButtonAttrs{
			Icon:     SymOptsV,
			TextSize: 11,
		}, func() {
			// Menu shell only pads vertically (Pad2(6,0)); wrap content so the
			// panel has even inset around the title and checkboxes.
			Container(Attrs(Pad2(5, 10), Gap(10), MinWidth(148)), func() {
				Label("Show in list", FontSize(10), FontStyle(StyleItalic), TextColor(0, 0, 45, 1))
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

// statPill is a compact +n / −m chip: white bold mono on a solid hue.
func statPill(text string, bg Vec4) {
	Container(Attrs(Row, CrossMid, Pad2(1, 5), Corners(3), BackgroundVec(bg)), func() {
		Label(text, FontSize(10), FontWeight(WeightBold), Fonts(Monospace...),
			TextColor(0, 0, 100, 1))
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

	ContainerWithKey(e.ID, Attrs(Expand, FixHeight(rowH), MaxWidth(width), Clip, Gap(historyRowGap), Pad4(4, 10, 4, 10)), func() {
		ModAttrs(UnsetMaxCross)
		if selected {
			ModAttrs(Background(210, 70, 50, 1))
		} else if IsHovered() {
			ModAttrs(Background(0, 0, 90, 1))
		}
		if PressAction() {
			selectEntry(t, e.ID)
		}

		textColor := Vec4{0, 0, 12, 1}
		muteColor := Vec4{0, 0, 40, 1}
		if selected {
			textColor = Vec4{0, 0, 100, 1}
			muteColor = Vec4{0, 0, 88, 1}
		}

		switch e.Kind {
		case KindWorkingTree, KindStaging:
			if !selected {
				textColor = Vec4{30, 70, 35, 1}
			}
			Container(Attrs(Row, CrossMid, Expand, Grow(1)), func() {
				historyText(e.SidebarLabel(), q,
					FontSize(12), FontWeight(WeightSemibold), TextColorVec(textColor))
			})
		default:
			Container(Attrs(Row, CrossMid, Expand, Gap(8), Clip), func() {
				ModAttrs(UnsetMaxCross)
				historyText(e.Short, q,
					FontSize(11), Fonts(Monospace...), TextColorVec(muteColor))
				historyText(e.Subject, q,
					FontSize(12), TextColorVec(textColor))
			})
			// Optional meta line: author and/or timestamp.
			if t.showAuthor || t.showTime {
				Container(Attrs(Row, CrossMid, Expand, Gap(6), Clip), func() {
					ModAttrs(UnsetMaxCross)
					if t.showAuthor && e.Author != "" {
						historyText(e.Author, q,
							FontSize(10), TextColorVec(muteColor))
					}
					if t.showTime {
						if ts := formatHistoryTime(e.When); ts != "" {
							if t.showAuthor && e.Author != "" {
								Label("·", FontSize(10), TextColorVec(muteColor))
							}
							Label(ts, FontSize(10), Fonts(Monospace...), TextColorVec(muteColor))
						}
					}
				})
			}
			// Optional stats line (lazy-loaded when shown).
			if t.showStats {
				requestCommitStats(t, e.ID)
				if st, ok := t.commitStats[e.ID]; ok && st.Ready {
					Container(Attrs(Row, CrossMid, Gap(4)), func() {
						statPill(fmt.Sprintf("+%d", st.Added), Vec4{130, 55, 42, 1})
						statPill(fmt.Sprintf("−%d", st.Deleted), Vec4{8, 60, 48, 1})
						files := "files"
						if st.Files == 1 {
							files = "file"
						}
						Label(fmt.Sprintf("· %d %s", st.Files, files), FontSize(10),
							Fonts(Monospace...), TextColorVec(muteColor))
					})
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
	bg := Vec4{55, 45, 90, 0.85} // pale amber (same family as diff find)
	spans := make([]TextSpan, 0, len(ranges))
	for _, r := range ranges {
		spans = append(spans, Span(r[0], r[1], TextBackgroundVec(bg)))
	}
	Text(text, TextStyle(mods...), spans...)
}

func MainContent(t *RepoTab) {
	// Extrinsic+Clip: width comes from the pane, not from long header/find/diff
	// lines (otherwise the find bar and scrollbar get pushed off-screen).
	Container(Attrs(Grow(1), Expand, Extrinsic, Clip, Background(0, 0, 100, 1)), func() {
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
		if t.docLoading && t.doc == nil {
			centeredMessage("Loading…")
			return
		}
		if t.docErr != "" && t.doc == nil {
			centeredMessage(t.docErr)
			return
		}

		DiffHeader(t)
		if t.diffFindOpen && t.doc != nil && t.docID == t.selected {
			DiffFindBar(t)
		}
		DiffStream(t)
	})
}

// DiffFindBar is optional (⌘/Ctrl+F). Hidden by default; Esc dismisses.
//
//	[ icon | field_wrapper | matches | ↑↓ ]
//
// field_wrapper is Extrinsic+Grow(1)+Expand: its size comes from leftover flex
// space, never from the TextInput. Inside it, pin the field to the offer.
// MinHeight: Expand only matches sibling height; icon/matches/buttons are
// shorter than the default TextInput, so the row would otherwise stay too short.
func DiffFindBar(t *RepoTab) {
	Container(Attrs(Row, Expand, Clip, CrossMid, Gap(6), Pad2(6, 12),
		MinHeight(40), CrossMid,
		Background(220, 8, 96, 1), BorderColor(0, 0, 88, 1), BorderWidth(1)), func() {
		Icon(SymSearch, FontSize(12), TextColor(0, 0, 55, 1))

		Container(Attrs(Grow(1), Expand, Extrinsic, Clip), func() {
			sz := GetAvailableSize()
			if sz[0] < 1 || sz[1] < 1 {
				RequestNextFrame()
				return
			}
			attrs := DefaultTextInputAttrs()
			attrs.FontSize = 12
			attrs.MinWidth = sz[0]
			attrs.MaxWidth = sz[0]
			attrs.FixedWidth = true
			attrs.NoAutoFocus = true
			attrs.Placeholder = "Find in diff…"
			TextInputExt(&t.findQuery, attrs)
			if t.diffFindFocusReq {
				FocusImmediateOn(GetLastId())
				t.diffFindFocusReq = false
			}
			if HasFocusWithin() {
				findBarFocused = true
				diffFindFocused = true
			}
		})

		syncDiffFind(t)
		if t.findQuery != "" {
			if findClearButton() {
				t.findQuery = ""
				syncDiffFind(t)
			} else {
				n := len(t.findMatches)
				note := "0 matches"
				if n > 0 {
					note = fmt.Sprintf("%d/%d", t.findIdx+1, n)
				}
				Label(note, FontSize(10), TextColor(0, 0, 50, 1))
			}
		}
		n := len(t.findMatches)
		canNav := n > 0
		if CtrlButton(SymArrowUp, "", canNav) {
			diffFindStep(t, -1)
		}
		if CtrlButton(SymArrowDown, "", canNav) {
			diffFindStep(t, +1)
		}

		if diffFindFocused {
			switch GetFrameInput().Key {
			case KeyEnter:
				if GetInputState().Modifiers&ModShift != 0 {
					diffFindStep(t, -1)
				} else {
					diffFindStep(t, +1)
				}
			case KeyEscape:
				// Dismiss the bar; keep query so reopen resumes the same search.
				t.diffFindOpen = false
				t.diffFindFocusReq = false
				ClearFocus()
			}
		}
	})
}

// HistoryFindBar is optional (⌘/Ctrl+L). Filters the commit list to matches.
func HistoryFindBar(t *RepoTab) {
	Container(Attrs(Row, Expand, Clip, CrossMid, Gap(4), Pad2(4, 8),
		MinHeight(36),
		Background(220, 8, 96, 1), BorderColor(0, 0, 88, 1), BorderWidth(1)), func() {
		Icon(SymSearch, FontSize(11), TextColor(0, 0, 55, 1))

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
			attrs.Placeholder = "Filter history…"
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
				Label(note, FontSize(9), TextColor(0, 0, 50, 1))
			}
		}

		if histFindFocused {
			switch GetFrameInput().Key {
			case KeyEscape:
				// Dismiss and clear so the list is unfiltered again.
				t.histFindQuery = ""
				syncHistFind(t)
				t.histFindOpen = false
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
			ModAttrs(Background(0, 0, 0, 0.08))
		}
		if PressAction() {
			clicked = true
		}
		Icon(SymICross, FontSize(11), TextColor(0, 0, 45, 1))
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
// near the middle.
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
	VirtualListView_ScrollToIndexAt([2]any{t, t.docID}, row, frac)
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
	Container(Attrs(Expand, Clip, Pad4(14, 16, 12, 16), Gap(6), Background(0, 0, 98, 1),
		BorderColor(0, 0, 88, 1), BorderWidth(1)), func() {
		switch {
		case entry != nil && entry.Kind == KindWorkingTree:
			historyText("Working tree changes", q, FontWeight(WeightBold), FontSize(15))
		case entry != nil && entry.Kind == KindStaging:
			historyText("Staged changes", q, FontWeight(WeightBold), FontSize(15))
		case entry != nil && entry.Kind == KindCommit && entry.Subject != "":
			historyText(entry.Subject, q, FontWeight(WeightBold), FontSize(15))
		case docReady && doc.Subject != "":
			historyText(doc.Subject, q, FontWeight(WeightBold), FontSize(15))
		case entry != nil && entry.Short != "":
			historyText(entry.Short, q, FontWeight(WeightBold), FontSize(14), Fonts(Monospace...))
		default:
			historyText(t.selected, q, FontWeight(WeightBold), FontSize(14), Fonts(Monospace...))
		}

		if docReady && entry != nil && entry.Kind == KindCommit {
			meta := strings.TrimSpace(fmt.Sprintf("%s <%s>  ·  %s", doc.Author, doc.Email, doc.Date))
			if meta != "<>  ·" && meta != "" {
				Label(meta, FontSize(11), TextColor(0, 0, 40, 1))
			}
			if len(doc.Parents) > 1 {
				Label(fmt.Sprintf("merge commit (%d parents) — showing first-parent diff", len(doc.Parents)),
					FontSize(11), FontStyle(StyleItalic), TextColor(30, 60, 40, 1))
			}
			if body := strings.TrimSpace(doc.Body); body != "" {
				preview := body
				if lines := strings.Split(preview, "\n"); len(lines) > 8 {
					preview = strings.Join(lines[:8], "\n") + "\n…"
				}
				historyText(preview, q, FontSize(12), TextColor(0, 0, 28, 1))
			}
		}

		if docReady {
			Label(formatStatsLine(doc), FontSize(12), FontWeight(WeightSemibold), TextColor(0, 0, 35, 1))
		}
		if t.docLoading && !docReady {
			Label("Loading…", FontSize(11), FontStyle(StyleItalic), TextColor(0, 0, 50, 1))
		}
		if t.docErr != "" && t.selected == t.docID {
			Label(t.docErr, FontSize(11), TextColor(0, 70, 45, 1))
		}
	})
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

	Container(Attrs(Viewport, Expand, Clip, Background(0, 0, 100, 1)), func() {
		type diffSelState struct {
			entryID string
			sel     LineSelection
		}
		st := Use[diffSelState]("diff-sel")

		if doc == nil {
			if t.docLoading {
				Container(Attrs(Expand, Center, Pad(30)), func() {
					Label("Loading…", FontStyle(StyleItalic), FontSize(13), TextColor(0, 0, 50, 1))
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
				if docReady {
					Label("No changes", FontStyle(StyleItalic), FontSize(13), TextColor(0, 0, 50, 1))
				} else {
					Label("Loading…", FontStyle(StyleItalic), FontSize(13), TextColor(0, 0, 50, 1))
				}
			})
			return
		}
		n := len(doc.Rows)
		lineText := func(i int) string {
			if i < 0 || i >= n {
				return ""
			}
			return doc.Rows[i].Text
		}
		if docReady {
			LineSelectionFrame(&st.sel, IsHovered(), n, lineText)
			syncDiffFind(t)
		}

		// Key by tab+doc so each repo keeps independent scroll.
		listKey := [2]any{t, t.docID}
		var scrollY, maxScroll f32
		var firstVis, lastVis int
		VirtualListViewExt(listKey, VirtualListAttrs{
			ItemCount: n,
			ItemKey:   func(i int) any { return i },
			ItemHeight: func(i int, w f32) f32 {
				return rowHeight(doc.Rows[i])
			},
			ItemView: func(i int, w f32) {
				var sel *LineSelection
				if docReady {
					sel = &st.sel
				}
				diffRowView(t, i, doc.Rows[i], w, sel)
			},
			OutScrollOffset:    &scrollY,
			OutMaxScrollOffset: &maxScroll,
			OutFirstVisible:    &firstVis,
			OutLastVisible:     &lastVis,
		})

		// File-header jumps from list-reported painted range.
		headers := fileHeaderIndices(doc.Rows)
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

		diffFileNavButtons()
	})
}

// diffFileNavButtons: prev (↑) stacked above next (↓), bottom-right, 30px inset.
func diffFileNavButtons() {
	const (
		pad    f32 = 30
		btn    f32 = 36
		gap    f32 = 6
		corner f32 = 6
	)
	sz := GetResolvedSize()
	stackH := btn*2 + gap
	if sz[0] < btn+pad*2 || sz[1] < stackH+pad*2 {
		if sz[0] < 1 || sz[1] < 1 {
			RequestNextFrame()
		}
		return
	}
	x := sz[0] - pad - btn
	yDown := sz[1] - pad - btn
	yUp := yDown - gap - btn
	if x < pad {
		x = pad
	}

	diffFileNavButton(x, yUp, SymArrowUp, diffFileNav.prevEnabled, jumpDiffPrevFile)
	diffFileNavButton(x, yDown, SymArrowDown, diffFileNav.nextEnabled, jumpDiffNextFile)
}

func diffFileNavButton(x, y f32, icon rune, enabled bool, onClick func()) {
	const (
		btn    f32 = 36
		corner f32 = 6
	)
	Container(Attrs(NoAnimate, InFront, Float(x, y), FixSize(btn, btn),
		Corners(corner), Center), func() {
		bg := Vec4{210, 40, 45, 1}
		fg := Vec4{0, 0, 100, 1}
		if !enabled {
			bg = Vec4{0, 0, 88, 1}
			fg = Vec4{0, 0, 55, 1}
		} else if IsHovered() {
			bg = Vec4{210, 45, 50, 1}
		}
		ModAttrs(BackgroundVec(bg))
		if enabled && PressAction() {
			onClick()
		}
		Icon(icon, FontSize(16), TextColorVec(fg))
	})
}

func rowHeight(r DiffRow) f32 {
	switch r.Kind {
	case RowFileHeader:
		return fileHeaderH
	case RowHunkHeader:
		return hunkHeaderH
	default:
		return diffLineH
	}
}

func diffRowTextStyle(r DiffRow) TextStyleAttrs {
	st := DefaultTextStyle()
	st.FontFamilies = append([]string{}, Monospace...)
	switch r.Kind {
	case RowFileHeader:
		st.FontSize = 12
		st.Weight = WeightBold
		st.TextColor = Vec4{220, 25, 22, 1}
	case RowHunkHeader:
		st.FontSize = 11
		st.TextColor = Vec4{210, 40, 35, 1}
	case RowAdd:
		st.FontSize = monoSize
		st.TextColor = Vec4{120, 70, 28, 1}
	case RowDel:
		st.FontSize = monoSize
		st.TextColor = Vec4{8, 70, 35, 1}
	case RowMeta:
		st.FontSize = 11
		st.Style = StyleItalic
		st.TextColor = Vec4{0, 0, 45, 1}
	default:
		st.FontSize = monoSize
		st.TextColor = Vec4{0, 0, 18, 1}
	}
	return st
}

func diffRowView(t *RepoTab, idx int, r DiffRow, width f32, sel *LineSelection) {
	h := rowHeight(r)
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
			bg := Vec4{55, 45, 90, 0.85} // pale amber — other hits
			if m.row == cur.row && m.from == cur.from && m.to == cur.to {
				bg = Vec4{48, 80, 72, 0.95} // strong amber — current
			}
			findSpans = append(findSpans, ResolveSpan(m.from, m.to, style, TextBackgroundVec(bg)))
		}
	}

	Container(Attrs(Expand, FixHeight(h), MaxWidth(width), Clip, Pad2(0, 10)), func() {
		ModAttrs(UnsetMaxCross)
		switch r.Kind {
		case RowFileHeader:
			ModAttrs(Background(214, 18, 92, 1))
		case RowHunkHeader:
			ModAttrs(Background(210, 25, 96, 1))
		case RowAdd:
			ModAttrs(Background(120, 35, 94, 1))
		case RowDel:
			ModAttrs(Background(8, 45, 95, 1))
		}
		// Whole-row tint for the line that holds the current match.
		if t != nil && t.diffFindOpen && t.findIdx >= 0 && t.findIdx < len(t.findMatches) && t.findMatches[t.findIdx].row == idx {
			if r.Kind == RowContext || r.Kind == RowMeta {
				ModAttrs(Background(55, 35, 96, 1))
			}
		}

		var selFrom, selTo int
		if sel != nil {
			if IsHovered() {
				sel.Hit(idx, shaped)
			}
			selFrom, selTo = sel.LineRange(idx, len(shaped.Runes))
		}

		Container(Attrs(Row, CrossMid, Expand), func() {
			ShapedTextLayout(shaped, style, selFrom, selTo, findSpans...)
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
		Label(msg, FontSize(13), FontStyle(StyleItalic), TextColor(0, 0, 45, 1))
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
