// Behavior test: large text files open instantly and stay scrollable.
//
// Same stack as demos/text-view: ReadFileContent → stable string → LargeText
// tip + background full index → VirtualList.
//
// Default drive cycles local gitignored corpora (not generated):
//
//	resources/data/large200mb.txt → large10mb.txt → large100mb.txt
//
// Per file, after load:
//  1. content visible (tip paint under budget)
//  2. wheel scroll down works immediately
//  3. jump scroll via mid-track scrollbar click
//  4. wheel up, then wheel down again
//
//	go run ./behavior_test/text-view-large
//	go run ./behavior_test/text-view-large -size 100m   # single file
//	go run ./behavior_test/text-view-large -file PATH
//	go run ./behavior_test/text-view-large --close
//	go run ./behavior_test/text-view-large --manual

package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unsafe"

	"go.hasen.dev/shirei/app"
	"go.hasen.dev/shirei/behavior_test/btmode"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

type f32 = float32

const (
	winW, winH f32 = 720, 560

	frameHangTimeout = 2 * time.Second
	tipPaintBudget   = 200 * time.Millisecond

	loadingMotionFrames = 40
	wheelFrames         = 25
	wheelDelta          = f32(80)

	// Chrome above the list (title row + rule) — used for scrollbar hit estimate.
	chromeH = f32(42)
)

var defaultCycle = []string{
	"large200mb.txt",
	"large10mb.txt",
	"large100mb.txt",
}

var (
	openPath    string
	contentPath string
	contentText string
	fileAsync   bool // size ≥ 64MiB → async ReadFileContent

	loading      bool
	hasContent   bool
	frameN       int
	lastFrameDur time.Duration
	maxFrameDur  time.Duration
	tipPaintDur  time.Duration

	scrollY   f32
	maxScroll f32
	firstVis  int
	lastVis   int

	mode          *btmode.Mode
	verdictDone   bool
	verdictOK     bool
	verdictDetail string

	corpusPaths []string
	fileIdx     int

	drivePhase    string
	driveT0       time.Time
	loadSeq0      uint64
	motionSeen    int
	tipSettleLeft int
	wheelLeft     int
	scrollAtMark  f32
	jumpArmed     bool // true after hover frame; next frame clicks
)

func main() {
	mode = btmode.RegisterFlags(nil)
	sizeStr := flag.String("size", "", "single fixture: 10m, 100m, or 200m (default: cycle 200→10→100)")
	fileFlag := flag.String("file", "", "explicit corpus path (single file; overrides -size)")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: go run ./behavior_test/text-view-large [flags]\n\n%s  -size      single resources/data/large*.txt (10m/100m/200m)\n  -file      explicit corpus path\n  (default)  cycle large200mb → large10mb → large100mb\n", btmode.FlagHelp())
	}
	flag.Parse()
	mode.AfterParse()
	if err := mode.Validate(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	paths, err := resolveCorpusList(*fileFlag, *sizeStr)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	corpusPaths = paths

	fmt.Println("=== behavior_test: text-view-large ===")
	for i, p := range corpusPaths {
		st, _ := os.Stat(p)
		fmt.Printf("  [%d] %s (%.1fMB)\n", i+1, p, float64(st.Size())/(1<<20))
	}

	beginFile(0)
	if mode.Drive {
		drivePhase = "open"
		driveT0 = time.Now()
	}
	app.SetupWindow("behavior_test: text-view-large", int(winW), int(winH))
	app.Run(windowFrame)
}

func resolveCorpusList(explicit, sizeStr string) ([]string, error) {
	root, err := findResourcesData()
	if explicit == "" && sizeStr == "" {
		if err != nil {
			return nil, err
		}
		var out []string
		for _, name := range defaultCycle {
			p := filepath.Join(root, name)
			if _, err := os.Stat(p); err != nil {
				return nil, fmt.Errorf("%s not found under %s — use local gitignored resources/data", name, root)
			}
			out = append(out, p)
		}
		return out, nil
	}
	if explicit != "" {
		p := explicit
		if abs, err := filepath.Abs(explicit); err == nil {
			p = abs
		}
		if _, err := os.Stat(p); err != nil {
			return nil, err
		}
		return []string{p}, nil
	}
	if err != nil {
		return nil, err
	}
	size, err := parseSize(sizeStr)
	if err != nil {
		return nil, fmt.Errorf("size: %w", err)
	}
	name, err := sizeToResourceName(size)
	if err != nil {
		return nil, err
	}
	p := filepath.Join(root, name)
	if _, err := os.Stat(p); err != nil {
		return nil, fmt.Errorf("%s not found under %s", name, root)
	}
	return []string{p}, nil
}

