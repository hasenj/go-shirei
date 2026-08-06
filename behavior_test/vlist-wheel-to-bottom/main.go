package main

// Behavior test: continuous wheel-to-bottom on a variable-height VirtualList.
//
// Not part of the normal `go test` suite — run on demand:
//
//	go run ./behavior_test/vlist-wheel-to-bottom
//	    Headless drive; PASS/FAIL on stdout; exit 0/1.
//
//	go run ./behavior_test/vlist-wheel-to-bottom --window --drive --close
//	    Live window, auto wheel, SUCCESS/FAIL banner, then exit.
//
//	go run ./behavior_test/vlist-wheel-to-bottom --window --drive
//	    Auto wheel; stay open after verdict (banner stays).
//
//	go run ./behavior_test/vlist-wheel-to-bottom --window
//	    Manual: you wheel; no auto verdict/exit.
//
// Two failure modes under variable heights:
//
//  1. STALL — scrollY stops while fromBottom = maxScroll−scrollY stays large.
//  2. FALSE BOTTOM — scrollY reaches maxScroll but the last rows never render.
//
// Drive path: mutates GetInputState().MousePoint + GetFrameInput().Scroll each frame
// (same as a real trackpad via ScrollOnInput while hovered).

import (
	"flag"
	"fmt"
	"math/rand"
	"os"
	"strings"

	"go.hasen.dev/shirei/app"
	"go.hasen.dev/shirei/behavior_test/btmode"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

const winW, winH = 960, 720

type f32 = float32

const (
	itemCount     = 500
	fontSize  f32 = 14
	vpad      f32 = 4
	// ~one mouse-wheel notch / small trackpad flick, in points.
	wheelDelta f32 = 40
	// Consecutive wheel frames with no scrollY progress ⇒ stall.
	stuckIdleFrames = 12
	// fromBottom at or below this counts as "at reported max".
	atMaxEpsilon   f32 = 2
	maxWheelFrames     = 6000
	// Fixed seed: reliably hits FALSE BOTTOM headless today.
	rngSeed int64 = 1
)

type item struct {
	id   int64
	text string
}

var (
	listKey = new(int)
	rng     *rand.Rand

	items  []item
	nextID int64

	scrollY   f32
	maxScroll f32

	autoWheel    = true
	phase        = "settle"
	settleLeft   = 8
	wheelFrames  int
	idleProgress int
	lastScrollY  f32
	status       = "settling"
	verdictOK    bool
	done         bool
	reported     bool

	verbose bool
	mode    *btmode.Mode

	trueTotal     f32
	trueMaxScroll f32
	listWidth     f32
	listViewport  f32

	firstVisibleID int64
	lastVisibleID  int64

	// headless presets GetFrameInput() before RunFrameFn
	headlessWheelPreset bool
	wheeledThisFrame    bool
)

func main() {
	rng = rand.New(rand.NewSource(rngSeed))
	seedList(itemCount)

	mode = btmode.RegisterFlags(nil)
	flag.BoolVar(&verbose, "v", false, "verbose progress")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: go run ./behavior_test/vlist-wheel-to-bottom [flags]\n\n%s  -v         verbose progress\n", btmode.FlagHelp())
	}
	flag.Parse()
	mode.AfterParse()
	if err := mode.Validate(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	autoWheel = mode.Drive

	if !mode.Window {
		os.Exit(runHeadless())
	}

	if !mode.Drive {
		phase = "manual"
		status = "manual — wheel the list yourself"
	}
	app.SetupWindow("behavior_test: vlist wheel-to-bottom", winW, winH)
	app.Run(frameFn)
}

func runHeadless() int {
	ResetInputSession()
	GetHost().WindowSize = Vec2{winW, winH}

	for range 10 {
		driveFrame(false)
	}
	for wheelFrames < maxWheelFrames && !done {
		driveFrame(true)
	}
	// Force a terminal verdict if the budget ended without one.
	if !done {
		status = fmt.Sprintf("BUDGET EXHAUSTED  fromBottom=%.1f  last #%d / #%d",
			max(0, maxScroll-scrollY), lastVisibleID, items[len(items)-1].id)
		verdictOK = false
		done = true
	}
	return reportAndCode()
}

func driveFrame(wheel bool) {
	GetInputState().MousePoint = Vec2{winW / 2, winH / 2}
	GetFrameInput().Mouse = 0
	GetFrameInput().Motion = Vec2{}
	GetFrameInput().Key = 0
	GetFrameInput().Text = ""
	if wheel {
		GetFrameInput().Scroll = Vec2{0, wheelDelta}
	} else {
		GetFrameInput().Scroll = Vec2{}
	}
	headlessWheelPreset = wheel
	RunFrameFn(func() {
		ModAttrs(func(a *AttrSet) { a.Animations = 0 })
		frameFn()
	})
	headlessWheelPreset = false
}

func seedList(n int) {
	items = make([]item, 0, n)
	nextID = 1
	for range n {
		items = append(items, newItem())
	}
}

func newItem() item {
	id := nextID
	nextID++
	return item{id: id, text: randomLine(id)}
}

var words = []string{
	"a", "bb", "ccc", "dddd", "word", "longerword", "wrapping", "line", "text",
	"virtual", "list", "scroll", "anchor", "viewport", "height", "bottom",
}

func randomLine(id int64) string {
	n := 3 + rng.Intn(50)
	var b strings.Builder
	fmt.Fprintf(&b, "#%d ", id)
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(words[rng.Intn(len(words))])
	}
	return b.String()
}

