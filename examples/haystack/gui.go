package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	g "go.hasen.dev/generic"
	"go.hasen.dev/shirei/ext/darkmode"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

type f32 = float32

// App is all durable UI state. The inputs are edited in place by the widgets.
// Each explicit search (Enter / the Search button) becomes a tab in `searches`,
// so prior results stay around to return to; `active` is the selected tab.
type App struct {
	pathInput string
	query     string
	matchCase bool
	wholeWord bool
	regex     bool

	include   string // filename globs to search (empty = all)
	exclude   string // filename globs to skip
	gitignore bool   // honour .gitignore

	searches []*Search // one per tab, in the order they were run
	active   *Search   // the selected tab (nil before the first search)

	editors        []Editor
	startupFocused bool
	focusSearch    bool
	browseOpen     bool
	browseCwd      string
	browseFilter   string
	browseSelected int
}

var appData = new(App)

// Result rows have fixed heights so only visible code lines need building.
const (
	headerH  f32 = 28
	lineH    f32 = 18
	rowGap   f32 = 6
	hPad     f32 = 12
	numColW  f32 = 48
	monoSz   f32 = 12
	controlH f32 = 28
)

// Negative line indices identify structural rows; other indices address Context.
const (
	resultHeader = -1
	resultGap    = -2
	resultEnd    = -3
)

type resultRow struct {
	match *Match
	line  int
	count int // total matching lines in a file header
}

func currentParams() Params {
	return Params{
		Root:      appData.pathInput,
		Query:     appData.query,
		MatchCase: appData.matchCase,
		WholeWord: appData.wholeWord,
		Regex:     appData.regex,
		Include:   appData.include,
		Exclude:   appData.exclude,
		Gitignore: appData.gitignore,
	}
}

func RootView() {
	SetDarkMode(darkmode.OSDarkMode())
	if GetFrameInput().Key == KeyF && GetInputState().Modifiers == PrimaryMod() {
		appData.focusSearch = true
		GetFrameInput().Key = 0
	}
	Container(Attrs(Viewport, UseSurface(SurfaceCanvas), AmendTextStyle(FontSize(12))), func() {
		TopPanel()
		if len(appData.searches) > 0 {
			TabBar()
		}
		ResultsList(appData.active)
		Container(Attrs(Row, Expand, CrossMid, Gap(8), Pad2(5, 12), Clip, UseSurface(SurfaceCanvas), AmendTextStyle(FontSize(11))), func() {
			StatusLine(appData.active)
		})
		ProfileButton("haystack")
	})
}

// loadParams pulls a tab's search terms back into the input fields, so
// selecting a tab lets you see and tweak the query that produced it.
func loadParams(p Params) {
	appData.pathInput = p.Root
	appData.query = p.Query
	appData.matchCase = p.MatchCase
	appData.wholeWord = p.WholeWord
	appData.regex = p.Regex
	appData.include = p.Include
	appData.exclude = p.Exclude
	appData.gitignore = p.Gitignore
}

// activateTab makes s the shown tab: it restores s's first visible row (the
// list was hidden and rebuilt, so we ScrollToIndex back into place) and
// reloads its search terms into the inputs. The outgoing tab's firstVis is
// already saved because ResultsList mirrors it every frame.
func activateTab(s *Search) {
	appData.active = s
	if s.firstVis > 0 {
		VirtualListView_ScrollToIndex(s, s.firstVis)
	}
	loadParams(s.params)
}

// closeTab removes a tab (cancelling its search if still running) and selects a
// neighbour so the view never lands on nothing when other tabs remain.
func closeTab(s *Search) {
	s.cancelled.Store(true)
	idx := slices.Index(appData.searches, s)
	if idx < 0 {
		return
	}
	g.RemoveAt(&appData.searches, idx, 1)
	if appData.active == s {
		if len(appData.searches) == 0 {
			appData.active = nil
		} else {
			activateTab(appData.searches[min(idx, len(appData.searches)-1)])
		}
	}
	RequestNextFrame()
}

