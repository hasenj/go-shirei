package main

import (
	"fmt"
	"strings"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

type botanicalPalette struct {
	canvasBg       Vec4
	cardBg         Vec4
	cardDoneBg     Vec4
	cardHoverBg    Vec4
	cardBorder     Vec4
	cardDoneBorder Vec4
	summaryBg      Vec4
	summaryBorder  Vec4
	segBg          Vec4
	segActiveBg    Vec4
	segActiveText  Vec4
	textPrimary    Vec4
	textMuted      Vec4
	textSubtle     Vec4
	trackBg        Vec4
	divider        Vec4

	overdueAccent Vec4
	overdueBg     Vec4
	overdueText   Vec4

	todayAccent Vec4
	todayBg     Vec4
	todayText   Vec4

	soonAccent Vec4
	soonBg     Vec4
	soonText   Vec4

	happyAccent Vec4
	happyBg     Vec4
	happyText   Vec4

	waterPrimaryBg     Vec4
	waterPrimaryHover  Vec4
	waterPrimaryActive Vec4
	waterPrimaryText   Vec4

	waterSoftBg     Vec4
	waterSoftHover  Vec4
	waterSoftBorder Vec4
	waterSoftText   Vec4

	doneBtnBg     Vec4
	doneBtnHover  Vec4
	doneBtnBorder Vec4
	doneBtnText   Vec4

	undoBg     Vec4
	undoBorder Vec4
	undoText   Vec4

	headerBtnBg     Vec4
	headerBtnHover  Vec4
	headerBtnBorder Vec4
}

func currentBotanicalPalette(dark bool) botanicalPalette {
	if dark {
		return botanicalPalette{
			canvasBg:       Vec4{150, 10, 10, 1},
			cardBg:         Vec4{148, 10, 15, 1},
			cardDoneBg:     Vec4{146, 18, 16, 1},
			cardHoverBg:    Vec4{148, 11, 18, 1},
			cardBorder:     Vec4{148, 10, 24, 1},
			cardDoneBorder: Vec4{146, 30, 28, 1},
			summaryBg:      Vec4{148, 14, 17, 1},
			summaryBorder:  Vec4{148, 14, 26, 1},
			segBg:          Vec4{148, 10, 15, 1},
			segActiveBg:    Vec4{148, 36, 26, 1},
			segActiveText:  Vec4{144, 52, 88, 1},
			textPrimary:    Vec4{44, 16, 92, 1},
			textMuted:      Vec4{142, 10, 68, 1},
			textSubtle:     Vec4{142, 8, 52, 1},
			trackBg:        Vec4{148, 10, 23, 1},
			divider:        Vec4{148, 10, 24, 1},

			overdueAccent: Vec4{12, 78, 58, 1},
			overdueBg:     Vec4{12, 46, 22, 1},
			overdueText:   Vec4{14, 84, 76, 1},

			todayAccent: Vec4{36, 86, 56, 1},
			todayBg:     Vec4{34, 48, 22, 1},
			todayText:   Vec4{38, 86, 74, 1},

			soonAccent: Vec4{188, 56, 52, 1},
			soonBg:     Vec4{188, 40, 21, 1},
			soonText:   Vec4{188, 66, 74, 1},

			happyAccent: Vec4{146, 54, 48, 1},
			happyBg:     Vec4{146, 38, 21, 1},
			happyText:   Vec4{144, 62, 74, 1},

			waterPrimaryBg:     Vec4{198, 72, 42, 1},
			waterPrimaryHover:  Vec4{198, 76, 48, 1},
			waterPrimaryActive: Vec4{198, 76, 36, 1},
			waterPrimaryText:   Vec4{0, 0, 100, 1},

			waterSoftBg:     Vec4{198, 36, 21, 1},
			waterSoftHover:  Vec4{198, 42, 26, 1},
			waterSoftBorder: Vec4{198, 42, 34, 1},
			waterSoftText:   Vec4{198, 72, 76, 1},

			doneBtnBg:     Vec4{146, 34, 21, 1},
			doneBtnHover:  Vec4{146, 38, 26, 1},
			doneBtnBorder: Vec4{146, 38, 34, 1},
			doneBtnText:   Vec4{144, 60, 74, 1},

			undoBg:     Vec4{146, 34, 22, 1},
			undoBorder: Vec4{146, 42, 34, 1},
			undoText:   Vec4{144, 56, 84, 1},

			headerBtnBg:     Vec4{148, 10, 18, 1},
			headerBtnHover:  Vec4{148, 12, 24, 1},
			headerBtnBorder: Vec4{148, 10, 28, 1},
		}
	}
	return botanicalPalette{
		canvasBg:       Vec4{40, 28, 95, 1},
		cardBg:         Vec4{42, 36, 99, 1},
		cardDoneBg:     Vec4{142, 34, 96, 1},
		cardHoverBg:    Vec4{40, 30, 97, 1},
		cardBorder:     Vec4{38, 18, 86, 1},
		cardDoneBorder: Vec4{144, 34, 80, 1},
		summaryBg:      Vec4{42, 34, 98, 1},
		summaryBorder:  Vec4{38, 18, 85, 1},
		segBg:          Vec4{39, 22, 90, 1},
		segActiveBg:    Vec4{42, 40, 99, 1},
		segActiveText:  Vec4{150, 32, 20, 1},
		textPrimary:    Vec4{152, 22, 16, 1},
		textMuted:      Vec4{148, 10, 42, 1},
		textSubtle:     Vec4{145, 8, 54, 1},
		trackBg:        Vec4{38, 18, 90, 1},
		divider:        Vec4{38, 16, 88, 1},

		overdueAccent: Vec4{11, 76, 52, 1},
		overdueBg:     Vec4{12, 82, 93, 1},
		overdueText:   Vec4{10, 70, 38, 1},

		todayAccent: Vec4{34, 88, 48, 1},
		todayBg:     Vec4{40, 88, 91, 1},
		todayText:   Vec4{30, 76, 30, 1},

		soonAccent: Vec4{186, 56, 40, 1},
		soonBg:     Vec4{186, 56, 92, 1},
		soonText:   Vec4{188, 58, 28, 1},

		happyAccent: Vec4{146, 48, 38, 1},
		happyBg:     Vec4{144, 48, 91, 1},
		happyText:   Vec4{146, 54, 26, 1},

		waterPrimaryBg:     Vec4{198, 76, 42, 1},
		waterPrimaryHover:  Vec4{198, 80, 37, 1},
		waterPrimaryActive: Vec4{198, 80, 32, 1},
		waterPrimaryText:   Vec4{0, 0, 100, 1},

		waterSoftBg:     Vec4{198, 60, 93, 1},
		waterSoftHover:  Vec4{198, 64, 88, 1},
		waterSoftBorder: Vec4{198, 48, 77, 1},
		waterSoftText:   Vec4{200, 68, 31, 1},

		doneBtnBg:     Vec4{144, 46, 90, 1},
		doneBtnHover:  Vec4{144, 48, 85, 1},
		doneBtnBorder: Vec4{144, 38, 74, 1},
		doneBtnText:   Vec4{146, 54, 26, 1},

		undoBg:     Vec4{146, 44, 24, 1},
		undoBorder: Vec4{146, 44, 32, 1},
		undoText:   Vec4{0, 0, 98, 1},

		headerBtnBg:     Vec4{42, 34, 99, 1},
		headerBtnHover:  Vec4{40, 26, 94, 1},
		headerBtnBorder: Vec4{38, 18, 85, 1},
	}
}

func kindVisuals(kind PlantKind, dark bool) (IconGlyph, Vec4, Vec4) {
	// Returns (glyph, tileBg, iconColor)
	switch kind {
	case KindFern:
		if dark {
			return TypSpiral, Vec4{158, 38, 22, 1}, Vec4{156, 62, 68, 1}
		}
		return TypSpiral, Vec4{158, 46, 90, 1}, Vec4{158, 54, 30, 1}
	case KindTree:
		if dark {
			return TypTree, Vec4{124, 34, 22, 1}, Vec4{122, 56, 68, 1}
		}
		return TypTree, Vec4{122, 42, 90, 1}, Vec4{124, 48, 28, 1}
	case KindTrailing:
		if dark {
			return TypWaves, Vec4{172, 38, 22, 1}, Vec4{172, 60, 68, 1}
		}
		return TypWaves, Vec4{172, 48, 90, 1}, Vec4{174, 54, 28, 1}
	case KindFlowering:
		if dark {
			return TypStarburst, Vec4{340, 36, 23, 1}, Vec4{342, 68, 74, 1}
		}
		return TypStarburst, Vec4{342, 56, 92, 1}, Vec4{342, 54, 38, 1}
	case KindSucculent:
		if dark {
			return TypWeatherSunny, Vec4{86, 34, 22, 1}, Vec4{86, 56, 68, 1}
		}
		return TypWeatherSunny, Vec4{88, 44, 90, 1}, Vec4{88, 48, 28, 1}
	default:
		if dark {
			return TypLeaf, Vec4{142, 40, 22, 1}, Vec4{140, 64, 68, 1}
		}
		return TypLeaf, Vec4{142, 48, 90, 1}, Vec4{142, 54, 28, 1}
	}
}

func urgencyVisuals(u Urgency, pal botanicalPalette) (accent, pillBg, pillText Vec4) {
	switch u {
	case UrgencyOverdue:
		return pal.overdueAccent, pal.overdueBg, pal.overdueText
	case UrgencyToday:
		return pal.todayAccent, pal.todayBg, pal.todayText
	case UrgencySoon:
		return pal.soonAccent, pal.soonBg, pal.soonText
	default:
		return pal.happyAccent, pal.happyBg, pal.happyText
	}
}

// RootView renders the full mobile-friendly Water Plants interface.
func RootView() {
	SetDarkMode(state.DarkMode)
	pal := currentBotanicalPalette(state.DarkMode)

	summary := ComputeSummary(state.Plants, state.Today)
	visible := SortedPlants(state.Plants, state.Today, state.Filter)

	winW := GetHost().WindowSize[0]
	var shellAttrs AttrsFn = Expand
	if winW > 520 {
		shellAttrs = FixWidth(520)
	}

	Container(Attrs(
		Viewport,
		BackgroundVec(pal.canvasBg),
		AmendTextStyle(TextColorVec(pal.textPrimary)),
		CrossMid,
	), func() {
		Container(Attrs(shellAttrs, Grow(1), Extrinsic, Gap(10)), func() {
			Container(Attrs(Expand, Pad4(14, 14, 0, 14), Gap(10)), func() {
				headerView(pal)
				summaryCardView(pal, summary)
				filterBarView(pal, summary)

				if state.SaveErr != "" {
					errorBannerView(pal, state.SaveErr)
				}
				if state.LastUndo != nil && state.LastUndo.Plant != nil {
					undoBannerView(pal, state.LastUndo)
				}
			})

			plantListView(pal, visible)
		})

		if state.Selected != nil {
			detailModalView(pal, state.Selected)
		} else if state.ShowAdd {
			addPlantModalView(pal)
		}
	})
}

func headerView(pal botanicalPalette) {
	Container(Attrs(Row, Expand, CrossMid, Gap(10)), func() {
		// Brand icon badge
		Container(Attrs(
			FixSize(38, 38), Corners(11), Center,
			Background(146, 52, 36, 1), Grad(8, 4, -8, 0),
		), func() {
			Icon(TypLeaf, FontSize(22), TextColor(0, 0, 100, 1))
		})

		Container(Attrs(Grow(1), Gap(1)), func() {
			Label("Water Plants", FontSize(19), FontWeight(WeightBold), TextColorVec(pal.textPrimary))
			Label(state.Today.Format("Monday, Jan 2"), FontSize(12), TextColorVec(pal.textMuted))
		})

		// Light / Dark toggle button
		themeIcon := TypWeatherNight
		if state.DarkMode {
			themeIcon = TypWeatherSunny
		}
		if iconActionButton("toggle-theme", themeIcon, pal.headerBtnBg, pal.headerBtnHover, pal.headerBtnBorder, pal.textPrimary) {
			state.DarkMode = !state.DarkMode
		}

		// Add plant primary button
		if iconActionButton("add-plant", TypPlus, Vec4{146, 50, 38, 1}, Vec4{146, 54, 33, 1}, Vec4{146, 50, 34, 1}, Vec4{0, 0, 100, 1}) {
			state.openAdd()
		}
	})
}

func iconActionButton(accessName string, glyph IconGlyph, bg, hoverBg, border, fg Vec4) bool {
	clicked := false
	Container(Attrs(
		FixSize(38, 38), Corners(11), Center,
		BackgroundVec(bg), BorderWidth(1), BorderColorVec(border),
	), func() {
		NextAccessName(accessName)
		AssignAccess()
		st := ProcessButtonEvents(false)
		if st.Hovered || st.Active {
			ModAttrs(BackgroundVec(hoverBg))
		}
		if st.FocusVisible {
			ModAttrs(BorderWidth(2), BorderColorVec(CurrentColorScheme.FocusRing))
		}
		Icon(glyph, FontSize(19), TextColorVec(fg))
		clicked = st.Clicked
	})
	return clicked
}

func summaryCardView(pal botanicalPalette, sum SummaryCounts) {
	Container(Attrs(
		Expand, Corners(14), Pad2(11, 13), Gap(8),
		BackgroundVec(pal.summaryBg),
		BorderWidth(1), BorderColorVec(pal.summaryBorder),
	), func() {
		Container(Attrs(Row, Expand, CrossMid, Gap(8)), func() {
			// Status dot + headline
			var headline string
			var dotColor Vec4
			switch {
			case sum.Total == 0:
				headline = "No houseplants yet"
				dotColor = pal.textSubtle
			case sum.Thirsty == 0:
				headline = "All plants hydrated"
				dotColor = pal.happyAccent
			case sum.Thirsty == 1:
				headline = "1 plant needs water"
				if sum.Overdue > 0 {
					dotColor = pal.overdueAccent
				} else {
					dotColor = pal.todayAccent
				}
			default:
				headline = fmt.Sprintf("%d plants need water", sum.Thirsty)
				if sum.Overdue > 0 {
					dotColor = pal.overdueAccent
				} else {
					dotColor = pal.todayAccent
				}
			}

			Element(Attrs(FixSize(9, 9), Corners(5), BackgroundVec(dotColor)))
			Label(headline, FontSize(14), FontWeight(WeightSemibold), TextColorVec(pal.textPrimary))

			Filler(1)

			if sum.WateredToday > 0 {
				Container(Attrs(Row, CrossMid, Gap(3), Pad2(2, 7), Corners(99), BackgroundVec(pal.happyBg)), func() {
					Icon(TypTick, FontSize(12), TextColorVec(pal.happyText))
					Label(fmt.Sprintf("%d today", sum.WateredToday), FontSize(11), FontWeight(WeightSemibold), TextColorVec(pal.happyText))
				})
			}
		})

		// Compact status breakdown + hydration bar
		Container(Attrs(Row, Expand, CrossMid, Gap(10)), func() {
			if sum.Overdue > 0 {
				miniCountTag(fmt.Sprintf("%d overdue", sum.Overdue), pal.overdueAccent, pal.textMuted)
			}
			if sum.DueToday > 0 {
				miniCountTag(fmt.Sprintf("%d today", sum.DueToday), pal.todayAccent, pal.textMuted)
			}
			if sum.Soon > 0 {
				miniCountTag(fmt.Sprintf("%d soon", sum.Soon), pal.soonAccent, pal.textMuted)
			}
			miniCountTag(fmt.Sprintf("%d happy", sum.Happy), pal.happyAccent, pal.textMuted)
		})

		if sum.Total > 0 {
			hydratedFrac := float32(sum.Total-sum.Thirsty) / float32(sum.Total)
			Container(Attrs(
				Expand, FixHeight(5), Corners(3),
				BackgroundVec(pal.trackBg), Clip, NoAnimate,
			), func() {
				if w := GetContentWidth(); w > 0 && hydratedFrac > 0 {
					fillW := w * hydratedFrac
					if fillW < 6 {
						fillW = 6
					}
					barCol := pal.happyAccent
					if sum.Overdue > 0 {
						barCol = pal.soonAccent
					}
					Element(Attrs(FixSize(fillW, 5), Corners(3), BackgroundVec(barCol), NoAnimate))
				}
			})
		}
	})
}

func miniCountTag(label string, dotCol, textCol Vec4) {
	Container(Attrs(Row, CrossMid, Gap(4)), func() {
		Element(Attrs(FixSize(6, 6), Corners(3), BackgroundVec(dotCol)))
		Label(label, FontSize(11.5), TextColorVec(textCol))
	})
}

func filterBarView(pal botanicalPalette, sum SummaryCounts) {
	type tabSpec struct {
		tab   FilterTab
		name  string
		label string
		count int
	}
	tabs := []tabSpec{
		{FilterAll, "filter-all", "All", sum.Total},
		{FilterThirsty, "filter-thirsty", "Thirsty", sum.Thirsty},
		{FilterSoon, "filter-soon", "Soon", sum.Soon},
		{FilterHappy, "filter-happy", "Happy", sum.Happy},
	}

	Container(Attrs(
		Row, Expand, Pad(3), Corners(11), Gap(3),
		BackgroundVec(pal.segBg),
	), func() {
		for _, t := range tabs {
			active := state.Filter == t.tab
			bg := Vec4{0, 0, 0, 0}
			fg := pal.textMuted
			weight := WeightMedium
			if active {
				bg = pal.segActiveBg
				fg = pal.segActiveText
				weight = WeightSemibold
			}

			ContainerWithKey(t.tab, Attrs(
				Row, Grow(1), FixHeight(34), Corners(8), Center, Gap(4),
				BackgroundVec(bg),
			), func() {
				NextAccessName(t.name)
				AssignAccess()
				st := ProcessButtonEvents(false)
				if st.Clicked {
					state.Filter = t.tab
				}
				if !active && st.Hovered {
					ModAttrs(BackgroundVec(pal.cardHoverBg))
				}
				if st.FocusVisible {
					ModAttrs(BorderWidth(2), BorderColorVec(CurrentColorScheme.FocusRing))
				}

				Label(t.label, FontSize(12), FontWeight(weight), TextColorVec(fg))
				if t.count > 0 {
					countCol := pal.textSubtle
					if active {
						countCol = pal.happyAccent
						if t.tab == FilterThirsty && sum.Overdue > 0 {
							countCol = pal.overdueAccent
						}
					} else if t.tab == FilterThirsty {
						countCol = pal.overdueAccent
					}
					Label(fmt.Sprintf("%d", t.count), FontSize(11), FontWeight(WeightBold), TextColorVec(countCol))
				}
			})
		}
	})
}

func errorBannerView(pal botanicalPalette, msg string) {
	Container(Attrs(
		Row, Expand, CrossMid, Pad2(8, 12), Corners(10), Gap(8),
		BackgroundVec(pal.overdueBg), BorderWidth(1), BorderColorVec(pal.overdueAccent),
	), func() {
		Icon(TypWarning, FontSize(15), TextColorVec(pal.overdueText))
		Label("Save error: "+msg, FontSize(12), TextColorVec(pal.overdueText))
	})
}

func undoBannerView(pal botanicalPalette, undo *UndoRecord) {
	Container(Attrs(
		Row, Expand, CrossMid, Pad2(8, 12), Corners(11), Gap(8),
		BackgroundVec(pal.undoBg), BorderWidth(1), BorderColorVec(pal.undoBorder),
	), func() {
		Icon(TypTick, FontSize(15), TextColorVec(pal.undoText))
		Container(Attrs(Grow(1)), func() {
			Label("Watered "+undo.Plant.Name, FontSize(12.5), FontWeight(WeightMedium), TextColorVec(pal.undoText))
		})

		Container(Attrs(
			Pad2(4, 10), Corners(7), Center,
			Background(0, 0, 100, 0.14),
		), func() {
			NextAccessName("undo-water")
			AssignAccess()
			st := ProcessButtonEvents(false)
			if st.Hovered {
				ModAttrs(Background(0, 0, 100, 0.24))
			}
			if st.Clicked {
				state.UndoLastWatering()
			}
			Label("Undo", FontSize(12), FontWeight(WeightBold), TextColorVec(pal.undoText))
		})
	})
}

func plantListView(pal botanicalPalette, visible []*Plant) {
	Container(Attrs(Viewport, Grow(1), Expand), func() {
		ScrollOnInput()

		Container(Attrs(Expand, Gap(9), Pad4(1, 14, 14, 14)), func() {
			if len(visible) == 0 {
				emptyListView(pal)
			} else {
				for _, p := range visible {
					plantCardView(pal, p)
				}
			}
		})

		ScrollBars()
	})
}

func emptyListView(pal botanicalPalette) {
	msg := "No plants in this view"
	sub := "Tap + in the top right to add a houseplant."
	if state.Filter == FilterThirsty && len(state.Plants) > 0 {
		msg = "Nothing is thirsty right now"
		sub = "All your houseplants are on schedule."
	} else if state.Filter == FilterSoon && len(state.Plants) > 0 {
		msg = "No plants due in the next 3 days"
		sub = "Switch to All to inspect the full schedule."
	}

	Container(Attrs(
		Expand, Pad2(36, 20), Corners(14), Center, Gap(8),
		BackgroundVec(pal.cardBg), BorderWidth(1), BorderColorVec(pal.cardBorder),
	), func() {
		Icon(TypLeaf, FontSize(30), TextColorVec(pal.happyAccent))
		Label(msg, FontSize(15), FontWeight(WeightSemibold), TextColorVec(pal.textPrimary))
		Label(sub, FontSize(12.5), TextColorVec(pal.textMuted))
	})
}

func plantCardView(pal botanicalPalette, p *Plant) {
	ps := p.Status(state.Today)
	accent, pillBg, pillText := urgencyVisuals(ps.Urgency, pal)
	glyph, tileBg, tileFg := kindVisuals(p.Kind, state.DarkMode)

	cardBg := pal.cardBg
	cardBorder := pal.cardBorder
	if ps.WateredToday {
		cardBg = pal.cardDoneBg
		cardBorder = pal.cardDoneBorder
	}

	ContainerWithKey(p, Attrs(
		Expand, Corners(14),
		BackgroundVec(cardBg),
		BorderWidth(1), BorderColorVec(cardBorder),
		Pad4(11, 11, 10, 10), Gap(9),
	), func() {
		// Main row: urgency stripe + tappable plant info + one-tap Water button
		Container(Attrs(Row, Expand, CrossMid, Gap(9)), func() {
			// Subtle vertical urgency accent bar
			Element(Attrs(
				FixSize(4, 52), Corners(2),
				BackgroundVec(accent),
			))

			// Tappable info area opens plant details / editing
			Container(Attrs(Row, Grow(1), CrossMid, Gap(10), Corners(10), Pad2(2, 2)), func() {
				NextAccessName("card-" + p.ID)
				AssignAccess()
				infoBtn := ProcessButtonEvents(false)
				if infoBtn.Hovered {
					ModAttrs(BackgroundVec(pal.cardHoverBg))
				}
				if infoBtn.FocusVisible {
					ModAttrs(BorderWidth(2), BorderColorVec(CurrentColorScheme.FocusRing))
				}
				if infoBtn.Clicked {
					state.openDetail(p)
				}

				// Plant icon tile
				Container(Attrs(
					FixSize(40, 40), Corners(11), Center,
					BackgroundVec(tileBg),
				), func() {
					Icon(glyph, FontSize(21), TextColorVec(tileFg))
				})

				// Name + room/cadence + due pill
				Container(Attrs(Grow(1), Gap(3)), func() {
					Label(p.Name, FontSize(14.5), FontWeight(WeightBold), TextColorVec(pal.textPrimary))

					Label(fmt.Sprintf("%s · every %dd", p.Location, p.IntervalDays),
						FontSize(11.5), TextColorVec(pal.textMuted))

					// Due status pill
					Container(Attrs(Row, CrossMid, Gap(4), Pad2(2, 7), Corners(99), BackgroundVec(pillBg)), func() {
						var pillIcon IconGlyph
						switch ps.Urgency {
						case UrgencyOverdue:
							pillIcon = TypWarning
						case UrgencyToday:
							pillIcon = TypTime
						case UrgencyWateredToday:
							pillIcon = TypTick
						default:
							pillIcon = TypCalendar
						}
						Icon(pillIcon, FontSize(11.5), TextColorVec(pillText))
						Label(ps.DueHeadline(), FontSize(11), FontWeight(WeightSemibold), TextColorVec(pillText))
					})
				})
			})

			// One-tap Water / Done action button (generous mobile touch target)
			oneTapWaterButton(pal, p, ps)
		})

		// Bottom moisture progress track + last/next micro-captions
		Container(Attrs(Expand, Gap(4), Pad4(0, 2, 0, 12)), func() {
			Container(Attrs(
				Expand, FixHeight(4), Corners(2),
				BackgroundVec(pal.trackBg), Clip, NoAnimate,
			), func() {
				if w := GetContentWidth(); w > 0 {
					fillFrac := ps.MoistureFrac
					fillCol := accent
					if ps.Urgency == UrgencyOverdue || ps.Urgency == UrgencyToday {
						// Small visible stub so overdue bars read as depleted, not missing
						fillFrac = 0.08
					}
					fillW := w * fillFrac
					if fillW < 6 {
						fillW = 6
					}
					Element(Attrs(FixSize(fillW, 4), Corners(2), BackgroundVec(fillCol), NoAnimate))
				}
			})

			Container(Attrs(Row, Expand, CrossMid), func() {
				Label(ps.LastWateredCaption(), FontSize(10.5), TextColorVec(pal.textSubtle))
				Filler(1)
				Label(ps.NextDateCaption(), FontSize(10.5), FontWeight(WeightMedium), TextColorVec(pal.textMuted))
			})
		})
	})
}

func oneTapWaterButton(pal botanicalPalette, p *Plant, ps PlantStatus) {
	isThirsty := ps.Urgency == UrgencyOverdue || ps.Urgency == UrgencyToday

	var bg, hoverBg, activeBg, border, fg Vec4
	var icon IconGlyph
	var text string
	var borderW float32 = 1

	switch {
	case ps.WateredToday:
		bg = pal.doneBtnBg
		hoverBg = pal.doneBtnHover
		activeBg = pal.doneBtnHover
		border = pal.doneBtnBorder
		fg = pal.doneBtnText
		icon = TypTick
		text = "Done"
	case isThirsty:
		bg = pal.waterPrimaryBg
		hoverBg = pal.waterPrimaryHover
		activeBg = pal.waterPrimaryActive
		border = pal.waterPrimaryBg
		fg = pal.waterPrimaryText
		icon = TypWeatherShower
		text = "Water"
		borderW = 0
	default:
		bg = pal.waterSoftBg
		hoverBg = pal.waterSoftHover
		activeBg = pal.waterSoftHover
		border = pal.waterSoftBorder
		fg = pal.waterSoftText
		icon = TypWeatherShower
		text = "Water"
	}

	Container(Attrs(
		FixSize(74, 48), Corners(12), Center, Gap(2),
		BackgroundVec(bg),
		BorderWidth(borderW), BorderColorVec(border),
	), func() {
		if isThirsty {
			ModAttrs(Grad(-8, 4, -6, 0))
		}
		NextAccessName("water-" + p.ID)
		AssignAccess()
		st := ProcessButtonEvents(false)
		if st.Active {
			ModAttrs(BackgroundVec(activeBg))
		} else if st.Hovered {
			ModAttrs(BackgroundVec(hoverBg))
		}
		if st.FocusVisible {
			ModAttrs(BorderWidth(2), BorderColorVec(CurrentColorScheme.FocusRing))
		}
		if st.Clicked {
			state.ToggleWaterToday(p)
		}

		Icon(icon, FontSize(16), TextColorVec(fg))
		Label(text, FontSize(11.5), FontWeight(WeightBold), TextColorVec(fg))
	})
}

func modalWidth() float32 {
	w := GetHost().WindowSize[0] - 28
	if w > 380 {
		w = 380
	}
	if w < 280 {
		w = 280
	}
	return w
}

func detailModalView(pal botanicalPalette, p *Plant) {
	ps := p.Status(state.Today)
	_, pillBg, pillText := urgencyVisuals(ps.Urgency, pal)

	Modal(modalWidth(), func() { state.closeDetail() }, func() {
		// Modal header
		Container(Attrs(Row, Expand, CrossMid, Gap(8)), func() {
			Label(p.Name, FontSize(16), FontWeight(WeightBold))
			Filler(1)
			if CtrlButton(SymICross, "Close", true) {
				state.closeDetail()
			}
		})

		// Schedule overview strip
		Container(Attrs(
			Row, Expand, CrossMid, Pad2(8, 10), Corners(9), Gap(8),
			BackgroundVec(pillBg),
		), func() {
			Container(Attrs(Grow(1), Gap(1)), func() {
				Label(ps.DueHeadline(), FontSize(13), FontWeight(WeightBold), TextColorVec(pillText))
				Label(fmt.Sprintf("%s · %s · %d total", ps.LastWateredCaption(), ps.NextDateCaption(), p.WaterCount),
					FontSize(11), TextColorVec(pillText))
			})
			if !ps.WateredToday {
				NextButtonType(ButtonPrimary)
				if CtrlButton(TypWeatherShower, "Water", true) {
					state.WaterNow(p)
				}
			} else if p.PrevLastWatered != "" {
				if CtrlButton(SymUndo, "Undo", true) {
					state.ToggleWaterToday(p)
				}
			}
		})

		plantFormFields(pal, &state.EditName, &state.EditLocation, &state.EditInterval, &state.EditKind, &state.EditLight, &state.EditNotes)

		// Action footer
		Container(Attrs(Row, Expand, CrossMid, Gap(8), Pad4(4, 0, 0, 0)), func() {
			if state.ConfirmDelete {
				NextButtonType(ButtonDestructive)
				if CtrlButton(SymDelete, "Confirm Delete", true) {
					state.deleteSelected()
				}
				if CtrlButton(NoIcon, "Keep", true) {
					state.ConfirmDelete = false
				}
			} else {
				NextButtonType(ButtonDestructive)
				if CtrlButton(SymDelete, "Delete", true) {
					state.ConfirmDelete = true
				}
			}

			Filler(1)

			NextButtonType(ButtonPrimary)
			NextButtonDisabled(strings.TrimSpace(state.EditName) == "")
			if CtrlButton(SymPass, "Save", true) {
				state.saveDetail()
			}
		})
	})
}

func addPlantModalView(pal botanicalPalette) {
	Modal(modalWidth(), func() { state.ShowAdd = false }, func() {
		Container(Attrs(Row, Expand, CrossMid, Gap(8)), func() {
			Icon(TypLeaf, FontSize(18), TextColorVec(pal.happyAccent))
			Label("Add Houseplant", FontSize(16), FontWeight(WeightBold))
			Filler(1)
			if CtrlButton(SymICross, "Close", true) {
				state.ShowAdd = false
			}
		})

		plantFormFields(pal, &state.AddName, &state.AddLocation, &state.AddInterval, &state.AddKind, &state.AddLight, &state.AddNotes)

		Container(Attrs(Row, Expand, CrossMid, Gap(8), Pad4(4, 0, 0, 0)), func() {
			if CtrlButton(NoIcon, "Cancel", true) {
				state.ShowAdd = false
			}
			Filler(1)
			NextButtonType(ButtonPrimary)
			NextButtonDisabled(strings.TrimSpace(state.AddName) == "")
			if CtrlButton(SymPlus, "Add Plant", true) {
				state.submitAdd()
			}
		})
	})
}

func plantFormFields(pal botanicalPalette, name, location *string, interval *int, kind *PlantKind, light *LightLevel, notes *string) {
	// Name
	Container(Attrs(Expand, Gap(4)), func() {
		Label("Name", FontSize(11.5), FontWeight(WeightSemibold), TextColorVec(pal.textMuted))
		attrs := DefaultTextInputAttrs()
		attrs.Placeholder = "e.g., Monstera Deliciosa"
		attrs.NoAutoFocus = true
		TextInputExt(name, attrs)
	})

	// Room / Location
	Container(Attrs(Expand, Gap(4)), func() {
		Label("Room / Location", FontSize(11.5), FontWeight(WeightSemibold), TextColorVec(pal.textMuted))
		attrs := DefaultTextInputAttrs()
		attrs.Placeholder = "e.g., Living Room"
		attrs.NoAutoFocus = true
		TextInputExt(location, attrs)
	})

	// Watering interval presets + stepper
	Container(Attrs(Expand, Gap(5)), func() {
		Container(Attrs(Row, Expand, CrossMid), func() {
			Label("Water every", FontSize(11.5), FontWeight(WeightSemibold), TextColorVec(pal.textMuted))
			Filler(1)
			if CtrlButton(SymMinus, "", *interval > 1) {
				*interval--
			}
			Spacer(6)
			Label(fmt.Sprintf("%d days", *interval), FontSize(13), FontWeight(WeightBold))
			Spacer(6)
			if CtrlButton(SymPlus, "", *interval < 90) {
				*interval++
			}
		})

		presets := []int{3, 5, 7, 10, 14}
		Container(Attrs(Row, Expand, Gap(5)), func() {
			for _, d := range presets {
				selected := *interval == d
				bg := pal.segBg
				fg := pal.textMuted
				if selected {
					bg = pal.happyBg
					fg = pal.happyText
				}
				ContainerWithKey(d, Attrs(
					Grow(1), FixHeight(28), Corners(7), Center,
					BackgroundVec(bg),
				), func() {
					st := ProcessButtonEvents(false)
					if st.Clicked {
						*interval = d
					}
					Label(fmt.Sprintf("%dd", d), FontSize(11.5), FontWeight(WeightSemibold), TextColorVec(fg))
				})
			}
		})
	})

	// Light selector
	Container(Attrs(Expand, Gap(4)), func() {
		Label("Light", FontSize(11.5), FontWeight(WeightSemibold), TextColorVec(pal.textMuted))
		Container(Attrs(Row, Expand, Gap(5)), func() {
			for _, lv := range AllLightLevels {
				selected := *light == lv
				bg := pal.segBg
				fg := pal.textMuted
				if selected {
					bg = pal.todayBg
					fg = pal.todayText
				}
				ContainerWithKey(lv, Attrs(
					Grow(1), FixHeight(28), Corners(7), Center,
					BackgroundVec(bg),
				), func() {
					st := ProcessButtonEvents(false)
					if st.Clicked {
						*light = lv
					}
					Label(string(lv), FontSize(11.5), FontWeight(WeightSemibold), TextColorVec(fg))
				})
			}
		})
	})

	// Plant style / icon picker
	Container(Attrs(Expand, Gap(4)), func() {
		Label("Plant Type", FontSize(11.5), FontWeight(WeightSemibold), TextColorVec(pal.textMuted))
		Container(Attrs(Row, Expand, Gap(5)), func() {
			for _, k := range AllPlantKinds {
				selected := *kind == k
				glyph, tileBg, tileFg := kindVisuals(k, state.DarkMode)
				bg := pal.segBg
				fg := pal.textMuted
				var bw float32 = 0
				if selected {
					bg = tileBg
					fg = tileFg
					bw = 1.5
				}
				ContainerWithKey(k, Attrs(
					Grow(1), FixHeight(32), Corners(8), Center,
					BackgroundVec(bg), BorderWidth(bw), BorderColorVec(tileFg),
				), func() {
					st := ProcessButtonEvents(false)
					if st.Clicked {
						*kind = k
					}
					Icon(glyph, FontSize(16), TextColorVec(fg))
				})
			}
		})
	})

	// Notes
	Container(Attrs(Expand, Gap(4)), func() {
		Label("Care Notes", FontSize(11.5), FontWeight(WeightSemibold), TextColorVec(pal.textMuted))
		attrs := DefaultTextInputAttrs()
		attrs.Placeholder = "Optional soil, misting, or feeding tip..."
		attrs.NoAutoFocus = true
		TextInputExt(notes, attrs)
	})
}
