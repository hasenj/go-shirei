package main

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/cli/browser"
	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

func activateScanner(s *Scanner) {
	appData.activeScanner = s
	VirtualListView_ScrollToIndex(s, s.firstVis)
}

func TabBar() {
	var closeReq *Scanner
	TabStrip(func() {
		for _, s := range appData.scanners {
			NextAccessName("scan_tab")
			NextAccessValue(s.rootPath)
			name := s.rootPath
			if s.root != nil {
				name = s.root.Name
			}
			if TabItem(s, name, s == appData.activeScanner, func() {
				if s.state == Running {
					Label("…", TextColorVec(CurrentColorScheme.List.Muted))
				}
				NextAccessName("close_tab")
				NextAccessValue(s.rootPath)
				if TabCloseButton() {
					closeReq = s
				}
			}) {
				activateScanner(s)
			}
		}
	}, func() {
		NextAccessName("new_scan")
		if ButtonExt("New scan", ButtonAttrs{Icon: TypPlus}, ButtonLook{TextSize: 11, PadScale: .8}) {
			openNewScanModal()
		}
	})
	if closeReq != nil {
		closeScanner(closeReq)
	}
}

func ScanResultPanel() {
	Container(Attrs(Viewport, UseSurface(SurfaceCanvas)), func() {
		TabBar()
		NewScanModal()
		s := appData.activeScanner
		if s == nil {
			EmptyScansView()
			return
		}
		ContainerWithKey(s, Attrs(Viewport, NoAnimate, UseSurface(SurfacePanel)), func() {
			ScanToolbar(s)
			entries := make([]*ScanEntry, 0, 256)
			ListupViewableEntries(s, s.root, &entries, false)
			flat := s.filter != ""
			if flat {
				slices.SortStableFunc(entries, func(a, b *ScanEntry) int { return cmp.Compare(b.Size, a.Size) })
			}
			total := flatListTotal(entries)
			if len(entries) == 0 {
				Container(Attrs(Viewport, Center, Gap(8)), func() {
					NextAccessName("empty_results")
					AssignAccess()
					if s.state == Running {
						Label("Measuring this folder…")
					} else if s.err != nil {
						Label("Unable to read this folder", FontSize(16), FontWeight(WeightSemibold))
						Label("Choose another folder, or view the scan details below.", TextColorVec(CurrentColorScheme.List.Muted))
					} else {
						Label("No entries to show", FontSize(16), FontWeight(WeightSemibold))
						Label("Try a smaller minimum size or another filter.", TextColorVec(CurrentColorScheme.List.Muted))
					}
				})
			} else {
				VirtualListViewExt(s, VirtualListAttrs{
					ItemCount:       len(entries),
					ItemKey:         func(i int) any { return entries[i] },
					ItemHeight:      func(i int, width f32) f32 { return 52 },
					ItemView:        func(i int, width f32) { SizeTreeRow(s, entries[i], flat, total, width) },
					OutFirstVisible: &s.firstVis,
				})
			}
			SelectionStrip(s)
			ScanStatus(s, len(entries), flat)
			if s.showReadErrors {
				Modal(min(620, GetHost().WindowSize[0]-32), func() { s.showReadErrors = false }, func() {
					NextAccessName("scan_details")
					AssignAccess()
					Label("Skipped folders", FontSize(16), FontWeight(WeightSemibold))
					Label("These folders could not be read. Their contents are excluded from the totals.", TextColorVec(CurrentColorScheme.List.Muted))
					Container(Attrs(Expand, MaxHeight(260), Clip, Gap(12), Pad(10), UseSurface(SurfaceCanvas)), func() {
						ScrollOnInput()
						ScrollBars()
						for _, err := range s.readErrors {
							NextAccessName("scan_issue")
							NextAccessValue(err.Error())
							Container(Attrs(Expand), func() { AssignAccess(); Label(err.Error(), FontSize(12)) })
						}
					})
					Container(Attrs(Row, Expand, CrossMid, Gap(8)), func() {
						if CtrlButton(SymCopy, "Copy details", true) {
							lines := make([]string, len(s.readErrors))
							for i, err := range s.readErrors {
								lines[i] = err.Error()
							}
							RequestTextCopy(strings.Join(lines, "\n"))
						}
						Filler(1)
						NextAccessName("close_scan_details")
						if CtrlButton(NoIcon, "Close", true) {
							s.showReadErrors = false
						}
					})
				})
			}
		})
	})
}