// TopPanel uses one control height for fields and command buttons.
func TopPanel() {
	var searchId, includeId, excludeId ContainerId
	look := ButtonLook{TextSize: 12, PadScale: 1}
	toolbarMuted := CurrentColorScheme.Surfaces.Toolbar.Text
	toolbarMuted[3] *= 0.75
	Container(Attrs(Expand, Pad2(8, 12), Gap(7), UseSurface(SurfaceToolbar)), func() {
		Container(Attrs(Row, CrossMid, Gap(8), Expand), func() {
			Icon(TypFolder, FontSize(16))
			Label("Folder", TextColorVec(toolbarMuted))
			compactInput("folder", &appData.pathInput, "Directory to search")
			NextAccessName("browse_folder")
			if ButtonExt("Browse…", ButtonAttrs{}, look) {
				appData.browseCwd = appData.pathInput
				if info, err := os.Stat(appData.browseCwd); err != nil || !info.IsDir() {
					appData.browseCwd, _ = os.Getwd()
				}
				appData.browseCwd, _ = filepath.Abs(appData.browseCwd)
				appData.browseOpen = true
				appData.browseFilter = ""
				appData.browseSelected = -1
			}
		})
		Container(Attrs(Row, CrossMid, Gap(12), Expand), func() {
			Icon(SymSearch, FontSize(16))
			compactInput("query", &appData.query, "Search for text…")
			searchId = GetLastId()
			if !appData.startupFocused || appData.focusSearch {
				FocusImmediateOn(searchId)
				appData.startupFocused, appData.focusSearch = true, false
			}
			NextAccessName("match_case")
			CheckBox(&appData.matchCase, "Match case")
			NextAccessName("whole_word")
			CheckBox(&appData.wholeWord, "Whole word")
			NextAccessName("regex")
			CheckBox(&appData.regex, "Regex")
			NextAccessName("search")
			if ButtonExt("Search", ButtonAttrs{Icon: SymSearch, Type: ButtonPrimary, Disabled: appData.query == "" || appData.pathInput == ""}, look) {
				runNewSearch(currentParams())
			}
		})
		Container(Attrs(Row, CrossMid, Gap(8), Expand), func() {
			Label("Include", FontSize(11), TextColorVec(toolbarMuted))
			compactInput("include", &appData.include, "All files (e.g. *.go)")
			includeId = GetLastId()
			Label("Exclude", FontSize(11), TextColorVec(toolbarMuted))
			compactInput("exclude", &appData.exclude, "e.g. vendor/**, testdata/**")
			excludeId = GetLastId()
			NextAccessName("gitignore")
			CheckBox(&appData.gitignore, "Respect .gitignore")
		})
	})
	if GetFrameInput().Key == KeyEnter &&
		(IdHasFocus(searchId) || IdHasFocus(includeId) || IdHasFocus(excludeId)) {
		GetFrameInput().Key = 0
		runNewSearch(currentParams())
	}
	if appData.browseOpen {
		Modal(580, func() { appData.browseOpen = false }, func() {
			NextAccessName("folder_picker")
			AssignAccess()
			Label("Choose search folder", FontWeight(WeightBold))
			if FileBrowserPanel(&appData.browseCwd, &appData.browseFilter, &appData.browseSelected, &appData.pathInput, DefaultFileBrowserAttrs()) {
				appData.browseOpen = false
			}
			NextAccessName("cancel_folder")
			if CtrlButton(NoIcon, "Cancel", true) {
				appData.browseOpen = false
			}
		})
	}
}

func compactInput(name string, value *string, placeholder string) {
	attrs := DefaultTextInputAttrs()
	attrs.NoAutoFocus, attrs.Depth = true, 0
	attrs.FontSize, attrs.Padding = 12, Vec4{(controlH - 12) / 2, 8, (controlH - 12) / 2, 8}
	attrs.MinWidth, attrs.Placeholder = 100, placeholder
	NextAccessName(name)
	TextInputExt(value, attrs)
}

// Tab close requests are applied after the loop that renders the tabs.
func TabBar() {
	var closeReq *Search
	TabStrip(func() {
		for _, s := range appData.searches {
			if SearchTab(s) {
				closeReq = s
			}
		}
	}, nil)
	if closeReq != nil {
		closeTab(closeReq)
	}
}

func SearchTab(s *Search) (closeClicked bool) {
	NextAccessName("search_tab")
	NextAccessValue(s.params.Query)
	if TabItem(s, s.params.Query, s == appData.active, func() {
		switch {
		case s.err != nil:
			Label("!", TextColorVec(CurrentColorScheme.List.Error))
		case s.running:
			Label("…", TextColorVec(CurrentColorScheme.List.Muted))
		default:
			Label(fmt.Sprint(s.matchCount.Load()), FontSize(11), TextColorVec(CurrentColorScheme.List.Muted))
		}
		NextAccessName("close_tab")
		NextAccessValue(s.params.Query)
		closeClicked = TabCloseButton()
	}) {
		activateTab(s)
	}
	return closeClicked
}

