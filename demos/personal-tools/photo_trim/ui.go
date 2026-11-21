package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

type f32 = float32

const (
	winWidth  = 1140
	winHeight = 720
	photoRowH = 62
	sidePaneW = 336
)

type palette struct {
	mutedText    Vec4
	subtleText   Vec4
	cardBg       Vec4
	cardBorder   Vec4
	headerBg     Vec4
	divider      Vec4
	rowHoverBg   Vec4
	rowActiveBg  Vec4
	rowActiveBdr Vec4
	badgeBg      Vec4
	previewBg    Vec4

	accentBg   Vec4
	accentText Vec4

	goodBg   Vec4
	goodText Vec4
	goodBdr  Vec4

	warnBg   Vec4
	warnText Vec4
	warnBdr  Vec4

	gpsBg   Vec4
	gpsText Vec4
	gpsBdr  Vec4

	cleanBg   Vec4
	cleanText Vec4
}

func currentPalette(dark bool) palette {
	if dark {
		return palette{
			mutedText:    Vec4{34, 10, 68, 1},
			subtleText:   Vec4{34, 8, 52, 1},
			cardBg:       Vec4{32, 8, 18, 1},
			cardBorder:   Vec4{32, 8, 28, 1},
			headerBg:     Vec4{32, 8, 15, 1},
			divider:      Vec4{32, 8, 27, 1},
			rowHoverBg:   Vec4{32, 10, 22, 1},
			rowActiveBg:  Vec4{208, 34, 24, 1},
			rowActiveBdr: Vec4{208, 66, 54, 0.8},
			badgeBg:      Vec4{32, 9, 25, 1},
			previewBg:    Vec4{32, 10, 12, 1},

			accentBg:   Vec4{208, 68, 52, 1},
			accentText: Vec4{208, 80, 76, 1},

			goodBg:   Vec4{152, 42, 22, 1},
			goodText: Vec4{152, 68, 72, 1},
			goodBdr:  Vec4{152, 48, 36, 1},

			warnBg:   Vec4{28, 56, 23, 1},
			warnText: Vec4{32, 86, 72, 1},
			warnBdr:  Vec4{28, 56, 38, 1},

			gpsBg:   Vec4{12, 56, 24, 1},
			gpsText: Vec4{16, 86, 74, 1},
			gpsBdr:  Vec4{14, 54, 40, 1},

			cleanBg:   Vec4{32, 8, 24, 1},
			cleanText: Vec4{34, 8, 60, 1},
		}
	}
	return palette{
		mutedText:    Vec4{222, 12, 42, 1},
		subtleText:   Vec4{222, 10, 54, 1},
		cardBg:       Vec4{38, 22, 97, 1},
		cardBorder:   Vec4{35, 16, 86, 1},
		headerBg:     Vec4{37, 20, 93, 1},
		divider:      Vec4{35, 14, 88, 1},
		rowHoverBg:   Vec4{37, 24, 94, 1},
		rowActiveBg:  Vec4{208, 64, 94, 1},
		rowActiveBdr: Vec4{208, 62, 62, 0.7},
		badgeBg:      Vec4{36, 20, 91, 1},
		previewBg:    Vec4{34, 14, 14, 1},

		accentBg:   Vec4{208, 70, 46, 1},
		accentText: Vec4{208, 72, 38, 1},

		goodBg:   Vec4{152, 54, 91, 1},
		goodText: Vec4{152, 66, 26, 1},
		goodBdr:  Vec4{152, 44, 74, 1},

		warnBg:   Vec4{34, 84, 91, 1},
		warnText: Vec4{26, 76, 32, 1},
		warnBdr:  Vec4{32, 68, 74, 1},

		gpsBg:   Vec4{14, 82, 92, 1},
		gpsText: Vec4{12, 72, 36, 1},
		gpsBdr:  Vec4{14, 62, 78, 1},

		cleanBg:   Vec4{36, 18, 92, 1},
		cleanText: Vec4{222, 10, 48, 1},
	}
}

func handleShortcuts() {
	if state.ShowFolderModal {
		return
	}
	mods := GetInputState().Modifiers
	if mods&(ModCmd|ModCtrl) == 0 {
		return
	}
	switch GetFrameInput().Key {
	case KeyO:
		openFolderModal()
	case KeyE:
		if !state.Exporting && state.ComputeStats().SelectedCount > 0 {
			startBackgroundExport()
		}
	}
}

func openFolderModal() {
	start := state.FolderPath
	if state.DemoMode || start == "" {
		if home, err := os.UserHomeDir(); err == nil {
			pics := filepath.Join(home, "Pictures")
			if fi, err := os.Stat(pics); err == nil && fi.IsDir() {
				start = pics
			} else {
				start = home
			}
		} else {
			start = "/"
		}
	}
	state.ShowFolderModal = true
	state.BrowseCwd = start
	state.BrowseFilter = ""
	state.BrowseSelected = -1
}