func frameFn() {
	textAttrs := TextStyle(FontSize(fontSize), TextColor(0, 0, 18, 1))
	wheeledThisFrame = false

	if autoWheel && !done {
		GetInputState().MousePoint = Vec2{winW / 2, winH / 2}
		if phase == "wheel" {
			if !headlessWheelPreset {
				GetFrameInput().Scroll = Vec2Add(GetFrameInput().Scroll, Vec2{0, wheelDelta})
			}
			wheeledThisFrame = headlessWheelPreset || GetFrameInput().Scroll[1] != 0
			RequestNextFrame()
		} else if phase == "settle" {
			RequestNextFrame()
		}
	}

	// Full-window list; HUD is an overlay so it does not change TotalHeight.
	Container(Attrs(Viewport, Background(220, 25, 96, 1)), func() {
		shapeLine := func(idx int, width f32) ShapedText {
			return ShapeTextMax(items[idx].text, textAttrs, width)
		}
		itemHeight := func(idx int, width f32) f32 {
			shaped := shapeLine(idx, width)
			var h f32
			for _, ln := range shaped.Lines {
				h += ln.Height
			}
			return max(h, fontSize) + vpad*2
		}
		itemView := func(idx int, width f32) {
			if firstVisibleID == 0 {
				firstVisibleID = items[idx].id
			}
			lastVisibleID = items[idx].id
			shaped := shapeLine(idx, width)
			Container(Attrs(Pad2(vpad, 8), Expand), func() {
				if idx%2 == 0 {
					ModAttrs(Background(220, 15, 98, 1))
				}
				ShapedTextLayout(shaped, textAttrs, 0, 0)
			})
		}

		firstVisibleID = 0
		lastVisibleID = 0

		VirtualListViewExt(listKey, VirtualListAttrs{
			ItemCount:          len(items),
			ItemKey:            func(i int) any { return items[i].id },
			ItemHeight:         itemHeight,
			ItemView:           itemView,
			OutScrollOffset:    &scrollY,
			OutMaxScrollOffset: &maxScroll,
		})

		rd := GetRenderData()
		if rd.ResolvedSize[0] > SCROLLBAR_WIDTH {
			w := rd.ResolvedSize[0] - SCROLLBAR_WIDTH
			vp := rd.ResolvedSize[1]
			if w != listWidth || vp != listViewport {
				listWidth = w
				listViewport = vp
				trueTotal = 0
				for i := range items {
					trueTotal += itemHeight(i, listWidth)
				}
				trueMaxScroll = max(0, trueTotal-listViewport)
			}
		}

		fromBottom := max(0, maxScroll-scrollY)
		Container(Attrs(Float(12, 12), InFront, Pad(10), Gap(4),
			Background(0, 0, 100, 0.92), Corners(8),
			BorderWidth(1), BorderColor(0, 0, 0, 0.08)), func() {
			Label("behavior_test: wheel → bottom", FontWeight(WeightBold), FontSize(16))
			Label("GetFrameInput().Scroll += (0,+Δ) while hovered", FontSize(12), TextColor(0, 0, 40, 1))
			readout("status", status)
			readout("scrollY", fmt.Sprintf("%.1f", scrollY))
			readout("maxScroll", fmt.Sprintf("%.1f", maxScroll))
			readout("fromBottom", fmt.Sprintf("%.1f", fromBottom))
			if trueMaxScroll > 0 {
				readout("trueMax", fmt.Sprintf("%.1f", trueMaxScroll))
				readout("underest", fmt.Sprintf("%.1f", max(0, trueMaxScroll-maxScroll)))
			}
			readout("wheelFrames", fmt.Sprintf("%d", wheelFrames))
			readout("idle", fmt.Sprintf("%d", idleProgress))
			if lastVisibleID > 0 {
				readout("visible", fmt.Sprintf("#%d…#%d / #%d",
					firstVisibleID, lastVisibleID, items[len(items)-1].id))
			}
		})
	})

	updateDriveState()

	if mode.Window && done && !reported {
		_ = reportAndCode()
	}
	detail := status
	btmode.VerdictBanner(done && mode.Drive, verdictOK, detail)
	if mode.Drive {
		mode.TickClose(done, verdictOK)
		if done && mode.Window && !mode.Close {
			RequestNextFrame() // keep banner painted while idle
		}
	}
}

