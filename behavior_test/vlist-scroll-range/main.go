// Behavior test: VirtualList TotalHeight / maxScroll under variable row heights.
//
// Not part of the normal `go test` suite — run on demand:
//
//	go run ./behavior_test/vlist-scroll-range
//	    Open window, auto-drive all scenarios; stay open after verdict.
//
//	go run ./behavior_test/vlist-scroll-range --close
//	    Auto-drive; SUCCESS/FAIL banner; exit.
//
//	go run ./behavior_test/vlist-scroll-range --manual
//	    One scenario (--case or last) and keep the window open.
//
//	go run ./behavior_test/vlist-scroll-range --case tall-head-full
//	    Run a single scenario by name.
//
// Scenarios:
//
//	uniform          fixed row height — maxScroll matches Σh − viewport
//	mild             small height variation — top-only default is fine
//	tall-tail        short head, tall last rows — continuous wheel to true end
//	tall-head-default  images-then-text-then-images with default avg sample
//	                   (top 50 only) — expects FAIL (overshoot / end snap)
//	tall-head-full   same corpus with AvgSampleTop/Bottom covering all rows
//	                   — expects PASS (exact mean)
//
// The tall-head shape mirrors git_history image-heavy commits (e.g. 7d62ae1).

package main

import (
	"flag"
	"fmt"
	"math"
	"os"
	"strings"

	"go.hasen.dev/shirei/app"
	"go.hasen.dev/shirei/behavior_test/btmode"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

const (
	winW, winH f32 = 400, 200
	wheelDelta f32 = 60
	maxEps     f32 = 2
	maxWheels      = 8000
)

// Visible pause between scenarios / major steps when a window is open (~0.7s at 60Hz).
const windowHoldFrames = 42

type f32 = float32

type scenario struct {
	name        string
	heights     []f32
	avgTop      int // 0,0 → VirtualList defaults
	avgBot      int
	wantPass    bool // false → known failure under defaults
	checkSettle bool // after settle, |maxScroll − trueMax| ≤ maxEps
	checkSnap   bool // continuous wheel: no huge maxScroll drop; reach true end
	checkWheel  bool // reach last row and fromBottom ≈ 0
}

var (
	mode *btmode.Mode

	// drive suite
	cases      []scenario
	caseIdx    int
	r          *runner
	phase      string // settle, wheel, hold
	settleLeft int
	holdLeft   int
	holdN      = windowHoldFrames
	wheelFrame int
	prevMax    f32
	maxDrop    f32
	dropDetail string
	reached    bool
	problems   []string
	results    []caseResult
	allMatch   = true

	verdictDone   bool
	verdictOK     bool
	verdictDetail string
	status        string

	currentResult    caseResult
	hasCurrentResult bool

	// manual window
	showScenario scenario
)

func main() {
	mode = btmode.RegisterFlags(nil)
	only := flag.String("case", "", "run only this scenario by name")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: go run ./behavior_test/vlist-scroll-range [flags]\n\n%s  --case NAME   run only one scenario\n\ncases:\n", btmode.FlagHelp())
		for _, s := range scenarios() {
			exp := "PASS"
			if !s.wantPass {
				exp = "FAIL"
			}
			fmt.Fprintf(os.Stderr, "  %-20s expect %s\n", s.name, exp)
		}
	}
	flag.Parse()
	mode.AfterParse()
	if err := mode.Validate(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	all := scenarios()
	if *only != "" {
		var filtered []scenario
		for _, s := range all {
			if s.name == *only {
				filtered = append(filtered, s)
			}
		}
		if len(filtered) == 0 {
			fmt.Fprintf(os.Stderr, "unknown case %q\n", *only)
			os.Exit(2)
		}
		all = filtered
	}

	showScenario = all[len(all)-1]
	if mode.Drive {
		initSuite(all)
	} else {
		r = newRunner(showScenario)
		status = "manual — " + showScenario.name
	}
	app.SetupWindow("behavior_test: vlist-scroll-range", int(winW), int(winH))
	app.Run(frameFn)
}

func scenarios() []scenario {
	return []scenario{
		{
			name:        "uniform",
			heights:     fill(100, 20),
			wantPass:    true,
			checkSettle: true,
			checkWheel:  true,
		},
		{
			name:        "mild",
			heights:     mild(120),
			wantPass:    true,
			checkSettle: true,
			checkWheel:  true,
		},
		{
			name:       "tall-tail",
			heights:    tallTail(150),
			wantPass:   true,
			checkWheel: true,
		},
		{
			name:        "tall-head-default",
			heights:     gitHistoryLike(),
			wantPass:    false,
			checkSettle: true,
			checkSnap:   true,
		},
		{
			name:        "tall-head-full",
			heights:     gitHistoryLike(),
			avgTop:      -1,
			avgBot:      -1,
			wantPass:    true,
			checkSettle: true,
			checkSnap:   true,
			checkWheel:  true,
		},
	}
}

func fill(n int, h f32) []f32 {
	out := make([]f32, n)
	for i := range out {
		out[i] = h
	}
	return out
}

func mild(n int) []f32 {
	out := make([]f32, n)
	for i := range out {
		out[i] = 18 + f32(i%5)
	}
	return out
}

func tallTail(n int) []f32 {
	out := make([]f32, n)
	for i := range out {
		if i >= n-10 {
			out[i] = 80
		} else {
			out[i] = 12
		}
	}
	return out
}

// gitHistoryLike: tall image rows, long short text middle, tall images at end.
func gitHistoryLike() []f32 {
	var h []f32
	for range 15 {
		h = append(h, 26, 400)
	}
	for range 180 {
		h = append(h, 18)
	}
	for range 4 {
		h = append(h, 26, 400)
	}
	return h
}

func trueTotal(heights []f32) f32 {
	var s f32
	for _, h := range heights {
		s += h
	}
	return s
}

type runner struct {
	sc        scenario
	heights   []f32
	scope     *int
	listKey   *int
	scrollY   f32
	maxScroll f32
	firstVis  int
	lastVis   int
}

func newRunner(sc scenario) *runner {
	h := sc.heights
	top, bot := sc.avgTop, sc.avgBot
	if top < 0 || bot < 0 {
		n := len(h)
		top = (n + 1) / 2
		bot = n / 2
	}
	sc.avgTop, sc.avgBot = top, bot
	return &runner{
		sc:       sc,
		heights:  h,
		scope:    new(int),
		listKey:  new(int),
		firstVis: -1,
		lastVis:  -1,
	}
}

func (r *runner) trueMax() f32 {
	// Prefer the live window height (backend-owned).
	vh := GetHost().WindowSize[1]
	if vh <= 0 {
		vh = winH
	}
	return max(f32(0), trueTotal(r.heights)-vh)
}

func (r *runner) absMaxErr() f32 {
	return f32(math.Abs(float64(r.maxScroll - r.trueMax())))
}

func buildVirtualList(r *runner) {
	n := len(r.heights)
	top, bot := r.sc.avgTop, r.sc.avgBot
	r.firstVis, r.lastVis = -1, -1
	ContainerWithKey(r.scope, Attrs(Viewport), func() {
		VirtualListViewExt(r.listKey, VirtualListAttrs{
			ItemCount: n,
			ItemKey:   func(i int) any { return i },
			ItemHeight: func(i int, w f32) f32 {
				if i < 0 || i >= n {
					return 1
				}
				return max(f32(1), r.heights[i])
			},
			ItemView: func(i int, w f32) {
				shade := 88 + f32(i%7)*2
				Container(Attrs(Pad2(2, 6), Expand, Background(220, shade, 94, 1)), func() {
					Label(fmt.Sprintf("%d", i), FontSize(10))
				})
			},
			AvgSampleTop:       top,
			AvgSampleBottom:    bot,
			OutScrollOffset:    &r.scrollY,
			OutMaxScrollOffset: &r.maxScroll,
			OutFirstVisible:    &r.firstVis,
			OutLastVisible:     &r.lastVis,
		})
	})
}

type caseResult struct {
	name    string
	ok      bool
	want    bool
	matched bool // ok == want
	detail  string
}

func initSuite(sc []scenario) {
	cases = sc
	caseIdx = 0
	allMatch = true
	results = nil
	verdictDone = false
	startScenario()
}

func startScenario() {
	ResetInputSession()
	r = newRunner(cases[caseIdx])
	problems = nil
	phase = "settle"
	settleLeft = 6
	wheelFrame = 0
	prevMax = r.maxScroll
	maxDrop = 0
	dropDetail = ""
	reached = false
	hasCurrentResult = false
	status = fmt.Sprintf("%s: settling", cases[caseIdx].name)
}

func frameFn() {
	ModAttrs(func(a *AttrSet) { a.Animations = 0 })

	if mode.Drive && !verdictDone {
		driveBefore()
	}

	// Full-window list; HUD is a float overlay so viewport height stays winH.
	Container(Attrs(Viewport, Background(220, 25, 96, 1)), func() {
		if r != nil {
			buildVirtualList(r)
		}
		Container(Attrs(Float(8, 8), InFront, Pad(8), Gap(4),
			Background(0, 0, 100, 0.92), Corners(6),
			BorderWidth(1), BorderColor(0, 0, 0, 0.08)), func() {
			Label("vlist-scroll-range", FontWeight(WeightBold), FontSize(13))
			Label(status, FontSize(10), TextColor(0, 0, 40, 1))
			if r != nil {
				Label(fmt.Sprintf("scroll=%.0f max=%.0f trueMax=%.0f lastVis=%d",
					r.scrollY, r.maxScroll, r.trueMax(), r.lastVis),
					FontSize(10), TextColor(0, 0, 50, 1))
			}
			if hasCurrentResult && (phase == "hold" || verdictDone) {
				scenarioPanel(currentResult)
			}
		})
	})

	if mode.Drive && !verdictDone {
		driveAfter()
		RequestNextFrame()
	}

	if mode.Drive {
		btmode.VerdictBanner(verdictDone, verdictOK, verdictDetail)
		mode.TickClose(verdictDone, verdictOK)
		if verdictDone && !mode.Close {
			RequestNextFrame()
		}
	}
}

func driveBefore() {
	GetInputState().MousePoint = Vec2{winW / 2, winH / 2}
	GetFrameInput().Mouse = 0
	GetFrameInput().Motion = Vec2{}
	GetFrameInput().Key = 0
	GetFrameInput().Text = ""
	dy := f32(0)
	if phase == "wheel" {
		dy = wheelDelta
	}
	GetFrameInput().Scroll = Vec2{0, dy}
}

func driveAfter() {
	switch phase {
	case "settle":
		settleLeft--
		if settleLeft > 0 {
			return
		}
		sc := cases[caseIdx]
		if sc.checkSettle {
			if err := r.absMaxErr(); err > maxEps {
				problems = append(problems, fmt.Sprintf("settle maxScroll=%.1f trueMax=%.1f err=%.1f",
					r.maxScroll, r.trueMax(), err))
			}
		}
		if sc.checkSnap || sc.checkWheel {
			phase = "wheel"
			status = fmt.Sprintf("%s: wheeling", sc.name)
			return
		}
		finishScenario()

	case "wheel":
		wheelFrame++
		if d := prevMax - r.maxScroll; d > maxDrop {
			maxDrop = d
			dropDetail = fmt.Sprintf("frame %d drop %.1f (%.1f→%.1f) scroll=%.1f lastVis=%d",
				wheelFrame, d, prevMax, r.maxScroll, r.scrollY, r.lastVis)
		}
		prevMax = r.maxScroll
		if r.lastVis >= len(r.heights)-1 && r.maxScroll-r.scrollY <= maxEps+1 {
			reached = true
		}
		sc := cases[caseIdx]
		if !reached && wheelFrame < maxWheels {
			return
		}
		if sc.checkWheel && !reached {
			problems = append(problems, fmt.Sprintf("never reached true bottom lastVis=%d scroll=%.1f max=%.1f trueMax=%.1f",
				r.lastVis, r.scrollY, r.maxScroll, r.trueMax()))
		}
		if sc.checkSnap && maxDrop > 50 {
			problems = append(problems, "end snap: "+dropDetail)
		}
		if (sc.checkSettle || sc.checkSnap) && reached {
			if err := r.absMaxErr(); err > maxEps {
				problems = append(problems, fmt.Sprintf("at end maxScroll=%.1f trueMax=%.1f err=%.1f",
					r.maxScroll, r.trueMax(), err))
			}
		}
		finishScenario()

	case "hold":
		holdLeft--
		if holdLeft > 0 {
			return
		}
		caseIdx++
		startScenario()
	}
}

func finishScenario() {
	sc := cases[caseIdx]
	ok := len(problems) == 0
	detail := "ok"
	if !ok {
		detail = strings.Join(problems, "; ")
	}
	res := caseResult{
		name:    sc.name,
		ok:      ok,
		want:    sc.wantPass,
		matched: ok == sc.wantPass,
		detail:  detail,
	}
	results = append(results, res)
	currentResult = res
	hasCurrentResult = true

	tag := "PASS"
	if !res.ok {
		tag = "FAIL"
	}
	expect := "expect PASS"
	if !res.want {
		expect = "expect FAIL"
	}
	match := "ok"
	if !res.matched {
		match = "UNEXPECTED"
		allMatch = false
	}
	fmt.Printf("%-20s  %s  [%s]  %s  %s\n", res.name, tag, expect, match, res.detail)

	if caseIdx+1 < len(cases) {
		phase = "hold"
		holdLeft = holdN
		status = fmt.Sprintf("%s: %s — next: %s", sc.name, tag, cases[caseIdx+1].name)
		return
	}
	finishSuite()
}

func finishSuite() {
	verdictDone = true
	verdictOK = allMatch
	if verdictOK {
		verdictDetail = "ALL MATCHED"
		fmt.Println("ALL MATCHED")
	} else {
		verdictDetail = "SOME UNEXPECTED"
		fmt.Println("SOME UNEXPECTED")
	}
	status = verdictDetail
}

func scenarioPanel(res caseResult) {
	Container(Attrs(Pad(8), Gap(4), Background(0, 0, 100, 0.9), Corners(6)), func() {
		Label(res.name, FontWeight(WeightBold), FontSize(12))
		Label(fmt.Sprintf("ok=%v wantPass=%v matched=%v", res.ok, res.want, res.matched), FontSize(11))
		Label(res.detail, FontSize(10), TextColor(0, 0, 35, 1))
	})
}
