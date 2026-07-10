package main

import (
	"fmt"
	"slices"
	"time"

	g "go.hasen.dev/generic"

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
}

var appData = new(App)

// Row metrics. The virtual list needs an exact height per row, so every piece
// of a row renders at a fixed size and rowHeight sums the same numbers.
const (
	headerH     = 26
	lineH       = 18
	contentPadV = 6
	rowGap      = 8
	hPad        = 10
	numColW     = 52
	monoSz      = 13
)

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

	Container(Attrs(Viewport, Background(0, 0, 96, 1)), func() {
		TopPanel()
		if len(appData.searches) > 0 {
			TabBar()
		}
		Container(Attrs(Grow(1), Expand, Clip), func() {
			ResultsList(appData.active)
		})
		ProfileButton("haystack") // floating profiler toggle when DEBUG=1
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

// activateTab makes s the shown tab: it restores s's saved scroll position (the
// list was hidden and rebuilt, so we command it back into place) and reloads
// its search terms into the inputs. The outgoing tab's offset is already saved
// because ResultsList mirrors the active tab's offset every frame.
func activateTab(s *Search) {
	appData.active = s
	VirtualListView_ScrollTo(s, s.scrollY)
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

func TopPanel() {
	// input ids captured below; Enter in any of them runs the search
	var searchId, includeId, excludeId ContainerId

	Container(Attrs(Expand, Spacing(10), Background(220, 14, 90, 1), BorderColor(0, 0, 75, 1), BorderWidth(1)), func() {
		// Folder row
		Container(Attrs(Row, CrossMid, Gap(10), Expand), func() {
			LeadLabel("Folder")
			Container(Attrs(Grow(1)), func() {
				DirectoryBrowse(&appData.pathInput)
			})
		})

		// Search row: term, trigger, toggles
		Container(Attrs(Row, CrossMid, Gap(10), Expand), func() {
			LeadLabel("Search")
			Icon(SymSearch, TextColor(0, 0, 40, 1))
			TextInput(&appData.query)
			searchId = GetLastId()
			if !appData.startupFocused {
				// Prefer the search box over the auto-focused folder field.
				FocusImmediateOn(searchId)
				appData.startupFocused = true
			}
			if ButtonExt("Search", ButtonAttrs{Icon: SymSearch}) {
				runNewSearch(currentParams())
			}

			Spacer(10)
			CheckBox(&appData.matchCase, "Match case")
			CheckBox(&appData.wholeWord, "Whole word")
			CheckBox(&appData.regex, "Regex")
			Filler(1)
		})

		// Filter row: which files to search
		Container(Attrs(Row, CrossMid, Gap(8), Expand), func() {
			LeadLabel("Filter")
			Label("include", FontSize(12), TextColor(0, 0, 45, 1))
			filterInput(&appData.include)
			includeId = GetLastId()
			Label("exclude", FontSize(12), TextColor(0, 0, 45, 1))
			filterInput(&appData.exclude)
			excludeId = GetLastId()
			Spacer(12)
			CheckBox(&appData.gitignore, "Respect .gitignore")
			Filler(1)
		})
	})

	// Searching is explicit: Enter in any search field, or the Search button.
	// Each run opens a new tab, so editing never churns the results under you
	// and prior searches stay around to return to.
	if FrameInput.Key == KeyEnter &&
		(IdHasFocus(searchId) || IdHasFocus(includeId) || IdHasFocus(excludeId)) {
		runNewSearch(currentParams())
	}
}

// filterInput is a non-auto-focusing text field for the glob filters (the
// search box owns startup focus).
func filterInput(buf *string) {
	attrs := DefaultTextInputAttrs()
	attrs.NoAutoFocus = true
	TextInputExt(buf, attrs)
}

// TabBar is the row of search tabs plus the active search's live status. Each
// tab shows its query and match count; click to switch (which also reloads its
// terms into the inputs), or hit the × to close it.
func TabBar() {
	// A tab's × closes it, which removes it from appData.searches. Doing that
	// while ranging over the slice would hand a freed/nil element to the next
	// SearchTab (crash), so collect the request and apply it after the loop.
	var closeReq *Search
	// Extrinsic + Clip makes the strip take the window width and clip overflow
	// (rather than stretching to fit every tab); the inner row sizes to its
	// content and scrolls horizontally when there are more tabs than fit.
	Container(Attrs(Row, Extrinsic, Clip, Expand, FixHeight(44), Pad2(6, 10), Background(220, 12, 84, 1), BorderColor(0, 0, 75, 1), BorderWidth(1)), func() {
		ScrollOnInput()
		ScrollBars()
		Container(Attrs(Row, CrossMid, Gap(6)), func() {
			for _, s := range appData.searches {
				if SearchTab(s) {
					closeReq = s
				}
			}
		})
	})
	if closeReq != nil {
		closeTab(closeReq)
	}
	Container(Attrs(Row, CrossMid, Expand, Pad2(4, 12), Background(220, 14, 90, 1)), func() {
		StatusLine(appData.active)
	})
}

// SearchTab renders one tab and reports whether its × was pressed this frame
// (the caller closes it after the tab loop, not mid-iteration).
func SearchTab(s *Search) (closeClicked bool) {
	active := s == appData.active
	ContainerWithKey(s, Attrs(Row, CrossMid, Gap(6), Pad2(4, 8), Corners(5), MinHeight(24), MaxWidth(220), Background(220, 10, 92, 1)), func() {
		if !active && IsHovered() {
			ModAttrs(Background(220, 14, 95, 1))
		}
		if active {
			ModAttrs(Background(0, 0, 100, 1), BorderColor(210, 40, 55, 1), BorderWidth(1))
		}
		if PressAction() {
			activateTab(s)
		}

		// Cap the query width so a long term can't stretch the tab off-screen.
		Container(Attrs(MaxWidth(150), Clip), func() {
			Label(s.params.Query, FontWeight(WeightBold), TextColor(0, 0, 20, 1))
		})

		switch {
		case s.err != nil:
			Label("!", FontWeight(WeightBold), TextColor(0, 70, 45, 1))
		case s.running:
			Label("…", TextColor(0, 0, 45, 1))
		default:
			Label(fmt.Sprintf("(%d)", s.matchCount.Load()), TextColor(0, 0, 45, 1))
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

// LeadLabel renders one of the two leading field labels in a fixed-width box
// so the Folder and Search fields line up.
func LeadLabel(text string) {
	Container(Attrs(FixWidth(52)), func() {
		Label(text, TextColor(0, 0, 30, 1))
	})
}

func StatusLine(s *Search) {
	Container(Attrs(Row, CrossMid, Gap(8), Expand), func() {
		if s == nil || s.params.Query == "" {
			Label("Type a search term…", FontStyle(StyleItalic), TextColor(0, 0, 45, 1))
			return
		}
		if s.err != nil {
			Icon(TypTimes, TextColor(0, 70, 45, 1))
			Label("Invalid pattern: "+s.err.Error(), TextColor(0, 70, 40, 1))
			return
		}

		var elapsed time.Duration
		if s.running {
			elapsed = time.Since(s.started)
			Icon(SymClock, TextColor(0, 0, 40, 1))
			RequestNextFrame() // keep the timer and streamed results flowing
		} else {
			elapsed = s.done.Sub(s.started)
			Icon(SymPass, TextColor(140, 45, 40, 1))
		}

		Label(fmt.Sprintf("%d matches in %d files", s.matchCount.Load(), s.filesMatched.Load()), FontWeight(WeightBold))
		Label(fmt.Sprintf("· %d scanned", s.filesScanned.Load()), TextColor(0, 0, 45, 1))
		Label(fmt.Sprintf("· %.2fs", elapsed.Seconds()), TextColor(0, 0, 45, 1))
	})
}

func ResultsList(s *Search) {
	ContainerWithKey(s, Attrs(Viewport, Background(0, 0, 93, 1)), func() {
		if s == nil {
			Container(Attrs(Expand, Center, Pad(30)), func() {
				Label("Type a search term and press Enter", FontStyle(StyleItalic), TextColor(0, 0, 50, 1))
			})
			return
		}
		if s.params.Query == "" || s.err != nil {
			return
		}

		matches := s.matches // safe snapshot: workers publish under the frame lock
		if len(matches) == 0 {
			Container(Attrs(Expand, Center, Pad(30)), func() {
				if s.running {
					Label("Searching…", TextColor(0, 0, 50, 1))
				} else {
					Label("No matches", TextColor(0, 0, 50, 1))
				}
			})
			return
		}

		// Mirror this (active) tab's scroll offset every frame, so it's saved by
		// the time the tab is hidden — a hidden list can't answer the request.
		idFn := func(i int) any { return matches[i] }
		heightFn := func(i int, width f32) f32 { return rowHeight(matches[i]) }
		viewFn := func(i int, width f32) { MatchRow(matches[i]) }
		// Key the list by the search so each tab keeps its own scroll position
		// and switching tabs never carries one tab's offset onto another's
		// (shorter) results.
		VirtualListViewExt(s, VirtualListAttrs{
			ItemCount:       len(matches),
			ItemKey:         idFn,
			ItemHeight:      heightFn,
			ItemView:        viewFn,
			OutScrollOffset: &s.scrollY,
		})
	})
}

func rowHeight(m *Match) f32 {
	return headerH + contentPadV*2 + f32(len(m.Context))*lineH + rowGap
}

func MatchRow(m *Match) {
	fr := m.File
	// Outer slot is exactly rowHeight tall; the card sits at the top and the
	// rowGap shows through below it as list background.
	Container(Attrs(Expand, FixHeight(rowHeight(m)), NoAnimate), func() {
		Container(Attrs(Expand, Clip, Corners(4), BorderColor(0, 0, 78, 1), BorderWidth(1)), func() {
			// Header strip: cool slate background, path:line, and controls.
			Container(Attrs(Row, CrossMid, Expand, FixHeight(headerH), Pad2(0, hPad), Gap(8), Background(214, 20, 88, 1)), func() {
				Icon(TypDocument, TextColor(220, 16, 42, 1))
				Label(fmt.Sprintf("%s:%d", fr.RelPath, m.Line), FontWeight(WeightBold), TextColor(220, 22, 28, 1))
				Filler(1)
				if CtrlButton(SymCopy, "Copy", true) {
					RequestTextCopy(fr.Path)
				}
				for _, ed := range appData.editors {
					if CtrlButton(SymCode, ed.Name, true) {
						ed.Open(fr.Path, m.Line)
					}
				}
			})

			// Content: the context lines, monospaced.
			Container(Attrs(Expand, Clip, Pad2(contentPadV, hPad), Background(0, 0, 100, 1)), func() {
				for _, cl := range m.Context {
					Container(Attrs(Row, CrossMid, Expand, FixHeight(lineH), Gap(8)), func() {
						if cl.IsMatch {
							ModAttrs(Background(48, 90, 90, 1)) // soft yellow on the matching line
						}
						Container(Attrs(FixWidth(numColW), Row, MainAlign(AlignEnd)), func() {
							Label(fmt.Sprintf("%d", cl.Num), TextColor(0, 0, 58, 1), FontSize(monoSz), Fonts(Monospace...))
						})
						Label(cl.Text, TextColor(0, 0, 12, 1), FontSize(monoSz), Fonts(Monospace...))
					})
				}
			})
		})
	})
}