func sizeToResourceName(size int64) (string, error) {
	switch {
	case size >= 150<<20:
		return "large200mb.txt", nil
	case size >= 50<<20:
		return "large100mb.txt", nil
	case size >= 5<<20:
		return "large10mb.txt", nil
	default:
		return "", fmt.Errorf("size must be 10m, 100m, or 200m; got %d bytes", size)
	}
}

func findResourcesData() (string, error) {
	start, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := start
	for {
		cand := filepath.Join(dir, "resources", "data")
		if st, err := os.Stat(cand); err == nil && st.IsDir() {
			return cand, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("resources/data not found walking up from %s", start)
}

func parseSize(s string) (int64, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	mult := int64(1)
	switch {
	case strings.HasSuffix(s, "mb"), strings.HasSuffix(s, "m"):
		mult = 1 << 20
		s = strings.TrimSuffix(strings.TrimSuffix(s, "mb"), "m")
	case strings.HasSuffix(s, "gb"), strings.HasSuffix(s, "g"):
		mult = 1 << 30
		s = strings.TrimSuffix(strings.TrimSuffix(s, "gb"), "g")
	case strings.HasSuffix(s, "kb"), strings.HasSuffix(s, "k"):
		mult = 1 << 10
		s = strings.TrimSuffix(strings.TrimSuffix(s, "kb"), "k")
	}
	n, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, err
	}
	return int64(n * float64(mult)), nil
}

func beginFile(i int) {
	fileIdx = i
	openPath = corpusPaths[i]
	st, _ := os.Stat(openPath)
	fileAsync = st != nil && st.Size() >= 64<<20
	contentPath = ""
	contentText = ""
	loading = false
	hasContent = false
	scrollY, maxScroll = 0, 0
	firstVis, lastVis = -1, -1
	tipPaintDur = 0
	motionSeen = 0
	tipSettleLeft = 0
	wheelLeft = 0
	scrollAtMark = 0
	jumpArmed = false
	driveT0 = time.Now()
	// Only after a frame has run — PostCommand needs a live UI session.
	if frameN > 0 {
		VirtualListView_ScrollToIndex(LargeTextListKey, 0)
	}
	fmt.Printf("-- file %d/%d %s (async=%v)\n", i+1, len(corpusPaths), filepath.Base(openPath), fileAsync)
}

func windowFrame() {
	t0 := time.Now()
	injectInput()
	ModAttrs(func(a *AttrSet) { a.Animations = 0 })
	rootView()
	noteFrame(time.Since(t0))

	if mode.Drive && !verdictDone {
		if lastFrameDur > frameHangTimeout {
			failDrive(fmt.Sprintf("frame hung >%v (n=%d phase=%s file=%s)",
				frameHangTimeout, frameN, drivePhase, filepath.Base(openPath)))
		} else {
			stepDrive()
		}
	}
	if mode.Drive {
		btmode.VerdictBanner(verdictDone, verdictOK, verdictDetail)
		mode.TickClose(verdictDone, verdictOK)
		if !verdictDone || !mode.Close {
			RequestNextFrame()
		}
	}
}

func noteFrame(dur time.Duration) {
	lastFrameDur = dur
	if dur > maxFrameDur {
		maxFrameDur = dur
	}
	frameN++
}

func injectInput() {
	GetFrameInput().Key = 0
	GetFrameInput().Text = ""
	GetFrameInput().Motion = Vec2{}
	GetFrameInput().Scroll = Vec2{}
	GetFrameInput().Mouse = 0

	if !mode.Drive || verdictDone {
		GetInputState().MousePoint = Vec2{winW / 2, winH / 2}
		return
	}

	switch drivePhase {
	case "open", "motion":
		GetInputState().MousePoint = Vec2{winW / 2, winH / 2}
		GetFrameInput().Motion = Vec2{1, 0}
	case "wheelDown", "wheelDown2":
		GetInputState().MousePoint = Vec2{winW / 2, winH / 2}
		GetFrameInput().Scroll = Vec2{0, wheelDelta}
	case "wheelUp":
		GetInputState().MousePoint = Vec2{winW / 2, winH / 2}
		GetFrameInput().Scroll = Vec2{0, -wheelDelta}
	case "jumpHover", "jumpClick":
		GetInputState().MousePoint = scrollbarMidPoint()
		if drivePhase == "jumpClick" {
			GetFrameInput().Mouse = MouseClick
		}
	default:
		GetInputState().MousePoint = Vec2{winW / 2, winH / 2}
	}
}

func scrollbarMidPoint() Vec2 {
	// Track floats at the right edge of the list viewport (below chrome).
	x := winW - SCROLLBAR_WIDTH/2
	y := chromeH + (winH-chromeH)/2
	return Vec2{x, y}
}

// ── drive state machine ───────────────────────────────────────────────────

func stepDrive() {
	switch drivePhase {
	case "open":
		stats := DebugGetFileCacheStats()
		loadSeq0 = stats.NextLoadID
		if hasContent {
			enterTipSettle()
			return
		}
		if fileAsync && stats.InFlight != 1 && stats.ContentBytes == 0 {
			failDrive(fmt.Sprintf("after open: want InFlight=1, got %d", stats.InFlight))
			return
		}
		drivePhase = "motion"
		motionSeen = 0

	case "motion":
		if hasContent {
			fmt.Printf("  loading-motion frames=%d loadSeq=%d open→tip %v\n",
				motionSeen, loadSeq0, time.Since(driveT0))
			enterTipSettle()
			return
		}
		motionSeen++
		if fileAsync {
			st := DebugGetFileCacheStats()
			if st.ContentBytes == 0 {
				if st.InFlight != 1 {
					failDrive(fmt.Sprintf("motion: InFlight=%d want 1", st.InFlight))
					return
				}
				if st.NextLoadID != loadSeq0 && loadSeq0 != 0 {
					failDrive(fmt.Sprintf("motion: load seq %d→%d", loadSeq0, st.NextLoadID))
					return
				}
			}
		}
		if motionSeen >= loadingMotionFrames {
			drivePhase = "waitTip"
		}

	case "waitTip":
		if hasContent {
			fmt.Printf("  open→tip %v\n", time.Since(driveT0))
			enterTipSettle()
			return
		}
		if time.Since(driveT0) > 60*time.Second {
			failDrive("timed out waiting for content")
			return
		}

	case "tipSettle":
		if lastFrameDur > tipPaintDur {
			tipPaintDur = lastFrameDur
		}
		tipSettleLeft--
		if tipSettleLeft <= 0 {
			fmt.Printf("  visible tipPaint=%v scrollY=%.0f max=%.0f\n", tipPaintDur, scrollY, maxScroll)
			if tipPaintDur > tipPaintBudget {
				failDrive(fmt.Sprintf("tip paint %v > budget %v", tipPaintDur, tipPaintBudget))
				return
			}
			if !hasContent {
				failDrive("content not visible after tip settle")
				return
			}
			drivePhase = "wheelDown"
			wheelLeft = wheelFrames
			scrollAtMark = scrollY
		}

	case "wheelDown":
		wheelLeft--
		if wheelLeft > 0 {
			return
		}
		if maxScroll < 1 {
			failDrive(fmt.Sprintf("not scrollable after tip (maxScroll=%.1f)", maxScroll))
			return
		}
		if scrollY <= scrollAtMark+10 {
			failDrive(fmt.Sprintf("wheel down did nothing (scrollY %.1f→%.1f)", scrollAtMark, scrollY))
			return
		}
		fmt.Printf("  wheel↓ scrollY %.0f→%.0f (max %.0f)\n", scrollAtMark, scrollY, maxScroll)
		drivePhase = "jumpHover"
		jumpArmed = false

	case "jumpHover":
		// One frame of hover so the track is hit-tested.
		jumpArmed = true
		drivePhase = "jumpClick"

	case "jumpClick":
		if !jumpArmed {
			drivePhase = "jumpHover"
			return
		}
		drivePhase = "jumpCheck"

	case "jumpCheck":
		// Mid-track jump should leave a substantial mid-range offset.
		if maxScroll < 1 {
			failDrive("maxScroll lost before jump check")
			return
		}
		frac := scrollY / maxScroll
		if frac < 0.2 || frac > 0.9 {
			failDrive(fmt.Sprintf("jump scroll expected mid-range, got scrollY=%.0f max=%.0f frac=%.2f",
				scrollY, maxScroll, frac))
			return
		}
		fmt.Printf("  jump scrollY=%.0f (%.0f%% of max)\n", scrollY, frac*100)
		drivePhase = "wheelUp"
		wheelLeft = wheelFrames
		scrollAtMark = scrollY

	case "wheelUp":
		wheelLeft--
		if wheelLeft > 0 {
			return
		}
		if scrollY >= scrollAtMark-10 {
			failDrive(fmt.Sprintf("wheel up did nothing (scrollY %.1f→%.1f)", scrollAtMark, scrollY))
			return
		}
		fmt.Printf("  wheel↑ scrollY %.0f→%.0f\n", scrollAtMark, scrollY)
		drivePhase = "wheelDown2"
		wheelLeft = wheelFrames
		scrollAtMark = scrollY

	case "wheelDown2":
		wheelLeft--
		if wheelLeft > 0 {
			return
		}
		if scrollY <= scrollAtMark+10 {
			failDrive(fmt.Sprintf("second wheel down did nothing (scrollY %.1f→%.1f)", scrollAtMark, scrollY))
			return
		}
		fmt.Printf("  wheel↓ again scrollY %.0f→%.0f\n", scrollAtMark, scrollY)
		fmt.Printf("  PASS %s\n", filepath.Base(openPath))
		if fileIdx+1 < len(corpusPaths) {
			beginFile(fileIdx + 1)
			drivePhase = "open"
			driveT0 = time.Now()
			return
		}
		passDrive(fmt.Sprintf("%d files ok maxFrame=%v", len(corpusPaths), maxFrameDur))
	}
}

func enterTipSettle() {
	drivePhase = "tipSettle"
	tipSettleLeft = 8
	tipPaintDur = lastFrameDur
}

func passDrive(detail string) {
	verdictDone, verdictOK = true, true
	verdictDetail = detail
	drivePhase = "done"
	fmt.Printf("PASS: %s\n", detail)
}

func failDrive(detail string) {
	verdictDone, verdictOK = true, false
	verdictDetail = detail
	drivePhase = "done"
	fmt.Printf("FAIL: %s\n", detail)
}

// ── UI ────────────────────────────────────────────────────────────────────

func rootView() {
	Container(Attrs(Viewport, Background(0, 0, 98, 1)), func() {
		contentb := ReadFileContent(openPath)
		if openPath != contentPath || contentb == nil {
			contentPath = openPath
			contentText = ""
		}
		if contentb != nil && contentText == "" {
			contentText = unsafe.String(unsafe.SliceData(contentb), len(contentb))
		}

		loading = contentText == ""
		hasContent = contentText != ""

		Container(Attrs(Row, CrossMid, Gap(10), Pad2(6, 10), Expand), func() {
			Label(filepath.Base(openPath), FontSize(13), FontWeight(WeightBold), TextColor(220, 25, 22, 1))
			size := "…"
			if contentText != "" {
				size = fmt.Sprintf("%.1fMB", float64(len(contentText))/(1<<20))
			}
			Label(size, FontSize(12), TextColor(0, 0, 45, 1))
			if mode.Drive && drivePhase != "" && drivePhase != "done" {
				Label(drivePhase, FontSize(11), TextColor(0, 0, 55, 1))
			}
		})
		Element(Attrs(MinHeight(1), Expand, Background(0, 0, 0, 0.12)))

		if contentText == "" {
			Container(Attrs(Expand, Center), func() {
				Label("Loading…", FontSize(13), TextColor(0, 0, 50, 1))
			})
			scrollY, maxScroll = 0, 0
			firstVis, lastVis = -1, -1
		} else {
			probeLargeText(contentText, FontSize(12), Fonts(Monospace...))
		}
	})
}

// probeLargeText mirrors LargeText but reports scroll / visible range for asserts.
func probeLargeText(text string, styleFn ...TextStyleFn) {
	Container(Attrs(Viewport, NoAnimate), func() {
		type _LargeText struct {
			gen     atomic.Uint64
			text    string
			starts  []int
			lastEnd int
		}
		data := Use[_LargeText]("large-text")
		if !StringHeadersEqual(data.text, text) {
			data.text = text
			gen := data.gen.Add(1)
			data.starts, data.lastEnd = scanLineStartsLocal(text, 500)
			RequestNextFrame()
			go func(text string, gen uint64) {
				t0 := time.Now()
				starts, lastEnd := scanLineStartsLocal(text, 0)
				log.Printf("%d lines indexed in %v", len(starts), time.Since(t0))
				WithFrameLock(func() {
					if data.gen.Load() != gen {
						return
					}
					data.starts = starts
					data.lastEnd = lastEnd
				})
				RequestNextFrame()
			}(text, gen)
		}

		vpad := TextStyle().FontSize / 4
		n := len(data.starts)
		type LineNo int
		itemKey := func(idx int) any { return LineNo(idx) }
		itemView := func(idx int, width f32) {
			line := lineAtLocal(data.text, data.starts, data.lastEnd, idx)
			Container(Attrs(Pad2(vpad, 0), Expand, MaxWidth(width)), func() {
				Label(line, styleFn...)
			})
		}
		itemHeight := func(idx int, width f32) f32 {
			shaped := ShapeTextMax(lineAtLocal(data.text, data.starts, data.lastEnd, idx), TextStyle(styleFn...), width)
			var height f32
			for _, shapedLine := range shaped.Lines {
				height += shapedLine.Height
			}
			return height + (vpad * 2)
		}

		VirtualListViewExt(LargeTextListKey, VirtualListAttrs{
			ItemCount:          n,
			ItemKey:            itemKey,
			ItemHeight:         itemHeight,
			ItemView:           itemView,
			OutScrollOffset:    &scrollY,
			OutMaxScrollOffset: &maxScroll,
			OutFirstVisible:    &firstVis,
			OutLastVisible:     &lastVis,
		})
	})
}

// Local copies of unexported LargeText helpers (same algorithm as widgets).
func scanLineStartsLocal(text string, maxLines int) (starts []int, lastEnd int) {
	lastEnd = -1
	if text == "" {
		return []int{0}, -1
	}
	capHint := 64
	if maxLines > 0 {
		capHint = maxLines
	} else if len(text) > 64 {
		capHint = len(text)/32 + 1
	}
	starts = make([]int, 0, capHint)
	starts = append(starts, 0)
	for i := 0; i < len(text); i++ {
		if text[i] != '\n' {
			continue
		}
		if maxLines > 0 && len(starts) >= maxLines {
			lastEnd = i
			return starts, lastEnd
		}
		if i+1 < len(text) {
			starts = append(starts, i+1)
		} else {
			starts = append(starts, len(text))
		}
	}
	return starts, -1
}

func lineAtLocal(text string, starts []int, lastEnd, idx int) string {
	if idx < 0 || idx >= len(starts) {
		return ""
	}
	lo := starts[idx]
	if lo > len(text) {
		return ""
	}
	hi := len(text)
	if idx+1 < len(starts) {
		hi = starts[idx+1]
		if hi > 0 && hi <= len(text) && text[hi-1] == '\n' {
			hi--
		}
	} else if lastEnd >= 0 {
		hi = lastEnd
	} else if hi > lo && text[hi-1] == '\n' {
		hi--
	}
	if lo > hi {
		return ""
	}
	return text[lo:hi]
}
