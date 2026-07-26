// The see_exe GUI: header stacked bar (the whole file at a glance), a
// sortable module table annotated with why-chains, and a detail pane with
// the selected module's require edges in both directions.
//
// Interaction follows the app-wide shirei convention established in
// see_pprof: single click SELECTS a module (everywhere — table rows, bar
// segments, breadcrumb links, edge tables), and the detail pane follows the
// selection. The exe is watched via fsnotify, so rebuilding your program
// refreshes the window — a live feedback loop for binary trimming.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/cespare/xxhash/v2"
	"github.com/fsnotify/fsnotify"

	app "go.hasen.dev/shirei/app"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

type f32 = float32

const (
	guiRowHeight   = 28
	barHeight      = 26
	splitterHeight = 6

	colSize    = 80
	colPct     = 55
	colFuncs   = 60
	colVersion = 150
)

// Model is everything derived from one load of the exe. Rebuilt wholesale on
// reload (under WithFrameLock), so a frame always sees one consistent load.
type Model struct {
	exePath    string
	info       *ExeInfo
	requiredBy map[string][]string
	requires   map[string][]string
	noModFile  []string
	chains     map[string][]string // dep path -> shortest chain, root..target inclusive
	cum        map[string]uint64   // module path -> own code + everything chain-attributed under it
	byPath     map[string]*Module  // deps + main + stdlib
	rows       []*Module           // table rows: stdlib, main, all deps
	loadedAt   time.Time
	loadErr    error // last *re*load failure; the previous good model stays up
}

var model Model

// selectedPath is the app-wide selection (a module path, "" = none).
// A package var, like see_pprof's sidebarWidth: there's only one window.
var selectedPath string

var mainSplitRatio f32 = 0.55

func loadModel(path string) error {
	info, err := LoadExe(path)
	if err != nil {
		return err
	}
	requiredBy, requires, noModFile := depEdges(info)

	m := Model{
		exePath:    path,
		info:       info,
		requiredBy: requiredBy,
		requires:   requires,
		noModFile:  noModFile,
		chains:     map[string][]string{},
		byPath:     map[string]*Module{},
		loadedAt:   time.Now(),
	}
	m.byPath[info.Main.Path] = info.Main
	m.byPath[info.Stdlib.Path] = &info.Stdlib
	m.byPath[info.Unknown.Path] = &info.Unknown
	for _, d := range info.Deps {
		m.byPath[d.Path] = d
		m.chains[d.Path] = shortestChain(d.Path, requiredBy)
	}
	m.rows = append([]*Module{&info.Stdlib, info.Main}, info.Deps...)

	// Cumulative cost, pprof-style: a module's cum is its own code plus every
	// module whose shortest chain passes through it — "what does dropping this
	// dependency plausibly save". Attribution by shortest chain only, so
	// shared deps count toward one route (the one the table displays).
	m.cum = map[string]uint64{}
	var depsTotal uint64
	for _, d := range info.Deps {
		depsTotal += d.CodeSize
		for _, p := range m.chains[d.Path] { // includes d itself as the last element
			m.cum[p] += d.CodeSize
		}
	}
	m.cum[info.Main.Path] = info.Main.CodeSize + depsTotal
	m.cum[info.Stdlib.Path] = info.Stdlib.CodeSize
	m.cum[info.Unknown.Path] = info.Unknown.CodeSize

	model = m
	if selectedPath != "" && m.byPath[selectedPath] == nil {
		selectedPath = ""
	}
	return nil
}

// RunGUI opens the window: on a loaded model when launched with a file
// argument, or on the picker (initial == "") when launched bare.
func RunGUI(initial string) {
	scanRoot, _ = os.Getwd()
	if initial == "" {
		browsing = true
		scanBinaries()
	} else {
		watchExe(model.exePath)
	}
	app.SetupWindow("see_exe", 1150, 720)
	app.SetupIconBytes(iconPNG)
	app.Run(RootView)
}

