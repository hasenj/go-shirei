package main

import (
	"fmt"
	"sort"
	"strconv"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

const (
	hotCount            = 10
	hotSparkHeight  f32 = 72
	hotMetricWidth  f32 = 64
	hotMemoryWidth  f32 = 74
	hotPowerWidth   f32 = 62
	hotNameMaxChars     = 80
)

const (
	metricCPU = iota
	metricMemory
	metricPower
)

// HotView is the full-window dashboard, chosen by the -hot flag or the Hot
// toolbar button.
func HotView() {
	procs := topHotProcesses(hotCount)
	if appData.hotSelected != nil && !hotSelectionPresent(procs, appData.hotSelected) {
		clearHotSelection()
	}
	if appData.hotSelected != nil && GetFrameInput().Key == KeyEscape {
		clearHotSelection()
		GetFrameInput().Key = 0
	}
	Container(Attrs(Viewport, UseSurface(SurfacePanel)), func() {
		hotToolbar()
		Container(Attrs(Grow(1), Expand, Viewport), func() {
			ScrollOnInput()
			ScrollBars()
			Container(Attrs(Expand, Pad2(10, 12), Gap(6)), func() {
				if len(procs) == 0 {
					Label("Collecting process data…", FontSize(12), TextColorVec(CurrentColorScheme.List.Muted))
					return
				}
				for _, p := range procs {
					hotProcessRow(p)
				}
			})
		})
		if appData.hotSelected != nil {
			hotSelectionPanel()
		}
	})
}

func hotToolbar() {
	Container(Attrs(Row, Expand, CrossMid, Gap(8), Pad2(7, 10), UseSurface(SurfaceToolbar)), func() {
		Icon(TypChartLine, FontSize(14), TextColorVec(metricColor(38)))
		Label("Hot Processes", FontSize(13), FontWeight(WeightSemibold))
		Filler(1)
		NextAccessName("btn_full_list")
		if Button(SymArrowLeft, "Full List") {
			appData.hotMode = false
			clearHotSelection()
			wakeSampler()
			RequestNextFrame()
		}
	})
}

func hotProcessRow(p *Process) {
	name := p.Name
	if name == "" {
		name = p.Cmdline
	}
	NextAccessName("hot_row_" + strconv.Itoa(p.PID))
	NextAccessRole("button")
	NextAccessValue(name)
	AssignAccess()
	ContainerWithKey(p, Attrs(Expand, Gap(4), Pad2(3, 4), Corners(4)), func() {
		st := ProcessButtonEvents(false)
		switch {
		case appData.hotSelected == p:
			color := CurrentColorScheme.List.Selected.Background
			color[3] = 0.28
			ModAttrs(BackgroundVec(color))
		case st.Hovered:
			ModAttrs(BackgroundVec(CurrentColorScheme.List.Hovered.Background))
		}
		if st.Clicked {
			if appData.hotSelected == p {
				clearHotSelection()
			} else {
				appData.hotSelected = p
				appData.hotKillArmed = false
			}
		}
		Container(Attrs(Row, Expand, CrossMid, Gap(6)), func() {
			processIconView(p.PID, p.ExePath, 26)
			Container(Attrs(Grow(1), Clip), func() {
				Label(truncate(name, hotNameMaxChars), FontSize(16))
			})
			hotMetricValue(metricCPU, formatCPU(hotCPU(p)), hotMetricWidth, 38)
			hotMetricValue(metricMemory, p.RSSText(), hotMemoryWidth, 210)
			hotMetricValue(metricPower, p.PowerText(), hotPowerWidth, 150)
		})
		Container(Attrs(Row, Expand, Gap(6)), func() {
			width := max(f32(120), (GetResolvedWidth()-12)/3)
			hotSparkline(p, func(pt ProcessPoint) float64 { return pt.CPUPercent }, 100, 50, width, hotSparkHeight, 38)
			hotSparkline(p, func(pt ProcessPoint) float64 { return float64(pt.RSSBytes) }, 256<<20, 256<<20, width, hotSparkHeight, 210)
			hotSparkline(p, func(pt ProcessPoint) float64 { return pt.PowerWatts }, 1, 1, width, hotSparkHeight, 150)
		})
	})
}

func hotSelectionPresent(procs []*Process, p *Process) bool {
	if p == nil || !p.Running() {
		return false
	}
	for _, x := range procs {
		if x == p {
			return true
		}
	}
	return false
}

func clearHotSelection() {
	appData.hotSelected = nil
	appData.hotKillArmed = false
}

func hotSelectionPanel() {
	p := appData.hotSelected
	if p == nil {
		return
	}
	Element(Attrs(Expand, FixHeight(1), BackgroundVec(CurrentColorScheme.Surfaces.Panel.Border)))
	Container(Attrs(Row, Expand, CrossMid, Gap(8), Pad2(7, 12), UseSurface(SurfaceCanvas)), func() {
		processIconView(p.PID, p.ExePath, 22)
		Container(Attrs(Grow(1), Gap(1), Clip), func() {
			Label(p.Name, FontSize(13), FontWeight(WeightSemibold))
			Label(fmt.Sprintf("PID %d · %s · running for %s", p.PID, p.User, formatUptime(p)),
				FontSize(10), TextColorVec(CurrentColorScheme.List.Muted))
		})
		NextAccessName("hot_details")
		if Button(SymExternal, "Open details") {
			openHotDetails()
		}
		if appData.hotKillArmed {
			NextAccessName("hot_end_confirm")
			NextButtonType(ButtonDestructive)
			if Button(SymDelete, "Confirm end") {
				endHotSelected()
			}
		} else {
			NextAccessName("hot_end")
			NextButtonType(ButtonDestructive)
			if Button(SymDelete, "End process…") {
				appData.hotKillArmed = true
			}
		}
		hotDeselectButton()
	})
}

func hotDeselectButton() {
	NextAccessName("hot_deselect")
	NextAccessRole("button")
	NextAccessValue("Close selection")
	AssignAccess()
	Container(Attrs(FixSize(20, 20), Center, Corners(3)), func() {
		st := ProcessButtonEvents(false)
		if st.Hovered || st.Active {
			ModAttrs(BackgroundVec(CurrentColorScheme.List.Hovered.Background))
		}
		Icon(SymCancel, FontSize(10), TextColorVec(CurrentColorScheme.List.Muted))
		if st.Clicked {
			clearHotSelection()
		}
	})
}

func openHotDetails() {
	p := appData.hotSelected
	if p == nil {
		return
	}
	appData.selected = p
	appData.filter = ""
	appData.scope = 0
	appData.tableSort.Column = sortColumnIndex("cpu")
	appData.tableSort.Desc = true
	requestDetails(p)
	appData.killArmed = false
	appData.hotMode = false
	clearHotSelection()
	wakeSampler()
	RequestNextFrame()
}

func endHotSelected() {
	p := appData.hotSelected
	if p == nil {
		return
	}
	appData.hotKillArmed = false
	if err := Kill(p.PID); err != nil {
		ToastExt(ToastAttrs{
			Icon:       SymFail,
			Title:      "Kill failed",
			Body:       err.Error(),
			Background: ToastBackgroundDanger,
		})
		return
	}
	Toast(SymPass, "Kill sent", fmt.Sprintf("pid %d", p.PID))
}

func hotMetricValue(kind int, text string, width f32, hue f32) {
	Container(Attrs(FixWidth(width), Row, CrossMid, Gap(3), MainAlign(AlignEnd)), func() {
		hotMetricIcon(kind, hue)
		Label(text, Fonts(Monospace...), FontSize(12), TextColorVec(metricColor(hue)))
	})
}

func hotMetricIcon(kind int, hue f32) {
	color := metricColor(hue)
	Container(Attrs(FixSize(16, 16), NoAnimate), func() {
		switch kind {
		case metricCPU:
			Container(Attrs(Float(3, 3), FixSize(10, 10), BorderWidth(1), BorderColorVec(color), Corners(2)), func() {})
			Container(Attrs(Float(6, 6), FixSize(4, 4), BackgroundVec(color)), func() {})
			for _, x := range []f32{4, 7, 10} {
				Container(Attrs(Float(x, 1), FixSize(1, 2), BackgroundVec(color)), func() {})
				Container(Attrs(Float(x, 13), FixSize(1, 2), BackgroundVec(color)), func() {})
			}
			for _, y := range []f32{5, 9} {
				Container(Attrs(Float(1, y), FixSize(2, 1), BackgroundVec(color)), func() {})
				Container(Attrs(Float(13, y), FixSize(2, 1), BackgroundVec(color)), func() {})
			}
		case metricMemory:
			Container(Attrs(Float(1, 4), FixSize(14, 8), BorderWidth(1), BorderColorVec(color), Corners(1)), func() {})
			for _, x := range []f32{4, 7, 10} {
				Container(Attrs(Float(x, 6), FixSize(1, 4), BackgroundVec(color)), func() {})
			}
			for _, x := range []f32{3, 6, 9, 12} {
				Container(Attrs(Float(x, 12), FixSize(1, 2), BackgroundVec(color)), func() {})
			}
			Container(Attrs(Float(7, 11), FixSize(2, 2),
				BackgroundVec(CurrentColorScheme.Surfaces.Panel.Background)), func() {})
		case metricPower:
			Container(Attrs(FixSize(16, 16), Center), func() {
				Icon(TypFlash, FontSize(13), TextColorVec(color))
			})
		}
	})
}

func hotSparkline(p *Process, valueFn func(ProcessPoint) float64, minScale, step float64, width, height f32, hue f32) {
	buckets := resampleHistory(p.History, historyWindow, historyBucket, valueFn)
	peak := 0.0
	for _, b := range buckets {
		if b.HasData {
			peak = max(peak, b.Value)
		}
	}
	scale := steppedScale(peak, minScale, step)
	Container(Attrs(FixSize(width, height), Corners(2), Clip,
		BackgroundVec(CurrentColorScheme.Surfaces.Canvas.Background)), func() {
		historyPlot(buckets, scale, width, height, metricColor(hue), true)
	})
}

// hotCPU always uses the smoothed CPU, regardless of the full-list setting.
func hotCPU(p *Process) float64 {
	if p.CPUPercent < 0 {
		return CPUPercentUnknown
	}
	if p.CPUSmooth.Ready {
		return p.CPUSmooth.Value
	}
	return p.CPUPercent
}

func topHotProcesses(n int) []*Process {
	procs := appData.store.Processes()
	live := make([]*Process, 0, len(procs))
	for _, p := range procs {
		if p.Running() && p.CPUPercent >= 0 {
			live = append(live, p)
		}
	}
	sort.SliceStable(live, func(i, j int) bool {
		ai, aj := hotCPU(live[i]), hotCPU(live[j])
		if ai != aj {
			return ai > aj
		}
		return live[i].PID < live[j].PID
	})
	if len(live) > n {
		live = live[:n]
	}
	return live
}
