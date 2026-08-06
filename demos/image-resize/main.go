package main

// image-resize is both an interactive resize stress demo and a controlled
// software-render benchmark.
//
//	go run ./demos/image-resize
//	go run ./demos/image-resize --bench
//	go run ./demos/image-resize --bench --frames=120 --scale=2 --filter=linear

import (
	"flag"
	"fmt"
	"image"
	"os"
	"runtime"
	"sort"
	"time"

	"github.com/anthonynsimon/bild/transform"
	"go.hasen.dev/shirei/app"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

const (
	sourceWidth  = 2048
	sourceHeight = 480
	imageCount   = 3
)

var (
	benchFlag    = flag.Bool("bench", false, "run a controlled headless resize sweep and exit")
	framesFlag   = flag.Int("frames", 120, "measured frames in --bench mode")
	minWidthFlag = flag.Int("min-width", 720, "first logical width in --bench mode")
	maxWidthFlag = flag.Int("max-width", 1320, "largest logical width in --bench mode")
	heightFlag   = flag.Int("height", 760, "logical window height in --bench mode")
	scaleFlag    = flag.Float64("scale", 2, "device pixels per logical point in --bench mode")
	quantizeFlag = flag.Int("quantize", 0, "motion-scale quantization step (0 disables)")
	filterFlag   = flag.String("filter", "nearest", "motion scaler: linear or nearest")
	alphaFlag    = flag.Bool("alpha", false, "add one transparent source pixel to force alpha compositing")

	demoImages [imageCount]*image.RGBA
	hudPos     = Vec2{10, 10}
)

func main() {
	flag.Parse()
	for i := range demoImages {
		demoImages[i] = makeDemoImage(i)
	}
	ScaleMotionQuantize = *quantizeFlag
	switch *filterFlag {
	case "linear":
		ScaleMotionFilter = transform.Linear
	case "nearest":
		ScaleMotionFilter = transform.NearestNeighbor
	default:
		fmt.Fprintf(os.Stderr, "unknown -filter %q (want linear or nearest)\n", *filterFlag)
		os.Exit(2)
	}

	if *benchFlag {
		runResizeBench()
		return
	}

	app.SetupWindow("Image resize stress", 1000, 760)
	app.Run(rootView)
}

func makeDemoImage(seed int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, sourceWidth, sourceHeight))
	for y := 0; y < sourceHeight; y++ {
		for x := 0; x < sourceWidth; x++ {
			fx := byte(x * 255 / (sourceWidth - 1))
			fy := byte(y * 255 / (sourceHeight - 1))
			check := byte(35)
			if (x/64+y/48+seed)%2 == 0 {
				check = 0
			}
			i := img.PixOffset(x, y)
			switch seed {
			case 0:
				img.Pix[i+0] = fx
				img.Pix[i+1] = 80 + fy/2
				img.Pix[i+2] = 170 - check
			case 1:
				img.Pix[i+0] = 40 + fy/2
				img.Pix[i+1] = fx
				img.Pix[i+2] = 210 - check
			default:
				img.Pix[i+0] = 210 - check
				img.Pix[i+1] = 60 + fx/2
				img.Pix[i+2] = fy
			}
			img.Pix[i+3] = 255
		}
	}
	if *alphaFlag {
		// One transparent corner is enough to make the source non-opaque and
		// exercise the general per-pixel compositing path.
		img.Pix[0], img.Pix[1], img.Pix[2], img.Pix[3] = 0, 0, 0, 0
	}
	return img
}

func rootView() {
	ModAttrs(Viewport, Background(220, 10, 92, 1))

	var ids [imageCount]ImageId
	for i, img := range demoImages {
		ids[i] = UseImage(fmt.Sprintf("image-resize-demo/%d", i), img)
	}

	Container(Attrs(Viewport, Pad(10), Gap(8), Background(220, 10, 92, 1)), func() {
		width := GetAvailableSize()[0]
		if width < 1 {
			RequestNextFrame()
			return
		}
		for _, id := range ids {
			ImageView(id, Vec2{width, 0})
		}
	})

	resizeHUD()
	ProfileButton("image-resize")
}