// RootView builds the entire Photo Trim frame.
func RootView() {
	SetDarkMode(state.DarkMode)
	pal := currentPalette(state.DarkMode)

	handleShortcuts()

	visible := state.VisiblePhotos()
	if state.ActivePhoto == nil && len(visible) > 0 {
		state.ActivePhoto = visible[0]
	}
	stats := state.ComputeStats()

	Container(Attrs(Viewport, UseSurface(SurfaceCanvas), Pad(16), Gap(12)), func() {
		headerBarView(pal)
		summaryStripView(pal, stats)
		mainWorkspaceView(pal, visible, stats)
		footerBarView(pal, stats)

		if state.ShowFolderModal {
			folderPickerModalView(pal)
		}
	})
}

func headerBarView(pal palette) {
	Container(Attrs(Row, Expand, CrossMid, Gap(10)), func() {
		// Brand emblem + title
		Container(Attrs(Row, CrossMid, Gap(9)), func() {
			Container(Attrs(
				FixSize(32, 32), Corners(8), Center,
				Background(208, 72, 48, 1), Grad(-18, 6, -8, 0),
			), func() {
				Icon(TypCamera, FontSize(18), TextColor(0, 0, 100, 1))
			})
			Container(Attrs(Gap(1)), func() {
				Label("Photo Trim", FontSize(15), FontWeight(WeightBold))
				Label("Resize & strip GPS metadata locally", FontSize(10.5), TextColorVec(pal.mutedText))
			})
		})

		Spacer(6)

		// Active folder pill
		Container(Attrs(
			Row, Grow(1), CrossMid, Gap(8), Pad2(6, 11),
			Corners(8), BackgroundVec(pal.cardBg), BorderWidth(1), BorderColorVec(pal.cardBorder), Clip,
		), func() {
			Icon(TypFolderOpen, FontSize(15), TextColorVec(pal.mutedText))
			folderLabel := state.FolderPath
			if state.DemoMode {
				folderLabel = "~/Pictures/Spring-Trip-2026"
			} else if folderLabel == "" {
				folderLabel = "No folder selected"
			}
			Container(Attrs(Grow(1), Clip), func() {
				Label(folderLabel, FontSize(12), FontWeight(WeightMedium))
			})
			if state.DemoMode {
				Container(Attrs(Pad2(2, 7), Corners(99), BackgroundVec(pal.badgeBg)), func() {
					Label("Sample folder", FontSize(10), FontWeight(WeightMedium), TextColorVec(pal.mutedText))
				})
			} else if state.Scanning {
				Label("Scanning…", FontSize(11), TextColorVec(pal.accentText))
			}
		})

		NextAccessName("choose-folder")
		if CtrlButton(TypFolderOpen, "Choose Folder…", true) {
			openFolderModal()
		}

		if !state.DemoMode && state.FolderPath != "" {
			NextAccessName("reload-folder")
			if CtrlButton(TypRefresh, "Reload", true) {
				loadRealFolder(state.FolderPath)
			}
		}

		themeLabel := "Dark"
		themeIcon := TypAdjustContrast
		if state.DarkMode {
			themeLabel = "Light"
			themeIcon = TypWeatherSunny
		}
		NextAccessName("toggle-theme")
		if CtrlButton(themeIcon, themeLabel, true) {
			state.DarkMode = !state.DarkMode
		}
	})
}

