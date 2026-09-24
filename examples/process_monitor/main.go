package main

import (
	"flag"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	app "go.hasen.dev/shirei/app"
	"go.hasen.dev/shirei/ext/darkmode"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

type f32 = float32

type AppState struct {
	snapshot *ProcSnapshot
	err      error

	filter         string
	filterFocusReq bool
	scope          int
	paused         bool
	hostHistory    []ProcessPoint
	treeMode       bool
	selected       *Process
	store          *ProcessStore

	samples      int
	period       time.Duration
	refreshEvery time.Duration
	lastRefresh  time.Time

	// tableSort is threaded into the process table (TableAttrs.SortState)
	// so tree mode can order siblings by the active column, and the -sort
	// flag can pick the starting column.
	tableSort TableSortState

	// killArmed is true after the first click on Kill; the next click
	// actually sends the signal. Cleared when selection changes.
	killArmed bool

	// viewRows is recomputed when the snapshot, sort, filter, tree mode,
	// or pin/collapse set changes — not every frame.
	viewRows []*Process
	viewKey  viewKey
}

type viewKey struct {
	snap     time.Time
	filter   string
	col      int
	desc     bool
	tree     bool
	scope    int
	pins     uint64
	collapse uint64
}

var appData = &AppState{store: NewProcessStore()}

const rowHeight f32 = 22

var sampleWake = make(chan struct{}, 1)

var processScopes = []string{"All processes", "Running", "Pinned"}

func main() {
	samples := flag.Int("samples", 4, "number of process samples to collect per refresh")
	period := flag.Duration("period", 200*time.Millisecond, "total sampling period per refresh")
	limit := flag.Int("limit", 10, "number of processes to print in -once mode")
	sortBy := flag.String("sort", "cpu", "sort column: cpu, mem, uptime, pid, name")
	once := flag.Bool("once", false, "print one terminal report and exit instead of opening the GUI")
	refreshEvery := flag.Duration("refresh", time.Second, "GUI sampling interval")
	pngPath := flag.String("png", "", "render one frame of the GUI headlessly to this path and exit")
	flag.Parse()

	if err := validateSamplingArgs(*samples, *period); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if *limit < 0 {
		fmt.Fprintln(os.Stderr, "limit must be non-negative")
		os.Exit(2)
	}
	if *refreshEvery <= 0 {
		fmt.Fprintln(os.Stderr, "refresh must be positive")
		os.Exit(2)
	}

	if *once {
		runOnce(*samples, *period, *limit, *sortBy)
		return
	}

	appData.samples = *samples
	appData.period = *period
	appData.refreshEvery = *refreshEvery
	appData.tableSort.Column = sortColumnIndex(*sortBy)
	appData.tableSort.Desc = cachedProcessColumns[appData.tableSort.Column].DefaultDesc

	if *pngPath != "" {
		renderPNG(*pngPath)
		return
	}

	startSamplerLoop()

	app.SetupIconBytes(iconPNG)
	app.SetupWindow("Process Monitor", 1200, 820)
	app.SetupDrive()
	app.Run(RootView)
}

// renderPNG is the standard headless verification path (shirei tutorial
// §17): sample twice so CPU% has a real window, feed the app state exactly
// like one sampler-loop pass, and render the full UI without a window.
func renderPNG(path string) {
	sam := new(Sampler)
	sam.Sample()
	time.Sleep(300 * time.Millisecond)
	snap, err := sam.Sample()
	appData.snapshot = snap
	appData.err = err
	appData.lastRefresh = time.Now()
	appData.store.Update(snap, nil)
	if snap != nil {
		columns := cachedProcessColumns
		rows := visibleRows(appData.store.Processes(), "",
			columnLess(columns, appData.tableSort.Column), appData.tableSort.Desc, false)
		if len(rows) > 0 {
			appData.selected = rows[0]
			requestDetails(rows[0])
		}
	}
	if err := RenderToPNG(path, 1200, 820, RootView); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func validateSamplingArgs(samples int, period time.Duration) error {
	if samples < 2 {
		return fmt.Errorf("samples must be at least 2")
	}
	if period <= 0 {
		return fmt.Errorf("period must be positive")
	}
	return nil
}

func runOnce(samples int, period time.Duration, limit int, sortBy string) {
	snap, actualPeriod, err := CollectSampleWindow(samples, period)
	if err != nil {
		fmt.Fprintln(os.Stderr, "sample:", err)
		os.Exit(1)
	}

	procs := append([]ProcInfo(nil), snap.Processes...)
	sortProcesses(procs, sortBy)
	if limit > len(procs) {
		limit = len(procs)
	}

	fmt.Printf("Top %d processes by %s over %s (%d samples; requested %s)\n", limit, sortBy, actualPeriod.Round(time.Millisecond), samples, period.String())
	fmt.Printf("CPU: %s   Memory: %s used / %s total\n\n", formatCPUPercent(snap.HostCPUPercent), formatBytes(snap.UsedMemoryBytes), formatBytes(snap.TotalMemoryBytes))
	fmt.Printf("%-7s %7s %9s %7s %-12s %-5s %5s %s\n", "PID", "CPU%", "RSS", "MEM%", "USER", "STATE", "THR", "NAME")
	for _, p := range procs[:limit] {
		name := p.Name
		if name == "" {
			name = p.Cmdline
		}
		fmt.Printf("%-7d %7s %9s %7s %-12.12s %-5s %5s %s\n",
			p.PID,
			p.CPUText(),
			p.RSSText(),
			p.MemText(),
			p.User,
			p.State,
			p.ThreadsText(),
			truncate(name, 80),
		)
	}
}

func wakeSampler() {
	select {
	case sampleWake <- struct{}{}:
	default:
	}
}

func togglePause() {
	appData.paused = !appData.paused
	wakeSampler()
}

func startSamplerLoop() {
	go func() {
		sam := new(Sampler)
		for {
			var refresh time.Duration
			var paused bool
			WithFrameLock(func() { refresh, paused = appData.refreshEvery, appData.paused })
			if paused {
				sam.prev = nil
				<-sampleWake
				continue
			}
			started := time.Now()
			snap, err := sam.Sample()
			WithFrameLock(func() {
				// A pause can arrive while the OS sample is in flight.
				if appData.paused {
					return
				}
				appData.snapshot, appData.err = snap, err
				appData.lastRefresh = time.Now()
				appData.store.Update(snap, appData.selected)
				if snap != nil {
					appData.hostHistory = append(appData.hostHistory, ProcessPoint{Time: snap.Time, CPUPercent: snap.HostCPUPercent})
					if len(appData.hostHistory) > maxHistoryPoints {
						appData.hostHistory = appData.hostHistory[1:]
					}
				}
				if p := appData.selected; p != nil && !p.Details.Fetched {
					requestDetails(p)
				}
				RequestNextFrame()
			})
			timer := time.NewTimer(max(time.Millisecond, refresh-time.Since(started)))
			select {
			case <-timer.C:
			case <-sampleWake:
			}
			timer.Stop()
		}
	}()
}

func RootView() {
	SetDarkMode(darkmode.OSDarkMode())
	handleFindShortcut()
	// Space belongs to the focused control while editing or navigating controls.
	if GetFrameInput().Key == KeySpace && GetInputState().Modifiers == ModNone && FocusedId() == nil {
		togglePause()
		GetFrameInput().Key = 0
	}
	Container(Attrs(Viewport, UseSurface(SurfacePanel), AmendTextStyle(FontSize(11))), func() {
		Toolbar()
		Header()
		ProcessTable()
		SelectedPanel()
		StatusBar()
	})
}

func handleFindShortcut() {
	if GetFrameInput().Key == KeyF && GetInputState().Modifiers == PrimaryMod() {
		appData.filterFocusReq = true
		GetFrameInput().Key = 0
	}
}

func Toolbar() {
	Container(Attrs(Row, Expand, CrossMid, Gap(8), Pad2(7, 10), UseSurface(SurfaceToolbar)), func() {
		Container(Attrs(Row, CrossMid, Gap(5), FixWidth(290)), func() {
			NextAccessName(NameBtnFind)
			if Button(SymSearch, "") {
				appData.filterFocusReq = true
			}
			Container(Attrs(Grow(1)), func() {
				attrs := DefaultTextInputAttrs()
				attrs.NoAutoFocus, attrs.Depth = true, 0
				attrs.Placeholder = "Filter processes…"
				if appData.filter != "" {
					attrs.Padding[PAD_RIGHT] += 22
				}
				NextAccessName(NameFilter)
				TextInputExt(&appData.filter, attrs)
				if appData.filterFocusReq {
					FocusImmediateOn(GetLastId())
					appData.filterFocusReq = false
				}
				if HasFocusWithin() && GetFrameInput().Key == KeyEscape {
					appData.filter = ""
					ClearFocus()
					GetFrameInput().Key = 0
				}
				if appData.filter != "" {
					size := GetResolvedSize()
					if size[0] > 24 {
						const clearSize float32 = 18
						NextAccessName("clear_filter")
						Container(Attrs(Float(size[0]-clearSize-4, (size[1]-clearSize)/2),
							FixSize(clearSize, clearSize), Center, Corners(3), InFront, NoAnimate), func() {
							st := ProcessButtonEvents(false)
							NextAccessRole("button")
							AssignAccess()
							if st.Hovered || st.Active {
								ModAttrs(BackgroundVec(CurrentColorScheme.Surfaces.Panel.Border))
							}
							if st.FocusVisible {
								ModAttrs(BorderWidth(1), BorderColorVec(CurrentColorScheme.FocusRing))
							}
							if st.Clicked {
								appData.filter = ""
							}
							Icon(SymCancel, FontSize(10), TextColorVec(CurrentColorScheme.List.Muted))
						})
					}
				}
			})
		})
		NextAccessName("process_scope")
		MenuButton(SymDown, processScopes[appData.scope], func() {
			for i, label := range processScopes {
				if MenuItem(NoIcon, label) {
					appData.scope = i
				}
			}
		})
		NextAccessName("process_view")
		SegmentedControl(&appData.treeMode, func() {
			SegmentedCell("Flat", false)
			SegmentedCell("Tree", true)
		})
		Filler(1)
		RefreshControls()
		label, icon := "Pause", SymPause
		if appData.paused {
			label, icon = "Resume", SymPlay
		}
		NextAccessName("pause_sampling")
		NextAccessChecked(appData.paused)
		if Button(icon, label) {
			togglePause()
		}
	})
}

func RefreshControls() {
	NextAccessName("refresh_interval")
	MenuButton(SymDown, "Refresh  "+appData.refreshEvery.String(), func() {
		for _, d := range []time.Duration{time.Second, 2 * time.Second, 5 * time.Second, 10 * time.Second} {
			if MenuItem(NoIcon, d.String()) {
				appData.refreshEvery = d
				wakeSampler()
			}
		}
	})
}

// Header is the one-line system summary beneath the command toolbar.
func Header() {
	Container(Attrs(Row, Expand, CrossMid, Gap(12), Pad2(5, 12), UseSurface(SurfaceCanvas), Clip), func() {
		if appData.err != nil {
			Label("Sample error: "+appData.err.Error(), TextColorVec(CurrentColorScheme.List.Error))
			return
		}
		if appData.snapshot == nil {
			Label("Collecting process samples…")
			return
		}
		Label(fmt.Sprintf("%d processes", appData.store.ActiveCount()))
		summaryDivider()
		Container(Attrs(Row, CrossMid, Gap(8)), func() {
			NextAccessName(NameHostCPU)
			AssignAccess()
			Label("CPU", TextColorVec(CurrentColorScheme.List.Muted))
			Label(formatCPUPercent(appData.snapshot.HostCPUPercent), Fonts(Monospace...), TextColorVec(metricColor(38)))
			buckets := resampleHistory(appData.hostHistory, historyWindow, historyBucket, func(pt ProcessPoint) float64 { return pt.CPUPercent })
			historyPlot(buckets, 100, 100, 15, metricColor(38), false)
		})
		summaryDivider()
		Container(Attrs(Row, CrossMid, Gap(8)), func() {
			NextAccessName(NameHostMem)
			AssignAccess()
			Label("Memory", TextColorVec(CurrentColorScheme.List.Muted))
			Label(fmt.Sprintf("%s / %s", formatBytes(appData.snapshot.UsedMemoryBytes), formatBytes(appData.snapshot.TotalMemoryBytes)), Fonts(Monospace...))
			UsageBar(percent(appData.snapshot.UsedMemoryBytes, appData.snapshot.TotalMemoryBytes), 100, 210)
		})
		Filler(1)
		NextAccessName("sample_time")
		NextAccessValue(appData.lastRefresh.Format(time.RFC3339Nano))
		Container(Attrs(Row), func() {
			AssignAccess()
			text := "Updated " + formatTime(appData.lastRefresh)
			if appData.paused {
				text = "Paused · " + formatTime(appData.lastRefresh)
			}
			Label(text, FontSize(10), TextColorVec(CurrentColorScheme.List.Muted))
		})
	})
}

func summaryDivider() {
	Element(Attrs(FixSize(1, 12), BackgroundVec(CurrentColorScheme.Surfaces.Panel.Border)))
}

func StatusBar() {
	Container(Attrs(Row, Expand, CrossMid, Gap(8), Pad2(4, 12), UseSurface(SurfaceCanvas), AmendTextStyle(FontSize(10), TextColorVec(CurrentColorScheme.List.Muted))), func() {
		if appData.selected != nil {
			Label("1 selected")
		} else {
			Label("No selection")
		}
		if appData.filter != "" || appData.scope != 0 {
			Label(fmt.Sprintf("· %d shown", len(appData.viewRows)))
		}
		Filler(1)
		shortcut := "Ctrl+F"
		if PrimaryMod() == ModCmd {
			shortcut = "⌘F"
		}
		Label(shortcut + " Filter    Space Pause")
	})
}

// processColumns orders process identity first, followed by sortable resource metrics.
func processColumns() []TableColumn[*Process] {
	return []TableColumn[*Process]{
		{Label: "Process", AccessName: NameHeaderName,
			Cell: nameCell,
			Less: func(a, b *Process) bool {
				if a.Name != b.Name {
					return a.Name < b.Name
				}
				return a.PID < b.PID
			}},
		{Label: "PID", Alignment: AlignEnd, AccessName: NameHeaderPID, Width: 62,
			Cell: func(p *Process) { rowLabel(p, NameCellPID(p.PID), fmt.Sprintf("%d", p.PID)) },
			Less: func(a, b *Process) bool { return a.PID < b.PID }},
		{Label: "CPU %", Alignment: AlignEnd, AccessName: NameHeaderCPU, Width: 138, DefaultDesc: true,
			Cell: func(p *Process) {
				NextAccessName(NameCellCPU(p.PID))
				AssignAccess()
				Container(Attrs(Row, CrossMid, Gap(7)), func() {
					UsageBar(f32(max(p.CPUPercent, 0)), 100, 38)
					Container(Attrs(FixWidth(48), CrossAlign(AlignEnd)), func() { Label(p.CPUText(), FontSize(11), Fonts(Monospace...), TextColorVec(rowInk(p))) })
				})
			},
			Less: func(a, b *Process) bool {
				if a.CPUPercent != b.CPUPercent {
					return a.CPUPercent < b.CPUPercent
				}
				return a.PID < b.PID
			}},
		{Label: "Memory", Alignment: AlignEnd, AccessName: NameHeaderRSS, Width: 90, DefaultDesc: true,
			Cell: func(p *Process) { rowLabel(p, NameCellRSS(p.PID), p.RSSText()) },
			Less: lessRSS},
		{Label: "MEM %", Alignment: AlignEnd, AccessName: NameHeaderMem, Width: 65, DefaultDesc: true,
			Cell: func(p *Process) { rowLabel(p, NameCellMem(p.PID), p.MemText()) },
			Less: lessRSS},
		{Label: "Energy", Alignment: AlignEnd, AccessName: NameHeaderPower, Width: 75, DefaultDesc: true,
			Cell: func(p *Process) { rowLabel(p, NameCellPower(p.PID), p.PowerText()) },
			Less: func(a, b *Process) bool {
				if a.PowerWatts != b.PowerWatts {
					return a.PowerWatts < b.PowerWatts
				}
				return a.PID < b.PID
			}},
		{Label: "Threads", Alignment: AlignEnd, AccessName: NameHeaderThr, Width: 65, DefaultDesc: true,
			Cell: func(p *Process) { rowLabel(p, NameCellThr(p.PID), p.ThreadsText()) },
			Less: func(a, b *Process) bool {
				if a.Threads != b.Threads {
					return a.Threads < b.Threads
				}
				return a.PID < b.PID
			}},
		{Label: "User", AccessName: NameHeaderUser, Width: 95,
			Cell: func(p *Process) { rowLabel(p, NameCellUser(p.PID), p.User) },
			Less: func(a, b *Process) bool {
				if a.User != b.User {
					return a.User < b.User
				}
				return a.PID < b.PID
			}},
		{Label: "Uptime", Alignment: AlignEnd, AccessName: NameHeaderUptime, Width: 82, DefaultDesc: true,
			Cell: func(p *Process) { rowLabel(p, NameCellUptime(p.PID), formatUptime(p)) },
			Less: lessUptime},
		{Label: "State", AccessName: NameHeaderState, Width: 70,
			Cell: func(p *Process) { rowLabel(p, NameCellState(p.PID), lifeState(p)) },
			Less: func(a, b *Process) bool {
				ra, rb := a.Running(), b.Running()
				if ra != rb {
					return ra // running before exited when ascending
				}
				return a.PID < b.PID
			}},
	}
}

// Built once: column closures do not close over frame-local state.
var cachedProcessColumns = processColumns()

func lessRSS(a, b *Process) bool {
	if a.RSSBytes != b.RSSBytes {
		return a.RSSBytes < b.RSSBytes
	}
	return a.PID < b.PID
}

// processUptime is how long the process has been (or was) running, from
// StartTime until now, or until StoppedAt once it has exited.
func processUptime(p *Process) time.Duration {
	if p.StartTime.IsZero() {
		return -1
	}
	end := appData.lastRefresh
	if end.IsZero() {
		end = time.Now()
	}
	if !p.Running() && !p.StoppedAt.IsZero() {
		end = p.StoppedAt
	}
	d := end.Sub(p.StartTime)
	if d < 0 {
		return 0
	}
	return d
}

func lessUptime(a, b *Process) bool {
	ua, ub := processUptime(a), processUptime(b)
	if ua != ub {
		return ua < ub
	}
	return a.PID < b.PID
}

func formatUptime(p *Process) string {
	d := processUptime(p)
	if d < 0 {
		return "--"
	}
	return formatUptimeDuration(d)
}

func formatUptimeDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	d = d.Round(time.Second)
	days := d / (24 * time.Hour)
	d -= days * 24 * time.Hour
	hours := d / time.Hour
	d -= hours * time.Hour
	mins := d / time.Minute
	secs := d / time.Second % 60
	switch {
	case days > 0:
		return fmt.Sprintf("%dd %dh", days, hours)
	case hours > 0:
		return fmt.Sprintf("%dh %dm", hours, mins)
	case mins > 0:
		return fmt.Sprintf("%dm %ds", mins, secs)
	default:
		return fmt.Sprintf("%ds", secs)
	}
}

func lifeState(p *Process) string {
	if p.Running() {
		return "running"
	}
	return "exited"
}

func lifeAlpha(p *Process) f32 {
	if p.Running() {
		return 1
	}
	return 0.45
}

func rowSelected(p *Process) bool {
	return appData.selected == p
}

// rowInk fades exited processes while preserving selection contrast.
func rowInk(p *Process) Vec4 {
	color := CurrentColorScheme.List.Surface.Text
	if rowSelected(p) {
		color = CurrentColorScheme.List.Selected.Text
	}
	color[3] *= lifeAlpha(p)
	return color
}

func rowLabel(p *Process, name, text string) {
	NextAccessName(name)
	AssignAccess()
	Label(text, FontSize(11), Fonts(Monospace...), TextColorVec(rowInk(p)))
}

func nameCell(p *Process) {
	NextAccessName(NameCellName(p.PID))
	AssignAccess()
	name := p.Name
	if name == "" {
		name = p.Cmdline
	}
	Container(Attrs(Row, CrossMid, Clip, Gap(4)), func() {
		pinGlyph := TypPinOutline
		pinColor := rowInk(p)
		if rowSelected(p) {
			pinColor = rowInk(p)
		}
		if p.Pinned {
			pinGlyph = TypPin
			pinColor = Vec4{35, 80, 48, lifeAlpha(p)}
		}
		Container(Attrs(FixWidth(14), CrossMid), func() {
			if PressAction() {
				p.Pinned = !p.Pinned
			}
			Icon(pinGlyph, FontSize(12), TextColorVec(pinColor))
		})
		processIconView(p.PID, p.ExePath, 16)
		if appData.treeMode {
			if depth := min(p.TreeDepth, 8); depth > 0 {
				Element(Attrs(FixWidth(f32(depth) * 14)))
			}
			if p.TreeChildCount > 0 {
				Container(Attrs(FixWidth(14), CrossMid), func() {
					if PressAction() {
						p.Collapsed = !p.Collapsed
					}
					arrow := "▾"
					if p.Collapsed {
						arrow = "▸"
					}
					Label(arrow, FontSize(10), TextColorVec(rowInk(p)))
				})
			} else {
				Element(Attrs(FixWidth(14)))
			}
		}
		Label(name, FontSize(11), TextColorVec(rowInk(p)))
	})
}

// sortColumnIndex maps the -sort flag's key to a column index.
func sortColumnIndex(key string) int {
	name := NameHeaderCPU
	switch strings.ToLower(key) {
	case "pid":
		name = NameHeaderPID
	case "power", "watts":
		name = NameHeaderPower
	case "mem", "rss":
		name = NameHeaderRSS
	case "uptime":
		name = NameHeaderUptime
	case "user":
		name = NameHeaderUser
	case "state":
		name = NameHeaderState
	case "threads":
		name = NameHeaderThr
	case "name":
		name = NameHeaderName
	}
	for i, col := range cachedProcessColumns {
		if col.AccessName == name {
			return i
		}
	}
	return 0
}

// columnLess returns the active sort column's comparator (nil-safe).
func columnLess(columns []TableColumn[*Process], column int) func(a, b *Process) bool {
	if column >= 0 && column < len(columns) {
		return columns[column].Less
	}
	return nil
}

func ProcessTable() {
	Container(Attrs(Grow(1), Expand, Clip, NoAnimate, UseSurface(SurfacePanel)), func() {
		if appData.snapshot == nil {
			Container(Attrs(Viewport, Center), func() {
				Label("Waiting for first sample…")
			})
			return
		}

		// Rows are filtered and ordered here, not by the table's flat sort:
		// tree mode orders siblings within the hierarchy, and the filter
		// keeps matches' ancestors visible. The table still owns the header
		// UI and writes appData.tableSort, which this read picks up (a sort
		// click reorders on the next frame).
		columns := cachedProcessColumns
		rows := displayedRows()
		if len(rows) == 0 {
			Container(Attrs(Viewport, Center), func() {
				NextAccessName(NameNoMatches)
				AssignAccess()
				Label("No matching processes")
			})
			return
		}

		attrs := TableAttrs[*Process]{
			RowHeight: rowHeight,
			SortState: &appData.tableSort,
			OrderRows: func(rows []*Process, column int, desc bool) []*Process { return rows },
			OnRow: func(i int, p *Process) {
				NextAccessName(NameProc)
				NextAccessValue(strconv.Itoa(p.PID))
				NextAccessRole(lifeState(p))
				AssignAccess()
				color := CurrentColorScheme.List.Surface.Background
				if i%2 == 1 {
					color[2] = color[2]*0.78 + CurrentColorScheme.Surfaces.Canvas.Background[2]*0.22
				}
				if IsHovered() {
					color = CurrentColorScheme.List.Hovered.Background
				}
				if appData.selected == p {
					color = CurrentColorScheme.List.Selected.Background
				}
				ModAttrs(NoAnimate, BackgroundVec(color))
				if PressAction() {
					ClearFocus()
					if appData.selected == p {
						appData.selected = nil
					} else {
						appData.selected = p
						requestDetails(p)
					}
					appData.killArmed = false
				}
			},
		}
		style := CurrentColorScheme.Table
		style.Separator[3] = 0
		style.Sorted[3] *= 0.35
		TableStyled("procs", attrs, columns, rows, func(p *Process) any { return p }, style, CurrentColorScheme.FocusRing)
	})
}

// requestDetails loads cwd/environ once for the selected process. Called on
// select, not every sample — a per-sample sysctl + frame wake would keep
// the process in its own top-CPU rows.
func requestDetails(p *Process) {
	if p == nil {
		return
	}
	p.detailsSeq++
	seq := p.detailsSeq
	pid := p.PID
	go func() {
		d, _ := ReadDetails(pid)
		WithFrameLock(func() {
			if p.detailsSeq != seq {
				return
			}
			if d.ExePath != "" {
				p.ExePath = d.ExePath
				p.Details.ExePath = d.ExePath
			}
			p.Details.Cwd = d.Cwd
			p.Details.Environ = d.Environ
			p.Details.Fetched = true
		})
		RequestNextFrame()
	}()
}

func SelectedPanel() {
	p := appData.selected
	if appData.snapshot == nil || p == nil {
		return
	}
	height := min(f32(184), max(f32(128), GetHost().WindowSize[1]*0.25))
	Element(Attrs(Expand, FixHeight(1), BackgroundVec(CurrentColorScheme.Surfaces.Panel.Border)))
	NextAccessName("process_inspector")
	Container(Attrs(Expand, FixHeight(height), Clip, NoAnimate, Pad2(8, 12), Gap(4), UseSurface(SurfaceCanvas)), func() {
		AssignAccess()
		Container(Attrs(Row, Expand, CrossMid, Gap(8)), func() {
			NextAccessName(NameDetailPID)
			NextAccessValue(strconv.Itoa(p.PID))
			AssignAccess()
			Container(Attrs(Row, Grow(1), Extrinsic, Expand, CrossMid, Gap(8), Clip), func() {
				processIconView(p.PID, p.ExePath, 18)
				Label(p.Name, FontSize(12), FontWeight(WeightBold))
				Label(fmt.Sprintf("PID %d · %s · %s", p.PID, p.User, lifeState(p)), FontSize(10), TextColorVec(CurrentColorScheme.List.Muted))
			})
			pinLabel := "Pin"
			if p.Pinned {
				pinLabel = "Unpin"
			}
			NextAccessName(NameBtnPin)
			NextAccessChecked(p.Pinned)
			if CtrlButton(TypPin, pinLabel, true) {
				p.Pinned = !p.Pinned
			}
			if p.Running() {
				if appData.killArmed {
					NextAccessName(NameBtnKillConfirm)
					NextButtonType(ButtonDestructive)
					if CtrlButton(SymDelete, "Confirm end", true) {
						if err := Kill(p.PID); err != nil {
							ToastExt(ToastAttrs{
								Icon:       SymFail,
								Title:      "Kill failed",
								Body:       err.Error(),
								Background: ToastBackgroundDanger,
							})
						} else {
							Toast(SymPass, "Kill sent", fmt.Sprintf("pid %d", p.PID))
						}
						appData.killArmed = false
					}
				} else {
					NextAccessName(NameBtnKill)
					if CtrlButton(SymDelete, "End process…", true) {
						appData.killArmed = true
					}
				}
			}
			NextAccessName(NameBtnDeselect)
			if CtrlButton(SymCancel, "", true) {
				appData.selected = nil
				appData.killArmed = false
			}
		})
		cmd := p.Cmdline
		if cmd == "" {
			cmd = p.Name
		}
		detailLine("Command", cmd)
		Container(Attrs(Row, Expand, CrossMid, Gap(16), Clip), func() {
			exe := p.Details.ExePath
			if exe == "" {
				exe = p.ExePath
			}
			Container(Attrs(Grow(1), Extrinsic, Expand, Clip), func() { detailLine("Executable", exe) })
			Label(fmt.Sprintf("Parent %d", p.PPID), FontSize(10), TextColorVec(CurrentColorScheme.List.Muted))
			Label("Started "+formatTime(p.StartTime), FontSize(10), TextColorVec(CurrentColorScheme.List.Muted))
			NextAccessName("process_details")
			CtrlMenuButton(SymDown, "Details", func() {
				Container(Attrs(MaxWidth(520), Gap(6), Pad(6)), func() {
					detailLine("Working directory", p.Details.Cwd)
					detailLine("Executable", exe)
					if !p.Running() {
						detailLine("Exited", formatTime(p.StoppedAt))
					}
				})
			})
		})
		HistoryCharts(p, max(f32(28), height-120))
	})
}

func detailLine(label, value string) {
	if value == "" {
		value = "unavailable"
	}
	Container(Attrs(Row, CrossMid, Gap(8), Expand, Clip), func() {
		Label(label, FontSize(10), TextColorVec(CurrentColorScheme.List.Muted))
		Label(value, FontSize(10))
	})
}

func displayedRows() []*Process {
	k := currentViewKey()
	if appData.viewRows != nil && appData.viewKey == k {
		return appData.viewRows
	}
	filter := appData.filter
	procs := appData.store.Processes()
	if appData.scope != 0 {
		kept := procs[:0]
		for _, p := range procs {
			if appData.scope == 1 && p.Running() || appData.scope == 2 && p.Pinned {
				kept = append(kept, p)
			}
		}
		procs = kept
	}
	rows := visibleRows(procs, filter,
		columnLess(cachedProcessColumns, appData.tableSort.Column), appData.tableSort.Desc, appData.treeMode)
	appData.viewRows = rows
	appData.viewKey = k
	return rows
}

func currentViewKey() viewKey {
	var snap time.Time
	if appData.snapshot != nil {
		snap = appData.snapshot.Time
	}
	var pins, collapse uint64
	for _, p := range appData.store.ByKey {
		if p.Pinned {
			pins ^= uint64(p.PID)*0x9e3779b97f4a7c15 + uint64(p.StartTime.UnixNano())
		}
		if p.Collapsed {
			collapse ^= uint64(p.PID) * 0xbf58476d1ce4e5b9
		}
	}
	filter := appData.filter
	return viewKey{
		snap:     snap,
		filter:   filter,
		col:      appData.tableSort.Column,
		desc:     appData.tableSort.Desc,
		tree:     appData.treeMode,
		scope:    appData.scope,
		pins:     pins,
		collapse: collapse,
	}
}

func visibleRows(procs []*Process, filter string, less func(a, b *Process) bool, desc, tree bool) []*Process {
	needle := strings.ToLower(strings.TrimSpace(filter))
	if tree {
		return treeRows(procs, needle, less, desc)
	}
	rows := make([]*Process, 0, len(procs))
	for _, p := range procs {
		if needle == "" || processMatches(p, needle) {
			rows = append(rows, p)
		}
	}
	orderProcesses(rows, less, desc)
	return rows
}

// orderProcesses sorts in place by the given column comparator, honoring
// the table's sort direction. A nil comparator keeps the given order.
func orderProcesses(procs []*Process, less func(a, b *Process) bool, desc bool) {
	sort.SliceStable(procs, func(i, j int) bool {
		if procs[i].Pinned != procs[j].Pinned {
			return procs[i].Pinned
		}
		if less == nil {
			return false
		}
		if desc {
			return less(procs[j], procs[i])
		}
		return less(procs[i], procs[j])
	})
}

// treeRows arranges processes as a parent→child forest keyed by PPID, flattened
// depth-first into display order. Sibling order follows the active sort. Each
// returned process has TreeDepth/TreeChildCount filled in for rendering. Collapsed
// subtrees are hidden. When a filter is active, only matching processes and their
// ancestors are shown, and collapse state is ignored so matches stay visible.
func treeRows(procs []*Process, needle string, less func(a, b *Process) bool, desc bool) []*Process {
	byPID := make(map[int]*Process, len(procs))
	for _, p := range procs {
		byPID[p.PID] = p
	}

	parentOf := func(p *Process) *Process {
		parent, ok := byPID[p.PPID]
		if !ok || parent == p {
			return nil
		}
		return parent
	}

	children := make(map[int][]*Process)
	var roots []*Process
	for _, p := range procs {
		if parent := parentOf(p); parent != nil {
			children[parent.PID] = append(children[parent.PID], p)
		} else {
			roots = append(roots, p)
		}
	}

	// Filtering: include matches plus their ancestor chain.
	var visible map[*Process]bool
	if needle != "" {
		visible = make(map[*Process]bool)
		for _, p := range procs {
			if !processMatches(p, needle) {
				continue
			}
			for cur := p; cur != nil && !visible[cur]; cur = parentOf(cur) {
				visible[cur] = true
			}
		}
	}

	shownChildren := func(p *Process) []*Process {
		kids := children[p.PID]
		if visible == nil {
			return kids
		}
		var out []*Process
		for _, c := range kids {
			if visible[c] {
				out = append(out, c)
			}
		}
		return out
	}

	orderProcesses(roots, less, desc)

	var rows []*Process
	seen := make(map[*Process]bool)
	var walk func(p *Process, depth int)
	walk = func(p *Process, depth int) {
		if seen[p] {
			return // cycle guard
		}
		seen[p] = true

		kids := shownChildren(p)
		orderProcesses(kids, less, desc)
		p.TreeDepth = depth
		p.TreeChildCount = len(kids)
		rows = append(rows, p)

		expanded := !p.Collapsed || visible != nil
		if expanded {
			for _, c := range kids {
				walk(c, depth+1)
			}
		}
	}
	for _, r := range roots {
		if visible != nil && !visible[r] {
			continue
		}
		walk(r, 0)
	}
	return rows
}

func processMatches(p *Process, needle string) bool {
	return strings.Contains(strings.ToLower(p.Name), needle) ||
		strings.Contains(strings.ToLower(p.Cmdline), needle) ||
		strings.Contains(strings.ToLower(p.User), needle) ||
		strings.Contains(strconv.Itoa(p.PID), needle)
}

const (
	historyWindow = 60 * time.Second
	historyBucket = time.Second
)

func HistoryCharts(p *Process, height f32) {
	Container(Attrs(Row, Expand, Gap(16), NoAnimate), func() {
		width := max(f32(80), (GetResolvedWidth()-32)/3)
		UsageChart(p, "CPU", 38, 100, 50, width, height,
			func(pt ProcessPoint) float64 { return pt.CPUPercent },
			func(v float64) string { return fmt.Sprintf("%.1f%%", v) })
		UsageChart(p, "Memory", 210, 512<<20, 512<<20, width, height,
			func(pt ProcessPoint) float64 { return float64(pt.RSSBytes) },
			func(v float64) string { return formatBytes(uint64(v)) })
		UsageChart(p, "Energy", 150, 1, 1, width, height,
			func(pt ProcessPoint) float64 { return pt.PowerWatts }, formatWatts)
	})
}

func metricColor(hue f32) Vec4 {
	light := f32(44)
	if CurrentColorScheme.Surfaces.Panel.Background[2] < 50 {
		light = 65
	}
	return Vec4{hue, 65, light, 1}
}

// steppedScale is the chart y-max: at least min, then jumps of step so a
// spike (e.g. 120% CPU with min 100 and step 50) raises the axis to 150, not
// to the raw peak.
func steppedScale(peak, min, step float64) float64 {
	if min <= 0 {
		min = 1
	}
	if peak <= min {
		return min
	}
	if step <= 0 {
		return peak
	}
	n := math.Ceil((peak - min) / step)
	return min + n*step
}

// UsageChart plots a minute of sampled history with an adaptive vertical scale.
func UsageChart(p *Process, title string, hue, minScale, step, width, height f32, valueFn func(ProcessPoint) float64, fmtFn func(float64) string) {
	buckets := resampleHistory(p.History, historyWindow, historyBucket, valueFn)
	peak, current := 0.0, "--"
	for _, b := range buckets {
		if b.HasData {
			peak = max(peak, b.Value)
			current = fmtFn(b.Value)
		}
	}
	if p.MetricsUnknown || title == "Energy" && p.PowerWatts < 0 {
		current = "--"
	}
	scale := steppedScale(peak, float64(minScale), float64(step))
	color := metricColor(hue)
	Container(Attrs(FixWidth(width), Gap(4), NoAnimate), func() {
		Container(Attrs(Row, Expand, CrossMid, Gap(8)), func() {
			Label(title, FontWeight(WeightSemibold))
			Label(current, Fonts(Monospace...), TextColorVec(color))
			Filler(1)
			Label(fmtFn(scale), FontSize(9), TextColorVec(CurrentColorScheme.List.Muted))
		})
		historyPlot(buckets, scale, width, height, color, true)
		Container(Attrs(Row, Expand), func() {
			Label("60s", FontSize(9), TextColorVec(CurrentColorScheme.List.Muted))
			Filler(1)
			Label("now", FontSize(9), TextColorVec(CurrentColorScheme.List.Muted))
		})
	})
}

// historyPlot joins fixed time buckets with thin steps and a faint area fill.
// Geometry is fixed between samples so idle frames can settle.
func historyPlot(buckets []HistBucket, scale float64, width, height f32, color Vec4, grid bool) {
	Container(Attrs(FixSize(width, height), Clip, NoAnimate), func() {
		if grid {
			line := CurrentColorScheme.Surfaces.Panel.Border
			line[3] *= 0.35
			for i := 0; i <= 2; i++ {
				Element(Attrs(Float(0, f32(i)*(height-1)/2), FixSize(width, 1), BackgroundVec(line)))
			}
			for i := 0; i <= 6; i++ {
				Element(Attrs(Float(f32(i)*(width-1)/6, 0), FixSize(1, height), BackgroundVec(line)))
			}
		}
		if len(buckets) == 0 {
			return
		}
		step := width / f32(len(buckets))
		fill := color
		fill[3] = 0.12
		var lastY f32
		hasLast := false
		for i, b := range buckets {
			if !b.HasData {
				hasLast = false
				continue
			}
			y := (height - 2) * (1 - f32(max(0, min(1, b.Value/scale))))
			x := f32(i) * step
			if grid {
				Element(Attrs(Float(x, y), FixSize(step, height-y), BackgroundVec(fill)))
			}
			Element(Attrs(Float(x, y), FixSize(step+0.5, 1.5), BackgroundVec(color)))
			if hasLast {
				Element(Attrs(Float(x, min(y, lastY)), FixSize(1, max(1.5, abs32(y-lastY))), BackgroundVec(color)))
			}
			lastY, hasLast = y, true
		}
	})
}

func abs32(v f32) f32 {
	if v < 0 {
		return -v
	}
	return v
}

type HistBucket struct {
	Value        float64
	HasData      bool
	Interpolated bool
}

// resampleHistory buckets raw, irregularly-spaced samples into a fixed number of
// equal-duration time slots. The rightmost slot ends at the most recent sample,
// slots march left into the past, samples landing in the same slot are averaged,
// and empty slots *between* two real slots are linearly interpolated so slow
// sampling still draws a continuous line. Slots before the first real sample are
// left empty rather than inventing data.
func resampleHistory(hist []ProcessPoint, window, bucket time.Duration, valueFn func(ProcessPoint) float64) []HistBucket {
	n := int(window / bucket)
	if n < 1 {
		n = 1
	}
	buckets := make([]HistBucket, n)
	if len(hist) == 0 {
		return buckets
	}

	// Bucket on fixed wall-clock boundaries instead of anchoring to the exact
	// latest sample timestamp. Otherwise sub-second samples would slide every
	// bucket boundary forward on each redraw, causing historical bars to be
	// re-averaged and visually change. With fixed bucket indices, only the
	// current bucket changes until time advances into the next bucket.
	bucketNS := bucket.Nanoseconds()
	if bucketNS <= 0 {
		bucketNS = int64(time.Second)
	}
	latestBucket := hist[len(hist)-1].Time.UnixNano() / bucketNS
	sums := make([]float64, n)
	counts := make([]int, n)
	for _, pt := range hist {
		ptBucket := pt.Time.UnixNano() / bucketNS
		fromRight := latestBucket - ptBucket
		if fromRight < 0 {
			fromRight = 0
		}
		idx := n - 1 - int(fromRight)
		if idx < 0 || idx >= n {
			continue
		}
		sums[idx] += valueFn(pt)
		counts[idx]++
	}
	for i := range buckets {
		if counts[i] > 0 {
			buckets[i].Value = sums[i] / float64(counts[i])
			buckets[i].HasData = true
		}
	}

	prev := -1
	for i := 0; i < n; i++ {
		if !buckets[i].HasData {
			continue
		}
		if prev >= 0 && i-prev > 1 {
			for k := prev + 1; k < i; k++ {
				t := float64(k-prev) / float64(i-prev)
				buckets[k].Value = buckets[prev].Value + (buckets[i].Value-buckets[prev].Value)*t
				buckets[k].HasData = true
				buckets[k].Interpolated = true
			}
		}
		prev = i
	}
	return buckets
}

func sortProcesses(procs []ProcInfo, sortBy string) {
	switch strings.ToLower(sortBy) {
	case "cpu", "":
		sort.SliceStable(procs, func(i, j int) bool {
			if procs[i].CPUPercent != procs[j].CPUPercent {
				return procs[i].CPUPercent > procs[j].CPUPercent
			}
			return procs[i].PID < procs[j].PID
		})
	case "mem", "rss":
		sort.SliceStable(procs, func(i, j int) bool {
			if procs[i].RSSBytes != procs[j].RSSBytes {
				return procs[i].RSSBytes > procs[j].RSSBytes
			}
			return procs[i].PID < procs[j].PID
		})
	case "pid":
		sort.SliceStable(procs, func(i, j int) bool { return procs[i].PID < procs[j].PID })
	case "name":
		sort.SliceStable(procs, func(i, j int) bool {
			if procs[i].Name != procs[j].Name {
				return procs[i].Name < procs[j].Name
			}
			return procs[i].PID < procs[j].PID
		})
	case "user":
		sort.SliceStable(procs, func(i, j int) bool {
			if procs[i].User != procs[j].User {
				return procs[i].User < procs[j].User
			}
			return procs[i].PID < procs[j].PID
		})
	case "state":
		sort.SliceStable(procs, func(i, j int) bool {
			if procs[i].State != procs[j].State {
				return procs[i].State < procs[j].State
			}
			return procs[i].PID < procs[j].PID
		})
	case "threads":
		sort.SliceStable(procs, func(i, j int) bool {
			if procs[i].Threads != procs[j].Threads {
				return procs[i].Threads > procs[j].Threads
			}
			return procs[i].PID < procs[j].PID
		})
	default:
		sortProcesses(procs, "cpu")
	}
}

func UsageBar(value, maxValue, hue f32) {
	const width f32 = 64
	const height f32 = 6
	ratio := f32(0)
	if maxValue > 0 {
		ratio = value / maxValue
	}
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}
	// NOTE the fill sets both dimensions explicitly: Expand means expand
	// along the parent's CROSS axis, and in this (default column) track that
	// is the width — leaving the height unset, i.e. an invisible fill.
	Container(Attrs(FixSize(width, height), Corners(2), BackgroundVec(CurrentColorScheme.Surfaces.Panel.Border)), func() {
		Element(Attrs(FixSize(width*ratio, height), Corners(2), BackgroundVec(metricColor(hue))))
	})
}

