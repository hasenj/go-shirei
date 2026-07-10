package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/cespare/xxhash/v2"
	"github.com/fsnotify/fsnotify"
	"github.com/google/pprof/profile"

	app "go.hasen.dev/shirei/app"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

type f32 = float32

const (
	colFlat   = 90
	colPct    = 60
	colCum    = 90
	rowHeight = 30

	flameRowHeight     = 18
	flameMinLabelWidth = 30
	flameZoomSpeed     = 0.004 // tune to taste: higher = faster zoom per scroll tick
	flameMaxScale      = 100000

	sidebarSplitterWidth = 6
)

// sidebarWidth is draggable (see the splitter in RootView); a package var
// rather than AppState since there's only ever one window.
var sidebarWidth f32 = 240

// searchQuery filters the top list and dims non-matching flame frames.
// Deliberately NOT reset when switching profiles: keeping the filter while
// flipping between files is how you compare the same function across runs.
var searchQuery string

// searchTerm is the normalized form of searchQuery used for matching:
// case-insensitive, surrounding whitespace ignored. Empty means no filter.
func searchTerm() string {
	return strings.ToLower(strings.TrimSpace(searchQuery))
}

func nameMatches(name, term string) bool {
	return strings.Contains(strings.ToLower(name), term)
}

func filterStats(stats []*FuncStat, term string) []*FuncStat {
	if term == "" {
		return stats
	}
	out := make([]*FuncStat, 0, len(stats))
	for _, s := range stats {
		if nameMatches(s.Name, term) {
			out = append(out, s)
		}
	}
	return out
}

// FuncStat aggregates one sample-value axis (e.g. cpu/nanoseconds) for a
// single function: Flat is time/space attributed directly to it, Cum also
// includes everything it called. Keyed by name rather than *profile.Function
// so the same type can also represent a focused-subtree aggregation (see
// computeFocusedStats), which only has FlameNode names to work with.
type FuncStat struct {
	Name string
	Flat int64
	Cum  int64
}

// ProfileFileInfo is what the sidebar shows for one .pprof file without
// requiring the user to select it: cheap metadata (name, mtime) plus
// whatever we can learn by actually parsing it (the profiled program's name,
// from its first Mapping entry, and its sample count).
type ProfileFileInfo struct {
	Name        string
	ModTime     time.Time
	Size        int64
	ProgramName string
	SampleCount int
	ParseErr    error
}

type AppState struct {
	dir   string
	files []ProfileFileInfo // sorted newest-first

	selected     string
	selectedSize int64
	prof         *profile.Profile
	parseErr     error

	valueIndex int
	stats      []*FuncStat
	total      int64
	sampleTyp  string
	sampleUnt  string

	flameRoot     *FlameNode
	flameMaxDepth int
}

var appData = new(AppState)

func main() {
	appData.dir, _ = os.Getwd()
	refreshFileList()
	if len(appData.files) > 0 {
		selectFile(appData.files[0].Name)
	}

	// `see_pprof --png out.png` renders one settled frame (of the newest
	// profile in the current directory) and exits — headless verification.
	if len(os.Args) >= 3 && os.Args[1] == "--png" {
		if err := RenderToPNG(os.Args[2], 1100, 700, RootView); err != nil {
			fmt.Println("render to png failed:", err)
		}
		return
	}

	startWatchingDir(appData.dir)

	app.SetupWindow("see_pprof", 1100, 700)
	app.SetupIconBytes(iconPNG) // a tiny flame graph — distinct in the Dock
	app.Run(RootView)
}

func refreshFileList() {
	entries, _ := os.ReadDir(appData.dir)
	var files []ProfileFileInfo
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.EqualFold(filepath.Ext(e.Name()), ".pprof") {
			files = append(files, cachedProfileFileInfo(e.Name()))
		}
	}
	sort.SliceStable(files, func(i, j int) bool {
		if !files[i].ModTime.Equal(files[j].ModTime) {
			return files[i].ModTime.After(files[j].ModTime) // newest first
		}
		return files[i].Name < files[j].Name
	})
	appData.files = files
}

// cachedProfileFileInfo re-parses a file only the first time we see it at a
// given mtime; a rescan triggered by some other file changing in the
// directory just reuses the cached ProfileFileInfo. Cached via shirei's
// UseData, keyed by filename — a data hook rather than a UI hook, so it
// survives across the many frames where this file isn't being rebuilt.
func cachedProfileFileInfo(name string) ProfileFileInfo {
	fullPath := filepath.Join(appData.dir, name)
	var modTime time.Time
	var size int64
	if fi, err := os.Stat(fullPath); err == nil {
		modTime = fi.ModTime()
		size = fi.Size()
	}

	cached := UseData[ProfileFileInfo](name, "profile-file-info")
	if cached.Name == name && cached.ModTime.Equal(modTime) {
		return *cached
	}

	*cached = inspectProfileFile(name, modTime, size)
	return *cached
}

// inspectProfileFile parses the file and, if it parses, pulls out the
// profiled program's name and sample count. A parse failure (e.g. a file
// mid-write) just means degraded metadata, not an error shown to the user
// here — the real error surfaces if they select it.
func inspectProfileFile(name string, modTime time.Time, size int64) ProfileFileInfo {
	info := ProfileFileInfo{Name: name, ModTime: modTime, Size: size}

	fullPath := filepath.Join(appData.dir, name)
	f, err := os.Open(fullPath)
	if err != nil {
		info.ParseErr = err
		return info
	}
	defer f.Close()

	p, err := profile.Parse(f)
	if err != nil {
		info.ParseErr = err
		return info
	}
	info.SampleCount = len(p.Sample)
	if len(p.Mapping) > 0 && p.Mapping[0].File != "" {
		info.ProgramName = filepath.Base(p.Mapping[0].File)
	}
	return info
}