func summaryStripView(pal palette, stats SummaryStats) {
	savedBytes := stats.OrigBytes - stats.EstOutBytes
	if savedBytes < 0 {
		savedBytes = 0
	}
	savedPct := 0
	if stats.OrigBytes > 0 {
		savedPct = int(math.Round(float64(savedBytes) * 100.0 / float64(stats.OrigBytes)))
	}

	Container(Attrs(Row, Expand, Gap(10)), func() {
		// Card 1: Selected Photos
		kpiCard(pal, func() {
			Container(Attrs(Row, Expand, CrossMid, Gap(6)), func() {
				Label("PHOTOS", FontSize(9.5), FontWeight(WeightBold), TextColorVec(pal.subtleText))
				Filler(1)
				if stats.TotalCount > 0 {
					Container(Attrs(Row, Gap(6), CrossMid), func() {
						allSel := stats.SelectedCount == stats.TotalCount
						if inlineLink("All", allSel, pal) {
							for _, p := range state.Photos {
								p.Selected = true
							}
						}
						Label("·", FontSize(10), TextColorVec(pal.subtleText))
						noneSel := stats.SelectedCount == 0
						if inlineLink("None", noneSel, pal) {
							for _, p := range state.Photos {
								p.Selected = false
							}
						}
					})
				}
			})
			Container(Attrs(Row, CrossMid, Gap(8)), func() {
				Label(fmt.Sprintf("%d of %d selected", stats.SelectedCount, stats.TotalCount), FontSize(14), FontWeight(WeightBold))
				if stats.ExportedCount > 0 {
					pillBadge(fmt.Sprintf("%d exported", stats.ExportedCount), pal.goodBg, pal.goodText, pal.goodBdr)
				}
			})
		})

		// Card 2: Size Reduction
		kpiCard(pal, func() {
			Container(Attrs(Row, Expand, CrossMid), func() {
				sizeHeading := "ESTIMATED SIZE"
				if stats.ExportedCount > 0 && stats.ExportedCount == stats.SelectedCount {
					sizeHeading = "EXPORTED SIZE"
				}
				Label(sizeHeading, FontSize(9.5), FontWeight(WeightBold), TextColorVec(pal.subtleText))
				Filler(1)
				if savedBytes > 0 {
					Label(fmt.Sprintf("Save %s", FormatBytes(savedBytes)), FontSize(10.5), FontWeight(WeightMedium), TextColorVec(pal.goodText))
				}
			})
			Container(Attrs(Row, CrossMid, Gap(8)), func() {
				if stats.SelectedCount == 0 {
					Label("—", FontSize(14), FontWeight(WeightBold), TextColorVec(pal.mutedText))
				} else {
					Label(FormatBytes(stats.OrigBytes), FontSize(13), TextColorVec(pal.mutedText))
					Label("→", FontSize(12), TextColorVec(pal.subtleText))
					Label(FormatBytes(stats.EstOutBytes), FontSize(14), FontWeight(WeightBold))
					if savedPct > 0 {
						pillBadge(fmt.Sprintf("-%d%%", savedPct), pal.goodBg, pal.goodText, pal.goodBdr)
					}
				}
			})
		})

		// Card 3: GPS Privacy Status
		kpiCard(pal, func() {
			Container(Attrs(Row, Expand, CrossMid), func() {
				Label("GPS METADATA", FontSize(9.5), FontWeight(WeightBold), TextColorVec(pal.subtleText))
				Filler(1)
				if state.Settings.StripGPS || state.Settings.StripAllEXIF {
					Label("Auto-strip ON", FontSize(10.5), FontWeight(WeightMedium), TextColorVec(pal.goodText))
				} else {
					Label("Keeping GPS", FontSize(10.5), FontWeight(WeightMedium), TextColorVec(pal.warnText))
				}
			})
			Container(Attrs(Row, CrossMid, Gap(8)), func() {
				switch {
				case stats.SelectedGPSCount == 0:
					Icon(TypLockClosed, FontSize(14), TextColorVec(pal.goodText))
					Label("No GPS tags in selection", FontSize(13), FontWeight(WeightMedium), TextColorVec(pal.mutedText))
				case state.Settings.StripGPS || state.Settings.StripAllEXIF:
					Icon(TypLockClosed, FontSize(14), TextColorVec(pal.goodText))
					Label(fmt.Sprintf("%d tagged", stats.SelectedGPSCount), FontSize(14), FontWeight(WeightBold))
					pillBadge("Will strip GPS", pal.goodBg, pal.goodText, pal.goodBdr)
				default:
					Icon(TypWarning, FontSize(14), TextColorVec(pal.warnText))
					Label(fmt.Sprintf("%d with GPS", stats.SelectedGPSCount), FontSize(14), FontWeight(WeightBold))
					pillBadge("Location exposed", pal.gpsBg, pal.gpsText, pal.gpsBdr)
				}
			})
		})
	})
}

func kpiCard(pal palette, body func()) {
	Container(Attrs(
		Grow(1), Expand, Pad2(9, 13), Gap(4),
		Corners(9), BackgroundVec(pal.cardBg), BorderWidth(1), BorderColorVec(pal.cardBorder),
	), body)
}

func inlineLink(label string, muted bool, pal palette) bool {
	clicked := false
	Container(Attrs(Pad2(1, 3), Corners(4)), func() {
		st := ProcessButtonEvents(muted)
		clicked = st.Clicked
		col := pal.accentText
		if muted {
			col = pal.subtleText
		} else if st.Hovered {
			ModAttrs(BackgroundVec(pal.badgeBg))
		}
		Label(label, FontSize(10.5), FontWeight(WeightMedium), TextColorVec(col))
	})
	return clicked
}

func pillBadge(text string, bg, fg, bdr Vec4) {
	Container(Attrs(
		Pad2(2, 7), Corners(99),
		BackgroundVec(bg), BorderWidth(1), BorderColorVec(bdr),
	), func() {
		Label(text, FontSize(10), FontWeight(WeightBold), TextColorVec(fg))
	})
}

func mainWorkspaceView(pal palette, visible []*PhotoItem, stats SummaryStats) {
	Container(Attrs(Row, Grow(1), Expand, Gap(12)), func() {
		photoListPaneView(pal, visible, stats)
		inspectorPaneView(pal, stats)
	})
}