func StatusLine(s *Search) {
	NextAccessName("search_status")
	if s == nil {
		NextAccessValue("ready")
	} else if s.running {
		NextAccessValue("running")
	} else {
		NextAccessValue("done")
	}
	AssignAccess()
	muted := TextColorVec(CurrentColorScheme.List.Muted)
	if s == nil || s.params.Query == "" {
		Label("Ready to search", muted)
	} else if s.err != nil {
		Label("Invalid pattern: "+s.err.Error(), TextColorVec(CurrentColorScheme.List.Error))
	} else {
		var elapsed time.Duration
		if s.running {
			elapsed = time.Since(s.started)
			Icon(SymClock, muted)
			RequestNextFrame()
		} else {
			elapsed = s.done.Sub(s.started)
			Icon(SymPass, TextColorVec(CurrentColorScheme.FocusRing))
		}
		Label(fmt.Sprintf("%d matches in %d files", s.matchCount.Load(), s.filesMatched.Load()))
		Label(fmt.Sprintf("  ·  %d files scanned · %.2fs", s.filesScanned.Load(), elapsed.Seconds()), muted)
	}
	Filler(1)
	Label("Enter Search", muted)
}

// appendResultRows turns complete file batches into headers and individual code
// lines. The worker publishes each file atomically, so appended files are whole.
func appendResultRows(s *Search) {
	for start := s.groupedMatches; start < len(s.matches); {
		end, count := start, 0
		file := s.matches[start].File
		for end < len(s.matches) && s.matches[end].File == file {
			count += s.matches[end].MatchCount
			end++
		}
		s.rows = append(s.rows, resultRow{s.matches[start], resultHeader, count})
		if !s.collapsed[file] {
			for i := start; i < end; i++ {
				m := s.matches[i]
				if i > start {
					s.rows = append(s.rows, resultRow{m, resultGap, 0})
				}
				for j := range m.Context {
					s.rows = append(s.rows, resultRow{m, j, 0})
				}
			}
		}
		s.rows = append(s.rows, resultRow{s.matches[start], resultEnd, 0})
		start = end
	}
	s.groupedMatches = len(s.matches)
}

func ResultsList(s *Search) {
	ContainerWithKey(s, Attrs(Viewport, UseSurface(SurfacePanel)), func() {
		if s == nil || s.err != nil || len(s.matches) == 0 {
			Container(Attrs(Viewport, Center, Gap(8)), func() {
				NextAccessName("empty_results")
				AssignAccess()
				switch {
				case s == nil:
					Icon(SymSearch, FontSize(26), TextColorVec(CurrentColorScheme.List.Muted))
					Label("Search this folder", FontSize(15), FontWeight(WeightSemibold))
					Label("Enter a search term above. Each search stays in its own tab.", TextColorVec(CurrentColorScheme.List.Muted))
				case s.err != nil:
					Label("Check the search pattern", TextColorVec(CurrentColorScheme.List.Error))
				case s.running:
					Label("Searching…", TextColorVec(CurrentColorScheme.List.Muted))
				default:
					Label("No matches", FontSize(15), FontWeight(WeightSemibold))
					Label("Try another term or adjust the file filters.", TextColorVec(CurrentColorScheme.List.Muted))
				}
			})
			return
		}
		appendResultRows(s)
		// Collapse requests are applied after the list finishes building this frame.
		var collapse *FileResult
		VirtualListViewExt(s, VirtualListAttrs{
			ItemCount: len(s.rows),
			ItemKey:   func(i int) any { return s.rows[i] },
			ItemHeight: func(i int, width f32) f32 {
				switch s.rows[i].line {
				case resultHeader:
					return headerH
				case resultEnd:
					return rowGap
				default:
					return lineH
				}
			},
			ItemView: func(i int, width f32) {
				r := s.rows[i]
				switch r.line {
				case resultHeader:
					if ResultHeader(s, r) {
						collapse = r.match.File
					}
				case resultGap:
					Container(Attrs(Row, Expand, FixHeight(lineH), Pad2(0, hPad), UseSurface(SurfacePanel)), func() {
						Label("   ···", Fonts(Monospace...), TextColorVec(CurrentColorScheme.List.Muted))
					})
				case resultEnd:
					Element(Attrs(Expand, FixHeight(rowGap), BackgroundVec(CurrentColorScheme.Surfaces.Canvas.Background)))
				default:
					ResultLine(s, r)
				}
			},
			OutFirstVisible: &s.firstVis,
		})
		if collapse != nil {
			if s.collapsed == nil {
				s.collapsed = make(map[*FileResult]bool)
			}
			s.collapsed[collapse] = !s.collapsed[collapse]
			s.rows = nil
			s.groupedMatches = 0
			RequestNextFrame()
		}
	})
}