func renderPNG(out string) error {
	return RenderToPNG(out, 1150, 720, RootView)
}

// exeWatcher watches the currently inspected exe; opening another binary
// replaces it (Close ends the previous goroutine via its Events channel).
var exeWatcher *fsnotify.Watcher

// watchExe reloads the model whenever the exe is rewritten — `go build`
// replaces the file, so watch the directory and filter by name.
func watchExe(path string) {
	if exeWatcher != nil {
		exeWatcher.Close()
		exeWatcher = nil
	}
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		fmt.Println("exe watch: failed to start:", err)
		return
	}
	dir := filepath.Dir(path)
	if err := watcher.Add(dir); err != nil {
		fmt.Println("exe watch: failed to watch", dir, err)
		watcher.Close()
		return
	}
	exeWatcher = watcher
	base := filepath.Base(path)

	go func() {
		var debounce *time.Timer
		reload := func() {
			WithFrameLock(func() {
				if err := loadModel(path); err != nil {
					model.loadErr = err // keep showing the previous good load
				}
			})
			RequestNextFrame()
		}
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if filepath.Base(event.Name) != base {
					continue
				}
				if debounce != nil {
					debounce.Stop()
				}
				debounce = time.AfterFunc(250*time.Millisecond, reload)
			case _, ok := <-watcher.Errors:
				if !ok {
					return
				}
			}
		}
	}()
}

func moduleHue(path string) f32 {
	return f32(xxhash.Sum64String(path) % 360)
}

func formatSize(n uint64) string {
	if n >= 1e6 {
		return fmt.Sprintf("%.1f MB", float64(n)/1e6)
	}
	return fmt.Sprintf("%.0f KB", float64(n)/1e3)
}

func formatPct(part, total uint64) string {
	if total == 0 {
		return "0%"
	}
	return fmt.Sprintf("%.1f%%", float64(part)/float64(total)*100)
}

// viaText is the table's compact why answer: the chain root for transitive
// deps, "(direct)" for roots themselves, "" for main/stdlib/synthetic rows.
func viaText(m *Module) string {
	chain, ok := model.chains[m.Path]
	if !ok {
		return ""
	}
	if len(chain) < 2 {
		return "(direct)"
	}
	return "via " + chain[0]
}

func RootView() {
	Container(Attrs(Viewport, Background(0, 0, 100, 1)), func() {
		if browsing {
			PickerView()
		} else {
			InspectView()
		}
	})
}

func InspectView() {
	if GetFrameInput().Key == KeyEscape {
		browsing = true
		scanBinaries()
	}
	Container(Attrs(Viewport), func() {
		Header()

		Container(Attrs(Grow(1), Expand, Clip), func() {
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
				ModuleTable()
			})

			Container(Attrs(FixHeight(splitterHeight), Expand, Background(0, 0, 80, 1)), func() {
				if IsHovered() {
					ModAttrs(Background(210, 60, 60, 1))
				}
				PressAction()
				if IsActive() && totalHeight > 0 {
					mainSplitRatio = clampF32(mainSplitRatio+GetFrameInput().Motion[1]/(totalHeight-splitterHeight), 0.15, 0.85)
				}
			})

			Container(bottomAttrs, func() {
				DetailPane()
			})
		})
	})
}

func clampF32(v, lo, hi f32) f32 {
	return max(lo, min(v, hi))
}

// ---------------------------------------------------------------- header --

// barSeg is one segment of the header's stacked bar: a module (selectable)
// or a synthetic pool ("small deps", the non-code remainder).
type barSeg struct {
	label    string
	path     string // "" = not selectable
	size     uint64
	hue      f32
	sat, lit f32
}