func photoListPaneView(pal palette, visible []*PhotoItem, stats SummaryStats) {
	Container(Attrs(Grow(1), Expand, Gap(8)), func() {
		// Filter chips + sort + search bar
		Container(Attrs(Row, Expand, CrossMid, Gap(8)), func() {
			filterChip(pal, FilterAll, fmt.Sprintf("All (%d)", stats.TotalCount))
			filterChip(pal, FilterGPS, fmt.Sprintf("Has GPS (%d)", stats.TotalGPSCount))
			filterChip(pal, FilterLarge, fmt.Sprintf(">5 MB (%d)", stats.LargeCount))

			Filler(1)

			// Compact search box
			searchAttrs := CtrlTextInputAttrs()
			searchAttrs.NoAutoFocus = true
			searchAttrs.FixedWidth = true
			searchAttrs.MinWidth = 175
			searchAttrs.Placeholder = "Filter name or camera…"
			NextAccessName("photo-search")
			TextInputExt(&state.Search, searchAttrs)

			// Sort cycle button
			sortLabel := "Sort: Size"
			switch state.SortBy {
			case SortNameAsc:
				sortLabel = "Sort: Name"
			case SortPixelsDesc:
				sortLabel = "Sort: Pixels"
			case SortGPSFirst:
				sortLabel = "Sort: GPS"
			}
			NextAccessName("sort-photos")
			if CtrlButton(NoIcon, sortLabel, true) {
				state.SortBy = (state.SortBy + 1) % 4
			}
		})

		// Main bordered photo list card
		Container(Attrs(
			Viewport, Corners(10),
			BackgroundVec(pal.cardBg), BorderWidth(1), BorderColorVec(pal.cardBorder),
		), func() {
			// Table column header (extra right padding accounts for virtual list scrollbar gutter)
			Container(Attrs(Row, Expand, FixHeight(30), CrossMid, Pad4(0, 26, 0, 12), Gap(10), BackgroundVec(pal.headerBg)), func() {
				Element(Attrs(FixWidth(20), FixHeight(1))) // checkbox spacer
				Element(Attrs(FixWidth(56), FixHeight(1))) // thumbnail spacer
				Container(Attrs(Grow(1)), func() {
					Label("PHOTO", FontSize(9.5), FontWeight(WeightBold), TextColorVec(pal.subtleText))
				})
				Container(Attrs(FixWidth(92), CrossAlign(AlignEnd)), func() {
					Label("ORIGINAL", FontSize(9.5), FontWeight(WeightBold), TextColorVec(pal.subtleText))
				})
				Container(Attrs(FixWidth(110), CrossAlign(AlignEnd)), func() {
					Label("SHRUNK", FontSize(9.5), FontWeight(WeightBold), TextColorVec(pal.subtleText))
				})
				Container(Attrs(FixWidth(94), CrossAlign(AlignEnd)), func() {
					Label("METADATA", FontSize(9.5), FontWeight(WeightBold), TextColorVec(pal.subtleText))
				})
			})
			Element(Attrs(Expand, FixHeight(1), BackgroundVec(pal.divider)))

			if state.ScanErr != "" {
				Container(Attrs(Viewport, Center, Gap(8), Pad(24)), func() {
					Icon(TypWarning, FontSize(26), TextColorVec(pal.warnText))
					Label("Could not read folder", FontSize(14), FontWeight(WeightBold))
					Label(state.ScanErr, FontSize(11.5), TextColorVec(pal.mutedText))
				})
			} else if len(visible) == 0 {
				Container(Attrs(Viewport, Center, Gap(8), Pad(24)), func() {
					Icon(TypImageOutline, FontSize(28), TextColorVec(pal.subtleText))
					if len(state.Photos) == 0 {
						Label("No photos found in this folder", FontSize(14), FontWeight(WeightBold))
						Label("Choose a folder with JPEG, PNG, or WebP images, or try Sample Photos.", FontSize(11.5), TextColorVec(pal.mutedText))
						Spacer(4)
						Container(Attrs(Row, Gap(8)), func() {
							NextButtonType(ButtonPrimary)
							if Button(TypFolderOpen, "Choose Folder…") {
								openFolderModal()
							}
							if Button(TypImage, "Load Sample Photos") {
								loadDemoPhotos()
							}
						})
					} else {
						Label("No photos match your filter", FontSize(13.5), FontWeight(WeightBold))
						if CtrlButton(NoIcon, "Clear filter", true) {
							state.Filter = FilterAll
							state.Search = ""
						}
					}
				})
			} else {
				Container(Attrs(Viewport), func() {
					itemKey := func(i int) any { return visible[i] }
					itemHeight := func(i int, width f32) f32 { return photoRowH }
					itemView := func(i int, width f32) {
						photoRowView(pal, visible[i], width)
					}
					VirtualListView(state.ListKey, len(visible), itemKey, itemHeight, itemView)
				})
			}
		})
	})
}