// startWatchingDir keeps the sidebar's file list live: whenever a .pprof
// file is created, written, removed, or renamed in dir, it re-scans and
// wakes the render loop, so a program using ProfileButton (DEBUG=1) shows up
// here as soon as it commits its profile.
func startWatchingDir(dir string) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		fmt.Println("profile watch: failed to start:", err)
		return
	}
	if err := watcher.Add(dir); err != nil {
		fmt.Println("profile watch: failed to watch", dir, err)
		return
	}

	go func() {
		var debounce *time.Timer
		refresh := func() {
			WithFrameLock(refreshFileList)
			RequestNextFrame()
		}
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if !strings.EqualFold(filepath.Ext(event.Name), ".pprof") {
					continue
				}
				if debounce != nil {
					debounce.Stop()
				}
				debounce = time.AfterFunc(200*time.Millisecond, refresh)
			case _, ok := <-watcher.Errors:
				if !ok {
					return
				}
			}
		}
	}()
}

func selectFile(name string) {
	appData.selected = name
	appData.selectedSize = 0
	for _, f := range appData.files {
		if f.Name == name {
			appData.selectedSize = f.Size
			break
		}
	}
	appData.parseErr = nil
	appData.prof = nil
	appData.stats = nil
	appData.flameRoot = nil
	appData.flameMaxDepth = 0

	f, err := os.Open(filepath.Join(appData.dir, name))
	if err != nil {
		appData.parseErr = err
		return
	}
	defer f.Close()

	p, err := profile.Parse(f)
	if err != nil {
		appData.parseErr = err
		return
	}
	appData.prof = p
	computeStats(p)
}

// defaultValueIndex picks the sample-value axis to display, matching the
// convention `go tool pprof` uses: the profile's declared default, or the
// last axis (e.g. heap profiles list alloc_objects/alloc_space/
// inuse_objects/inuse_space in that order, and "inuse_space" is usually
// what you want).
func defaultValueIndex(p *profile.Profile) int {
	if p.DefaultSampleType != "" {
		for i, st := range p.SampleType {
			if st.Type == p.DefaultSampleType {
				return i
			}
		}
	}
	return len(p.SampleType) - 1
}

func computeStats(p *profile.Profile) {
	if len(p.SampleType) == 0 {
		return
	}
	valueIndex := defaultValueIndex(p)
	appData.valueIndex = valueIndex
	appData.sampleTyp = p.SampleType[valueIndex].Type
	appData.sampleUnt = p.SampleType[valueIndex].Unit

	byFunc := make(map[string]*FuncStat)
	getOrCreate := func(name string) *FuncStat {
		stat, ok := byFunc[name]
		if !ok {
			stat = &FuncStat{Name: name}
			byFunc[name] = stat
		}
		return stat
	}

	var total int64
	var seen = make(map[string]bool)
	for _, s := range p.Sample {
		if valueIndex >= len(s.Value) {
			continue
		}
		val := s.Value[valueIndex]
		total += val

		if len(s.Location) == 0 {
			continue
		}

		// flat: attribute to the innermost function at the leaf frame
		// (locations are leaf-first; within a location, Line[0] is the
		// innermost line, which matters when frames are inlined)
		if leaf := s.Location[0]; len(leaf.Line) > 0 && leaf.Line[0].Function != nil {
			getOrCreate(leaf.Line[0].Function.Name).Flat += val
		}

		// cumulative: every distinct function anywhere in the stack, once
		// per sample even if it recurses
		clear(seen)
		for _, loc := range s.Location {
			for _, line := range loc.Line {
				if line.Function == nil || seen[line.Function.Name] {
					continue
				}
				seen[line.Function.Name] = true
				getOrCreate(line.Function.Name).Cum += val
			}
		}
	}
	appData.total = total

	stats := make([]*FuncStat, 0, len(byFunc))
	for _, stat := range byFunc {
		stats = append(stats, stat)
	}
	sortStats(stats)
	appData.stats = stats

	appData.flameRoot, appData.flameMaxDepth = buildFlameTree(p, valueIndex)
}

// FlameNode is one merged call-tree node: the set of samples that share the
// same call path up to this frame. Value is self + all descendants, which is
// what determines the node's width in the flame graph.
type FlameNode struct {
	Name     string
	Value    int64
	Hue      f32
	Children []*FlameNode

	childIdx map[string]int // name -> index in Children; only needed while building
}

func buildFlameTree(p *profile.Profile, valueIndex int) (*FlameNode, int) {
	root := &FlameNode{Name: "root"}
	var stack []string
	maxDepth := 0
	for _, s := range p.Sample {
		if valueIndex >= len(s.Value) || len(s.Location) == 0 {
			continue
		}
		val := s.Value[valueIndex]

		// Locations are leaf-first, and within a location Line[0] is the
		// innermost (possibly inlined) frame — walk both in reverse to get
		// a root-to-leaf call stack.
		stack = stack[:0]
		for i := len(s.Location) - 1; i >= 0; i-- {
			loc := s.Location[i]
			for j := len(loc.Line) - 1; j >= 0; j-- {
				if fn := loc.Line[j].Function; fn != nil {
					stack = append(stack, fn.Name)
				}
			}
		}
		maxDepth = max(maxDepth, len(stack))
		addFlameStack(root, stack, val)
	}
	return root, maxDepth
}

func addFlameStack(root *FlameNode, stack []string, val int64) {
	node := root
	node.Value += val
	for _, name := range stack {
		if node.childIdx == nil {
			node.childIdx = make(map[string]int)
		}
		var child *FlameNode
		if idx, ok := node.childIdx[name]; ok {
			child = node.Children[idx]
		} else {
			child = &FlameNode{Name: name, Hue: f32(xxhash.Sum64String(name) % 360)}
			node.childIdx[name] = len(node.Children)
			node.Children = append(node.Children, child)
		}
		child.Value += val
		node = child
	}
}