func buildSegments() []barSeg {
	info := model.info
	var segs []barSeg
	segs = append(segs, barSeg{"Go runtime + stdlib", info.Stdlib.Path, info.Stdlib.CodeSize, 215, 12, 62})
	segs = append(segs, barSeg{info.Main.Path, info.Main.Path, info.Main.CodeSize, moduleHue(info.Main.Path), 70, 55})

	var pooled uint64
	for _, d := range info.Deps { // already sorted by size desc
		if d.CodeSize == 0 {
			continue
		}
		if float64(d.CodeSize) < float64(info.FileSize)*0.005 {
			pooled += d.CodeSize
			continue
		}
		segs = append(segs, barSeg{d.Path, d.Path, d.CodeSize, moduleHue(d.Path), 55, 65})
	}
	if pooled > 0 {
		segs = append(segs, barSeg{"smaller deps", "", pooled, 0, 0, 78})
	}
	if info.Unknown.CodeSize > 0 {
		segs = append(segs, barSeg{"unattributed", info.Unknown.Path, info.Unknown.CodeSize, 0, 0, 70})
	}
	if rest := info.FileSize - info.AttributedTotal(); rest > 0 {
		segs = append(segs, barSeg{"data, type metadata & debug info", "", rest, 0, 0, 90})
	}
	return segs
}

func Header() {
	info := model.info
	Container(Attrs(Expand, Pad4(12, 14, 10, 14), Gap(6), Background(0, 0, 97, 1)), func() {
		Container(Attrs(Row, Expand, CrossMid, Gap(10)), func() {
			if CtrlButton(NoIcon, "Back to files", true) {
				browsing = true
				scanBinaries() // fresh list — builds may have happened meanwhile
			}
			Label(filepath.Base(model.exePath), FontWeight(WeightBold), FontSize(15))
			Label(fmt.Sprintf("%s · %s · %d modules · code %s",
				formatSize(info.FileSize), info.GoVersion, len(info.Deps)+1,
				formatSize(info.AttributedTotal())),
				FontSize(11), TextColorVec(Vec4{0, 0, 45, 1}))
			Filler(1)
			if model.loadErr != nil {
				Label(fmt.Sprintf("reload failed: %v (showing %s)",
					model.loadErr, model.loadedAt.Format("15:04:05")),
					FontSize(10), TextColorVec(Vec4{5, 70, 45, 1}))
			}
			if selectedPath != "" {
				if CtrlButton(NoIcon, "Clear Selection", true) {
					selectedPath = ""
				}
			}
		})

		var hovered *barSeg
		segs := buildSegments()
		Container(Attrs(Row, Expand, FixHeight(barHeight), Clip), func() {
			width := GetResolvedSize()[0]
			if width <= 0 {
				RequestNextFrame()
				return
			}
			for i := range segs {
				seg := &segs[i]
				w := width * f32(seg.size) / f32(info.FileSize)
				Container(Attrs(FixWidth(w), Expand, Clip, CrossMid, Pad4(0, 4, 0, 4), Background(seg.hue, seg.sat, seg.lit, 1)), func() {
					selected := seg.path != "" && seg.path == selectedPath
					if selected {
						ModAttrs(BorderColor(0, 0, 10, 1), BorderWidth(2))
					}
					if IsHovered() {
						hovered = seg
						ModAttrs(BorderColor(0, 0, 15, 1), BorderWidth(1))
					}
					if seg.path != "" && PressAction() {
						selectedPath = seg.path
					}
					if w >= 70 {
						Label(lastElem(seg.label), FontSize(9), TextColorVec(Vec4{0, 0, 15, 1}))
					}
				})
			}
		})

		// caption under the bar: hovered segment beats hint; fixed line so
		// hovering never resizes the header
		caption := "each segment's width is its share of the file — click to select"
		if hovered != nil {
			caption = fmt.Sprintf("%s — %s (%s of file)", hovered.label,
				formatSize(hovered.size), formatPct(hovered.size, info.FileSize))
		}
		Label(caption, FontSize(10), TextColorVec(Vec4{0, 0, 45, 1}))
	})
}

