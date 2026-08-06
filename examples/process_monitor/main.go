package main

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	app "go.hasen.dev/shirei/app"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

type f32 = float32

type AppState struct {
	snapshot *ProcSnapshot
	err      error

	filter   string
	treeMode bool
	selected *Process
	store    *ProcessStore

	samples      int
	period       time.Duration
	refreshEvery time.Duration
	lastRefresh  time.Time

	// tableSort is threaded into the process table (TableAttrs.SortState)
	// so tree mode can order siblings by the active column, and the -sort
	// flag can pick the starting column.
	tableSort TableSortState
}

var appData = &AppState{store: NewProcessStore()}

const rowHeight f32 = 36

func main() {
	samples := flag.Int("samples", 4, "number of process samples to collect per refresh")
	period := flag.Duration("period", 200*time.Millisecond, "total sampling period per refresh")
	limit := flag.Int("limit", 10, "number of processes to print in -once mode")
	sortBy := flag.String("sort", "cpu", "sort column: cpu, mem, pid, name")
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
	appData.tableSort.Desc = processColumns()[appData.tableSort.Column].DefaultDesc

	if *pngPath != "" {
		renderPNG(*pngPath)
		return
	}

	startSamplerLoop()

	app.SetupIconBytes(iconPNG)
	app.SetupWindow("Process Monitor", 1100, 700)
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
		columns := processColumns()
		rows := visibleRows(appData.store.Processes(), "",
			columnLess(columns, appData.tableSort.Column), appData.tableSort.Desc, false)
		if len(rows) > 0 {
			appData.selected = rows[0]
		}
	}
	if err := RenderToPNG(path, 1100, 700, RootView); err != nil {
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
	fmt.Printf("Memory: %s used / %s total\n\n", formatBytes(snap.UsedMemoryBytes), formatBytes(snap.TotalMemoryBytes))
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

func startSamplerLoop() {
	go func() {
		// Continuous mode: keep the previous refresh's snapshot and diff the next
		// one against it. That is one Collect() per refresh (not a burst of
		// `samples`), so CPU% is measured over the whole refresh interval and the
		// monitor's own overhead drops by roughly the burst size. The multi-sample
		// burst is only used by -once, where there is no "previous frame" to diff.
		sam := new(Sampler)
		for {
			var refresh time.Duration
			WithFrameLock(func() {
				refresh = appData.refreshEvery
			})

			started := time.Now()
			snap, err := sam.Sample()
			WithFrameLock(func() {
				appData.snapshot = snap
				appData.err = err
				appData.lastRefresh = time.Now()
				appData.store.Update(snap, appData.selected)
				if snap != nil && appData.selected == nil {
					columns := processColumns()
					rows := visibleRows(appData.store.Processes(), "",
						columnLess(columns, appData.tableSort.Column), appData.tableSort.Desc, false)
					if len(rows) > 0 {
						appData.selected = rows[0]
					}
				}
			})
			RequestNextFrame()

			if sleep := refresh - time.Since(started); sleep > 0 {
				time.Sleep(sleep)
			}
		}
	}()
}

func RootView() {

	Container(Attrs(Viewport, Background(220, 10, 97, 1)), func() {
		Header()
		Toolbar()
		ProcessTable()
		SelectedPanel()
	})
}

func Header() {
	Container(Attrs(Expand, Pad4(14, 16, 12, 16), Gap(8), Background(220, 25, 18, 1)), func() {
		Container(Attrs(Row, CrossMid, Expand, Gap(12)), func() {
			Label("Process Monitor", FontSize(18), FontWeight(WeightBold), TextColor(0, 0, 100, 1))
			Filler(1)
			ProfileButton("process_monitor")
		})

		if appData.err != nil {
			Label(fmt.Sprintf("sample error: %v", appData.err), TextColor(0, 80, 75, 1))
			return
		}
		if appData.snapshot == nil {
			Label("Collecting process samples…", TextColor(0, 0, 78, 1))
			return
		}

		Container(Attrs(Row, Expand, CrossMid, Gap(18)), func() {
			statPill("Processes", fmt.Sprintf("%s active / %s kept", formatCount(int64(appData.store.ActiveCount())), formatCount(int64(len(appData.store.ByKey)))))
			statPill("Updated", formatTime(appData.lastRefresh))

			Container(Attrs(Row, CrossMid, Gap(8)), func() {
				Label("Memory", FontSize(11), TextColor(0, 0, 80, 1))
				UsageBar(percent(appData.snapshot.UsedMemoryBytes, appData.snapshot.TotalMemoryBytes), 100, 180, 70, 55)
				Label(fmt.Sprintf("%s / %s", formatBytes(appData.snapshot.UsedMemoryBytes), formatBytes(appData.snapshot.TotalMemoryBytes)), FontSize(11), TextColor(0, 0, 88, 1))
			})
		})
	})
}

func statPill(label, value string) {
	Container(Attrs(Row, CrossMid, Gap(6), Pad4(5, 9, 5, 9), Corners(6), Background(220, 18, 28, 1)), func() {
		Label(label, FontSize(10), TextColor(0, 0, 70, 1))
		Label(value, FontSize(11), FontWeight(WeightBold), TextColor(0, 0, 98, 1))
	})
}

func Toolbar() {
	Container(Attrs(Row, Expand, CrossMid, Gap(12), Pad4(10, 14, 10, 14), Background(220, 8, 94, 1)), func() {
		Label("Filter", FontSize(11), FontWeight(WeightBold), TextColor(0, 0, 25, 1))
		Container(Attrs(FixWidth(300)), func() {
			TextInput(&appData.filter)
		})

		Filler(1)

		ViewModeControls()

		RefreshControls()
	})
}

func ViewModeControls() {
	Container(Attrs(Row, CrossMid, Gap(6)), func() {
		Label("View", FontSize(11), FontWeight(WeightBold), TextColor(0, 0, 25, 1))
		SegmentedControl(&appData.treeMode,
			Cell("Flat", false),
			Cell("Tree", true))
	})
}

func RefreshControls() {
	Container(Attrs(Row, CrossMid, Gap(8)), func() {
		Label("Sample every", FontSize(11), FontWeight(WeightBold), TextColor(0, 0, 25, 1))
		SegmentedControl(&appData.refreshEvery,
			Cell("0.5s", 500*time.Millisecond),
			Cell("1s", time.Second),
			Cell("2s", 2*time.Second),
			Cell("5s", 5*time.Second))
	})
}

// processColumns defines the table: widths and header labels as before the
// shared-Table adoption, cell text via the ProcInfo *Text helpers, and
// ascending Less funcs with a PID tiebreak so rows with equal keys don't
// jiggle between refreshes (store iteration order is random).
func processColumns() []TableColumn[*Process] {
	return []TableColumn[*Process]{
		{Label: "PID", Width: 70,
			Render: func(p *Process) { rowLabel(fmt.Sprintf("%d", p.PID)) },
			Less:   func(a, b *Process) bool { return a.PID < b.PID }},
		{Label: "CPU", Width: 140, DefaultDesc: true,
			Render: func(p *Process) {
				Container(Attrs(Row, CrossMid, Gap(7)), func() {
					Label(fmt.Sprintf("%5s", p.CPUText()), FontSize(11), TextColor(0, 0, 15, 1))
					UsageBar(f32(max(p.CPUPercent, 0)), 100, 18, 75, 52)
				})
			},
			Less: func(a, b *Process) bool {
				if a.CPUPercent != b.CPUPercent {
					return a.CPUPercent < b.CPUPercent
				}
				return a.PID < b.PID
			}},
		{Label: "RSS", Width: 92, DefaultDesc: true,
			Render: func(p *Process) { rowLabel(p.RSSText()) },
			Less:   lessRSS},
		{Label: "MEM%", Width: 68, DefaultDesc: true,
			Render: func(p *Process) { rowLabel(p.MemText()) },
			Less:   lessRSS},
		{Label: "USER", Width: 105,
			Render: func(p *Process) { rowLabel(p.User) },
			Less: func(a, b *Process) bool {
				if a.User != b.User {
					return a.User < b.User
				}
				return a.PID < b.PID
			}},
		{Label: "STATE", Width: 62,
			Render: func(p *Process) { rowLabel(p.State) },
			Less: func(a, b *Process) bool {
				if a.State != b.State {
					return a.State < b.State
				}
				return a.PID < b.PID
			}},
		{Label: "THR", Width: 48, DefaultDesc: true,
			Render: func(p *Process) { rowLabel(p.ThreadsText()) },
			Less: func(a, b *Process) bool {
				if a.Threads != b.Threads {
					return a.Threads < b.Threads
				}
				return a.PID < b.PID
			}},
		{Label: "NAME", // flexible
			Render: nameCell,
			Less: func(a, b *Process) bool {
				if a.Name != b.Name {
					return a.Name < b.Name
				}
				return a.PID < b.PID
			}},
	}
}

func lessRSS(a, b *Process) bool {
	if a.RSSBytes != b.RSSBytes {
		return a.RSSBytes < b.RSSBytes
	}
	return a.PID < b.PID
}

func rowLabel(text string) {
	Label(text, FontSize(11), TextColor(0, 0, 18, 1))
}

func nameCell(p *Process) {
	name := p.Name
	if name == "" {
		name = p.Cmdline
	}
	Container(Attrs(Row, CrossMid, Clip), func() {
		if appData.treeMode {
			if depth := min(p.TreeDepth, 8); depth > 0 {
				Element(Attrs(FixWidth(f32(depth) * 14)))
			}
			if p.TreeChildCount > 0 {
				Container(Attrs(FixWidth(14), CrossMid), func() {
					if PressAction() {
						p.Collapsed = !p.Collapsed
					}
					if p.Collapsed {
						Label("▸", FontSize(10), TextColor(0, 0, 30, 1))
					} else {
						Label("▾", FontSize(10), TextColor(0, 0, 30, 1))
					}
				})
			} else {
				Element(Attrs(FixWidth(14)))
			}
		}
		Label(name, FontSize(11), TextColor(0, 0, 12, 1))
	})
}

// sortColumnIndex maps the -sort flag's key to a column index.
func sortColumnIndex(key string) int {
	switch strings.ToLower(key) {
	case "pid":
		return 0
	case "mem", "rss":
		return 2
	case "user":
		return 4
	case "state":
		return 5
	case "threads":
		return 6
	case "name":
		return 7
	default: // "cpu" and anything unrecognized
		return 1
	}
}

// columnLess returns the active sort column's comparator (nil-safe).
func columnLess(columns []TableColumn[*Process], column int) func(a, b *Process) bool {
	if column >= 0 && column < len(columns) {
		return columns[column].Less
	}
	return nil
}

func ProcessTable() {
	Container(Attrs(Grow(1), Expand, Clip, NoAnimate, Background(0, 0, 100, 1)), func() {
		if appData.snapshot == nil {
			Container(Attrs(Viewport, Center), func() {
				Label("Waiting for first sample…", TextColor(0, 0, 45, 1))
			})
			return
		}

		// Rows are filtered and ordered here, not by the table's flat sort:
		// tree mode orders siblings within the hierarchy, and the filter
		// keeps matches' ancestors visible. The table still owns the header
		// UI and writes appData.tableSort, which this read picks up (a sort
		// click reorders on the next frame).
		columns := processColumns()
		rows := visibleRows(appData.store.Processes(), appData.filter,
			columnLess(columns, appData.tableSort.Column), appData.tableSort.Desc, appData.treeMode)
		if len(rows) == 0 {
			Container(Attrs(Viewport, Center), func() {
				Label("No matching processes", TextColor(0, 0, 45, 1))
			})
			return
		}

		attrs := TableAttrs[*Process]{
			RowHeight: rowHeight,
			SortState: &appData.tableSort,
			OrderRows: func(rows []*Process, column int, desc bool) []*Process { return rows },
			OnRow: func(i int, p *Process) {
				bgLight := f32(100)
				if i%2 == 1 {
					bgLight = 97
				}
				if appData.selected == p {
					bgLight = 87
				}
				ModAttrs(NoAnimate, Background(220, 8, bgLight, 1))
				if IsHovered() {
					ModAttrs(Background(220, 12, 92, 1))
				}
				if PressAction() {
					appData.selected = p
				}
			},
		}
		TableExt(nil, attrs, columns, rows, func(p *Process) any { return p })
	})
}

func SelectedPanel() {
	Container(Attrs(Expand, Pad4(10, 14, 12, 14), Gap(5), Background(220, 10, 94, 1)), func() {
		if appData.snapshot == nil || appData.selected == nil {
			Label("Select a process", FontSize(11), TextColor(0, 0, 45, 1))
			return
		}
		p := appData.selected

		Container(Attrs(Row, CrossMid, Gap(12)), func() {
			Label(fmt.Sprintf("%s  pid %d", p.Name, p.PID), FontSize(13), FontWeight(WeightBold), TextColor(0, 0, 15, 1))
			Label(fmt.Sprintf("ppid %d", p.PPID), FontSize(11), TextColor(0, 0, 40, 1))
			Label("cpu "+formatCPUPercent(p.CPUPercent), FontSize(11), TextColor(0, 0, 40, 1))
			Label("rss "+p.RSSText(), FontSize(11), TextColor(0, 0, 40, 1))
			Label(fmt.Sprintf("started %s", formatTime(p.StartTime)), FontSize(11), TextColor(0, 0, 40, 1))
			if !p.Running() {
				Label(fmt.Sprintf("stopped %s", formatTime(p.StoppedAt)), FontSize(11), TextColor(0, 70, 45, 1))
			}
		})
		cmd := p.Cmdline
		if cmd == "" {
			cmd = p.Name
		}
		Label(cmd, FontSize(10), TextColor(0, 0, 30, 1))
		HistoryCharts(p)
	})
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
	if less == nil {
		return
	}
	sort.SliceStable(procs, func(i, j int) bool {
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

func HistoryCharts(p *Process) {
	Container(Attrs(Row, Gap(10)), func() {
		UsageChart(p, "CPU", 18, 100,
			func(pt ProcessPoint) float64 { return pt.CPUPercent },
			func(v float64) string { return fmt.Sprintf("%.0f%%", v) })
		UsageChart(p, "RAM", 210, ramHistoryScale(p.History),
			func(pt ProcessPoint) float64 { return float64(pt.RSSBytes) },
			func(v float64) string { return formatBytes(uint64(v)) })
	})
}

func ramHistoryScale(hist []ProcessPoint) f32 {
	const step = 500 * 1024 * 1024 // 500MB
	var maxRSS uint64
	for _, pt := range hist {
		if pt.RSSBytes > maxRSS {
			maxRSS = pt.RSSBytes
		}
	}
	if maxRSS == 0 {
		return f32(step)
	}
	scale := ((maxRSS + step - 1) / step) * step
	if scale < step {
		scale = step
	}
	return f32(scale)
}

// UsageChart draws one fixed-window, fixed-bucket history chart for the selected
// process. valueFn selects which metric to plot; minScale is the lowest the
// y-axis top is allowed to be (CPU pins it at 100%, RAM lets it float to the max
// seen); hue picks the bar color; fmtFn formats the scale/last readout.
func UsageChart(p *Process, title string, hue, minScale f32, valueFn func(ProcessPoint) float64, fmtFn func(float64) string) {
	const width f32 = 300
	const height f32 = 72
	const chartHeight f32 = height - 12
	const gap f32 = 1

	buckets := resampleHistory(p.History, historyWindow, historyBucket, valueFn)

	scale := float64(minScale)
	for _, b := range buckets {
		if b.HasData && b.Value > scale {
			scale = b.Value
		}
	}
	if scale <= 0 {
		scale = 1
	}

	Container(Attrs(FixWidth(width), Gap(4)), func() {
		Container(Attrs(FixWidth(width), FixHeight(height), Clip, Pad(6), Corners(6), Background(220, 8, 88, 1)), func() {
			// Top-left metric label, drawn floating and translucent so it sits
			// over the bars without taking layout space.
			Container(Attrs(Float(6, 6), InFront), func() {
				Label(title, FontSize(12), FontWeight(WeightBold), TextColor(0, 0, 0, 0.5))
			})

			Container(Attrs(Row, Expand, FixHeight(chartHeight), Gap(gap)), func() {
				if len(p.History) == 0 {
					return
				}
				// Each bucket is a fixed 1s time slot; oldest on the left, newest
				// on the right. Bars grow to fill the panel width so the x-axis is
				// a fixed time window, independent of the sampling rate.
				for _, b := range buckets {
					Container(Attrs(Grow(1), FixHeight(chartHeight), NoAnimate), func() {
						if !b.HasData {
							// No data for this time slot yet: faint baseline.
							Filler(1)
							Element(Attrs(FixHeight(1), Expand, Background(0, 0, 82, 1)))
							return
						}
						ratio := f32(b.Value / scale)
						if ratio < 0 {
							ratio = 0
						}
						if ratio > 1 {
							ratio = 1
						}
						barHeight := max(f32(1), ratio*chartHeight)
						sat, light := f32(75), f32(52)
						if b.Interpolated {
							sat, light = 40, 66 // interpolated slots are drawn lighter
						}
						Filler(1)
						Element(Attrs(FixHeight(barHeight), Expand, Background(hue, sat, light, 1)))
					})
				}
			})
		})

		Container(Attrs(Row, CrossMid, Gap(8)), func() {
			Label(fmt.Sprintf("scale %s", fmtFn(scale)), FontSize(9), TextColor(0, 0, 50, 1))
		})
	})
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

func UsageBar(value, maxValue, hue, sat, light f32) {
	const width f32 = 76
	const height f32 = 9
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
	Container(Attrs(FixSize(width, height), Corners(4), Background(0, 0, 86, 1)), func() {
		Element(Attrs(FixSize(width*ratio, height), Corners(4), Background(hue, sat, light, 1)))
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