// computeFocusedStats re-aggregates Flat/Cum for just the subtree rooted at
// focus, so clicking a frame can scope the top list to "everything inside
// this box" the same way clicking a frame does in other profilers. Same
// per-name aggregation rule as computeStats, just walked over the merged
// FlameNode tree instead of raw samples: Flat at a node is its own Value
// minus its children's (self time at that exact call-path occurrence), Cum
// is its Value, and an ancestor-name guard stops a recursive occurrence
// from being counted twice (mirroring computeStats' seen set, just
// expressed as a path check instead of a per-sample one).
func computeFocusedStats(focus *FlameNode) ([]*FuncStat, int64) {
	byName := make(map[string]*FuncStat)
	getOrCreate := func(name string) *FuncStat {
		stat, ok := byName[name]
		if !ok {
			stat = &FuncStat{Name: name}
			byName[name] = stat
		}
		return stat
	}

	ancestors := make(map[string]bool)
	var walk func(node *FlameNode)
	walk = func(node *FlameNode) {
		var childSum int64
		for _, c := range node.Children {
			childSum += c.Value
		}
		getOrCreate(node.Name).Flat += node.Value - childSum

		if !ancestors[node.Name] {
			ancestors[node.Name] = true
			getOrCreate(node.Name).Cum += node.Value
			defer delete(ancestors, node.Name)
		}

		for _, c := range node.Children {
			walk(c)
		}
	}
	walk(focus)

	stats := make([]*FuncStat, 0, len(byName))
	for _, stat := range byName {
		stats = append(stats, stat)
	}
	sortStats(stats)
	return stats, focus.Value
}

// sortStats orders by Flat descending with a name tie-break: the slices
// are built from map iteration, so without the tie-break equal-Flat rows
// (every zero-flat function) would come out in a different order on every
// recompute.
func sortStats(stats []*FuncStat) {
	sort.Slice(stats, func(i, j int) bool {
		if stats[i].Flat != stats[j].Flat {
			return stats[i].Flat > stats[j].Flat
		}
		return stats[i].Name < stats[j].Name
	})
}

// computeCallerCallee aggregates, over the given scope subtree, the direct
// callers and callees of the function named fn — the data behind PeekView.
// Only *topmost* occurrences of fn count (an fn nested under another fn is
// already part of the outer one's subtree, mirroring computeFocusedStats'
// ancestor guard): each unit of cost then flows into fn exactly once and
// out exactly once, so the callers, the callees ("(self)" included), and
// total all sum to the same number — every percentage is a fraction of the
// same total. Recursion consequently shows up on the callee side (cost
// flowing from fn back down into fn), never as fn being its own caller.
func computeCallerCallee(scope *FlameNode, fn string) (callers, callees []*FuncEdge, total int64) {
	callerMap := make(map[string]int64)
	calleeMap := make(map[string]int64)
	var self int64

	var walk func(node *FlameNode, parentName string, inside bool)
	walk = func(node *FlameNode, parentName string, inside bool) {
		top := node.Name == fn && !inside
		if top {
			total += node.Value
			callerMap[parentName] += node.Value
			var childSum int64
			for _, c := range node.Children {
				childSum += c.Value
				calleeMap[c.Name] += c.Value
			}
			self += node.Value - childSum
		}
		for _, c := range node.Children {
			walk(c, node.Name, inside || top)
		}
	}

	if scope == appData.flameRoot {
		// the root node is synthetic, not a real caller
		for _, c := range scope.Children {
			walk(c, "(root)", false)
		}
	} else {
		// a focused frame: whatever called it lies outside the scope
		walk(scope, "(outside focus)", false)
	}

	edgeList := func(m map[string]int64) []*FuncEdge {
		out := make([]*FuncEdge, 0, len(m))
		for name, v := range m {
			out = append(out, &FuncEdge{Name: name, Value: v})
		}
		// name tie-break: the map iteration order above is random
		sort.Slice(out, func(i, j int) bool {
			if out[i].Value != out[j].Value {
				return out[i].Value > out[j].Value
			}
			return out[i].Name < out[j].Name
		})
		return out
	}
	callers = edgeList(callerMap)
	if self > 0 {
		calleeMap["(self)"] = self
	}
	callees = edgeList(calleeMap)
	return
}

func formatValue(v int64, unit string) string {
	switch unit {
	case "nanoseconds":
		return time.Duration(v).String()
	case "bytes":
		return formatBytes(v)
	default:
		return formatCount(v)
	}
}

func formatBytes(v int64) string {
	const KB = 1024
	const MB = KB * 1024
	const GB = MB * 1024
	switch {
	case v >= GB:
		return fmt.Sprintf("%.2fGB", float64(v)/GB)
	case v >= MB:
		return fmt.Sprintf("%.2fMB", float64(v)/MB)
	case v >= KB:
		return fmt.Sprintf("%.2fKB", float64(v)/KB)
	default:
		return fmt.Sprintf("%dB", v)
	}
}

func formatCount(v int64) string {
	s := strconv.FormatInt(v, 10)
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	var out []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, c)
	}
	if neg {
		return "-" + string(out)
	}
	return string(out)
}

func formatPercent(part, total int64) string {
	if total == 0 {
		return "0%"
	}
	return fmt.Sprintf("%.1f%%", float64(part)/float64(total)*100)
}

// formatModTime shows a bare time for today's files (the common case, since
// these are usually just-generated) and a date for anything older.
func formatModTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	now := time.Now()
	if t.Year() == now.Year() && t.YearDay() == now.YearDay() {
		return t.Format("15:04:05")
	}
	return t.Format("Jan 2, 15:04")
}

// filePrimary is the sidebar's first line: what the profile actually is
// (program + when), rather than its filename.
func filePrimary(info ProfileFileInfo) string {
	ts := formatModTime(info.ModTime)
	if info.ProgramName == "" {
		return ts
	}
	return fmt.Sprintf("%s · %s", info.ProgramName, ts)
}

// fileStats is the sidebar's third line: sample count plus file size, kept
// off the filename line since long filenames were crowding them out.
func fileStats(info ProfileFileInfo) string {
	if info.ParseErr != nil {
		return formatBytes(info.Size)
	}
	return fmt.Sprintf("%s samples · %s", formatCount(int64(info.SampleCount)), formatBytes(info.Size))
}

func RootView() {
	ProfileButton("see_pprof")

	Container(Attrs(Row, Viewport), func() {
		Sidebar()

		Container(Attrs(FixWidth(sidebarSplitterWidth), Expand, Background(0, 0, 80, 1)), func() {
			if IsHovered() {
				ModAttrs(Background(210, 60, 60, 1))
			}
			PressAction()
			if IsActive() {
				sidebarWidth = clampF32(sidebarWidth+FrameInput.Motion[0], 160, 480)
			}
		})

		MainContent()
	})
}