// shortVersion middle-ellipsizes pseudo-versions so the informative parts —
// the date prefix and the commit-hash tail — both survive the Version
// column's width. The full version is always in the detail pane's header.
func shortVersion(v string) string {
	if len(v) <= 21 {
		return v
	}
	return v[:12] + "…" + v[len(v)-8:]
}

func lastElem(path string) string {
	if i := strings.LastIndexByte(path, '/'); i >= 0 {
		return path[i+1:]
	}
	return path
}

// ----------------------------------------------------------------- table --

// numCell right-aligns tabular numbers so magnitudes compare down the column.
func numCell(text string) {
	Container(Attrs(Row, Expand), func() {
		Filler(1)
		Label(text)
	})
}

// sizeCell is numCell plus a faint right-anchored underline bar sized by the
// value's share of all attributed code — the percentage column, visualized.
func sizeCell(size uint64, text string) {
	Container(Attrs(Row, Expand), func() {
		rs := GetResolvedSize()
		if total := model.info.AttributedTotal(); total > 0 && size > 0 && rs[0] > 0 && rs[1] > 0 {
			bw := max(2, rs[0]*f32(size)/f32(total))
			Container(Attrs(NoAnimate, ClickThrough, Float(rs[0]-bw, rs[1]-3), FixSize(bw, 3), Background(210, 45, 75, 1)), func() {})
		}
		Filler(1)
		Label(text)
	})
}

// ModuleNameCell: click selects, app-wide.
func ModuleNameCell(m *Module) {
	Container(Attrs(Row), func() {
		if IsClicked() {
			selectedPath = m.Path
		}
		switch {
		case selectedPath == m.Path:
			Label(m.Path, FontWeight(WeightBold), TextColor(210, 80, 40, 1))
		case IsHovered():
			Label(m.Path, TextColor(210, 70, 45, 1))
		default:
			Label(m.Path)
		}
	})
}

func moduleColumns() []TableColumn[*Module] {
	codeTotal := model.info.AttributedTotal()
	return []TableColumn[*Module]{
		{
			Label:  "Module",
			Render: func(m *Module) { ModuleNameCell(m) },
			Less:   func(a, b *Module) bool { return a.Path < b.Path },
		},
		{
			Label: "Code", Width: colSize, DefaultDesc: true,
			Render: func(m *Module) { sizeCell(m.CodeSize, formatSize(m.CodeSize)) },
			Less:   func(a, b *Module) bool { return a.CodeSize < b.CodeSize },
		},
		{
			Label: "%", Width: colPct, DefaultDesc: true,
			Render: func(m *Module) { numCell(formatPct(m.CodeSize, codeTotal)) },
			Less:   func(a, b *Module) bool { return a.CodeSize < b.CodeSize },
		},
		{
			Label: "Cum", Width: colSize, DefaultDesc: true,
			Render: func(m *Module) { sizeCell(model.cum[m.Path], formatSize(model.cum[m.Path])) },
			Less:   func(a, b *Module) bool { return model.cum[a.Path] < model.cum[b.Path] },
		},
		{
			Label: "Cum%", Width: colPct, DefaultDesc: true,
			Render: func(m *Module) { numCell(formatPct(model.cum[m.Path], codeTotal)) },
			Less:   func(a, b *Module) bool { return model.cum[a.Path] < model.cum[b.Path] },
		},
		{
			Label: "Funcs", Width: colFuncs, DefaultDesc: true,
			Render: func(m *Module) { numCell(formatCount(m.NumFuncs)) },
			Less:   func(a, b *Module) bool { return a.NumFuncs < b.NumFuncs },
		},
		{
			Label: "Version", Width: colVersion,
			Render: func(m *Module) { Label(shortVersion(m.Version), FontSize(11), TextColorVec(Vec4{0, 0, 45, 1})) },
			Less:   func(a, b *Module) bool { return a.Version < b.Version },
		},
		{
			Label: "Via",
			Render: func(m *Module) {
				via := viaText(m)
				if via == "(direct)" {
					Label(via, FontSize(11), FontStyle(StyleItalic), TextColorVec(Vec4{0, 0, 55, 1}))
				} else {
					Label(via, FontSize(11), TextColorVec(Vec4{0, 0, 45, 1}))
				}
			},
			Less: func(a, b *Module) bool { return viaText(a) < viaText(b) },
		},
	}
}