func percent(part, total uint64) f32 {
	if total == 0 {
		return 0
	}
	return f32(float64(part) / float64(total) * 100)
}

// formatCPU renders a CPU percentage value, with "--" for readings the OS
// would not let us take (CPUPercentUnknown).
func formatCPU(v float64) string {
	if v < 0 {
		return "--"
	}
	return fmt.Sprintf("%.1f", v)
}

// Metric cell text: "--" when the OS refused us the reading (MetricsUnknown),
// never a fake 0.
func (p *ProcInfo) CPUText() string { return formatCPU(p.CPUPercent) }

func (p *ProcInfo) PowerText() string {
	if p.PowerWatts < 0 {
		return "--"
	}
	return formatWatts(p.PowerWatts)
}

func formatWatts(v float64) string {
	if v < 0 {
		return "--"
	}
	if v < 10 {
		return fmt.Sprintf("%.2fW", v)
	}
	return fmt.Sprintf("%.1fW", v)
}

func (p *ProcInfo) RSSText() string {
	if p.MetricsUnknown {
		return "--"
	}
	return formatBytes(p.RSSBytes)
}

func (p *ProcInfo) MemText() string {
	if p.MetricsUnknown {
		return "--"
	}
	return fmt.Sprintf("%.2f%%", p.MemPercent)
}