func Sidebar() {
	Container(Attrs(FixWidth(sidebarWidth), Expand, Clip, Background(0, 0, 95, 1)), func() {
		Container(Attrs(Expand, Pad4(14, 14, 10, 14)), func() {
			Label("Profiles", FontWeight(WeightBold), FontSize(14))
		})

		Container(Attrs(Viewport), func() {
			ScrollOnInput()
			ScrollBars()

			for _, entry := range appData.files {
				Container(Attrs(Expand, Clip, Gap(2), Pad4(8, 14, 8, 14)), func() {
					selected := appData.selected == entry.Name
					if selected {
						ModAttrs(Background(210, 70, 50, 1))
					} else if IsHovered() {
						ModAttrs(Background(0, 0, 90, 1))
					}
					if PressAction() {
						selectFile(entry.Name)
					}
					textColor := Vec4{0, 0, 10, 1}
					subtitleColor := Vec4{0, 0, 45, 1}
					if selected {
						textColor = Vec4{0, 0, 100, 1}
						subtitleColor = Vec4{0, 0, 88, 1}
					}
					Label(filePrimary(entry), TextColorVec(textColor))
					Label(entry.Name, FontSize(10), TextColorVec(subtitleColor))
					Label(fileStats(entry), FontSize(10), TextColorVec(subtitleColor))
				})
			}

			if len(appData.files) == 0 {
				Container(Attrs(Expand, Pad4(8, 14, 8, 14)), func() {
					Label("No .pprof files here", FontStyle(StyleItalic), TextColorVec(Vec4{0, 0, 50, 1}))
				})
			}
		})
	})
}

// mainSplitRatio is the fraction of MainContent's height given to the top
// (function list) section; the rest goes to the flame graph, minus the
// splitter itself. There's only one MainContent in this app, so a package
// var is simpler than threading state through.
var mainSplitRatio f32 = 0.5

const splitterHeight = 6

func MainContent() {
	Container(Attrs(Grow(1), Expand, Clip, Background(0, 0, 100, 1)), func() {
		if appData.selected == "" {
			Container(Attrs(Viewport, Center), func() {
				Label("Select a profile from the list", FontSize(14), TextColorVec(Vec4{0, 0, 55, 1}))
			})
			return
		}

		if appData.parseErr != nil {
			Container(Attrs(Viewport, Center, Pad(20)), func() {
				Label(fmt.Sprintf("Failed to parse %s: %v", appData.selected, appData.parseErr),
					FontSize(13), TextColorVec(Vec4{0, 70, 45, 1}))
			})
			return
		}

		// scoped by flameRoot so switching profiles resets zoom/pan/focus
		ContainerWithKey(appData.flameRoot, Attrs(Grow(1), Expand, Clip), func() {
			state := UseWithInit[FlameState]("flame-state", func() *FlameState {
				return &FlameState{scale: 1}
			})

			totalHeight := GetResolvedSize()[1]
			topAttrs := Attrs(Grow(1), Expand, Clip)
			bottomAttrs := Attrs(Grow(1), Expand, Clip)
			if totalHeight > 0 {
				available := totalHeight - splitterHeight
				topHeight := available * mainSplitRatio
				topAttrs = Attrs(FixHeight(topHeight), Expand, Clip)
				bottomAttrs = Attrs(FixHeight(available-topHeight), Expand, Clip)
			} else {
				RequestNextFrame()
			}

			Container(topAttrs, func() {
				stats, total := visibleStats(state)
				ProfileHeader(state)
				if state.peekFunc != "" {
					PeekView(state, total)
				} else {
					filtered := filterStats(stats, searchTerm())
					SearchBar(state, stats, filtered, total)
					StatsTable(state, filtered, total)
				}
			})

			Container(Attrs(FixHeight(splitterHeight), Expand, Background(0, 0, 80, 1)), func() {
				if IsHovered() {
					ModAttrs(Background(210, 60, 60, 1))
				}
				PressAction()
				if IsActive() && totalHeight > 0 {
					mainSplitRatio = clampF32(mainSplitRatio+FrameInput.Motion[1]/(totalHeight-splitterHeight), 0.15, 0.85)
				}
			})

			Container(bottomAttrs, func() {
				FlameGraphSection(state)
			})
		})
	})
}

func ProfileHeader(state *FlameState) {
	Container(Attrs(Expand, Pad4(14, 14, 10, 14), Gap(4)), func() {
		Label(appData.selected, FontWeight(WeightBold), FontSize(15))
		summary := fmt.Sprintf("%s/%s   %s samples   %s   file total %s",
			appData.sampleTyp, appData.sampleUnt,
			formatCount(int64(len(appData.prof.Sample))),
			formatBytes(appData.selectedSize),
			formatValue(appData.total, appData.sampleUnt))
		// the top list below shows Flat/Cum relative to the focused frame,
		// not the whole file — say so explicitly here, since "total" alone
		// would otherwise silently mean two different things at once
		if state.focus != nil {
			summary += fmt.Sprintf("   ·   focused (%s): %s (%s of file)",
				state.focus.Name, formatValue(state.focusTotal, appData.sampleUnt),
				formatPercent(state.focusTotal, appData.total))
		}
		Label(summary, FontSize(11), TextColorVec(Vec4{0, 0, 45, 1}))
	})
}

// statsColumnDefaultSort is the index into statsColumns of the column the
// table starts sorted by (Flat, matching the previous fixed behavior).
const statsColumnDefaultSort = 1

// NameCell is a clickable function-name cell, following the app-wide
// interaction rule: single click SELECTS the name (bold-blue here,
// highlighted in the flame graph, toolbar selection buttons armed);
// double click also runs doubleAction (peek in the main table, re-pivot
// in the peek tables). Selection is deliberately not a toggle — that
// would fight the first click of a double — it's cleared via the
// toolbar's Clear Selection button or by clicking empty graph space.
func NameCell(sel *string, name string, doubleAction func()) {
	Container(Attrs(Row), func() {
		if IsDoubleClicked() {
			*sel = name
			if doubleAction != nil {
				doubleAction()
			}
		} else if IsClicked() {
			*sel = name
		}
		switch {
		case *sel == name:
			Label(name, FontWeight(WeightBold), TextColor(210, 80, 40, 1))
		case IsHovered():
			Label(name, TextColor(210, 70, 45, 1))
		default:
			Label(name)
		}
	})
}