func formatCount(n int) string {
	return fmt.Sprintf("%d", n)
}

func ModuleTable() {
	Table(nil, guiRowHeight, moduleColumns(), model.rows, func(m *Module) any { return m }, 1)
}

// ---------------------------------------------------------------- detail --

func DetailPane() {
	Container(Attrs(Grow(1), Expand, Clip, Background(0, 0, 98, 1)), func() {
		m := model.byPath[selectedPath]
		if m == nil {
			Container(Attrs(Viewport, Center), func() {
				Label("click a module to see why it's embedded and what it pulls in",
					FontSize(13), FontStyle(StyleItalic), TextColorVec(Vec4{0, 0, 55, 1}))
			})
			return
		}

		// scoped by module pointer so sort/scroll state resets per selection
		ContainerWithKey(m, Attrs(Grow(1), Expand, Clip), func() {
			Container(Attrs(Expand, Pad4(10, 14, 6, 14), Gap(4)), func() {
				Container(Attrs(Row, Expand, CrossMid, Gap(10)), func() {
					Label(m.Path, FontWeight(WeightBold), FontSize(14), TextColor(210, 80, 40, 1))
					codeTotal := model.info.AttributedTotal()
					summary := fmt.Sprintf("%s · %s · %d functions (%s of code)",
						m.Version, formatSize(m.CodeSize), m.NumFuncs,
						formatPct(m.CodeSize, codeTotal))
					if cum := model.cum[m.Path]; cum > m.CodeSize {
						summary += fmt.Sprintf(" · cum %s (%s) with everything it pulls in",
							formatSize(cum), formatPct(cum, codeTotal))
					}
					Label(summary, FontSize(11), TextColorVec(Vec4{0, 0, 45, 1}))
				})
				Breadcrumb(m)
			})

			switch m {
			case &model.info.Stdlib:
				detailMessage("The Go runtime and standard library — present in every Go binary.")
			case &model.info.Unknown:
				detailMessage("Functions whose symbol names could not be attributed to any module.")
			default:
				edgeTables(m)
			}
		})
	})
}

func detailMessage(msg string) {
	Container(Attrs(Grow(1), Expand, Center), func() {
		Label(msg, FontSize(12), FontStyle(StyleItalic), TextColorVec(Vec4{0, 0, 55, 1}))
	})
}

// Breadcrumb renders the shortest require-chain from the main module to m,
// each element clickable. For main/stdlib the chain is just the module.
func Breadcrumb(m *Module) {
	chain := model.chains[m.Path] // nil for main/stdlib/unknown
	Container(Attrs(Row, Expand, CrossMid, Gap(6), Clip), func() {
		crumb := func(mod *Module, note string) {
			Container(Attrs(Row), func() {
				if IsClicked() {
					selectedPath = mod.Path
				}
				switch {
				case mod.Path == selectedPath:
					Label(lastElem(mod.Path)+note, FontSize(11), FontWeight(WeightBold), TextColorVec(Vec4{0, 0, 10, 1}))
				case IsHovered():
					Label(lastElem(mod.Path)+note, FontSize(11), TextColor(210, 70, 45, 1))
				default:
					Label(lastElem(mod.Path)+note, FontSize(11), TextColorVec(Vec4{0, 0, 30, 1}))
				}
			})
		}
		sep := func() { Label("→", FontSize(11), TextColorVec(Vec4{0, 0, 60, 1})) }

		if m == model.info.Main {
			Label("this is the main module — the table's (direct) rows are its inferred direct dependencies",
				FontSize(11), FontStyle(StyleItalic), TextColorVec(Vec4{0, 0, 55, 1}))
			return
		}
		if chain == nil {
			Label(" ", FontSize(11)) // keep the line's height reserved
			return
		}
		crumb(model.info.Main, "")
		sep()
		for i, p := range chain {
			if i > 0 {
				sep()
			}
			note := ""
			if i == 0 {
				note = " (direct, inferred)"
			}
			crumb(model.byPath[p], note)
		}
	})
}