func resizeHUD() {
	host := GetHost()
	produceMs := float64(host.LayoutTime) / float64(time.Millisecond)
	paintMs := float64(host.PaintTime) / float64(time.Millisecond)
	scaleMs := float64(host.ImageScaleTime) / float64(time.Millisecond)
	paintRestMs := max(0, paintMs-scaleMs)
	workMs := produceMs + paintMs
	fps := 0.0
	if workMs > 0.01 {
		fps = 1000 / workMs
	}

	ContainerWithKey(&hudPos, Attrs(
		FloatVec(hudPos), InFront, NoAnimate,
		Background(0, 0, 0, 0.82), Corners(5), Pad(6), Gap(3),
	), func() {
		PressAction()
		if IsActive() {
			hudPos = Vec2Add(hudPos, GetFrameInput().Motion)
		}
		for _, line := range []string{
			fmt.Sprintf("frame=%d", ActiveUI().FrameNumber),
			fmt.Sprintf("produce=%.1fms", produceMs),
			fmt.Sprintf("paint=%.1fms", paintMs),
			fmt.Sprintf("  scale=%.1fms", scaleMs),
			fmt.Sprintf("  rest=%.1fms", paintRestMs),
			fmt.Sprintf("work=%.1fms", workMs),
			fmt.Sprintf("~%.0ffps", fps),
			fmt.Sprintf("filter=%s", *filterFlag),
			fmt.Sprintf("quantize=%d", ScaleMotionQuantize),
		} {
			Label(line, Fonts(Monospace...), FontSize(10), TextColor(0, 0, 100, 1))
		}
	})
}

type benchFrame struct {
	produce time.Duration
	paint   time.Duration
	scale   time.Duration
}

func runResizeBench() {
	if *framesFlag < 1 || *minWidthFlag < 1 || *maxWidthFlag <= *minWidthFlag ||
		*heightFlag < 1 || *scaleFlag <= 0 {
		flag.Usage()
		return
	}

	ResetInputSession()
	host := GetHost()
	host.HeadlessRender = false // exercise the motion-quality path
	host.WindowScale = float32(*scaleFlag)
	var renderer SoftRenderer

	run := func(width int) benchFrame {
		host.WindowSize = Vec2{float32(width), float32(*heightFlag)}

		t0 := time.Now()
		out := RunFrameFn(rootView)
		produce := time.Since(t0)

		devW := int(float32(width)*host.WindowScale + 0.5)
		devH := int(float32(*heightFlag)*host.WindowScale + 0.5)
		t1 := time.Now()
		renderer.Render(out.Surfaces, devW, devH, host.WindowScale)
		return benchFrame{
			produce: produce,
			paint:   time.Since(t1),
			scale:   host.ImageScaleTime,
		}
	}

	// Establish identities, image registrations, glyphs, and renderer buffers at
	// widths outside the measured sweep.
	for i := 0; i < 10; i++ {
		run(max(320, *minWidthFlag-100+i*7))
	}
	runtime.GC()

	span := *maxWidthFlag - *minWidthFlag
	samples := make([]benchFrame, *framesFlag)
	for i := range samples {
		// A triangular sweep avoids a discontinuous max→min jump when frames
		// exceed one pass through the width range.
		p := (i * 5) % (span * 2)
		if p > span {
			p = span*2 - p
		}
		samples[i] = run(*minWidthFlag + p)
	}

	produce := make([]time.Duration, len(samples))
	paint := make([]time.Duration, len(samples))
	scale := make([]time.Duration, len(samples))
	paintRest := make([]time.Duration, len(samples))
	work := make([]time.Duration, len(samples))
	for i, s := range samples {
		produce[i] = s.produce
		paint[i] = s.paint
		scale[i] = s.scale
		paintRest[i] = max(0, s.paint-s.scale)
		work[i] = s.produce + s.paint
	}

	fmt.Printf("image-resize: images=%d source=%dx%d frames=%d width=%d..%d height=%d scale=%.2f filter=%s quantize=%d alpha=%v\n",
		imageCount, sourceWidth, sourceHeight, len(samples),
		*minWidthFlag, *maxWidthFlag, *heightFlag, *scaleFlag,
		*filterFlag, ScaleMotionQuantize, *alphaFlag)
	printTiming("produce", produce)
	printTiming("paint", paint)
	printTiming("scale", scale)
	printTiming("rest", paintRest)
	ws := timingSummary(work)
	printTiming("work", work)
	fmt.Printf("throughput: ~%.1f fps from mean work\n", 1000/ws.meanMs)
}

type timingResult struct {
	meanMs float64
	p50Ms  float64
	p95Ms  float64
	maxMs  float64
}

func timingSummary(ds []time.Duration) timingResult {
	sorted := append([]time.Duration(nil), ds...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	var total time.Duration
	for _, d := range sorted {
		total += d
	}
	at := func(frac float64) time.Duration {
		i := int(float64(len(sorted)-1)*frac + 0.5)
		return sorted[i]
	}
	ms := func(d time.Duration) float64 {
		return float64(d) / float64(time.Millisecond)
	}
	return timingResult{
		meanMs: ms(total) / float64(len(sorted)),
		p50Ms:  ms(at(0.50)),
		p95Ms:  ms(at(0.95)),
		maxMs:  ms(sorted[len(sorted)-1]),
	}
}

func printTiming(name string, ds []time.Duration) {
	s := timingSummary(ds)
	fmt.Printf("%-7s mean=%6.2fms p50=%6.2fms p95=%6.2fms max=%6.2fms\n",
		name, s.meanMs, s.p50Ms, s.p95Ms, s.maxMs)
}