func filterChip(pal palette, f PhotoFilter, text string) {
	active := state.Filter == f
	Container(Attrs(Pad2(5, 10), Corners(7), BorderWidth(1)), func() {
		st := ProcessButtonEvents(false)
		if st.Clicked {
			state.Filter = f
		}
		bg := pal.cardBg
		bdr := pal.cardBorder
		fg := pal.mutedText
		if active {
			bg = pal.rowActiveBg
			bdr = pal.rowActiveBdr
			fg = pal.accentText
		} else if st.Hovered {
			bg = pal.rowHoverBg
		}
		ModAttrs(BackgroundVec(bg), BorderColorVec(bdr))
		weight := WeightMedium
		if active {
			weight = WeightBold
		}
		Label(text, FontSize(11), FontWeight(weight), TextColorVec(fg))
	})
}

func photoRowView(pal palette, p *PhotoItem, width f32) {
	isActive := state.ActivePhoto == p
	tw, th := TargetDimensions(p.Width, p.Height, state.Settings.MaxDim)
	estBytes := EstimateOutputBytes(p, state.Settings)
	outBytes := estBytes
	if p.Exported && p.ExportedBytes > 0 {
		outBytes = p.ExportedBytes
	}
	savePct := 0
	if p.FileBytes > 0 && outBytes < p.FileBytes {
		savePct = int(math.Round(float64(p.FileBytes-outBytes) * 100.0 / float64(p.FileBytes)))
	}

	Container(Attrs(FixSize(width, photoRowH)), func() {
		Container(Attrs(Row, Expand, Grow(1), CrossMid, Pad2(0, 12), Gap(10)), func() {
			if isActive {
				ModAttrs(BackgroundVec(pal.rowActiveBg))
			} else if IsHovered() {
				ModAttrs(BackgroundVec(pal.rowHoverBg))
			}
			if PressAction() {
				state.ActivePhoto = p
			}

			// Selection checkbox
			Container(Attrs(FixWidth(20), Center), func() {
				CheckBoxExt(&p.Selected, "", CheckBoxAttrs{Size: 13})
			})

			// Thumbnail box (56x38)
			Container(Attrs(
				FixSize(56, 38), Corners(5), Clip, Center,
				BackgroundVec(pal.previewBg), BorderWidth(1), BorderColorVec(pal.cardBorder),
			), func() {
				if p.Thumb != nil {
					id := UseImage(p.ThumbKey, p.Thumb)
					ImageViewAt(id, Vec2{56, 38})
				} else {
					Icon(TypImageOutline, FontSize(16), TextColorVec(pal.subtleText))
				}
			})

			// Name + camera/date subtitle
			Container(Attrs(Grow(1), Clip, Gap(2)), func() {
				nameWeight := WeightMedium
				if isActive {
					nameWeight = WeightBold
				}
				Label(p.Name, FontSize(12), FontWeight(nameWeight))
				sub := fmt.Sprintf("%d × %d", p.Width, p.Height)
				if p.Meta.Camera != "" {
					sub += "  ·  " + p.Meta.Camera
				}
				if p.Meta.DateTaken != "" {
					sub += "  ·  " + p.Meta.DateTaken
				}
				Label(sub, FontSize(10), TextColorVec(pal.subtleText))
			})

			// Original size column
			Container(Attrs(FixWidth(92), CrossAlign(AlignEnd), Gap(2)), func() {
				Label(FormatBytes(p.FileBytes), FontSize(12), FontWeight(WeightMedium))
				Label(strings.ToUpper(strings.TrimPrefix(p.Ext, ".")), FontSize(9.5), TextColorVec(pal.subtleText))
			})

			// Shrunk / target size column
			Container(Attrs(FixWidth(110), CrossAlign(AlignEnd), Gap(2)), func() {
				prefix := "~"
				if p.Exported && p.ExportedBytes > 0 {
					prefix = ""
				}
				Container(Attrs(Row, CrossMid, Gap(4)), func() {
					Label(prefix+FormatBytes(outBytes), FontSize(12), FontWeight(WeightBold), TextColorVec(pal.goodText))
					if savePct > 0 {
						Label(fmt.Sprintf("-%d%%", savePct), FontSize(9.5), FontWeight(WeightBold), TextColorVec(pal.goodText))
					}
				})
				Label(fmt.Sprintf("%d × %d", tw, th), FontSize(9.5), TextColorVec(pal.subtleText))
			})

			// GPS / Metadata badge column
			Container(Attrs(FixWidth(94), CrossAlign(AlignEnd)), func() {
				if p.ExportErr != "" {
					pillBadge("Error", pal.gpsBg, pal.gpsText, pal.gpsBdr)
				} else if p.Exported {
					pillBadge("Exported", pal.goodBg, pal.goodText, pal.goodBdr)
				} else if p.Meta.HasGPS {
					if state.Settings.StripGPS || state.Settings.StripAllEXIF {
						Container(Attrs(
							Row, CrossMid, Gap(4), Pad2(3, 7), Corners(99),
							BackgroundVec(pal.warnBg), BorderWidth(1), BorderColorVec(pal.warnBdr),
						), func() {
							Icon(TypLocation, FontSize(11), TextColorVec(pal.warnText))
							Label("GPS · Strip", FontSize(9.5), FontWeight(WeightBold), TextColorVec(pal.warnText))
						})
					} else {
						Container(Attrs(
							Row, CrossMid, Gap(4), Pad2(3, 7), Corners(99),
							BackgroundVec(pal.gpsBg), BorderWidth(1), BorderColorVec(pal.gpsBdr),
						), func() {
							Icon(TypLocation, FontSize(11), TextColorVec(pal.gpsText))
							Label("Has GPS", FontSize(9.5), FontWeight(WeightBold), TextColorVec(pal.gpsText))
						})
					}
				} else {
					Container(Attrs(Pad2(3, 8), Corners(99), BackgroundVec(pal.cleanBg)), func() {
						Label("No GPS", FontSize(9.5), FontWeight(WeightMedium), TextColorVec(pal.cleanText))
					})
				}
			})
		})
		Element(Attrs(Expand, FixHeight(1), BackgroundVec(pal.divider)))
	})
}