// FuncNameCell is the main table's name cell: click selects, double-click
// opens the peek view for the function.
func FuncNameCell(state *FlameState, name string) {
	NameCell(&state.selectedFunc, name, func() {
		state.peekFunc = name
	})
}

// statsColumns builds the column set fresh each frame since Render closures
// capture total/appData.sampleUnt, which vary with focus state.
func statsColumns(state *FlameState, total int64) []TableColumn[*FuncStat] {
	return []TableColumn[*FuncStat]{
		{
			Label:  "Function",
			Render: func(s *FuncStat) { FuncNameCell(state, s.Name) },
			Less:   func(a, b *FuncStat) bool { return a.Name < b.Name },
		},
		{
			Label: "Flat", Width: colFlat, DefaultDesc: true,
			Render: func(s *FuncStat) { Label(formatValue(s.Flat, appData.sampleUnt)) },
			Less:   func(a, b *FuncStat) bool { return a.Flat < b.Flat },
		},
		{
			Label: "Flat%", Width: colPct, DefaultDesc: true,
			Render: func(s *FuncStat) { Label(formatPercent(s.Flat, total)) },
			Less:   func(a, b *FuncStat) bool { return a.Flat < b.Flat },
		},
		{
			Label: "Cum", Width: colCum, DefaultDesc: true,
			Render: func(s *FuncStat) { Label(formatValue(s.Cum, appData.sampleUnt)) },
			Less:   func(a, b *FuncStat) bool { return a.Cum < b.Cum },
		},
		{
			Label: "Cum%", Width: colPct, DefaultDesc: true,
			Render: func(s *FuncStat) { Label(formatPercent(s.Cum, total)) },
			Less:   func(a, b *FuncStat) bool { return a.Cum < b.Cum },
		},
	}
}

// visibleStats is the top list's current scope: the whole file, or the
// focused subtree when a flame frame is focused. The search filter applies
// on top of whichever scope is active (see the caller in MainContent).
func visibleStats(state *FlameState) ([]*FuncStat, int64) {
	if state.focus != nil {
		return state.focusStats, state.focusTotal
	}
	return appData.stats, appData.total
}

// SearchBar is the toolbar strip above the table: the function-name filter
// (a case-insensitive substring match that narrows the top list and dims
// non-matching frames in the flame graph) on the left, and — when a table
// row is selected — action buttons for that selection on the right.
// all/filtered are the current scope's stats before and after filtering;
// total is that scope's total, so the matched-flat percentage is relative to
// what the user is currently looking at, consistent with the table below.
func SearchBar(state *FlameState, all, filtered []*FuncStat, total int64) {
	Container(Attrs(Row, Expand, CrossMid, Gap(10), Pad4(0, 14, 8, 14)), func() {
		Label("Filter", FontSize(11), TextColorVec(Vec4{0, 0, 45, 1}))

		// wrapper sizes itself to the input (its only flow child) so the ×
		// clear button can Float relative to the input's own box
		Container(Attrs(), func() {
			inputAttrs := DefaultTextInputAttrs()
			inputAttrs.FontSize = 11
			inputAttrs.Padding = N4(5)
			TextInputExt(&searchQuery, inputAttrs)

			size := GetResolvedSize()
			if searchQuery != "" && size[0] > 0 {
				const btn = 15
				Container(Attrs(NoAnimate, InFront, Float(size[0]-btn-4, (size[1]-btn)/2),
					FixSize(btn, btn), Corners(btn/2), Center), func() {
					if IsHovered() {
						ModAttrs(Background(0, 0, 82, 1))
					}
					if PressAction() {
						searchQuery = ""
					}
					Icon(SymICross, FontSize(9), TextColor(0, 0, 40, 1))
				})
			}
		})

		if searchTerm() == "" {
			Label("type to filter the list and flame graph", FontSize(10), TextColorVec(Vec4{0, 0, 65, 1}))
		} else {
			var matchedFlat int64
			for _, s := range filtered {
				matchedFlat += s.Flat
			}
			Label(fmt.Sprintf("%d of %d functions · flat %s (%s)",
				len(filtered), len(all),
				formatValue(matchedFlat, appData.sampleUnt),
				formatPercent(matchedFlat, total)),
				FontSize(10), TextColorVec(Vec4{0, 0, 45, 1}))
		}

		Filler(1)
		if state.selectedFunc != "" {
			if CtrlButton(0, "Caller/Callee View", true) {
				state.peekFunc = state.selectedFunc
			}
			if CtrlButton(0, "Clear Selection", true) {
				state.selectedFunc = ""
			}
		}
	})
}

func StatsTable(state *FlameState, stats []*FuncStat, total int64) {
	Table(nil, rowHeight, statsColumns(state, total), stats, func(s *FuncStat) any { return s }, statsColumnDefaultSort)
}

// edgeColumns is the column set for both peek tables. Percentages are
// fractions of the peeked function's own total, so a caller row reads "this
// much of the function's cost arrives via here" and a callee row "this much
// of it flows on into there". Real function names are selectable (arming
// the strip's "Peek Selected" re-pivot button); synthetic rows like
// "(self)" and "(root)" are not — there's nothing to pivot to.
func edgeColumns(state *FlameState, total int64) []TableColumn[*FuncEdge] {
	return []TableColumn[*FuncEdge]{
		{
			Label: "Function",
			Render: func(e *FuncEdge) {
				if strings.HasPrefix(e.Name, "(") {
					Label(e.Name, FontStyle(StyleItalic))
				} else {
					NameCell(&state.peekSelected, e.Name, func() {
						// double-click a caller/callee = pivot to it
						state.peekFunc = e.Name
						state.selectedFunc = e.Name
						state.peekSelected = ""
					})
				}
			},
			Less: func(a, b *FuncEdge) bool { return a.Name < b.Name },
		},
		{
			Label: "Value", Width: colFlat, DefaultDesc: true,
			Render: func(e *FuncEdge) { Label(formatValue(e.Value, appData.sampleUnt)) },
			Less:   func(a, b *FuncEdge) bool { return a.Value < b.Value },
		},
		{
			Label: "%", Width: colPct, DefaultDesc: true,
			Render: func(e *FuncEdge) { Label(formatPercent(e.Value, total)) },
			Less:   func(a, b *FuncEdge) bool { return a.Value < b.Value },
		},
	}
}