func updateDriveState() {
	if done || !autoWheel {
		return
	}

	fromBottom := max(0, maxScroll-scrollY)
	lastItem := items[len(items)-1].id
	showsLast := lastVisibleID == lastItem

	switch phase {
	case "settle":
		settleLeft--
		if settleLeft <= 0 {
			phase = "wheel"
			status = "wheeling down…"
			lastScrollY = scrollY
			idleProgress = 0
			if verbose {
				fmt.Printf("phase=wheel  scrollY=%.1f maxScroll=%.1f trueMax=%.1f\n",
					scrollY, maxScroll, trueMaxScroll)
			}
		}

	case "wheel":
		if !wheeledThisFrame {
			return
		}
		wheelFrames++
		advanced := scrollY > lastScrollY+0.5
		if advanced {
			idleProgress = 0
			lastScrollY = scrollY
		} else {
			idleProgress++
		}

		if verbose && (wheelFrames%50 == 0 || (!advanced && idleProgress == 1)) {
			fmt.Printf("wheel#%d  scrollY=%.1f max=%.1f fb=%.1f trueMax=%.1f underest=%.1f visible=#%d..#%d idle=%d\n",
				wheelFrames, scrollY, maxScroll, fromBottom, trueMaxScroll,
				max(0, trueMaxScroll-maxScroll), firstVisibleID, lastVisibleID, idleProgress)
		}

		if fromBottom <= atMaxEpsilon && showsLast {
			status = "REACHED TRUE BOTTOM"
			verdictOK = true
			done = true
			phase = "done"
			return
		}

		if fromBottom <= atMaxEpsilon && !showsLast {
			status = fmt.Sprintf("FALSE BOTTOM  at max but last visible #%d / #%d",
				lastVisibleID, lastItem)
			verdictOK = false
			done = true
			phase = "done"
			return
		}

		if idleProgress >= stuckIdleFrames && fromBottom > atMaxEpsilon {
			status = fmt.Sprintf("STALL  fromBottom=%.1f  last visible #%d / #%d",
				fromBottom, lastVisibleID, lastItem)
			verdictOK = false
			done = true
			phase = "done"
			return
		}

		if wheelFrames >= maxWheelFrames {
			status = fmt.Sprintf("BUDGET EXHAUSTED  fromBottom=%.1f  last #%d / #%d",
				fromBottom, lastVisibleID, lastItem)
			verdictOK = false
			done = true
			phase = "done"
		}
	}
}

// reportAndCode writes a machine-readable-ish summary to stdout and returns
// the process exit code (0 = PASS, 1 = FAIL).
func reportAndCode() int {
	reported = true
	lastItem := items[len(items)-1].id
	fmt.Println("=== behavior_test: vlist-wheel-to-bottom ===")
	fmt.Printf("status=%s\n", status)
	fmt.Printf("  scrollY=%.1f  maxScroll=%.1f  fromBottom=%.1f\n",
		scrollY, maxScroll, max(0, maxScroll-scrollY))
	fmt.Printf("  trueMax=%.1f  underest=%.1f  wheelFrames=%d\n",
		trueMaxScroll, max(0, trueMaxScroll-maxScroll), wheelFrames)
	fmt.Printf("  visible #%d…#%d  (list ends #%d)\n",
		firstVisibleID, lastVisibleID, lastItem)
	if verdictOK {
		fmt.Println("PASS: continuous wheel reached the true bottom")
		return 0
	}
	fmt.Println("FAIL: continuous wheel did not cleanly reach the true bottom")
	return 1
}

func readout(name, value string) {
	Container(Attrs(Row, Gap(4), CrossMid), func() {
		Label(name+":", FontSize(12), TextColor(0, 0, 45, 1))
		Label(value, FontSize(12), FontWeight(WeightSemibold), TextColor(0, 0, 15, 1), Fonts(Monospace...))
	})
}