func inspectorPaneView(pal palette, stats SummaryStats) {
	Container(Attrs(FixWidth(sidePaneW), Expand, Gap(10)), func() {
		previewInspectorCard(pal)
		exportControlsCard(pal, stats)
	})
}

func previewInspectorCard(pal palette) {
	p := state.ActivePhoto

	Container(Attrs(
		Expand, Pad(12), Gap(8),
		Corners(10), BackgroundVec(pal.cardBg), BorderWidth(1), BorderColorVec(pal.cardBorder),
	), func() {
		// Preview frame (312 x 144)
		const prevW f32 = sidePaneW - 24
		const prevH f32 = 144

		Container(Attrs(
			FixSize(prevW, prevH), Corners(7), Clip, Center,
			BackgroundVec(pal.previewBg), BorderWidth(1), BorderColorVec(pal.cardBorder),
		), func() {
			if p == nil {
				Label("Select a photo to inspect", FontSize(11.5), TextColorVec(pal.subtleText))
			} else if p.Preview != nil {
				id := UseImage(p.PrevKey, p.Preview)
				ImageView(id, Vec2{prevW, prevH})
			} else if p.Thumb != nil {
				id := UseImage(p.ThumbKey, p.Thumb)
				ImageView(id, Vec2{prevW, prevH})
			} else {
				Icon(TypImageOutline, FontSize(28), TextColorVec(pal.subtleText))
			}
		})

		if p == nil {
			Label("No photo selected", FontSize(12), FontWeight(WeightBold), TextColorVec(pal.mutedText))
			return
		}

		// File info + before/after size comparison
		tw, th := TargetDimensions(p.Width, p.Height, state.Settings.MaxDim)
		est := EstimateOutputBytes(p, state.Settings)
		if p.Exported && p.ExportedBytes > 0 {
			est = p.ExportedBytes
		}

		Container(Attrs(Expand, Gap(2), Clip), func() {
			Label(p.Name, FontSize(12.5), FontWeight(WeightBold))
			Container(Attrs(Row, Expand, CrossMid, Gap(6)), func() {
				Label(fmt.Sprintf("%d×%d (%s)", p.Width, p.Height, FormatBytes(p.FileBytes)), FontSize(10.5), TextColorVec(pal.mutedText))
				Label("→", FontSize(10.5), TextColorVec(pal.subtleText))
				Label(fmt.Sprintf("%d×%d (~%s)", tw, th, FormatBytes(est)), FontSize(10.5), FontWeight(WeightBold), TextColorVec(pal.goodText))
			})
		})

		// GPS inspection bar
		if p.Meta.HasGPS {
			bg, fg, bdr := pal.gpsBg, pal.gpsText, pal.gpsBdr
			actionTxt := "Keeping GPS"
			if state.Settings.StripGPS || state.Settings.StripAllEXIF {
				bg, fg, bdr = pal.goodBg, pal.goodText, pal.goodBdr
				actionTxt = "Will strip"
			}
			Container(Attrs(
				Row, Expand, CrossMid, Gap(6), Pad2(6, 9), Corners(7),
				BackgroundVec(bg), BorderWidth(1), BorderColorVec(bdr),
			), func() {
				Icon(TypLocation, FontSize(13), TextColorVec(fg))
				Container(Attrs(Grow(1), Clip), func() {
					Label(FormatGPS(p.Meta.GPSLat, p.Meta.GPSLon), FontSize(10.5), FontWeight(WeightMedium), TextColorVec(fg))
				})
				Label(actionTxt, FontSize(9.5), FontWeight(WeightBold), TextColorVec(fg))
			})
		} else {
			Container(Attrs(
				Row, Expand, CrossMid, Gap(6), Pad2(6, 9), Corners(7),
				BackgroundVec(pal.cleanBg),
			), func() {
				Icon(TypLockClosed, FontSize(12), TextColorVec(pal.cleanText))
				Label("No GPS coordinates in file", FontSize(10.5), TextColorVec(pal.cleanText))
			})
		}
	})
}