// PeekView replaces the toolbar+table with the caller/callee breakdown of
// state.peekFunc — named after `go tool pprof`'s `peek` command, which
// prints the same breakdown in text form. The scope is inherited live from
// the table's: the focused subtree while a frame is focused, the whole file
// otherwise — so focusing/unfocusing frames while peeking recomputes the
// view. scopeTotal is that scope's total, for the header's percentage.
func PeekView(state *FlameState, scopeTotal int64) {
	scope := appData.flameRoot
	if state.focus != nil {
		scope = state.focus
	}
	if state.peekFor != state.peekFunc || state.peekScope != scope {
		state.peekFor = state.peekFunc
		state.peekScope = scope
		state.peekCallers, state.peekCallees, state.peekTotal = computeCallerCallee(scope, state.peekFunc)
	}

	Container(Attrs(Row, Expand, CrossMid, Gap(10), Pad4(0, 14, 8, 14)), func() {
		Label("Peek", FontSize(11), TextColorVec(Vec4{0, 0, 45, 1}))
		Label(state.peekFunc, FontWeight(WeightBold), FontSize(12), TextColor(210, 80, 40, 1))
		Label(fmt.Sprintf("%s · %s of scope", formatValue(state.peekTotal, appData.sampleUnt),
			formatPercent(state.peekTotal, scopeTotal)), FontSize(10), TextColorVec(Vec4{0, 0, 45, 1}))
		Filler(1)
		// re-pivot: same select-then-act pattern as the main table — a
		// selected caller/callee name arms this button (the selected name
		// is already bold in the table, so the label stays fixed-width)
		if state.peekSelected != "" && state.peekSelected != state.peekFunc {
			if CtrlButton(0, "Peek Selected", true) {
				state.peekFunc = state.peekSelected
				state.selectedFunc = state.peekSelected // graph + main table follow the pivot
				state.peekSelected = ""
			}
		}
		if CtrlButton(0, "Exit Peek", true) {
			state.peekFunc = ""
			state.peekSelected = ""
		}
	})

	if state.peekTotal == 0 {
		Container(Attrs(Grow(1), Expand, Center), func() {
			Label("not present in the current scope", FontStyle(StyleItalic), FontSize(12), TextColorVec(Vec4{0, 0, 55, 1}))
		})
		return
	}

	// Both the table id (nil → auto) and the row ids (element pointers,
	// stable while the cached lists live) must NOT be strings: shirei
	// derives scope ids by hashing the raw interface bytes of the id, and a
	// string boxed into `any` carries a fresh data pointer every frame, so
	// all previous-frame state lookups (sizes, scroll, hover) miss forever —
	// the rows never render because VirtualListView never learns its size.
	// Same trap documented in widgets/logview_test.go's scope comment.
	peekSection := func(heading string, edges []*FuncEdge) {
		Container(Attrs(Grow(1), Expand, Clip), func() {
			Container(Attrs(Expand, Pad4(4, 14, 2, 14)), func() {
				Label(heading, FontWeight(WeightBold), FontSize(11), TextColorVec(Vec4{0, 0, 35, 1}))
			})
			Table(nil, rowHeight, edgeColumns(state, state.peekTotal), edges, func(e *FuncEdge) any { return e }, 1)
		})
	}
	peekSection("Callers — cost flowing in", state.peekCallers)
	peekSection("Callees — cost flowing out (incl. self)", state.peekCallees)
}

// FlameState is scroll/zoom-nav state for the flame graph: scale is how many
// times wider the virtual (zoomed-in) canvas is than the panel; panX/panY
// are the scroll offset into that virtual canvas, in pixels. focus is the
// clicked subtree (nil = whole profile) driving the top list's scope;
// focusStats/focusTotal are computed once when it's set, not per frame.
type FlameState struct {
	scale f32
	panX  f32
	panY  f32

	focus      *FlameNode
	focusStats []*FuncStat
	focusTotal int64

	// selectedFunc is the app-wide selection, set by a single click on a
	// function name in the table OR a frame in the graph: the name bolds
	// in the table, its frames highlight in the flame graph the same way
	// the search filter does (everything else dims, no table filtering),
	// and the toolbar's selection buttons arm (Caller/Callee View, Clear
	// Selection). By *name*, not node — one function is many frames.
	// Cleared by the Clear Selection button or clicking empty graph space
	// (not by re-clicking: selection isn't a toggle, since the first click
	// of a double-click gesture also lands here).
	selectedFunc string

	// peekFunc, when set, replaces the toolbar+table with PeekView: the
	// caller/callee breakdown of that function (pprof's `peek` command).
	// The peek* fields below it are the cached breakdown, keyed by
	// (peekFor, peekScope) — recomputed only when the pivot or the scope
	// changes (see PeekView), not per frame.
	peekFunc    string
	peekFor     string
	peekScope   *FlameNode
	peekCallers []*FuncEdge
	peekCallees []*FuncEdge
	peekTotal   int64

	// peekSelected is the selection *within* the peek tables: clicking a
	// caller/callee name selects it — highlighting it in the flame graph
	// (same visual as the main table's selection) and arming the "Peek
	// Selected" button in the strip, which re-pivots the peek view to it.
	// Double-clicking a name pivots directly. Same click-selects /
	// double-click-acts pattern as everywhere else.
	peekSelected string
}

// FuncEdge is one row of the peek view: a direct neighbor of the peeked
// function (a caller or a callee) and how much of its cost flows across
// that edge.
type FuncEdge struct {
	Name  string
	Value int64
}

func clampF32(v, lo, hi f32) f32 {
	return max(lo, min(v, hi))
}

func setFocus(state *FlameState, node *FlameNode) {
	state.focus = node
	state.focusStats, state.focusTotal = computeFocusedStats(node)
}

func clearFocus(state *FlameState) {
	state.focus = nil
	state.focusStats = nil
	state.focusTotal = 0
}