func ResultHeader(s *Search, r resultRow) (collapse bool) {
	file := r.match.File
	NextAccessName("file_header")
	NextAccessValue(file.RelPath)
	Container(Attrs(Row, CrossMid, Expand, FixHeight(headerH), Pad2(0, hPad), Gap(8), UseSurface(SurfaceCanvas)), func() {
		AssignAccess()
		NextAccessName("collapse_file")
		NextAccessValue(file.RelPath)
		NextAccessChecked(s.collapsed[file])
		Container(Attrs(Row, CrossMid, Grow(1), Extrinsic, Expand, Gap(8), Clip), func() {
			AssignAccess()
			st := ProcessButtonEvents(false)
			collapse = st.Clicked
			if st.FocusVisible {
				ModAttrs(BorderWidth(1), BorderColorVec(CurrentColorScheme.FocusRing))
			}
			icon := SymDown
			if s.collapsed[file] {
				icon = SymRight
			}
			Icon(icon, FontSize(10))
			Icon(TypDocument, FontSize(13), TextColorVec(CurrentColorScheme.List.Muted))
			Label(file.RelPath, FontWeight(WeightSemibold))
			count := fmt.Sprintf("%d matches", r.count)
			if r.count == 1 {
				count = "1 match"
			}
			Label(count, FontSize(11), TextColorVec(CurrentColorScheme.List.Muted))
		})
		NextAccessName("copy_path")
		NextAccessValue(file.RelPath)
		if CtrlButton(SymCopy, "", true) {
			RequestTextCopy(file.Path)
		}
		if len(appData.editors) > 0 {
			NextAccessName("open_editor")
			NextAccessValue(file.RelPath)
			CtrlMenuButton(SymDown, "Open in…", func() {
				line := r.match.Line
				if s.selectedFile == file {
					line = s.selectedLine
				}
				for _, ed := range appData.editors {
					if MenuItem(SymCode, ed.Name) {
						ed.Open(file.Path, line)
					}
				}
			})
		}
	})
	return collapse
}

func ResultLine(s *Search, r resultRow) {
	cl := r.match.Context[r.line]
	selected := s.selectedFile == r.match.File && s.selectedLine == cl.Num
	NextAccessName("result_line")
	NextAccessValue(fmt.Sprintf("%s:%d", r.match.File.RelPath, cl.Num))
	NextAccessChecked(selected)
	Container(Attrs(Row, CrossMid, Expand, FixHeight(lineH), Pad2(0, hPad), Gap(10), Clip, NoAnimate, UseSurface(SurfacePanel), AmendTextStyle(FontSize(monoSz), Fonts(Monospace...))), func() {
		AssignAccess()
		ModAttrs(UnsetMaxCross)
		if selected {
			color := CurrentColorScheme.List.Selected.Background
			color[3] = 0.3
			ModAttrs(BackgroundVec(color))
		} else if IsHovered() {
			ModAttrs(BackgroundVec(CurrentColorScheme.Table.Hovered))
		}
		if PressAction() {
			s.selectedFile, s.selectedLine = r.match.File, cl.Num
		}
		ink := CurrentColorScheme.List.Muted
		background, matchInk := Vec4{42, 90, 78, 1}, Vec4{35, 70, 15, 1}
		if CurrentColorScheme.Surfaces.Panel.Background[2] < 50 {
			background, matchInk = Vec4{40, 72, 60, 1}, Vec4{35, 60, 9, 1}
		}
		if len(cl.Highlights) > 0 {
			ink = Vec4{40, 70, 45, 1}
			if CurrentColorScheme.Surfaces.Panel.Background[2] < 50 {
				ink[2] = 70
			}
		}
		Container(Attrs(FixWidth(numColW), CrossAlign(AlignEnd)), func() { Label(fmt.Sprint(cl.Num), TextColorVec(ink)) })
		if len(cl.Highlights) == 0 {
			Label(cl.Text)
			return
		}
		spans := make([]TextSpan, 0, len(cl.Highlights))
		for _, h := range cl.Highlights {
			spans = append(spans, Span(h[0], h[1], TextBackgroundVec(background), TextColorVec(matchInk)))
		}
		Text(cl.Text, TextStyle(), spans...)
	})
}