func exportControlsCard(pal palette, stats SummaryStats) {
	Container(Attrs(
		Grow(1), Expand, Pad(12), Gap(7),
		Corners(10), BackgroundVec(pal.cardBg), BorderWidth(1), BorderColorVec(pal.cardBorder),
	), func() {
		// Section header + subfolder input
		Container(Attrs(Row, Expand, CrossMid, Gap(8)), func() {
			Label("EXPORT SETTINGS", FontSize(9.5), FontWeight(WeightBold), TextColorVec(pal.subtleText))
			Filler(1)
			Label("Folder:", FontSize(10.5), TextColorVec(pal.mutedText))
			subAttrs := CtrlTextInputAttrs()
			subAttrs.NoAutoFocus = true
			subAttrs.FixedWidth = true
			subAttrs.MinWidth = 104
			subAttrs.Placeholder = "trimmed"
			NextAccessName("output-subfolder")
			TextInputExt(&state.Settings.Subfolder, subAttrs)
		})

		// Max long edge presets
		Container(Attrs(Expand, Gap(4)), func() {
			Container(Attrs(Row, Expand, CrossMid), func() {
				Label("Max long edge", FontSize(11), FontWeight(WeightMedium))
				Filler(1)
				dimLbl := "Keep original"
				if state.Settings.MaxDim > 0 {
					dimLbl = fmt.Sprintf("%d px", state.Settings.MaxDim)
				}
				Label(dimLbl, FontSize(10.5), TextColorVec(pal.mutedText))
			})
			segAttrs := DefaultSegmentedControlAttrs()
			segAttrs.Expand = true
			segAttrs.MinCellWidth = 48
			segAttrs.CellPadH = 8
			NextAccessName("max-dim")
			SegmentedControlExt(&state.Settings.MaxDim, segAttrs, func() {
				SegmentedCell("1080", 1080)
				SegmentedCell("1600", 1600)
				SegmentedCell("2048", 2048)
				SegmentedCell("Orig", 0)
			})
		})

		// Output format + JPEG quality
		Container(Attrs(Expand, Gap(4)), func() {
			Label("Output format", FontSize(11), FontWeight(WeightMedium))
			fmtAttrs := DefaultSegmentedControlAttrs()
			fmtAttrs.Expand = true
			fmtAttrs.MinCellWidth = 56
			fmtAttrs.CellPadH = 8
			NextAccessName("output-format")
			SegmentedControlExt(&state.Settings.Format, fmtAttrs, func() {
				SegmentedCell("JPEG", FormatJPEG)
				SegmentedCell("PNG", FormatPNG)
				SegmentedCell("Same", FormatSame)
			})
		})

		if state.Settings.Format != FormatPNG {
			Container(Attrs(Row, Expand, CrossMid, Gap(10)), func() {
				Label("JPEG quality", FontSize(11), FontWeight(WeightMedium))
				Filler(1)
				Slider(&state.Settings.Quality, SliderAttrs{
					Min:   50,
					Max:   95,
					Step:  1,
					Width: 162,
				})
				Container(Attrs(FixWidth(34), CrossAlign(AlignEnd)), func() {
					Label(fmt.Sprintf("%d%%", int(math.Round(float64(state.Settings.Quality)))), FontSize(11), FontWeight(WeightBold))
				})
			})
		}

		Element(Attrs(Expand, FixHeight(1), BackgroundVec(pal.divider)))

		// Privacy toggles
		Container(Attrs(Expand, Gap(6)), func() {
			Container(Attrs(Row, Expand, CrossMid, Gap(8)), func() {
				Container(Attrs(Grow(1), Gap(1)), func() {
					Label("Strip GPS metadata", FontSize(11.5), FontWeight(WeightBold))
					Label("Remove location coordinates before sharing", FontSize(9.5), TextColorVec(pal.subtleText))
				})
				NextAccessName("strip-gps")
				ToggleSwitchExt(&state.Settings.StripGPS, ToggleSwitchAttrs{Height: 21})
			})
			NextAccessName("strip-all-exif")
			CheckBox(&state.Settings.StripAllEXIF, "Also strip camera model & date EXIF")
		})

		Filler(1)

		// Progress bar (during export) or primary one-click Export CTA
		if state.Exporting {
			frac := f32(0)
			if state.ExportTotal > 0 {
				frac = f32(state.ExportDone) / f32(state.ExportTotal)
			}
			Container(Attrs(Expand, Gap(6)), func() {
				Container(Attrs(Row, Expand, CrossMid), func() {
					Label("Exporting "+state.ExportCurrentName, FontSize(11), FontWeight(WeightMedium))
					Filler(1)
					Label(fmt.Sprintf("%d/%d", state.ExportDone, state.ExportTotal), FontSize(11), FontWeight(WeightBold))
				})
				ProgressBarExt(frac, ProgressBarAttrs{Width: sidePaneW - 24, Height: 9})
			})
		} else {
			disabled := stats.SelectedCount == 0
			ctaLabel := fmt.Sprintf("Export %d Resized Copies", stats.SelectedCount)
			if stats.SelectedCount == 1 {
				ctaLabel = "Export 1 Resized Copy"
			} else if stats.SelectedCount == 0 {
				ctaLabel = "Select Photos to Export"
			}
			if fullWidthPrimaryButton("export-photos", TypExport, ctaLabel, disabled) {
				startBackgroundExport()
			}
		}
	})
}