func ScanToolbar(s *Scanner) {
	Container(Attrs(Expand, Gap(10), Pad2(12, 16), UseSurface(SurfacePanel)), func() {
		Container(Attrs(Row, CrossMid, Expand, Gap(10)), func() {
			Icon(TypFolderOpen, FontSize(18), TextColorVec(CurrentColorScheme.FocusRing))
			Container(Attrs(Row, CrossMid, Grow(1), Extrinsic, Expand, Clip), func() { Label(s.rootPath, FontSize(14), FontWeight(WeightSemibold)) })
			if s.root != nil {
				Label(FmtBytes(s.root.Size, s.root.Size), FontSize(16), FontWeight(WeightBold))
			}
		})
		Container(Attrs(Row, CrossMid, Expand, Gap(10)), func() {
			Icon(SymSearch, TextColorVec(CurrentColorScheme.List.Muted))
			Container(Attrs(Grow(1), Extrinsic, FixHeight(28)), func() {
				attrs := DefaultTextInputAttrs()
				attrs.NoAutoFocus, attrs.Depth = true, 0
				attrs.FontSize, attrs.Padding = 12, Vec4{8, 8, 8, 8}
				attrs.Placeholder, attrs.MinWidth = "Filter names…", 80
				NextAccessName("filter")
				before := s.filter
				TextInputExt(&s.filter, attrs)
				if before != s.filter {
					VirtualListView_ScrollToIndex(s, 0)
				}
			})
			Label("Minimum size", FontSize(12), TextColorVec(CurrentColorScheme.List.Muted))
			NextAccessName("minimum_size")
			before := s.minsize
			Slider(&s.minsize, SliderAttrs{Min: 0, Max: GB1, Step: MB10, Width: 120})
			if before != s.minsize {
				VirtualListView_ScrollToIndex(s, 0)
			}
			Container(Attrs(FixWidth(65)), func() {
				label := "All sizes"
				if s.minsize > 0 {
					label = FmtBytes(int(s.minsize), int(s.minsize))
				}
				Label(label, FontSize(11))
			})
		})
	})
	Separator()
}

// SizeTreeRow keeps each size and name at one left edge. Indentation and rails
// show ancestry; the integrated fill measures the entry's share of its parent.
func SizeTreeRow(s *Scanner, entry *ScanEntry, flat bool, total int, width f32) {
	depth := entry.Depth
	if flat {
		depth = 0
	}
	// Leave room for labels even in a very deep directory tree.
	indent := min(f32(depth)*24, max(0, width-260))
	denominator := proportionDenominator(entry, flat, total)
	var proportion f32
	if denominator > 0 {
		proportion = min(1, f32(entry.Size)/f32(denominator))
	}
	border := CurrentColorScheme.Surfaces.Panel.Border
	border[3] *= .7
	Container(Attrs(Row, CrossMid, Expand, FixHeight(52), Pad2(0, 12)), func() {
		Container(Attrs(FixWidth(indent), Expand), func() {
			for x := f32(12); x < indent; x += 24 {
				Element(Attrs(Float(x, 0), FixSize(1, 52), BackgroundVec(border)))
			}
		})
		NextAccessName("entry")
		NextAccessValue(entry.Path)
		NextAccessChecked(s.selected == entry)
		Container(Attrs(Row, CrossMid, Grow(1), FixHeight(48), Gap(8), Corners(3), Clip, BorderWidth(1), BorderColorVec(border)), func() {
			NextAccessRole("button")
			AssignAccess()
			st := ProcessButtonEvents(false)
			if st.Clicked {
				s.selected = entry
			}
			bg := CurrentColorScheme.Surfaces.Canvas.Background
			fill := CurrentColorScheme.FocusRing
			fill[3] = .12
			text := CurrentColorScheme.Surfaces.Panel.Text
			if st.Hovered {
				bg = CurrentColorScheme.List.Hovered.Background
			}
			if s.selected == entry {
				bg = CurrentColorScheme.List.Hovered.Background
				text = CurrentColorScheme.List.Hovered.Text
				fill[3] = .24
			}
			ModAttrs(BackgroundVec(bg), AmendTextStyle(TextColorVec(text)))
			size := GetResolvedSize()
			Element(Attrs(Float(0, 1), Behind, FixSize(size[0]*proportion, max(0, size[1]-2)), BackgroundVec(fill)))
			if st.FocusVisible || s.selected == entry {
				Element(Attrs(Float(0, 1), FixSize(2, 46), BackgroundVec(CurrentColorScheme.FocusRing)))
			}
			if entry.IsDir && !flat {
				NextAccessName("entry_expand")
				NextAccessValue(entry.Path)
				NextAccessChecked(entry.Expanded)
				Container(Attrs(FixWidth(32), Expand, Center), func() {
					Container(Attrs(FixSize(28, 28), Center, Corners(3)), func() {
						NextAccessRole("button")
						NextAccessLabel("Expand or collapse folder")
						AssignAccess()
						button := ProcessButtonEvents(false)
						if button.Clicked {
							entry.Expanded = !entry.Expanded
							RequestNextFrame()
						}
						if button.Hovered {
							hover := CurrentColorScheme.FocusRing
							hover[3] = .16
							ModAttrs(BackgroundVec(hover))
						}
						if button.FocusVisible {
							ModAttrs(BorderWidth(1), BorderColorVec(CurrentColorScheme.FocusRing))
						}
						icon := SymRight
						if entry.Expanded {
							icon = SymDown
						}
						Icon(icon, FontSize(15))
					})
				})
			} else {
				Spacer(32)
			}
			icon := TypDocument
			if entry.IsDir {
				icon = TypFolder
			}
			Icon(icon, FontSize(16), TextColorVec(CurrentColorScheme.List.Muted))
			Container(Attrs(Grow(1), Extrinsic, FixHeight(36), Clip, Gap(2)), func() {
				Container(Attrs(Row, CrossMid, Gap(8)), func() {
					Label(FmtBytes(entry.Size, entry.Size), FontSize(14), FontWeight(WeightBold))
					Label(fmt.Sprintf("%.1f%%", proportion*100), FontSize(10), TextColorVec(CurrentColorScheme.List.Muted))
				})
				Label(entry.Name, FontSize(12))
			})
		})
	})
}