func (p *ProcInfo) ThreadsText() string {
	if p.MetricsUnknown {
		return "--"
	}
	return strconv.Itoa(p.Threads)
}

func formatCPUPercent(v float64) string {
	if v < 0 {
		return "--"
	}
	return fmt.Sprintf("%.1f%%", v)
}

func formatBytes(v uint64) string {
	const KB = 1024
	const MB = KB * 1024
	const GB = MB * 1024
	const TB = GB * 1024
	switch {
	case v >= TB:
		return fmt.Sprintf("%.1fT", float64(v)/TB)
	case v >= GB:
		return fmt.Sprintf("%.1fG", float64(v)/GB)
	case v >= MB:
		return fmt.Sprintf("%.1fM", float64(v)/MB)
	case v >= KB:
		return fmt.Sprintf("%.1fK", float64(v)/KB)
	default:
		return fmt.Sprintf("%dB", v)
	}
}

func formatCount(v int64) string {
	s := fmt.Sprintf("%d", v)
	if len(s) <= 3 {
		return s
	}
	var out []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, c)
	}
	return string(out)
}

func formatAge(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	age := time.Since(t)
	if age < time.Second {
		return "just now"
	}
	return age.Round(time.Second).String() + " ago"
}

func samplesPerSecond(d time.Duration) float64 {
	if d <= 0 {
		return 0
	}
	return float64(time.Second) / float64(d)
}

func formatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d/time.Millisecond)
	}
	if d%time.Second == 0 {
		return fmt.Sprintf("%ds", d/time.Second)
	}
	return fmt.Sprintf("%.2fs", d.Seconds())
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return "unknown"
	}
	if time.Since(t) < 24*time.Hour {
		return t.Format("15:04:05")
	}
	return t.Format("Jan 2 15:04")
}

func truncate(s string, max int) string {
	if max <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	if max == 1 {
		return "…"
	}
	return string(r[:max-1]) + "…"
}