func fullWidthPrimaryButton(accessName string, icon IconGlyph, label string, disabled bool) bool {
	clicked := false
	Container(Attrs(Row, Expand, Center, Gap(7), Pad2(9, 14), Corners(8), BorderWidth(1)), func() {
		st := ProcessButtonEvents(disabled)
		NextAccessName(accessName)
		NextAccessRole("button")
		NextAccessLabel(label)
		NextAccessDisabled(disabled)
		AssignAccess()
		clicked = st.Clicked

		style := CurrentColorScheme.Buttons.Primary
		paint := style.Normal
		switch {
		case st.Disabled:
			paint = style.Disabled
		case st.Active:
			paint = style.Pressed
		case st.Hovered:
			paint = style.Hovered
		}
		bdr := paint.Border
		if st.FocusVisible && !disabled {
			bdr = CurrentColorScheme.FocusRing
		}
		ModAttrs(BackgroundVec(paint.Background), GradVec(paint.Gradient), BorderColorVec(bdr))
		if icon != NoIcon {
			Icon(icon, FontSize(15), TextColorVec(paint.Text))
		}
		Label(label, FontSize(12.5), FontWeight(WeightBold), TextColorVec(paint.Text))
	})
	return clicked
}

func footerBarView(pal palette, stats SummaryStats) {
	Container(Attrs(Row, Expand, CrossMid, Gap(10)), func() {
		if state.LastExportErr != "" {
			Icon(TypWarning, FontSize(12), TextColorVec(pal.gpsText))
			Label("Export issue: "+state.LastExportErr, FontSize(10.5), TextColorVec(pal.gpsText))
		} else if state.LastExportSummary != "" {
			Icon(TypTick, FontSize(12), TextColorVec(pal.goodText))
			Label(state.LastExportSummary, FontSize(10.5), FontWeight(WeightMedium), TextColorVec(pal.goodText))
		} else {
			outDest := state.OutputDirectory()
			if state.DemoMode {
				outDest = "~/Pictures/PhotoTrim-Demo/" + strings.TrimSpace(state.Settings.Subfolder)
			}
			Label("Destination: "+outDest, FontSize(10.5), TextColorVec(pal.subtleText))
		}

		Filler(1)

		modKey := "Ctrl"
		if runtime.GOOS == "darwin" {
			modKey = "⌘"
		}
		Label(fmt.Sprintf("%sO open folder  ·  %sE export  ·  Original files are never modified", modKey, modKey), FontSize(10.5), TextColorVec(pal.subtleText))
	})
}

func folderPickerModalView(pal palette) {
	closeModal := func() {
		state.ShowFolderModal = false
	}

	Modal(560, closeModal, func() {
		Container(Attrs(Row, Expand, CrossMid), func() {
			Label("Choose Photo Folder", FontSize(14), FontWeight(WeightBold))
			Filler(1)
			if CtrlButton(TypImage, "Use Sample Photos", true) {
				loadDemoPhotos()
				closeModal()
			}
		})

		picked := ""
		attrs := DefaultFileBrowserAttrs()
		attrs.Title = "Choose Photo Folder"
		attrs.Width = 520
		if FileBrowserPanel(&state.BrowseCwd, &state.BrowseFilter, &state.BrowseSelected, &picked, attrs) {
			if picked != "" {
				loadRealFolder(picked)
			}
			closeModal()
			return
		}

		Container(Attrs(Row, Expand, CrossMid), func() {
			Filler(1)
			if Button(NoIcon, "Cancel") {
				closeModal()
			}
		})
	})
}