func SelectionStrip(s *Scanner) {
	Separator()
	Container(Attrs(Row, CrossMid, Expand, FixHeight(44), Pad2(0, 16), Gap(10), UseSurface(SurfacePanel)), func() {
		entry := s.selected
		if entry == nil {
			entry = s.root
		}
		Container(Attrs(Row, CrossMid, Grow(1), Extrinsic, Expand, Clip), func() {
			path := "Select an entry to browse or reveal it"
			if entry != nil {
				path = entry.Path
			}
			NextAccessName("selected_path")
			NextAccessValue(path)
			AssignAccess()
			Label(path, FontSize(12), TextColorVec(CurrentColorScheme.List.Muted))
		})
		NextAccessName("browse")
		if ButtonExt("Browse", ButtonAttrs{Icon: TypFolderOpen, Disabled: entry == nil || !entry.IsDir}, ButtonLook{TextSize: 12, PadScale: 1}) {
			browser.OpenFile(entry.Path)
		}
		NextAccessName("reveal")
		if ButtonExt("Reveal", ButtonAttrs{Icon: TypEye, Disabled: entry == nil}, ButtonLook{TextSize: 12, PadScale: 1}) {
			RevealInFileManager(entry.Path)
		}
	})
}

func ScanStatus(s *Scanner, count int, flat bool) {
	Separator()
	Container(Attrs(Row, CrossMid, Expand, FixHeight(28), Pad2(0, 16), Gap(8), UseSurface(SurfaceCanvas)), func() {
		state, label, icon := "done", "Scan complete", SymPass
		end := s.done
		if s.state == Running {
			state, label, icon, end = "running", "Scanning…", SymClock, time.Now()
			RequestNextFrame()
		}
		if s.state == Stopped {
			state, label = "stopped", "Scan stopped"
		}
		if s.err != nil {
			state, label, icon = "error", "Unable to read folder", SymFolder
		}
		NextAccessName("scan_status")
		NextAccessValue(state)
		AssignAccess()
		color := CurrentColorScheme.List.Muted
		ModAttrs(AmendTextStyle(FontSize(11), TextColorVec(color)))
		Icon(icon, FontSize(11))
		Container(Attrs(MaxWidth(300), Clip), func() { Label(label) })
		Label(fmt.Sprintf("·  %d / %d folders", s.scanned, s.submitted))
		if !end.IsZero() {
			Label(fmt.Sprintf("·  %.1f s", max(0, end.Sub(s.started).Seconds())))
		}
		if len(s.readErrors) > 0 {
			NextAccessName("skipped_folders")
			NextAccessValue(fmt.Sprint(len(s.readErrors)))
			label := fmt.Sprintf("%d folders skipped…", len(s.readErrors))
			if len(s.readErrors) == 1 {
				label = "1 folder skipped…"
			}
			look := DefaultCtrlButtonLook()
			look.TextSize = 11
			if ButtonExt(label, ButtonAttrs{}, look) {
				s.showReadErrors = true
			}
		}
		Filler(1)
		share := "parent"
		if flat {
			share = "results"
		}
		Label(fmt.Sprintf("%d shown  ·  Fill: share of %s", count, share))
	})
}
