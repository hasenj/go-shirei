// Behavior test: LogView stays pinned at the true bottom under high-rate streaming.
//
// Not in the normal `go test` suite. Always opens a window. Exit 0 = pass;
// 1 = fail. stdout carries a scrape-friendly summary.
//
//	go run ./behavior_test/logview-stream
//	go run ./behavior_test/logview-stream -rate 2000 -seconds 5
//	go run ./behavior_test/logview-stream --close
//	go run ./behavior_test/logview-stream --manual
//
// While lines append in the background, LogView must remain pinned: no scroll
// gaps, no false unpins, last rendered row matches this frame's item count,
// and after stream settle the view sits flush on the real bottom.

package main

import (
	"flag"
	"fmt"
	"math/rand"
	"os"
	"sync/atomic"
	"time"

	"go.hasen.dev/shirei/app"
	"go.hasen.dev/shirei/behavior_test/btmode"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

type f32 = float32

const (
	winW, winH  = 960, 640
	demoRingCap = 5 << 20 // 5 MiB — hit eviction quickly
	defaultRate = 1000
	defaultSecs = 3

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

	mode          *btmode.Mode
	verdictDone   bool
	verdictOK     bool
	verdictDetail string
	streamSecs    float64

	// Window drive phases: settle → scrollBottom → stream → post → done
	drivePhase      string
	settleLeft      int
	scrollLeft      int
	postLeft        int
	streamDeadline  time.Time
	streamSamples   int
	streamMaxLen    int
	streamGaps      int
	streamNotBottom int
	streamUnpins    int
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
	mode = btmode.RegisterFlags(nil)
	rate := flag.Int("rate", defaultRate, "lines/s while streaming")
	secs := flag.Float64("seconds", defaultSecs, "stream duration before verdict")
	v := flag.Bool("v", false, "verbose frame timing")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: go run ./behavior_test/logview-stream [flags]\n\n%s  -rate      lines/s (default %d)\n  -seconds   stream duration (default %d)\n  -v         verbose frame timing\n", btmode.FlagHelp(), defaultRate, defaultSecs)
	}
	flag.Parse()
	mode.AfterParse()
	if err := mode.Validate(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	verbose = *v
	streamSecs = *secs

	rateHz.Store(int64(*rate))
	seedRing(40, rand.New(rand.NewSource(1)))
	go streamLoop()

	fmt.Println("=== behavior_test: logview-stream ===")

	if mode.Drive {
		drivePhase = "settle"
		settleLeft = 12
		scrollLeft = 40
		postLeft = 6
		verdictDone = false
	}
	app.SetupWindow("behavior_test: logview-stream", winW, winH)
	app.Run(frameFn)
}

func seedRing(n int, rng *rand.Rand) {
	for i := 0; i < n; i++ {
		id := lineNo.Add(1)
		ring.AppendLine(makeGarbageLine(rng, id))
	}
}

var (
	listKey = new(int)
	probe   LogViewProbe
)

func atBottom() bool {
	if ring.Len() == 0 {
		return true
	}
	return probe.Pinned &&
		probe.MaxScroll-probe.ScrollY <= 2 &&
		probe.LastVisible == ring.Len()-1
}

func frameFn() {
	t0 := time.Now()
	wheel := f32(0)
	if mode.Drive && !verdictDone && drivePhase == "scrollBottom" {
		wheel = 800
	}
	GetInputState().MousePoint = Vec2{winW / 2, winH * 0.6}
	GetFrameInput().Scroll = Vec2{0, wheel}

	ModAttrs(Background(0, 0, 92, 1), Pad(10), Gap(8))

	Label("behavior_test: LogView stream",
		FontSize(12), FontWeight(WeightBold), TextColor(0, 0, 20, 1))
	if mode.Drive && !verdictDone {
		Label("driving: "+drivePhase, FontSize(11), TextColor(0, 0, 40, 1))
	} else {
		Label("LogView pin under streaming append",
			FontSize(11), TextColor(0, 0, 40, 1))
	}

	if !mode.Drive {
		Container(Attrs(Row, Gap(8), CrossAlign(AlignMiddle), Wrap), func() {
			if streaming.Load() {
				if Button(NoIcon, "Stop") {
					streaming.Store(false)
				}
			} else {
				if Button(NoIcon, "Start") {
					streaming.Store(true)
				}
			}
			for _, hz := range []int64{50, 200, 1000, 5000, 10000} {
				label := fmt.Sprintf("%d/s", hz)
				on := rateHz.Load() == hz
				if ButtonExt(label, ButtonAttrs{Accent: AccentMeadow, Disabled: on}, DefaultButtonLook()) && !on {
					rateHz.Store(hz)
				}
			}
			if Button(NoIcon, "Clear") {
				*ring = *NewTextRing(demoRingCap)
				lineNo.Store(0)
			}
		})
	}

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
		if mode.Drive {
			LogViewExt(ring, attrs, listKey, &probe)
		} else {
			LogView(ring, attrs)
		}
	})

	dur := time.Since(t0)
	lastFrameDur = dur
	if dur > maxFrameDur {
		maxFrameDur = dur
	}
	frameN++

	if mode.Drive && !verdictDone {
		if dur > frameHangTimeout {
			failWindowDrive(fmt.Sprintf("frame hung >%v (len=%d)", frameHangTimeout, ring.Len()))
		} else {
			stepWindowDrive()
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

func stepWindowDrive() {
	switch drivePhase {
	case "settle":
		settleLeft--
		if settleLeft <= 0 {
			drivePhase = "scrollBottom"
		}

	case "scrollBottom":
		scrollLeft--
		if scrollLeft <= 0 {
			if !atBottom() {
				failWindowDrive(fmt.Sprintf("not at bottom before stream (scrollY=%.1f max=%.1f lastVis=%d len=%d pinned=%v)",
					probe.ScrollY, probe.MaxScroll, probe.LastVisible, ring.Len(), probe.Pinned))
				return
			}
			streaming.Store(true)
			streamDeadline = time.Now().Add(time.Duration(streamSecs * float64(time.Second)))
			streamSamples = 0
			streamMaxLen = 0
			streamGaps = 0
			streamNotBottom = 0
			streamUnpins = 0
			drivePhase = "stream"
		}

	case "stream":
		// Sample every frame; throttle isn't needed — live frames are the probe.
		streamSamples++
		if n := ring.Len(); n > streamMaxLen {
			streamMaxLen = n
		}
		if ring.Len() > 0 {
			if !probe.Pinned {
				streamUnpins++
			}
			if probe.MaxScroll-probe.ScrollY > 2 {
				streamGaps++
			}
			if probe.ItemCount > 0 && probe.LastVisible >= 0 &&
				probe.LastVisible != probe.ItemCount-1 {
				streamNotBottom++
			}
		}
		if time.Now().After(streamDeadline) {
			streaming.Store(false)
			drivePhase = "post"
			postLeft = 6
		}

	case "post":
		postLeft--
		if postLeft > 0 {
			return
		}
		fmt.Printf("  rate=%d/s duration=%.1fs samples=%d maxLen=%d dropped=%d\n",
			rateHz.Load(), streamSecs, streamSamples, streamMaxLen, ring.DroppedLines())
		fmt.Printf("  final scrollY=%.1f max=%.1f lastVis=%d itemCount=%d pinned=%v\n",
			probe.ScrollY, probe.MaxScroll, probe.LastVisible, probe.ItemCount, probe.Pinned)
		fmt.Printf("  gaps=%d notAtBottom=%d unpins=%d maxFrame=%v\n",
			streamGaps, streamNotBottom, streamUnpins, maxFrameDur)

		if streamGaps > 0 || streamNotBottom > 0 || streamUnpins > 0 {
			failWindowDrive(fmt.Sprintf("pin broke under stream (gaps=%d notAtBottom=%d unpins=%d)",
				streamGaps, streamNotBottom, streamUnpins))
			return
		}
		if !atBottom() {
			failWindowDrive(fmt.Sprintf("not at true bottom after stream settle (scrollY=%.1f max=%.1f lastVis=%d len=%d pinned=%v)",
				probe.ScrollY, probe.MaxScroll, probe.LastVisible, ring.Len(), probe.Pinned))
			return
		}
		passWindowDrive(fmt.Sprintf("rate=%d/s duration=%.1fs", rateHz.Load(), streamSecs))
	}
}

func passWindowDrive(detail string) {
	verdictDone, verdictOK = true, true
	verdictDetail = detail
	drivePhase = "done"
	fmt.Println("PASS: LogView stayed pinned at true bottom while streaming")
}

func failWindowDrive(detail string) {
	streaming.Store(false)
	verdictDone, verdictOK = true, false
	verdictDetail = detail
	drivePhase = "done"
	fmt.Printf("FAIL: %v\n", detail)
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