func FlameGraphSection(state *FlameState) {
	// Viewport (not just Grow+Expand+Clip): without ExtrinsicSize, a long
	// "focused: ..." label in the header below can inflate this container's
	// own measured width past what's actually visible, which then feeds a
	// wrong (too-wide) width into FlameGraph's pixel math below — looking
	// exactly like an unexplained pan jump even though scale/panX/panY never
	// change.
	Container(Attrs(Viewport), func() {
		Container(Attrs(Expand, Pad4(10, 14, 6, 14), Gap(2), Background(0, 0, 97, 1)), func() {
			Container(Attrs(Row, Expand, CrossMid, Gap(10)), func() {
				Label("Flame Graph", FontWeight(WeightBold), FontSize(13))
				Label("scroll to pan · ctrl+scroll (or pinch) to zoom · click to select · double-click to focus", FontSize(10), TextColorVec(Vec4{0, 0, 55, 1}))
				Filler(1)
				if state.focus != nil {
					if CtrlButton(0, "Clear Focus", true) {
						clearFocus(state)
					}
				}
				if CtrlButton(0, "Reset Zoom", true) {
					state.scale = 1
					state.panX = 0
					state.panY = 0
				}
			})

			// MaxWidth, not Viewport: Viewport's ExtrinsicSize ignores
			// content on BOTH axes, and this row has no external height to
			// size from (unlike the section-level Viewport above, which
			// gets its height from MainContent's split), so it collapsed to
			// zero height — invisible. MaxWidth caps only the width (still
			// content-sized height) at the header's own previous-frame
			// width, so a long name can't inflate anything above it either
			// — see the 2026-07-01 ExtrinsicSize post-mortem.
			//
			// Always rendered (never conditional on state.focus) so this
			// row's height is reserved whether or not something is focused
			// — otherwise focusing/clearing a frame would resize this
			// section and, with it, how much room FlameGraph gets below.
			// A single space stands in for the label when there's nothing
			// to show, so the reserved height comes from real font metrics
			// rather than depending on how this text shaper happens to
			// size an empty string; alpha 0 makes it invisible without
			// changing the layout.
			focusText := " "
			focusAlpha := f32(0)
			if state.focus != nil {
				focusText = "focused: " + state.focus.Name
				focusAlpha = 1
			}
			Container(Attrs(MaxWidth(GetResolvedSize()[0]), Clip), func() {
				Label(focusText, FontSize(10), TextColorVec(Vec4{0, 0, 45, focusAlpha}))
			})
		})

		FlameGraph(state)
	})
}

func FlameGraph(state *FlameState) {
	Container(Attrs(Grow(1), Expand, Clip), func() {
		size := GetResolvedSize()
		width, height := size[0], size[1]
		if width <= 0 {
			RequestNextFrame()
			return
		}

		contentHeight := f32(appData.flameMaxDepth+1) * flameRowHeight

		if IsHovered() {
			dy := FrameInput.Scroll[1]
			dx := FrameInput.Scroll[0]
			// Trackpad pinch reaches us as a scroll event with Ctrl held (see
			// the comment on cocoa_darwin.m's scrollWheel:); holding Ctrl and
			// scrolling manually works the same way as a fallback.
			if InputState.Modifiers&ModCtrl != 0 {
				if dy != 0 {
					rd := GetRenderData()
					mouseX := InputState.MousePoint[0] - rd.ResolvedOrigin[0]
					oldScale := state.scale
					newScale := clampF32(oldScale*f32(math.Exp(float64(-dy)*flameZoomSpeed)), 1, flameMaxScale)
					// keep the point under the cursor fixed as we rescale
					virtualX := state.panX + mouseX
					state.panX = virtualX*(newScale/oldScale) - mouseX
					state.scale = newScale
				}
			} else {
				if dy != 0 {
					state.panY += dy
				}
				if dx != 0 {
					state.panX += dx
				}
			}
		}

		virtualWidth := width * state.scale
		state.panX = clampF32(state.panX, 0, max(0, virtualWidth-width))
		state.panY = clampF32(state.panY, 0, max(0, contentHeight-height))

		const rowH = f32(flameRowHeight)

		// normalized once per frame, not per node; only visible (non-culled)
		// frames pay the per-node ToLower below
		term := searchTerm()

		// the highlighted function: normally the table selection, but while
		// a peek-table row is selected, that candidate takes over — so you
		// can see where a caller/callee lives before pivoting to it
		highlighted := state.selectedFunc
		if state.peekFunc != "" && state.peekSelected != "" {
			highlighted = state.peekSelected
		}

		var clicked *FlameNode
		var doubleClicked *FlameNode
		var hovered *FlameNode

		var draw func(node *FlameNode, depth int, x0, x1 f32)
		draw = func(node *FlameNode, depth int, x0, x1 f32) {
			y := f32(depth)*rowH - state.panY
			if y >= height {
				return // this row and everything deeper is below the view; whole subtree is cullable
			}
			screenX0 := x0 - state.panX
			screenX1 := x1 - state.panX
			if screenX1 <= 0 || screenX0 >= width || screenX1-screenX0 < 1 {
				return // horizontally offscreen (or sub-pixel); also cullable regardless of depth
			}

			if y+rowH > 0 { // only above the view; still need to recurse, deeper rows may be visible
				clippedX0 := max(screenX0, 0)
				clippedX1 := min(screenX1, width)

				// A frame keeps its color only if it passes every active
				// emphasis — the search filter and the table selection dim
				// the same way, and combine conjunctively (you typically use
				// the filter to find the function you then select). Dimming
				// never changes layout: widths still mean what they mean.
				dim := term != "" && !nameMatches(node.Name, term)
				if highlighted != "" && node.Name != highlighted {
					dim = true
				}
				sat, lit := f32(55), f32(65)
				labelColor := Vec4{0, 0, 10, 1}
				if dim {
					sat, lit = 8, 84
					labelColor = Vec4{0, 0, 60, 1}
				}

				ContainerWithKey(node, Attrs(Float(clippedX0, y), FixSize(clippedX1-clippedX0, rowH-1),
					Background(node.Hue, sat, lit, 1), Pad4(1, 4, 1, 4), CrossMid, Clip), func() {
					if IsHovered() {
						hovered = node
						ModAttrs(BorderColor(0, 0, 15, 1), BorderWidth(1))
					}
					if IsDoubleClicked() {
						doubleClicked = node
					} else if IsClicked() {
						clicked = node
					}
					if clippedX1-clippedX0 >= flameMinLabelWidth {
						Label(node.Name, FontSize(10), TextColorVec(labelColor))
					}
				})
			}

			if node.Value <= 0 || len(node.Children) == 0 {
				return
			}
			childX := x0
			for _, child := range node.Children {
				childWidth := (x1 - x0) * f32(child.Value) / f32(node.Value)
				draw(child, depth+1, childX, childX+childWidth)
				childX += childWidth
			}
		}

		root := appData.flameRoot
		if root != nil && root.Value > 0 {
			childX := f32(0)
			for _, child := range root.Children {
				childWidth := virtualWidth * f32(child.Value) / f32(root.Value)
				draw(child, 0, childX, childX+childWidth)
				childX += childWidth
			}
		}

		if doubleClicked != nil {
			// double-click = drill in: focus the subtree, rescoping the
			// table (the pair's first click already selected the function)
			setFocus(state, doubleClicked)
		} else if clicked != nil {
			// single click = select only, mirroring the table's name cells
			state.selectedFunc = clicked.Name
		} else if FrameInput.Mouse == MouseClick && IsHoveredDirectly() {
			// clicked the panel itself, not any frame in it (and not the
			// scrollbars below, which are hit-tested more specifically);
			// resets both "pointers" — the focus scope and the selection
			clearFocus(state)
			state.selectedFunc = ""
		}

		flameScrollbars(state, width, height, virtualWidth, contentHeight)

		if hovered != nil {
			flameTooltip(hovered, width, height)
		}
	})
}