// edgeTables is the callers/callees analog: who requires m, and what m
// requires — both restricted to modules actually in the binary.
func edgeTables(m *Module) {
	requirers := modulesFor(model.requiredBy[m.Path])
	requiresHeading := "requires (in this binary)"
	var required []*Module
	if m == model.info.Main {
		requiresHeading = "direct dependencies (inferred)"
		for _, d := range model.info.Deps {
			if len(model.requiredBy[d.Path]) == 0 {
				required = append(required, d)
			}
		}
		sortBySize(required)
	} else {
		required = modulesFor(model.requires[m.Path])
	}

	requirersHeading := "required by"
	if len(requirers) == 0 && m != model.info.Main {
		requirers = []*Module{model.info.Main}
		requirersHeading = "required by (inferred — nothing in the cache requires it)"
	}

	Container(Attrs(Row, Grow(1), Expand, Clip, Gap(1)), func() {
		edgeSection(requirersHeading, requirers)
		edgeSection(requiresHeading, required)
	})

	if len(model.noModFile) > 0 {
		Container(Attrs(Expand, Pad4(4, 14, 6, 14)), func() {
			Label(fmt.Sprintf("requires of %d local/replaced module(s) unknown: %s",
				len(model.noModFile), strings.Join(model.noModFile, ", ")),
				FontSize(9), TextColorVec(Vec4{0, 0, 55, 1}))
		})
	}
}

func modulesFor(paths []string) []*Module {
	out := make([]*Module, 0, len(paths))
	for _, p := range paths {
		if m := model.byPath[p]; m != nil {
			out = append(out, m)
		}
	}
	sortBySize(out)
	return out
}

func sortBySize(mods []*Module) {
	sort.Slice(mods, func(i, j int) bool {
		if mods[i].CodeSize != mods[j].CodeSize {
			return mods[i].CodeSize > mods[j].CodeSize
		}
		return mods[i].Path < mods[j].Path
	})
}

func edgeSection(heading string, mods []*Module) {
	Container(Attrs(Grow(1), Expand, Clip), func() {
		Container(Attrs(Expand, Pad4(4, 14, 2, 14)), func() {
			Label(heading, FontWeight(WeightBold), FontSize(11), TextColorVec(Vec4{0, 0, 35, 1}))
		})
		if len(mods) == 0 {
			Container(Attrs(Expand, Pad4(4, 14, 2, 14)), func() {
				Label("none in this binary", FontSize(11), FontStyle(StyleItalic), TextColorVec(Vec4{0, 0, 55, 1}))
			})
			return
		}
		codeTotal := model.info.AttributedTotal()
		cols := []TableColumn[*Module]{
			{
				Label:  "Module",
				Render: func(m *Module) { ModuleNameCell(m) },
				Less:   func(a, b *Module) bool { return a.Path < b.Path },
			},
			{
				Label: "Code", Width: colSize, DefaultDesc: true,
				Render: func(m *Module) { sizeCell(m.CodeSize, formatSize(m.CodeSize)) },
				Less:   func(a, b *Module) bool { return a.CodeSize < b.CodeSize },
			},
			{
				Label: "%", Width: colPct, DefaultDesc: true,
				Render: func(m *Module) { numCell(formatPct(m.CodeSize, codeTotal)) },
				Less:   func(a, b *Module) bool { return a.CodeSize < b.CodeSize },
			},
		}
		Table(nil, guiRowHeight, cols, mods, func(m *Module) any { return m }, 1)
	})
}
