package main

import (
	"fmt"
	"strings"
	"time"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

const (
	leftColW float32 = 276
	rulerH   float32 = 46
	rowH     float32 = 68
	barH     float32 = 48
	rowGap   float32 = 8
	trackPad float32 = 16
)

type palette struct {
	mutedText   Vec4
	subtleText  Vec4
	cardBg      Vec4
	cardBaseBg  Vec4
	cardBorder  Vec4
	baseBorder  Vec4
	divider     Vec4
	badgeBg     Vec4
	scrubber    Vec4
	scrubberHot Vec4
	scrubberHal Vec4
	nowColor    Vec4
	overlapPeak Vec4
	overlapGood Vec4
	overlapLow  Vec4
	barBorder   Vec4
}

func currentPalette(dark bool) palette {
	if dark {
		return palette{
			mutedText:   Vec4{34, 10, 68, 1},
			subtleText:  Vec4{34, 8, 54, 1},
			cardBg:      Vec4{32, 8, 19, 1},
			cardBaseBg:  Vec4{34, 24, 22, 1},
			cardBorder:  Vec4{32, 8, 28, 1},
			baseBorder:  Vec4{34, 72, 52, 0.78},
			divider:     Vec4{32, 8, 29, 1},
			badgeBg:     Vec4{32, 9, 24, 1},
			scrubber:    Vec4{12, 86, 58, 1},
			scrubberHot: Vec4{14, 92, 64, 1},
			scrubberHal: Vec4{0, 0, 0, 0.48},
			nowColor:    Vec4{164, 68, 52, 0.85},
			overlapPeak: Vec4{154, 62, 46, 0.95},
			overlapGood: Vec4{176, 46, 42, 0.55},
			overlapLow:  Vec4{32, 8, 26, 0.85},
			barBorder:   Vec4{35, 16, 95, 0.14},
		}
	}
	return palette{
		mutedText:   Vec4{222, 12, 44, 1},
		subtleText:  Vec4{222, 10, 56, 1},
		cardBg:      Vec4{38, 22, 97, 1},
		cardBaseBg:  Vec4{40, 58, 94, 1},
		cardBorder:  Vec4{35, 16, 86, 1},
		baseBorder:  Vec4{28, 76, 56, 0.7},
		divider:     Vec4{35, 14, 88, 1},
		badgeBg:     Vec4{36, 20, 92, 1},
		scrubber:    Vec4{11, 82, 52, 1},
		scrubberHot: Vec4{11, 88, 46, 1},
		scrubberHal: Vec4{0, 0, 0, 0.25},
		nowColor:    Vec4{168, 66, 38, 0.85},
		overlapPeak: Vec4{154, 58, 42, 0.92},
		overlapGood: Vec4{172, 46, 56, 0.55},
		overlapLow:  Vec4{35, 14, 88, 0.85},
		barBorder:   Vec4{225, 25, 20, 0.16},
	}
}

func phaseVisuals(p Phase, dark bool) (IconGlyph, Vec4, Vec4) {
	// Returns (icon, pillBg, pillText)
	switch p {
	case PhaseWork:
		if dark {
			return TypWeatherSunny, Vec4{202, 52, 26, 1}, Vec4{200, 82, 78, 1}
		}
		return TypWeatherSunny, Vec4{202, 74, 91, 1}, Vec4{206, 68, 30, 1}
	case PhaseMorning:
		if dark {
			return TypAdjustBrightness, Vec4{36, 52, 25, 1}, Vec4{40, 86, 74, 1}
		}
		return TypAdjustBrightness, Vec4{40, 82, 90, 1}, Vec4{32, 72, 30, 1}
	case PhaseEvening:
		if dark {
			return TypWeatherPartlySunny, Vec4{18, 48, 25, 1}, Vec4{22, 84, 74, 1}
		}
		return TypWeatherPartlySunny, Vec4{24, 78, 91, 1}, Vec4{18, 70, 34, 1}
	default:
		if dark {
			return TypWeatherNight, Vec4{232, 32, 25, 1}, Vec4{226, 48, 78, 1}
		}
		return TypWeatherNight, Vec4{230, 34, 91, 1}, Vec4{232, 42, 36, 1}
	}
}

// RootView renders the full Sunbelt application frame.
func RootView() {
	SetDarkMode(state.DarkMode)
	pal := currentPalette(state.DarkMode)

	base := state.baseCity()
	baseMid := state.baseMidnight()
	inst := state.scrubbedInstant()
	baseLocal := inst.In(base.Loc)
	slotStats, overlapWindows := ComputeOverlapScores(state.Cities, baseMid)
	curStats := EvaluateInstant(state.Cities, inst)

	Container(Attrs(Viewport, UseSurface(SurfaceCanvas), Pad(16), Gap(12)), func() {
		toolbarView(pal, baseLocal)
		summaryStripView(pal, base, baseLocal, inst, curStats, overlapWindows)
		timelinePanelView(pal, base, baseMid, baseLocal, inst, slotStats)
		legendFooterView(pal)

		if state.ShowAddCity {
			addCityModalView(pal, inst)
		}
	})
}

func toolbarView(pal palette, baseLocal time.Time) {
	Container(Attrs(Row, Expand, CrossMid, Gap(10)), func() {
		// Brand title
		Container(Attrs(Row, CrossMid, Gap(9)), func() {
			Container(Attrs(
				FixSize(30, 30), Corners(8), Center,
				Background(32, 86, 54, 1), Grad(-12, 4, -8, 0),
			), func() {
				Icon(TypWeatherSunny, FontSize(19), TextColor(0, 0, 100, 1))
			})
			Container(Attrs(Gap(0)), func() {
				Label("Sunbelt", FontSize(17), FontWeight(WeightBold))
			})
		})

		Spacer(4)

		// Date stepper + Now jump
		Container(Attrs(Row, CrossMid, Gap(4)), func() {
			NextAccessName("prev-day")
			if CtrlButton(SymLeft, "", true) {
				state.shiftDay(-1)
			}
			Container(Attrs(
				Pad2(5, 11), Corners(6),
				UseSurface(SurfacePanel),
				BorderWidth(1), BorderColorVec(pal.cardBorder),
			), func() {
				Label(baseLocal.Format("Mon, Jan 2, 2006"), FontSize(12), FontWeight(WeightSemibold))
			})
			NextAccessName("next-day")
			if CtrlButton(SymRight, "", true) {
				state.shiftDay(1)
			}
			Spacer(2)
			NextAccessName("jump-now")
			if CtrlButton(SymClock, "Now", true) {
				state.jumpToInstant(state.Now)
			}
		})

		Filler(1)

		// 24h / 12h toggle
		SegmentedControl(&state.Use24h, func() {
			SegmentedCell("24h", true)
			SegmentedCell("12h", false)
		})

		// Snap step selector
		SegmentedControl(&state.SnapStep, func() {
			SegmentedCell("15m", 15)
			SegmentedCell("30m", 30)
			SegmentedCell("1h", 60)
		})

		// Theme toggle
		themeIcon := TypWeatherNight
		if state.DarkMode {
			themeIcon = TypWeatherSunny
		}
		NextAccessName("toggle-theme")
		if CtrlButton(themeIcon, "", true) {
			state.DarkMode = !state.DarkMode
		}

		// Add city button
		NextAccessName("add-city")
		NextButtonType(ButtonPrimary)
		if Button(SymPlus, "Add City") {
			state.ShowAddCity = true
			state.CitySearch = ""
		}
	})
}

func summaryStripView(pal palette, base *City, baseLocal, inst time.Time, stats SlotStats, windows []OverlapWindow) {
	Container(Attrs(
		Row, Expand, CrossMid,
		Pad2(10, 14), Gap(14), Corners(10),
		UseSurface(SurfacePanel),
		BorderWidth(1), BorderColorVec(pal.cardBorder),
	), func() {
		// Current scrubbed instant in base city + UTC
		Container(Attrs(Row, CrossMid, Gap(10)), func() {
			Container(Attrs(
				FixSize(10, 10), Corners(5),
				BackgroundVec(pal.scrubber),
			), func() {})
			Label(FormatTime(baseLocal, state.Use24h), FontSize(18), FontWeight(WeightBold))
			zone, _ := baseLocal.Zone()
			Label(fmt.Sprintf("%s (%s)", base.Name, zone), FontSize(13), FontWeight(WeightSemibold))
			Label("·", FontSize(13), TextColorVec(pal.subtleText))
			Label(fmt.Sprintf("%s UTC", FormatTime(inst.UTC(), state.Use24h)), FontSize(12), TextColorVec(pal.mutedText))
		})

		// Live overlap counts pill
		Container(Attrs(
			Row, CrossMid, Gap(8),
			Pad2(4, 10), Corners(12),
			BackgroundVec(pal.badgeBg),
		), func() {
			statDotLabel(Vec4{154, 58, 44, 1}, fmt.Sprintf("%d working", stats.Working), pal.mutedText)
			if awakeOther := stats.Awake - stats.Working; awakeOther > 0 {
				statDotLabel(Vec4{36, 80, 52, 1}, fmt.Sprintf("%d awake", awakeOther), pal.mutedText)
			}
			if stats.Sleeping > 0 {
				statDotLabel(Vec4{230, 40, 52, 1}, fmt.Sprintf("%d night", stats.Sleeping), pal.mutedText)
			}
		})

		Filler(1)

		// Best overlap jump pills
		if len(windows) > 0 {
			Container(Attrs(Row, CrossMid, Gap(6)), func() {
				Label("Best overlap:", FontSize(12), TextColorVec(pal.mutedText))
				shown := len(windows)
				if shown > 2 {
					shown = 2
				}
				for i := 0; i < shown; i++ {
					w := windows[i]
					label := fmt.Sprintf("%s – %s", FormatMinutes(w.StartMin, state.Use24h), FormatMinutes(w.EndMin, state.Use24h))
					ContainerWithKey(w.StartMin, Attrs(
						Pad2(4, 9), Corners(6),
						BorderWidth(1),
						BorderColor(154, 52, 44, 0.45),
						Background(154, 52, 44, 0.14),
					), func() {
						bst := ProcessButtonEvents(false)
						if bst.Hovered {
							ModAttrs(Background(154, 58, 44, 0.24))
						}
						if bst.Active {
							ModAttrs(Background(154, 58, 44, 0.34))
						}
						if bst.Clicked {
							mid := w.MidMin()
							step := float32(state.SnapStep)
							if step > 0 {
								mid = float32(int((mid+step/2)/step)) * step
							}
							state.ScrubMin = mid
						}
						textCol := Vec4{156, 62, 28, 1}
						if state.DarkMode {
							textCol = Vec4{154, 68, 72, 1}
						}
						Label(label, FontSize(12), FontWeight(WeightSemibold), TextColorVec(textCol))
					})
				}
			})
		}
	})
}

func statDotLabel(dotColor Vec4, text string, textColor Vec4) {
	Container(Attrs(Row, CrossMid, Gap(4)), func() {
		Element(Attrs(FixSize(7, 7), Corners(3.5), BackgroundVec(dotColor)))
		Label(text, FontSize(11), FontWeight(WeightSemibold), TextColorVec(textColor))
	})
}

func timelinePanelView(pal palette, base *City, baseMid, baseLocal, inst time.Time, slotStats []SlotStats) {
	Container(Attrs(
		Grow(1), Expand, Extrinsic, Clip,
		Corners(12), UseSurface(SurfacePanel),
		BorderWidth(1), BorderColorVec(pal.cardBorder),
	), func() {
		// Inner scrollable viewport keeps scrollbar cleanly inside rounded border.
		Container(Attrs(Grow(1), Expand, Extrinsic, Clip, Pad4(12, 14, 12, 14)), func() {
			ScrollOnInput()

			Container(Attrs(Row, Expand, Gap(14)), func() {
				// Left column: city cards
				Container(Attrs(FixWidth(leftColW), Gap(rowGap)), func() {
					// Header aligned with ruler height
					Container(Attrs(Expand, FixHeight(rulerH), MainAlign(AlignEnd), Pad4(0, 4, 8, 4)), func() {
						Container(Attrs(Row, Expand, CrossMid), func() {
							Label("CITIES", FontSize(11), FontWeight(WeightBold), TextColorVec(pal.mutedText))
							Filler(1)
							Container(Attrs(Row, CrossMid, Gap(3)), func() {
								Icon(TypStar, FontSize(11), TextColor(36, 86, 48, 1))
								Label("axis city", FontSize(11), TextColorVec(pal.subtleText))
							})
						})
					})

					for i, city := range state.Cities {
						cityCardView(pal, city, i, baseLocal, inst)
					}
				})

				// Right column: continuous 24h ruler + bars + unified vertical scrubber
				timelineTracksView(pal, baseMid, slotStats)
			})

			ScrollBars()
		})
	})
}

func cityCardView(pal palette, city *City, index int, baseLocal, inst time.Time) {
	isBase := city.ID == state.BaseID
	lt := inst.In(city.Loc)
	dayOff := DayOffset(lt, baseLocal)
	phase := LocalPhase(lt)
	phaseIcon, phaseBg, phaseFg := phaseVisuals(phase, state.DarkMode)

	bg := pal.cardBg
	border := pal.cardBorder
	if isBase {
		bg = pal.cardBaseBg
		border = pal.baseBorder
	}

	ContainerWithKey(city, Attrs(
		Expand, FixHeight(rowH),
		Pad2(8, 10), Corners(9),
		BackgroundVec(bg),
		BorderWidth(1), BorderColorVec(border),
		MainAlign(AlignMiddle), Gap(4),
	), func() {
		hovered := IsHovered()

		// Top row: star pin, city name, offset/actions
		Container(Attrs(Row, Expand, CrossMid, Gap(5)), func() {
			// Star button to make this city the reference axis
			Container(Attrs(FixSize(18, 18), Corners(4), Center), func() {
				bst := ProcessButtonEvents(false)
				NextAccessName("pin-" + city.ID)
				NextAccessLabel("Set " + city.Name + " as reference axis")
				AssignAccess()
				if bst.Hovered {
					ModAttrs(Background(36, 75, 52, 0.18))
				}
				if bst.Clicked {
					state.setBaseCity(city.ID)
				}
				if isBase {
					Icon(TypStar, FontSize(14), TextColor(36, 86, 48, 1))
				} else {
					Icon(TypStarOutline, FontSize(14), TextColorVec(pal.subtleText))
				}
			})

			Label(city.Name, FontSize(13.5), FontWeight(WeightBold))

			Filler(1)

			if hovered {
				// Reorder / remove controls on hover
				Container(Attrs(Row, CrossMid, Gap(2)), func() {
					if miniIconBtn(SymUp, index == 0, pal, "Move up") {
						state.moveCity(index, -1)
					}
					if miniIconBtn(SymDown, index == len(state.Cities)-1, pal, "Move down") {
						state.moveCity(index, 1)
					}
					if miniIconBtn(SymICross, len(state.Cities) <= 1, pal, "Remove city") {
						state.removeCity(city.ID)
					}
				})
			} else {
				zone, _ := lt.Zone()
				rel := FormatRelOffset(lt, baseLocal)
				meta := fmt.Sprintf("%s · %s", zone, FormatUTCOffset(lt))
				if !isBase {
					meta = fmt.Sprintf("%s · %s", rel, FormatUTCOffset(lt))
				}
				Label(meta, FontSize(11), TextColorVec(pal.mutedText))
			}
		})

		// Bottom row: scrubbed local time + day badge + phase badge
		Container(Attrs(Row, Expand, CrossMid, Gap(6)), func() {
			Label(FormatTime(lt, state.Use24h), FontSize(17.5), FontWeight(WeightBold))

			if dayOff != 0 {
				tag := fmt.Sprintf("+%dd", dayOff)
				if dayOff < 0 {
					tag = fmt.Sprintf("%dd", dayOff)
				}
				Container(Attrs(
					Pad2(1, 5), Corners(4),
					Background(260, 48, 52, 0.16),
				), func() {
					dayFg := Vec4{262, 54, 40, 1}
					if state.DarkMode {
						dayFg = Vec4{260, 72, 78, 1}
					}
					Label(tag, FontSize(10), FontWeight(WeightBold), TextColorVec(dayFg))
				})
			}

			Filler(1)

			// Phase pill
			Container(Attrs(
				Row, CrossMid, Gap(3),
				Pad2(2, 7), Corners(10),
				BackgroundVec(phaseBg),
			), func() {
				Icon(phaseIcon, FontSize(12), TextColorVec(phaseFg))
				Label(phase.Label(), FontSize(10.5), FontWeight(WeightSemibold), TextColorVec(phaseFg))
			})
		})
	})
}

func miniIconBtn(icon IconGlyph, disabled bool, pal palette, accessLabel string) bool {
	var clicked bool
	Container(Attrs(FixSize(18, 18), Corners(4), Center), func() {
		st := ProcessButtonEvents(disabled)
		NextAccessLabel(accessLabel)
		AssignAccess()
		if !disabled && st.Hovered {
			ModAttrs(BackgroundVec(pal.badgeBg))
		}
		clicked = st.Clicked
		col := pal.mutedText
		if disabled {
			col = Vec4{col[0], col[1], col[2], 0.3}
		}
		Icon(icon, FontSize(12), TextColorVec(col))
	})
	return clicked
}

func timelineTracksView(pal palette, baseMid time.Time, slotStats []SlotStats) {
	Container(Attrs(Grow(1), Gap(rowGap), NoAnimate), func() {
		trackW := GetResolvedWidth()
		st := ProcessSlider(&state.ScrubMin, SliderConfig{
			Min:         0,
			Max:         1440,
			Step:        float32(state.SnapStep),
			Width:       trackW,
			HandleInset: trackPad,
		})
		NextAccessName("scrubber")
		NextAccessRole("slider")
		NextAccessLabel("24-hour timezone scrubber")
		AssignAccess()

		innerW := trackW - 2*trackPad
		hasLayout := innerW > 20

		// 1. Top Ruler + Overlap Quality Ribbon
		rulerView(pal, trackW, innerW, hasLayout, slotStats)

		// 2. City 24-hour Solar Bars
		for _, city := range state.Cities {
			cityBarView(pal, city, baseMid, innerW, hasLayout)
		}

		// 3. Full-height "NOW" indicator + Vertical Scrubber Overlay
		if hasLayout {
			totalH := rulerH + float32(len(state.Cities))*(rowH+rowGap)
			if len(state.Cities) > 0 {
				// Stop right at the bottom of the last bar.
				totalH -= (rowH - barH) / 2
			}

			// Subtle NOW line if current time falls within the displayed base day.
			if nowMin, ok := state.nowOffsetMinutes(); ok {
				nowX := trackPad + (nowMin/1440.0)*innerW
				Element(Attrs(
					Float(nowX-0.75, rulerH-12),
					FixSize(1.5, totalH-(rulerH-12)),
					BackgroundVec(pal.nowColor),
					ClickThrough, NoAnimate,
				))
				Element(Attrs(
					Float(nowX-3, rulerH-15),
					FixSize(6, 6), Corners(3),
					BackgroundVec(pal.nowColor),
					ClickThrough, NoAnimate,
				))
			}

			// Interactive vertical scrubber beam + nodes + handle pill.
			scrubberX := trackPad + st.T*innerW
			beamColor := pal.scrubber
			if st.Active || st.Hovered {
				beamColor = pal.scrubberHot
			}

			// Subtle contrast halo behind beam so it reads crisply on both day & night segments.
			Element(Attrs(
				Float(scrubberX-2.5, 24),
				FixSize(5, totalH-24),
				BackgroundVec(pal.scrubberHal),
				ClickThrough, NoAnimate,
			))
			// Crisp vertical scrubber line.
			Element(Attrs(
				Float(scrubberX-1, 24),
				FixSize(2, totalH-24),
				BackgroundVec(beamColor),
				ClickThrough, NoAnimate,
			))

			// Bead indicator centered on each city bar.
			for i := range state.Cities {
				cy := rulerH + rowGap + float32(i)*(rowH+rowGap) + rowH/2
				Element(Attrs(
					Float(scrubberX-5, cy-5),
					FixSize(10, 10), Corners(5),
					Background(0, 0, 100, 1),
					BorderWidth(2), BorderColorVec(beamColor),
					BoxShadow(2),
					ClickThrough, NoAnimate,
				))
			}

			// Top floating handle pill showing the base city's scrubbed time.
			pillW := float32(58)
			if !state.Use24h {
				pillW = 74
			}
			pillX := scrubberX - pillW/2
			if pillX < 2 {
				pillX = 2
			}
			if pillX > trackW-pillW-2 {
				pillX = trackW - pillW - 2
			}
			pillAttrs := Attrs(
				Float(pillX, 1),
				FixSize(pillW, 23), Corners(11.5),
				BackgroundVec(beamColor),
				Center, BoxShadow(4),
				ClickThrough, NoAnimate,
			)
			if st.FocusVisible {
				pillAttrs = AttrsWith(pillAttrs, BorderWidth(2), BorderColorVec(CurrentColorScheme.FocusRing))
			}
			Container(pillAttrs, func() {
				Label(FormatMinutes(int(state.ScrubMin+0.5), state.Use24h),
					FontSize(11.5), FontWeight(WeightBold), TextColor(0, 0, 100, 1))
			})
		}
	})
}

func rulerView(pal palette, trackW, innerW float32, hasLayout bool, slotStats []SlotStats) {
	Container(Attrs(Expand, FixHeight(rulerH), ClickThrough, NoAnimate), func() {
		if !hasLayout {
			return
		}

		// Hour ticks and labels along reference axis (00..24)
		for h := 0; h <= 24; h++ {
			hx := trackPad + (float32(h)/24.0)*innerW
			isMajor := h%2 == 0
			tickH := float32(4)
			tickY := float32(27)
			if isMajor {
				tickH = 7
				tickY = 24
			}
			Element(Attrs(
				Float(hx-0.5, tickY),
				FixSize(1, tickH),
				BackgroundVec(pal.divider),
				ClickThrough, NoAnimate,
			))

			if isMajor {
				const lw float32 = 30
				Container(Attrs(
					Float(hx-lw/2, 6),
					FixSize(lw, 16), Center,
					ClickThrough, NoAnimate,
				), func() {
					Label(FormatHourTick(h, state.Use24h), FontSize(10.5), FontWeight(WeightMedium), TextColorVec(pal.mutedText))
				})
			}
		}

		// Overlap quality ribbon directly below the ticks
		maxScore := 0
		for _, s := range slotStats {
			if s.Score > maxScore {
				maxScore = s.Score
			}
		}
		Container(Attrs(
			Float(trackPad, 35),
			FixSize(innerW, 6), Corners(3), Clip,
			ClickThrough, NoAnimate,
		), func() {
			for i, s := range slotStats {
				x0 := float32(i) * innerW / 48.0
				x1 := float32(i+1) * innerW / 48.0
				col := pal.overlapLow
				if maxScore > 0 {
					if s.Score >= maxScore {
						col = pal.overlapPeak
					} else if s.Sleeping == 0 || s.Score*4 >= maxScore*3 {
						col = pal.overlapGood
					}
				}
				Element(Attrs(
					Float(x0, 0),
					FixSize(x1-x0+0.8, 6),
					BackgroundVec(col),
					ClickThrough, NoAnimate,
				))
			}
		})
	})
}

func cityBarView(pal palette, city *City, baseMid time.Time, innerW float32, hasLayout bool) {
	ContainerWithKey(city, Attrs(Expand, FixHeight(rowH), ClickThrough, NoAnimate), func() {
		if !hasLayout {
			return
		}

		barY := (rowH - barH) / 2
		Container(Attrs(
			Float(trackPad, barY),
			FixSize(innerW, barH), Corners(9), Clip,
			BorderWidth(1), BorderColorVec(pal.barBorder),
			ClickThrough, NoAnimate,
		), func() {
			// 96 quarter-hour color segments for smooth dawn/dusk transitions and fractional offsets.
			const segCount = 96
			for s := 0; s < segCount; s++ {
				slotMid := baseMid.Add(time.Duration(s*15+7)*time.Minute + 30*time.Second)
				lt := slotMid.In(city.Loc)
				localHour := float64(lt.Hour()) + float64(lt.Minute())/60.0
				col := SolarColor(localHour)

				x0 := float32(s) * innerW / float32(segCount)
				x1 := float32(s+1) * innerW / float32(segCount)
				segW := (x1 - x0) + 0.8

				Element(Attrs(
					Float(x0, 0),
					FixSize(segW, barH),
					BackgroundVec(col),
					ClickThrough, NoAnimate,
				))

				// Highlight core working hours (09:00-17:00) with a crisp bottom bar.
				if IsWorkHour(lt) {
					Element(Attrs(
						Float(x0, barH-4),
						FixSize(segW, 4),
						Background(208, 76, 36, 0.82),
						ClickThrough, NoAnimate,
					))
				}
			}

			// Local hour markers & midnight date-boundary lines.
			// Find the first local whole hour at or after baseMid.
			startLocal := baseMid.In(city.Loc)
			firstWhole := time.Date(startLocal.Year(), startLocal.Month(), startLocal.Day(), startLocal.Hour(), 0, 0, 0, city.Loc)
			if firstWhole.Before(baseMid) {
				firstWhole = firstWhole.Add(time.Hour)
			}
			endInst := baseMid.Add(24 * time.Hour)

			for t := firstWhole; !t.After(endInst); t = t.Add(time.Hour) {
				offMin := t.Sub(baseMid).Minutes()
				if offMin < 0 || offMin > 1440 {
					continue
				}
				lx := float32(offMin/1440.0) * innerW
				hr := t.Hour()

				if hr == 0 {
					// Midnight boundary divider
					if lx > 2 && lx < innerW-2 {
						Element(Attrs(
							Float(lx-0.75, 0),
							FixSize(1.5, barH),
							Background(0, 0, 100, 0.55),
							ClickThrough, NoAnimate,
						))
					}
					// Compact weekday chip right after local midnight
					if lx >= 0 && lx < innerW-40 {
						Container(Attrs(
							Float(lx+4, 4),
							Pad2(1, 5), Corners(3),
							Background(230, 35, 10, 0.62),
							ClickThrough, NoAnimate,
						), func() {
							Label(strings.ToUpper(t.Format("Mon")), FontSize(8.5), FontWeight(WeightBold), TextColor(0, 0, 96, 0.95))
						})
					}
				} else if hr%3 == 0 {
					// Subtle top tick for every 3 local hours
					bgCol := SolarColor(float64(hr))
					ink := Vec4{215, 30, 94, 0.88}
					tickCol := Vec4{0, 0, 100, 0.28}
					if bgCol[2] >= 56 {
						ink = Vec4{222, 42, 16, 0.84}
						tickCol = Vec4{222, 40, 18, 0.22}
					}
					Element(Attrs(
						Float(lx-0.5, 0),
						FixSize(1, 6),
						BackgroundVec(tickCol),
						ClickThrough, NoAnimate,
					))

					if lx >= 16 && lx <= innerW-16 {
						const lw float32 = 28
						Container(Attrs(
							Float(lx-lw/2, (barH-16)/2-1),
							FixSize(lw, 16), Center,
							ClickThrough, NoAnimate,
						), func() {
							weight := WeightMedium
							if hr == 9 || hr == 12 || hr == 17 {
								weight = WeightBold
							}
							Label(FormatHourTick(hr, state.Use24h), FontSize(10.5), FontWeight(weight), TextColorVec(ink))
						})
					}
				}
			}
		})
	})
}

func legendFooterView(pal palette) {
	Container(Attrs(Row, Expand, CrossMid, Gap(14), Pad2(0, 4)), func() {
		legendSwatch(Vec4{232, 46, 18, 1}, "Night", pal.mutedText)
		legendSwatch(Vec4{28, 78, 62, 1}, "Dawn / Dusk", pal.mutedText)
		legendSwatch(Vec4{200, 74, 70, 1}, "Daylight", pal.mutedText)
		legendBarSwatch(Vec4{208, 76, 36, 0.85}, "Work 09–17", pal.mutedText)
		legendSwatch(pal.overlapPeak, "Peak overlap", pal.mutedText)
		legendSwatch(pal.nowColor, "Now", pal.mutedText)

		Filler(1)

		Label("Drag anywhere on the timeline to scrub  ·  ←/→ steps when focused", FontSize(11), TextColorVec(pal.subtleText))
	})
}

func legendSwatch(col Vec4, label string, textCol Vec4) {
	Container(Attrs(Row, CrossMid, Gap(5)), func() {
		Element(Attrs(FixSize(11, 11), Corners(3), BackgroundVec(col)))
		Label(label, FontSize(11), TextColorVec(textCol))
	})
}

func legendBarSwatch(col Vec4, label string, textCol Vec4) {
	Container(Attrs(Row, CrossMid, Gap(5)), func() {
		Element(Attrs(FixSize(14, 4), Corners(2), BackgroundVec(col)))
		Label(label, FontSize(11), TextColorVec(textCol))
	})
}

func addCityModalView(pal palette, inst time.Time) {
	Modal(460, func() { state.ShowAddCity = false }, func() {
		Container(Attrs(Row, Expand, CrossMid), func() {
			Label("Add City", FontSize(16), FontWeight(WeightBold))
			Filler(1)
			if CtrlButton(SymICross, "Close", true) {
				state.ShowAddCity = false
			}
		})

		inputAttrs := DefaultTextInputAttrs()
		inputAttrs.Placeholder = "Search city, country, or IANA zone..."
		TextInputExt(&state.CitySearch, inputAttrs)

		// Scrollable list of matching cities
		Container(Attrs(
			Expand, FixHeight(340), Extrinsic, Clip,
			Corners(8), BorderWidth(1), BorderColorVec(pal.cardBorder),
			Pad(6),
		), func() {
			ScrollOnInput()

			Container(Attrs(Expand, Gap(4)), func() {
				matched := 0
				for _, c := range state.Catalog {
					if !MatchCity(c, state.CitySearch) {
						continue
					}
					matched++
					added := state.hasCity(c.ID)
					lt := inst.In(c.Loc)

					ContainerWithKey(c, Attrs(
						Row, Expand, CrossMid,
						Pad2(7, 10), Corners(6), Gap(8),
						BackgroundVec(pal.cardBg),
					), func() {
						Container(Attrs(Gap(1)), func() {
							Label(c.Name, FontSize(13), FontWeight(WeightSemibold))
							Label(fmt.Sprintf("%s · %s", c.Region, FormatUTCOffset(lt)), FontSize(11), TextColorVec(pal.mutedText))
						})
						Filler(1)
						Label(FormatTime(lt, state.Use24h), FontSize(12), FontWeight(WeightSemibold), TextColorVec(pal.mutedText))
						Spacer(4)
						if added {
							if CtrlButton(SymICross, "Remove", len(state.Cities) > 1) {
								state.removeCity(c.ID)
							}
						} else {
							NextButtonType(ButtonPrimary)
							if CtrlButton(SymPlus, "Add", true) {
								state.addCity(c)
							}
						}
					})
				}
				if matched == 0 {
					Container(Attrs(Expand, Pad(24), Center), func() {
						Label("No matching cities", FontSize(13), TextColorVec(pal.mutedText))
					})
				}
			})

			ScrollBars()
		})
	})
}