// flameTooltip floats a small dark panel near the cursor with the hovered
// frame's full name and value — narrow frames elide or omit their labels
// entirely, so this is often the only way to read them. ClickThrough keeps
// it out of hit testing: if it could be hovered itself, the frame under the
// cursor would lose hover, hiding the tooltip, restoring hover, and so on —
// a flicker loop. Text is measured up front so the tooltip can be kept
// inside the panel (flipping above the cursor near the bottom edge) instead
// of clipping at the panel's boundary.
func flameTooltip(node *FlameNode, panelWidth, panelHeight f32) {
	const padV, padH, gap = 5, 8, 2

	nameAttrs := DefaultTextAttrs()
	nameAttrs.Size = 11
	valueAttrs := DefaultTextAttrs()
	valueAttrs.Size = 10

	valueLine := fmt.Sprintf("%s · %s of file",
		formatValue(node.Value, appData.sampleUnt),
		formatPercent(node.Value, appData.total))

	nameLine := ShapeText(node.Name, nameAttrs).Lines[0]
	valueShaped := ShapeText(valueLine, valueAttrs).Lines[0]
	w := max(nameLine.Width, valueShaped.Width) + padH*2
	h := nameLine.Height + valueShaped.Height + padV*2 + gap

	rd := GetRenderData()
	mouseX := InputState.MousePoint[0] - rd.ResolvedOrigin[0]
	mouseY := InputState.MousePoint[1] - rd.ResolvedOrigin[1]
	x := clampF32(mouseX+12, 0, max(0, panelWidth-w))
	y := mouseY + 16
	if y+h > panelHeight {
		y = mouseY - h - 8
	}

	Container(Attrs(NoAnimate, ClickThrough, InFront, Float(x, y),
		Pad2(padV, padH), Gap(gap), Corners(4), Background(0, 0, 12, 0.92)), func() {
		Label(node.Name, FontSize(11), TextColor(0, 0, 98, 1))
		Label(valueLine, FontSize(10), TextColor(0, 0, 72, 1))
	})
}

// flameScrollbars draws draggable thumbs for both axes of the flame graph's
// virtual canvas. It can't reuse widgets.ScrollBars() because that widget is
// built on the normal ScrollOffset/ContentSize flow-layout machinery, and
// this canvas positions everything itself via Float — but it borrows the
// same track+thumb technique (a floating track container with a spacer then
// the thumb, using normal flow inside it).
func flameScrollbars(state *FlameState, width, height, virtualWidth, contentHeight f32) {
	const thickness = 10
	const pad = 2

	if virtualWidth > width {
		trackLength := width - thickness - pad
		thumbLength := max(20, trackLength*(width/virtualWidth))
		maxThumbOffset := max(0, trackLength-thumbLength)
		maxScroll := virtualWidth - width
		var thumbOffset f32
		if maxScroll > 0 {
			thumbOffset = maxThumbOffset * (state.panX / maxScroll)
		}

		Container(Attrs(NoAnimate, Float(0, height-thickness-pad), InFront, Row,
			FixSize(trackLength+thickness, thickness), Background(0, 0, 50, 0.3)), func() {
			Element(Attrs(YesAnimate, FixWidth(f32(int(thumbOffset)))))
			Container(Attrs(YesAnimate, FixWidth(f32(int(thumbLength))), Expand, Corners(4), Background(0, 0, 35, 0.8)), func() {
				PressAction()
				if IsActive() && maxThumbOffset > 0 {
					desired := thumbOffset + FrameInput.Motion[0]
					state.panX = clampF32(desired/maxThumbOffset*maxScroll, 0, maxScroll)
				}
			})
		})
	}

	if contentHeight > height {
		trackLength := height - thickness - pad
		thumbLength := max(20, trackLength*(height/contentHeight))
		maxThumbOffset := max(0, trackLength-thumbLength)
		maxScroll := contentHeight - height
		var thumbOffset f32
		if maxScroll > 0 {
			thumbOffset = maxThumbOffset * (state.panY / maxScroll)
		}

		Container(Attrs(NoAnimate, Float(width-thickness-pad, 0), InFront,
			FixSize(thickness, trackLength+thickness), Background(0, 0, 50, 0.3)), func() {
			Element(Attrs(YesAnimate, FixHeight(f32(int(thumbOffset)))))
			Container(Attrs(YesAnimate, FixHeight(f32(int(thumbLength))), Expand, Corners(4), Background(0, 0, 35, 0.8)), func() {
				PressAction()
				if IsActive() && maxThumbOffset > 0 {
					desired := thumbOffset + FrameInput.Motion[1]
					state.panY = clampF32(desired/maxThumbOffset*maxScroll, 0, maxScroll)
				}
			})
		})
	}
}
