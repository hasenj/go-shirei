package main

// Behavior test: LogView stays pinned at the true bottom under high-rate streaming.
//
// Headless integration-style check — not in the normal `go test` suite.
// Exit 0 = pass; 1 = fail. stdout carries a scrape-friendly summary.
//
//	go run ./behavior_test/logview-stream
//	go run ./behavior_test/logview-stream -rate 2000 -seconds 5
//	go run ./behavior_test/logview-stream --window
//	go run ./behavior_test/logview-stream --window --auto
//
// While lines append in the background, LogView must remain pinned: no scroll
// gaps, no false unpins, last rendered row matches this frame's item count,
// and after stream settle the view sits flush on the real bottom.

import (
	"flag"
	"fmt"
	"math/rand"
	"os"
	"sync/atomic"
	"time"

	"go.hasen.dev/shirei/app"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

type f32 = float32

const (
	winW, winH  = 960, 640
	demoRingCap = 5 << 20 // 5 MiB — hit eviction quickly
	defaultRate = 1000
	defaultSecs = 3
	probeEvery  = 30 * time.Millisecond

	// Single-frame budget; exceeding it is a hang regression.
	frameHangTimeout = 2 * time.Second
)

var (
	ring = NewTextRing(demoRingCap)

	streaming atomic.Bool
	rateHz    atomic.Int64
	lineNo    atomic.Int64

	verbose      bool
	frameN       int
	lastFrameDur time.Duration
	maxFrameDur  time.Duration
)

var words = []string{
	"lorem", "ipsum", "dolor", "sit", "amet", "consectetur", "adipiscing",
	"elit", "sed", "do", "eiusmod", "tempor", "incididunt", "ut", "labore",
	"et", "dolore", "magna", "aliqua", "enim", "ad", "minim", "veniam",
	"quis", "nostrud", "exercitation", "ullamco", "laboris", "nisi",
	"aliquip", "commodo", "consequat", "duis", "aute", "irure", "reprehenderit",
}

var levels = []string{"INFO", "DEBUG", "WARN", "ERROR", "TRACE"}

func makeGarbageLine(rng *rand.Rand, i int64) string {
	n := 8 + rng.Intn(24)
	if i%17 == 0 {
		n = 40 + rng.Intn(40)
	}
	var b []byte
	b = fmt.Appendf(b, "line %08d | %-5s | ", i, levels[rng.Intn(len(levels))])
	for j := 0; j < n; j++ {
		if j > 0 {
			b = append(b, ' ')
		}
		b = append(b, words[rng.Intn(len(words))]...)
	}
	return string(b)
}

func streamLoop() {
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	for {
		if !streaming.Load() {
			time.Sleep(50 * time.Millisecond)
			continue
		}
		hz := rateHz.Load()
		if hz < 1 {
			hz = 1
		}
		batch := hz / 20
		if batch < 1 {
			batch = 1
		}
		// Background path: WithFrameLock serializes against the frame builder.
		WithFrameLock(func() {
			for i := int64(0); i < batch; i++ {
				n := lineNo.Add(1)
				ring.AppendLine(makeGarbageLine(rng, n))
			}
		})
		RequestNextFrame()
		sleep := time.Second * time.Duration(batch) / time.Duration(hz)
		if sleep < time.Millisecond {
			sleep = time.Millisecond
		}
		time.Sleep(sleep)
	}
}

func main() {
	rate := flag.Int("rate", defaultRate, "lines/s while streaming")
	secs := flag.Float64("seconds", defaultSecs, "stream duration before verdict")
	window := flag.Bool("window", false, "interactive window (not a regression gate)")
	auto := flag.Bool("auto", false, "with --window: start streaming immediately")
	v := flag.Bool("v", false, "verbose frame timing")
	flag.Parse()
	verbose = *v

	rateHz.Store(int64(*rate))
	seedRing(40, rand.New(rand.NewSource(1)))
	go streamLoop()

	if *window {
		if *auto {
			streaming.Store(true)
		}
		app.SetupWindow("behavior_test: logview-stream", winW, winH)
		app.Run(frameFn)
		return
	}

	os.Exit(runHeadless(*secs))
}

func seedRing(n int, rng *rand.Rand) {
	for i := 0; i < n; i++ {
		id := lineNo.Add(1)
		ring.AppendLine(makeGarbageLine(rng, id))
	}
}

// ── headless ──────────────────────────────────────────────────────────────

var (
	listKey    = new(int)
	probeScope = new(int)
	probe      LogViewProbe
)

func runHeadless(seconds float64) int {
	fmt.Println("=== behavior_test: logview-stream ===")
	if err := caseStreamPin(seconds); err != nil {
		fmt.Printf("FAIL: %v\n", err)
		return 1
	}
	fmt.Println("PASS: LogView stayed pinned at true bottom while streaming")
	return 0
}

func caseStreamPin(seconds float64) error {
	ResetInputSession()
	GetHost().WindowSize = Vec2{winW, winH}
	maxFrameDur = 0

	for range 12 {
		if err := driveFrame(0); err != nil {
			return fmt.Errorf("settle: %w", err)
		}
	}
	for range 40 {
		if err := driveFrame(800); err != nil {
			return fmt.Errorf("scroll bottom: %w", err)
		}
	}
	if err := driveFrame(0); err != nil {
		return err
	}
	if !atBottom() {
		return fmt.Errorf("not at bottom before stream (scrollY=%.1f max=%.1f lastVis=%d len=%d pinned=%v)",
			probe.ScrollY, probe.MaxScroll, probe.LastVisible, ring.Len(), probe.Pinned)
	}

	streaming.Store(true)
	deadline := time.Now().Add(time.Duration(seconds * float64(time.Second)))
	var gaps, notAtBottom, unpins, samples, maxLen int

	for time.Now().Before(deadline) {
		for range 3 {
			if err := driveFrame(0); err != nil {
				streaming.Store(false)
				return fmt.Errorf("stream frame: %w", err)
			}
			samples++
			if n := ring.Len(); n > maxLen {
				maxLen = n
			}
			if ring.Len() == 0 {
				continue
			}
			if !probe.Pinned {
				unpins++
			}
			if probe.MaxScroll-probe.ScrollY > 2 {
				gaps++
			}
			// ItemCount is from the same frame as LastVisible (not live ring.Len()).
			if probe.ItemCount > 0 && probe.LastVisible >= 0 &&
				probe.LastVisible != probe.ItemCount-1 {
				notAtBottom++
			}
		}
		time.Sleep(probeEvery)
	}
	streaming.Store(false)
	for range 6 {
		if err := driveFrame(0); err != nil {
			return fmt.Errorf("post-stream: %w", err)
		}
	}

	fmt.Printf("  rate=%d/s duration=%.1fs samples=%d maxLen=%d dropped=%d\n",
		rateHz.Load(), seconds, samples, maxLen, ring.DroppedLines())
	fmt.Printf("  final scrollY=%.1f max=%.1f lastVis=%d itemCount=%d pinned=%v\n",
		probe.ScrollY, probe.MaxScroll, probe.LastVisible, probe.ItemCount, probe.Pinned)
	fmt.Printf("  gaps=%d notAtBottom=%d unpins=%d maxFrame=%v\n",
		gaps, notAtBottom, unpins, maxFrameDur)

	if gaps > 0 || notAtBottom > 0 || unpins > 0 {
		return fmt.Errorf("pin broke under stream (gaps=%d notAtBottom=%d unpins=%d)",
			gaps, notAtBottom, unpins)
	}
	if !atBottom() {
		return fmt.Errorf("not at true bottom after stream settle (scrollY=%.1f max=%.1f lastVis=%d len=%d pinned=%v)",
			probe.ScrollY, probe.MaxScroll, probe.LastVisible, ring.Len(), probe.Pinned)
	}
	return nil
}

func atBottom() bool {
	if ring.Len() == 0 {
		return true
	}
	return probe.Pinned &&
		probe.MaxScroll-probe.ScrollY <= 2 &&
		probe.LastVisible == ring.Len()-1
}

func driveFrame(wheel f32) error {
	type result struct{ dur time.Duration }
	ch := make(chan result, 1)
	go func() {
		t0 := time.Now()
		runFrame(wheel)
		ch <- result{time.Since(t0)}
	}()
	select {
	case r := <-ch:
		lastFrameDur = r.dur
		if r.dur > maxFrameDur {
			maxFrameDur = r.dur
		}
		frameN++
		if verbose {
			fmt.Printf("  frame#%d wheel=%.0f dur=%v len=%d scrollY=%.1f max=%.1f pinned=%v\n",
				frameN, wheel, r.dur, ring.Len(), probe.ScrollY, probe.MaxScroll, probe.Pinned)
		}
		return nil
	case <-time.After(frameHangTimeout):
		return fmt.Errorf("frame hung >%v (len=%d)", frameHangTimeout, ring.Len())
	}
}

func runFrame(wheel f32) {
	GetInputState().MousePoint = Vec2{winW / 2, winH * 0.6}
	GetFrameInput().Mouse = 0
	GetFrameInput().Scroll = Vec2{0, wheel}
	GetFrameInput().Motion = Vec2{}
	GetFrameInput().Key = 0
	GetFrameInput().Text = ""
	RunFrameFn(func() {
		ModAttrs(func(a *AttrSet) { a.Animations = 0 })
		ContainerWithKey(probeScope, Attrs(Viewport, Pad(10), Gap(8)), func() {
			Label("chrome", FontSize(12))
			Container(Attrs(Grow(1), Expand, MinSize(0, 200), Viewport), func() {
				attrs := DefaultTextStyle()
				attrs.FontFamilies = Monospace
				attrs.FontSize = 12
				LogViewExt(ring, attrs, listKey, &probe)
			})
		})
	})
}

// ── window mode ───────────────────────────────────────────────────────────

func frameFn() {
	ModAttrs(Background(0, 0, 92, 1), Pad(10), Gap(8))

	Label("behavior_test: LogView stream",
		FontSize(12), FontWeight(WeightBold), TextColor(0, 0, 20, 1))
	Label("Headless: go run ./behavior_test/logview-stream",
		FontSize(11), TextColor(0, 0, 40, 1))

	Container(Attrs(Row, Gap(8), CrossAlign(AlignMiddle), Wrap), func() {
		if streaming.Load() {
			if Button(0, "Stop") {
				streaming.Store(false)
			}
		} else {
			if Button(0, "Start") {
				streaming.Store(true)
			}
		}
		for _, hz := range []int64{50, 200, 1000, 5000, 10000} {
			label := fmt.Sprintf("%d/s", hz)
			on := rateHz.Load() == hz
			if ButtonExt(label, ButtonAttrs{Accent: AccentMeadow, Disabled: on}) && !on {
				rateHz.Store(hz)
			}
		}
		if Button(0, "Clear") {
			// UI path already holds the frame lock — do not WithFrameLock.
			*ring = *NewTextRing(demoRingCap)
			lineNo.Store(0)
		}
	})

	Container(Attrs(Row, Gap(16), CrossAlign(AlignMiddle)), func() {
		Label(fmt.Sprintf("%d lines · %s / %s",
			ring.Len(), fmtBytes(ring.Bytes()), fmtBytes(int64(ring.Cap()))),
			FontSize(11), TextColor(0, 0, 40, 1))
		if d := ring.DroppedLines(); d > 0 {
			Label(fmt.Sprintf("evicted %d lines (%s)", d, fmtBytes(ring.DroppedBytes())),
				FontSize(11), TextColor(20, 60, 40, 1))
		}
		if streaming.Load() {
			Label(fmt.Sprintf("streaming @ %d lines/s", rateHz.Load()),
				FontSize(11), TextColor(140, 50, 35, 1))
		}
	})

	Container(Attrs(Grow(1), Expand, Extrinsic, Viewport,
		Background(0, 0, 100, 1), Corners(4), BorderWidth(1), BorderColor(0, 0, 78, 1), Pad(6)), func() {
		attrs := DefaultTextStyle()
		attrs.FontFamilies = Monospace
		attrs.FontSize = 12
		LogView(ring, attrs)
	})
}

func fmtBytes(n int64) string {
	const KB = 1024
	const MB = 1024 * 1024
	switch {
	case n < MB:
		return fmt.Sprintf("%.1fKB", float64(n)/KB)
	default:
		return fmt.Sprintf("%.2fMB", float64(n)/MB)
	}
}
