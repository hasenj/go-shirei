package main

import (
	"fmt"
	"math"
	"os"
	"strings"
	"time"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

func (s *AppState) rootView() {
	ModAttrs(Viewport, Expand, Background(220, 10, 96, 1))
	// findBarFocused is set while the find bar paints; use last frame's value
	// for key routing (same pattern as git_history).
	findFocused := s.findBarFocused
	s.findBarFocused = false
	s.handleFindShortcut()
	s.handleListKeys(findFocused)

	Container(Attrs(Pad(12), Gap(10), Grow(1), Expand), func() {
		// Header
		Container(Attrs(Row, CrossMid, Gap(12), Expand), func() {
			Label("shirei_tester", FontSize(18), FontWeight(WeightBold))
			pass, fail, run, total := s.countsUnsafe()
			Label(fmt.Sprintf("%d tests · %d pass · %d fail · %d running", total, pass, fail, run),
				FontSize(12), TextColor(0, 0, 45, 1))
			Filler(1)
			s.lock()
			nErr := s.errorCount()
			s.unlock()
			if nErr > 0 {
				if ButtonExt("Next fail", ButtonAttrs{}, DefaultButtonLook()) {
					s.lock()
					s.moveToError(1)
					s.unlock()
				}
			}
			s.lock()
			anyRun := s.anyRunningLocked()
			s.unlock()
			if anyRun {
				if ButtonExt("Stop", ButtonAttrs{}, DefaultButtonLook()) {
					s.stopAll()
				}
			}
			s.lock()
			scanning := s.Scanning
			s.unlock()
			// Run all only when nothing is in flight (exclusive) and catalog ready.
			if !anyRun && !scanning {
				if ButtonExt("Run all", ButtonAttrs{Accent: AccentMeadow}, DefaultButtonLook()) {
					s.startRun(runSpec{All: true})
				}
			}
		})
		s.lock()
		errMsg := s.Err
		rootLabel := s.Root
		scanning := s.Scanning
		s.unlock()
		if errMsg != "" {
			Label(errMsg, FontSize(12), TextColor(10, 70, 40, 1))
		}
		Label(rootLabel, FontSize(11), TextColor(0, 0, 55, 1))
		Label("↑↓ select · Enter re-run · ⌘/Ctrl+F find · F8 / ⇧F8 next/prev fail · drag list edge to resize",
			FontSize(10), TextColor(0, 0, 50, 1))

		// Body: list | splitter | detail
		Container(Attrs(Row, Grow(1), Expand, Clip), func() {
			s.treePanel()
			s.listSplitter()
			s.detailPanel()
		})
		if scanning {
			// Keep the loop alive while discovery finishes (go list is ~200ms).
			RequestNextFrame()
		}
	})
	// Frame timing on the core DebugPanel (DEBUG=1); panel is drawn by RunFrame.
	host := GetHost()
	produceMs := float64(host.LayoutTime) / float64(time.Millisecond)
	paintMs := float64(host.PaintTime) / float64(time.Millisecond)
	// Wall-clock Δ between produces is misleading (settle can run 2 passes in
	// ~1ms; present-skip leaves PaintTime sticky). Estimate fps from last
	// produce+paint work instead.
	workMs := produceMs + paintMs
	var fps float64
	if workMs > 0.01 {
		fps = 1000 / workMs
	}
	DebugMessage(fmt.Sprintf("f=%d", ActiveUI().FrameNumber))
	DebugMessage(fmt.Sprintf("produce=%.1fms", produceMs))
	DebugMessage(fmt.Sprintf("paint=%.1fms", paintMs))
	DebugMessage(fmt.Sprintf("work=%.1fms", workMs))
	DebugMessage(fmt.Sprintf("~%.0ffps", fps))
	ProfileButton("shirei_tester") // floating CPU profiler when DEBUG=1
}

const (
	listWidthDefault float32 = 340
	listWidthMin     float32 = 200
	listWidthMax     float32 = 560
	listSplitterW    float32 = 6
)

func (s *AppState) listWidth() float32 {
	w := s.ListWidth
	if w < listWidthMin {
		return listWidthDefault
	}
	if w > listWidthMax {
		return listWidthMax
	}
	return w
}

// listSplitter is a draggable vertical bar between the test list and detail pane.
func (s *AppState) listSplitter() {
	Container(Attrs(FixWidth(listSplitterW), Expand, Background(0, 0, 82, 1)), func() {
		if IsHovered() || IsActive() {
			ModAttrs(Background(210, 55, 55, 1))
		}
		PressAction()
		if IsActive() {
			w := s.listWidth() + GetFrameInput().Motion[0]
			if w < listWidthMin {
				w = listWidthMin
			}
			if w > listWidthMax {
				w = listWidthMax
			}
			s.ListWidth = w
		}
	})
}

// handleFindShortcut opens / re-focuses the list find bar (⌘/Ctrl+F).
func (s *AppState) handleFindShortcut() {
	in := GetFrameInput()
	if in.Key != KeyF {
		return
	}
	if GetInputState().Modifiers != PrimaryMod() {
		return
	}
	s.findOpen = true
	s.findFocusReq = true
	in.Key = 0
}

// handleListKeys moves selection with arrow keys (IDE-style) and F8 next/prev fail.
// When the find bar had focus last frame, arrows/enter stay with the find field.
func (s *AppState) handleListKeys(findFocused bool) {
	if findFocused {
		return
	}
	in := GetFrameInput()
	mods := GetInputState().Modifiers
	// Don't steal keys while a modifier chord is held (find is handled separately).
	if mods&(ModCmd|ModCtrl|ModAlt) != 0 {
		return
	}
	switch in.Key {
	case KeyUp:
		s.lock()
		s.moveSel(-1)
		s.unlock()
		in.Key = 0
	case KeyDown:
		s.lock()
		s.moveSel(1)
		s.unlock()
		in.Key = 0
	case KeyF8:
		// F8 = next fail; Shift+F8 = previous fail (JetBrains-style).
		s.lock()
		if mods&ModShift != 0 {
			s.moveToError(-1)
		} else {
			s.moveToError(1)
		}
		s.unlock()
		in.Key = 0
	case KeyEnter:
		// Re-run selected test when it is not already covered by an active run.
		s.lock()
		t := s.selectedTest()
		busy := t != nil && s.testRunCoveredLocked(t.PkgDir, t.Name)
		s.unlock()
		if t != nil && !busy {
			s.startRun(runSpec{PkgDir: t.PkgDir, Test: t.Name})
		}
		in.Key = 0
	}
}

// countsUnsafe must be called on the UI thread; state is only mutated under
// mu from background, but we snapshot under lock.
func (s *AppState) countsUnsafe() (pass, fail, run, total int) {
	s.lock()
	defer s.unlock()
	return s.counts()
}

func (s *AppState) treePanel() {
	// Fixed width (user-resized via listSplitter). Do not Grow on the row —
	// that made list width depend on detail content when selection changed.
	// Window chrome hue (same as rootView) for the find strip so it reads as
	// app chrome, not a second floating card inside the list panel.
	const chromeH, chromeS, chromeL, chromeA float32 = 220, 10, 96, 1

	Container(Attrs(FixWidth(s.listWidth()), Expand, Clip,
		Background(0, 0, 100, 1), BorderWidth(1), BorderColor(0, 0, 82, 1), Corners(8)), func() {
		// Find strip sits above "Tests", flush to the card top, chrome-colored.
		if s.findOpen {
			Container(Attrs(Expand, Pad2(6, 8), Gap(0),
				Background(chromeH, chromeS, chromeL, chromeA)), func() {
				s.listFindBar()
			})
		}

		Container(Attrs(Pad(8), Gap(6), Grow(1), Expand), func() {
			Container(Attrs(Row, CrossMid, Expand, Gap(6)), func() {
				Label("Tests", FontSize(14), FontWeight(WeightBold))
				Filler(1)
				if !s.findOpen {
					if CtrlButton(SymSearch, "", true) {
						s.findOpen = true
						s.findFocusReq = true
					}
				}
			})
			s.lock()
			scanning := s.Scanning
			s.unlock()
			if scanning {
				Label("Discovering packages…", FontSize(12), FontStyle(StyleItalic), TextColor(0, 0, 50, 1))
				return
			}
			// Snapshot under the mutex once per frame — runners mutate TestItem concurrently.
			pkgs, selPkg, selTest, wantScroll := s.snapshotTree()
			if len(pkgs) == 0 {
				Label("No snapshot packages found.", FontSize(12), TextColor(0, 0, 50, 1))
				return
			}

			// Find match set for row highlighting (package headers + named tests).
			s.lock()
			var matchHits []findHit
			scrollHit := s.findScrollHit
			if s.findOpen && strings.TrimSpace(s.findQuery) != "" {
				matchHits = s.findMatchLocs()
			}
			s.unlock()
			pkgMatch := map[int]bool{}
			testMatch := map[[2]int]bool{}
			for _, h := range matchHits {
				if h.isPkg() {
					pkgMatch[h.p] = true
				} else {
					testMatch[[2]int{h.p, h.t}] = true
				}
			}

			rows := flattenTreeRows(pkgs)
			if wantScroll {
				pi, ti := selPkg, selTest
				if scrollHit.p >= 0 {
					pi = scrollHit.p
					if scrollHit.isPkg() {
						ti = -1
					} else {
						ti = scrollHit.t
					}
					// Consume so a later keyboard selection scrolls to the
					// selection, not a stale find hit.
					s.lock()
					s.findScrollHit = findHit{-1, -1}
					s.unlock()
				}
				if idx := flatTreeIndex(pkgs, pi, ti); idx >= 0 {
					VirtualListView_ScrollToIndex(&s.treeListKey, idx)
				}
			}

			const (
				treePkgRowH  float32 = 30
				treeTestRowH float32 = 28
			)
			// Grow so the Viewport inside VirtualListView gets a real height budget.
			Container(Attrs(Grow(1), Expand, Clip), func() {
				VirtualListView(&s.treeListKey, len(rows),
					func(i int) any {
						r := rows[i]
						if r.pkg {
							return treeRowKey{Dir: pkgs[r.pi].Dir, Test: ""}
						}
						return treeRowKey{Dir: pkgs[r.pi].Dir, Test: pkgs[r.pi].Tests[r.ti].Name}
					},
					func(i int, _ float32) float32 {
						if rows[i].pkg {
							return treePkgRowH
						}
						return treeTestRowH
					},
					func(i int, w float32) {
						r := rows[i]
						pkg := pkgs[r.pi]
						if r.pkg {
							pkgBg := Vec4{}
							if pkgMatch[r.pi] {
								pkgBg = findMatchRowBG
							}
							Container(Attrs(Row, CrossMid, Gap(6), Expand, FixHeight(treePkgRowH), MaxWidth(w),
								Pad2(2, 4), BackgroundVec(pkgBg), Corners(4)), func() {
								Label(pkg.Label, FontSize(13), FontWeight(WeightBold))
								Filler(1)
								if CtrlButton(NoIcon, "Run", !pkg.BusyPkg) {
									s.startRun(runSpec{PkgDir: pkg.Dir})
								}
							})
							return
						}
						test := pkg.Tests[r.ti]
						selected := r.pi == selPkg && r.ti == selTest
						bg := testRowBG(test.Status, selected)
						if testMatch[[2]int{r.pi, r.ti}] {
							bg = blendFindMatch(bg, selected)
						}
						Container(Attrs(Row, CrossMid, Gap(6), Expand, FixHeight(treeTestRowH), MaxWidth(w), Clip,
							Pad2(4, 6), BackgroundVec(bg), Corners(4)), func() {
							st := ProcessButtonEvents(false)
							if st.Clicked {
								s.lock()
								s.SelPkg = r.pi
								s.SelTest = r.ti
								s.SelSnap = 0
								s.unlock()
							}
							if CtrlButton(NoIcon, "▶", !test.Busy) {
								s.startRun(runSpec{PkgDir: pkg.Dir, Test: test.Name})
								s.lock()
								s.SelPkg = r.pi
								s.SelTest = r.ti
								s.unlock()
							}
							statusDot(test.Status, 12)
							snapIndicatorView(test)
							Label(test.Name, FontSize(12))
						})
					},
				)
			})
		})
	})
}

// treeFlatRow is one VirtualList entry: a package header or a test row.
type treeFlatRow struct {
	pkg bool
	pi  int
	ti  int
}

type treeRowKey struct {
	Dir  string
	Test string // empty = package header
}

func flattenTreeRows(pkgs []treePkgView) []treeFlatRow {
	n := 0
	for _, p := range pkgs {
		n += 1 + len(p.Tests)
	}
	rows := make([]treeFlatRow, 0, n)
	for pi, pkg := range pkgs {
		rows = append(rows, treeFlatRow{pkg: true, pi: pi})
		for ti := range pkg.Tests {
			rows = append(rows, treeFlatRow{pkg: false, pi: pi, ti: ti})
		}
	}
	return rows
}

// flatTreeIndex returns the VirtualList index for package pi (ti < 0) or test (pi, ti).
func flatTreeIndex(pkgs []treePkgView, pi, ti int) int {
	if pi < 0 || pi >= len(pkgs) {
		return -1
	}
	idx := 0
	for i := 0; i < pi; i++ {
		idx += 1 + len(pkgs[i].Tests)
	}
	if ti < 0 {
		return idx
	}
	if ti >= len(pkgs[pi].Tests) {
		return -1
	}
	return idx + 1 + ti
}

// findMatchRowBG is a soft amber wash for package/test rows that match find.
var findMatchRowBG = Vec4{48, 55, 92, 0.92}

// blendFindMatch layers the find highlight under selection/status washes.
func blendFindMatch(base Vec4, selected bool) Vec4 {
	// Prefer a clear find tint; selection already deepens status colors.
	hl := findMatchRowBG
	if selected {
		hl[2] = 88
		hl[3] = 1
	}
	if base[3] < 0.01 {
		return hl
	}
	// Status wash present: nudge toward amber while keeping pass/fail hue.
	base[1] = min(base[1]+15, 70)
	base[2] = min(base[2]+4, 96)
	base[3] = max(base[3], 0.85)
	return base
}

// listFindBar is the chrome-colored strip above "Tests" (⌘/Ctrl+F).
func (s *AppState) listFindBar() {
	focused := false
	Container(Attrs(Row, Expand, CrossMid, Gap(4)), func() {
		// Compact field fills leftover width; no Clip so inset border isn't trimmed.
		Container(Attrs(Grow(1), Expand, Extrinsic), func() {
			sz := GetAvailableSize()
			if sz[0] < 1 || sz[1] < 1 {
				RequestNextFrame()
				return
			}
			attrs := CtrlTextInputAttrs()
			attrs.MinWidth = sz[0]
			attrs.MaxWidth = sz[0]
			attrs.FixedWidth = true
			attrs.NoAutoFocus = true
			attrs.Placeholder = "Package or test…"
			TextInputExt(&s.findQuery, attrs)
			if s.findFocusReq {
				FocusImmediateOn(GetLastId())
				s.findFocusReq = false
			}
			if HasFocusWithin() {
				focused = true
				s.findBarFocused = true
			}
		})

		s.lock()
		if s.findQuery != s.findQueryApplied {
			s.findApplyQueryLocked()
		}
		matches := s.findMatchLocs()
		matchIdx := s.findMatchIdx
		s.unlock()

		if s.findQuery != "" {
			if listFindClearButton() {
				s.findQuery = ""
				s.lock()
				s.findQueryApplied = ""
				s.findMatchIdx = 0
				s.findScrollHit = findHit{-1, -1}
				s.unlock()
			} else {
				n := len(matches)
				note := "0"
				if n > 0 {
					note = fmt.Sprintf("%d/%d", matchIdx+1, n)
				}
				Label(note, FontSize(11), TextColor(0, 0, 45, 1))
			}
		}
		n := len(matches)
		canNav := n > 0
		if CtrlButton(SymArrowUp, "", canNav) {
			s.findStep(-1)
		}
		if CtrlButton(SymArrowDown, "", canNav) {
			s.findStep(+1)
		}

		if focused {
			switch GetFrameInput().Key {
			case KeyEnter:
				if GetInputState().Modifiers&ModShift != 0 {
					s.findStep(-1)
				} else {
					s.findStep(+1)
				}
				GetFrameInput().Key = 0
			case KeyEscape:
				// Dismiss; keep query so reopen resumes the same search.
				s.findOpen = false
				s.findFocusReq = false
				ClearFocus()
				GetFrameInput().Key = 0
			}
		}
	})
}

// listFindClearButton is a compact × control for the list find bar.
func listFindClearButton() bool {
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

// testRowBG is a soft full-row wash for pass/fail/run; selection deepens it.
func testRowBG(st testStatus, selected bool) Vec4 {
	var bg Vec4
	switch st {
	case statusPass:
		bg = Vec4{140, 45, 94, 0.72} // soft green
	case statusFail:
		bg = Vec4{8, 55, 94, 0.72} // soft red
	case statusRunning:
		bg = Vec4{40, 50, 94, 0.7} // soft amber
	case statusPending:
		bg = Vec4{0, 0, 96, 0.95} // faint wait wash
	case statusSkip:
		bg = Vec4{0, 0, 94, 0.9}
	default:
		bg = Vec4{0, 0, 100, 1}
	}
	if selected {
		// Cool selection wash on top of status tint.
		switch st {
		case statusPass, statusFail, statusRunning, statusPending:
			bg[1] = minf32(bg[1]+12, 80)
			bg[2] = maxf32(bg[2]-6, 88)
		default:
			bg = Vec4{220, 25, 94, 1}
		}
	}
	return bg
}

func minf32(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}
func maxf32(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}

// statusDot is a colored circle ~80% of textSize, vertically centered by the row.
// Pass/fail solid; running pulses gold; pending (queued in a live run) pulses gray;
// unknown (never targeted) is a quiet static dim dot.
func statusDot(st testStatus, textSize float32) {
	if textSize <= 0 {
		textSize = 12
	}
	d := textSize * 0.8
	var h, s, l, a float32
	switch st {
	case statusPass:
		h, s, l, a = 140, 60, 42, 1 // green
	case statusFail:
		h, s, l, a = 8, 70, 48, 1 // red
	case statusRunning:
		h, s, l = 42, 85, 52 // gold / amber
		a = statusPulse(0.35, 1.0, 900*time.Millisecond)
		RequestNextFrame()
	case statusPending:
		// Queued in an active run (not idle).
		h, s, l = 0, 0, 62
		a = statusPulse(0.25, 0.75, 1100*time.Millisecond)
		RequestNextFrame()
	case statusSkip:
		h, s, l, a = 0, 0, 55, 0.85 // muted gray
	default: // unknown — never run
		h, s, l, a = 0, 0, 62, 0.4
	}
	Element(Attrs(
		FixSize(d, d),
		Corners(d/2),
		Background(h, s, l, a),
		NoAnimate,
	))
}

// statusPulse returns alpha oscillating between lo and hi (gentle sine).
func statusPulse(lo, hi float32, period time.Duration) float32 {
	if period <= 0 {
		period = time.Second
	}
	t := float64(time.Now().UnixNano()) / float64(period)
	// 0..1 sine
	u := 0.5 + 0.5*math.Sin(t*2*math.Pi)
	return lo + float32(u)*(hi-lo)
}

// statusLabel is used in the detail header (same dots as the list).
func statusLabel(st testStatus) {
	statusDot(st, 12)
}

// snapIndicatorView: fixed-width slot so columns never jump between idle / run / done.
// Image icon when the harness reported ≥1 snapshot; empty otherwise.
func snapIndicatorView(test treeTestView) {
	const slot float32 = 16
	Container(Attrs(FixSize(slot, slot), Center, NoAnimate), func() {
		if !test.SawReport {
			return
		}
		clr := Vec4{140, 50, 35, 1}
		if test.HasMismatch {
			clr = Vec4{10, 70, 45, 1}
		}
		Icon(SymImage, FontSize(13), TextColorVec(clr))
	})
}

func (s *AppState) detailPanel() {
	// Extrinsic: pane width/height from the row flex budget, not from wide
	// snapshot content (see notes/layout-extrinsic-clip.md).
	Container(Attrs(Grow(1), Expand, Extrinsic, Clip, Gap(8),
		Background(0, 0, 100, 1), BorderWidth(1), BorderColor(0, 0, 82, 1), Corners(8), Pad(10)), func() {
		tv, ok, testBusy, selSnap, _ := s.snapshotDetail()
		if !ok {
			Label("Select a test on the left. Run all, a package, or a single test.",
				FontSize(13), TextColor(0, 0, 45, 1))
			return
		}

		Container(Attrs(Row, CrossMid, Gap(8), Expand), func() {
			Label(tv.Name, FontSize(16), FontWeight(WeightBold))
			statusLabel(tv.Status)
			Label(tv.Status.Label(), FontSize(12), TextColor(0, 0, 45, 1))
			Filler(1)
			if ButtonExt("Re-run", ButtonAttrs{Accent: AccentMeadow, Disabled: testBusy}, DefaultButtonLook()) {
				s.startRun(runSpec{PkgDir: tv.PkgDir, Test: tv.Name})
			}
		})
		Label(tv.PkgDir, FontSize(11), TextColor(0, 0, 50, 1))

		if len(tv.Snaps) > 0 {
			Label("Snapshots", FontSize(13), FontWeight(WeightBold))
			Container(Attrs(Row, Gap(6), Wrap), func() {
				for i, sn := range tv.Snaps {
					i, sn := i, sn
					lab := sn.Name + " (" + sn.Status + ")"
					accent := Vec4{}
					if sn.Status == "mismatch" {
						accent = Vec4{10, 70, 45, 1}
					}
					if ButtonExt(lab, ButtonAttrs{Accent: accent}, DefaultButtonLook()) {
						s.lock()
						s.SelSnap = i
						s.unlock()
					}
				}
			})
		} else if tv.Status == statusPass || tv.Status == statusFail {
			Label("No snapshot report lines (test may not use snap harness, or still running).",
				FontSize(11), TextColor(0, 0, 50, 1))
		}

		// Diff view — operate on a value copy from the snapshot.
		var snap *SnapResult
		if selSnap >= 0 && selSnap < len(tv.Snaps) {
			snap = &tv.Snaps[selSnap]
		} else if len(tv.Snaps) > 0 {
			for i := range tv.Snaps {
				if tv.Snaps[i].Status == "mismatch" {
					snap = &tv.Snaps[i]
					break
				}
			}
			if snap == nil {
				snap = &tv.Snaps[0]
			}
		}

		if snap != nil {
			s.snapViewer(tv.PkgDir, tv.Name, *snap)
		}

		if tv.Output != "" {
			Label("go test output", FontSize(12), FontWeight(WeightBold))
			Container(Attrs(Viewport, FixSize(0, 120), Expand,
				Background(0, 0, 97, 1), Corners(4), Pad(6)), func() {
				ScrollOnInput()
				const chunk = 2000
				out := tv.Output
				if len(out) > chunk {
					out = out[len(out)-chunk:]
				}
				Label(out, FontSize(11))
				ScrollBars()
			})
		}
	})
}

// snapWipeHLOn: purple tint on differing pixels when highlight is enabled.
// Full-resolution pixel compare — off by default (see ShowWipeDiffHL).
var snapWipeHLOn = Vec4{280, 50, 48, 0.5}

func (s *AppState) snapViewer(pkgDir, testName string, snap SnapResult) {
	both := s.pathExists(snap.Golden) && s.pathExists(snap.Actual)

	// Pane width from the Extrinsic detail panel (current container). Capture
	// before nesting into content-sized chrome that a wide image could inflate.
	paneW := GetAvailableSize()[0]

	Container(Attrs(Gap(6), Expand, Clip), func() {
		Container(Attrs(Row, CrossMid, Gap(8), Expand, Clip), func() {
			Label(snap.Name+" · "+snap.Status, FontSize(13), FontWeight(WeightBold))
			Filler(1)
			if both {
				CheckBox(&s.ShowWipeDiffHL, "Highlight diffs")
			}
			if snap.Status == "mismatch" && snap.Actual != "" && snap.Golden != "" {
				if ButtonExt("Accept actual → golden", ButtonAttrs{Accent: AccentMeadow}, DefaultButtonLook()) {
					s.acceptSnap(pkgDir, testName, snap.Name, snap.Golden, snap.Actual)
				}
			}
			if snap.Golden != "" {
				if CtrlButton(SymFolder, "Golden", true) {
					_ = revealPath(snap.Golden)
				}
			}
			if snap.Actual != "" {
				if CtrlButton(SymFolder, "Actual", true) {
					_ = revealPath(snap.Actual)
				}
			}
		})
		Label(snap.Golden, FontSize(10), TextColor(0, 0, 50, 1))
		if snap.Actual != "" {
			Label(snap.Actual, FontSize(10), TextColor(10, 50, 40, 1))
		}

		if both {
			s.snapWipe(snap.Golden, snap.Actual, paneW)
		} else if s.pathExists(snap.Golden) {
			s.imageCard("Golden", snap.Golden)
		} else if s.pathExists(snap.Actual) {
			s.imageCard("Actual", snap.Actual)
		} else {
			Label("(no images)", FontSize(11), TextColor(0, 0, 55, 1))
		}
	})
}

// snapWipe: left = actual (new), right = golden (old); drag to compare.
// Display = image pixels, scaled down only if wider than maxW (detail pane).
func (s *AppState) snapWipe(golden, actual string, maxW float32) {
	type wipePos struct {
		T    float32
		Init bool
	}
	st := Use[wipePos](golden + "\x00" + actual)
	if !st.Init {
		st.T = 0.5
		st.Init = true
	}

	LoadImage(actual)
	LoadImage(golden)
	leftId := GetImageId(actual)
	rightId := GetImageId(golden)

	hl := Vec4{} // alpha 0 = off
	if s.ShowWipeDiffHL {
		hl = snapWipeHLOn
	}
	// Size width = pane budget; height 0 → aspect only (no height cap).
	ImageWipe(ImageWipeAttrs{
		LeftImage:          leftId,
		RightImage:         rightId,
		OutSlider:          &st.T,
		LeftAccentColor:    ImageWipeLeftAccent,  // green = actual
		RightAccentColor:   ImageWipeRightAccent, // red = golden
		OutlineThickness:   6,
		LeftLabel:          "",
		RightLabel:         "",
		DiffHighlightColor: hl,
		MaxSize:            Vec2{maxW, 0},
	})
}

func (s *AppState) imageCard(title, path string) {
	Container(Attrs(Gap(4), MinWidth(200)), func() {
		Label(title, FontSize(11), FontWeight(WeightBold))
		if path == "" {
			Label("(none)", FontSize(11), TextColor(0, 0, 55, 1))
			return
		}
		if !s.pathExists(path) {
			Label("missing", FontSize(11), TextColor(10, 60, 40, 1))
			return
		}
		Container(Attrs(FixSize(280, 200), Clip, Background(0, 0, 92, 1), Corners(4)), func() {
			Image(path, Vec2{280, 200})
		})
	})
}

func revealPath(path string) error {
	// Reuse open semantics: prefer select file if exists
	if st, err := os.Stat(path); err == nil && st.IsDir() {
		return openDir(path)
	}
	return revealFile(path)
}
